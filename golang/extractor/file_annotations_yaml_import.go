// SPDX-License-Identifier: Apache-2.0

// Package extractor — file_annotations schema importer.
//
// Imports YAML files with top-level keys: file_annotations, source_patterns,
// rendering_groups. These are frontend-centric schemas that connect source
// files to invariants and define structural enforcement rules.
//
// file_annotations: maps source files to invariants they enforce/protect.
//
//	Each entry creates SourceFile -> enforces/protects -> Invariant edges.
//
// source_patterns: regex-based structural rules checked against source files.
//
//	Each entry creates a SourcePattern node with pattern, scope, and message.
//
// rendering_groups: groups of files that must render the same concept
//
//	consistently. Each entry creates a RenderingGroup node linked to its files.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"github.com/globulario/awareness-graph/golang/rdf"
	"gopkg.in/yaml.v3"
)

// ── file_annotations schema ──────────────────────────────────────────────────

type yamlFileAnnotation struct {
	File     string   `yaml:"file"`
	Enforces []string `yaml:"enforces"`
	Protects []string `yaml:"protects"`
	Notes    string   `yaml:"notes"`
}

// fileAnnotationsFile supports multiple top-level grouping keys so authors
// can organise entries by domain (sdk_files, core_files, cluster_pages, etc.)
// All groups are flattened into a single annotation list for import.
type fileAnnotationsFile struct {
	// Direct list under "file_annotations:"
	FileAnnotations []yamlFileAnnotation `yaml:"file_annotations"`
}

func importFileAnnotations(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	// First try the direct schema
	var f fileAnnotationsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// If file_annotations key is empty, try to collect from any top-level key
	// that contains a list of objects with a "file" field.
	annotations := f.FileAnnotations
	if len(annotations) == 0 {
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("parse raw: %w", err)
		}
		annotations = collectAnnotationsFromMap(raw)
	}

	for _, ann := range annotations {
		if ann.File == "" {
			continue
		}
		fileIRI := rdf.MintIRI(rdf.ClassSourceFile, ann.File)
		ensureNode(e, rdf.ClassSourceFile, ann.File)

		for _, invID := range ann.Enforces {
			invIRI := rdf.MintIRI(rdf.ClassInvariant, invID)
			e.Triple(fileIRI, rdf.IRI(rdf.PropEnforces), invIRI)
			e.Triple(invIRI, rdf.IRI(rdf.PropImplements), fileIRI)
		}
		for _, invID := range ann.Protects {
			invIRI := rdf.MintIRI(rdf.ClassInvariant, invID)
			e.Triple(fileIRI, rdf.IRI(rdf.PropProtects), invIRI)
			e.Triple(invIRI, rdf.IRI(rdf.PropImplements), fileIRI)
		}
		if ann.Notes != "" {
			e.Triple(fileIRI, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(ann.Notes)))
		}
	}
	return nil
}

// collectAnnotationsFromMap walks any top-level map looking for lists of
// objects that have a "file" key. This supports the multi-section format
// used by sdk_annotations.yaml and web_annotations.yaml where entries are
// grouped under domain-specific keys like "sdk_files:", "cluster_pages:", etc.
func collectAnnotationsFromMap(raw map[string]any) []yamlFileAnnotation {
	var out []yamlFileAnnotation
	for _, v := range raw {
		list, ok := v.([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			filePath, _ := m["file"].(string)
			if filePath == "" {
				continue
			}
			ann := yamlFileAnnotation{File: filePath}
			if enforces, ok := m["enforces"].([]any); ok {
				for _, e := range enforces {
					if s, ok := e.(string); ok {
						ann.Enforces = append(ann.Enforces, s)
					}
				}
			}
			if protects, ok := m["protects"].([]any); ok {
				for _, p := range protects {
					if s, ok := p.(string); ok {
						ann.Protects = append(ann.Protects, s)
					}
				}
			}
			if notes, ok := m["notes"].(string); ok {
				ann.Notes = notes
			}
			out = append(out, ann)
		}
	}
	return out
}

// ── source_patterns schema ───────────────────────────────────────────────────

type yamlSourcePattern struct {
	ID      string   `yaml:"id"`
	Pattern string   `yaml:"pattern"`
	Scope   string   `yaml:"scope"` // "file" | "class"
	Except  []string `yaml:"except"`
	Message string   `yaml:"message"`
	Related []string `yaml:"related_invariants"`
}

type sourcePatternsFile struct {
	SourcePatterns []yamlSourcePattern `yaml:"source_patterns"`
}

// ClassSourcePattern is the RDF class for source pattern rules.
const ClassSourcePattern = rdf.AwNS + "SourcePattern"

func importSourcePatterns(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var f sourcePatternsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, sp := range f.SourcePatterns {
		if sp.ID == "" {
			continue
		}
		subj := rdf.MintIRI(ClassSourcePattern, sp.ID)
		e.Typed(subj, ClassSourcePattern)
		if sp.Message != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(sp.Message))
		}
		if sp.Pattern != "" {
			e.Triple(subj, rdf.IRI(rdf.AwNS+"pattern"), rdf.Lit(sp.Pattern))
		}
		if sp.Scope != "" {
			e.Triple(subj, rdf.IRI(rdf.AwNS+"scope"), rdf.Lit(sp.Scope))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		for _, invID := range sp.Related {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, invID))
		}
	}
	return nil
}

// ── rendering_groups schema ──────────────────────────────────────────────────

type yamlRenderingGroup struct {
	ID       string   `yaml:"id"`
	Concept  string   `yaml:"concept"`
	Files    []string `yaml:"files"`
	Contract string   `yaml:"contract"`
}

type renderingGroupsFile struct {
	RenderingGroups []yamlRenderingGroup `yaml:"rendering_groups"`
}

// ClassRenderingGroup is the RDF class for cross-file rendering consistency groups.
const ClassRenderingGroup = rdf.AwNS + "RenderingGroup"

func importRenderingGroups(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var f renderingGroupsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, rg := range f.RenderingGroups {
		if rg.ID == "" {
			continue
		}
		subj := rdf.MintIRI(ClassRenderingGroup, rg.ID)
		e.Typed(subj, ClassRenderingGroup)
		if rg.Concept != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(rg.Concept))
		}
		if rg.Contract != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(rg.Contract)))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		for _, filePath := range rg.Files {
			fileIRI := rdf.MintIRI(rdf.ClassSourceFile, filePath)
			ensureNode(e, rdf.ClassSourceFile, filePath)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), fileIRI)
			// Reverse edge: file -> memberOf -> rendering group
			e.Triple(fileIRI, rdf.IRI(rdf.PropMemberOfGroup), subj)
		}
	}
	return nil
}
