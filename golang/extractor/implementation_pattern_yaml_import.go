// SPDX-License-Identifier: Apache-2.0

// Importer for ImplementationPattern YAML files.
//
// An ImplementationPattern is a project-specific code recipe — the established
// shape Globular wants for a recurring code structure (e.g. how to write a
// gRPC service client, how to lay out an MCP composer). They are distinct
// from invariants (which encode "must not break") and from design Patterns
// (architectural abstractions) — patterns answer "what shape should the
// code have in THIS project?"
//
// Schema (8 fields, all earn their place):
//
//	id:              globular.pattern.<name>
//	class:           ImplementationPattern   (discriminator — required)
//	label:           Human-readable title
//	status:          active | draft | deprecated
//	when_to_use:     []string — task-text activation triggers
//	reference_files: [{path, role}]  ≥2 required
//	must_follow:     []string — human-readable required steps
//	required_calls:  []string — symbol names that MUST appear
//	forbidden_calls: []string — symbol names that MUST NOT appear
//	rationale:       free-text WHY
//
// Each file produces one ImplementationPattern node. Reference files become
// literal anchors of form "role:path" via aw:referenceFile — we do not mint
// SourceFile IRIs for them in v1 because the reference role (canonical vs
// richer-example) is part of the link semantics.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

// ── YAML shape ────────────────────────────────────────────────────────────────

type yamlImplementationPatternReference struct {
	Path string `yaml:"path"`
	Role string `yaml:"role"`
}

type yamlImplementationPattern struct {
	ID             string                               `yaml:"id"`
	Class          string                               `yaml:"class"`
	Label          string                               `yaml:"label"`
	Status         string                               `yaml:"status"`
	WhenToUse      []string                             `yaml:"when_to_use"`
	ReferenceFiles []yamlImplementationPatternReference `yaml:"reference_files"`
	MustFollow     []string                             `yaml:"must_follow"`
	RequiredCalls  []string                             `yaml:"required_calls"`
	ForbiddenCalls []string                             `yaml:"forbidden_calls"`
	Rationale      string                               `yaml:"rationale"`
	// Design-pattern awareness fields (all optional, additive).
	Name                 string     `yaml:"name"`
	Description          string     `yaml:"description"`
	ImplementsPattern    string     `yaml:"implements_pattern"` // design pattern id
	ProjectScope         string     `yaml:"project_scope"`
	RequiredSteps        []string   `yaml:"required_steps"` // additional must_follow steps
	ForbiddenShortcuts   []string   `yaml:"forbidden_shortcuts"`
	Confidence           string     `yaml:"confidence"`
	UsedByComponents     []string   `yaml:"used_by_components"`
	ImplementsDecisions  []string   `yaml:"implements_decisions"`
	EnforcesContracts    []string   `yaml:"enforces_contracts"`
	ProtectsBoundaries   []string   `yaml:"protects_boundaries"`
	SatisfiesInvariants  []string   `yaml:"satisfies_invariants"`
	PreventsFailureModes []string   `yaml:"prevents_failure_modes"`
	Blocks               []string   `yaml:"blocks"` // class-qualified: forbidden_fix:.. | pattern_misuse:..
	Tests                []string   `yaml:"tests"`
	Evidence             []string   `yaml:"evidence"`
	SourceFiles          []string   `yaml:"source_files"`
	CodeSymbols          []string   `yaml:"code_symbols"`
	Uml                  umlProfile `yaml:"uml"`
}

// importImplementationPattern imports a single implementation-pattern YAML
// file and emits its typed node with all required relations. Empty id is
// a soft skip — file produces no triples but importer does not error.
func importImplementationPattern(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var p yamlImplementationPattern
	if err := yaml.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}

	if p.ID == "" {
		return nil
	}

	subj := rdf.MintIRI(rdf.ClassImplementationPattern, p.ID)

	// Class.
	e.Typed(subj, rdf.ClassImplementationPattern)

	// Core literals.
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(p.Label, p.Name, p.ID)))
	if p.Status != "" {
		e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(p.Status))
	}
	if p.Rationale != "" {
		e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(p.Rationale)))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	// Multi-value literals.
	for _, s := range p.WhenToUse {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropActivationTrigger), rdf.Lit(s))
		}
	}
	for _, s := range p.MustFollow {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropMustFollow), rdf.Lit(s))
		}
	}
	for _, s := range p.RequiredCalls {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropRequiresCall), rdf.Lit(s))
		}
	}
	for _, s := range p.ForbiddenCalls {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropForbidsCall), rdf.Lit(s))
		}
	}

	// Reference files — emitted as "role:path" literals so downstream
	// consumers can both display the role label and follow the path
	// without parsing nested structured data from RDF.
	for _, r := range p.ReferenceFiles {
		path := strings.TrimSpace(r.Path)
		if path == "" {
			continue
		}
		role := strings.TrimSpace(r.Role)
		if role == "" {
			role = "reference"
		}
		e.Triple(subj, rdf.IRI(rdf.PropReferenceFile), rdf.Lit(role+":"+path))
	}

	// ── Design-pattern awareness edges (all optional) ──
	if p.Description != "" {
		e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(p.Description)))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(p.Confidence)))
	for _, s := range p.RequiredSteps {
		if s = strings.TrimSpace(s); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropMustFollow), rdf.Lit(s))
		}
	}
	emitOptLits(e, subj, rdf.PropForbiddenShortcut, p.ForbiddenShortcuts)

	// REALIZES the design pattern (+ reverse realizedBy so the pattern surfaces
	// its concrete realisations).
	if ip := strings.TrimSpace(p.ImplementsPattern); ip != "" {
		dp := rdf.MintIRI(rdf.ClassDesignPattern, ip)
		e.Triple(subj, rdf.IRI(rdf.PropRealizes), dp)
		e.Triple(dp, rdf.IRI(rdf.PropRealizedBy), subj)
	}
	emitPatternEdges(e, subj, rdf.PropUsedByComponent, rdf.ClassComponent, p.UsedByComponents)
	emitBareEdges(e, subj, rdf.PropImplementsDecision, rdf.ClassDecision, p.ImplementsDecisions)
	emitPatternEdges(e, subj, rdf.PropEnforcesContract, rdf.ClassContract, p.EnforcesContracts)
	emitPatternEdges(e, subj, rdf.PropProtects, rdf.ClassBoundary, p.ProtectsBoundaries)
	emitPatternEdges(e, subj, rdf.PropSatisfiesInvariant, rdf.ClassInvariant, p.SatisfiesInvariants)
	emitPatternEdges(e, subj, rdf.PropPrevents, rdf.ClassFailureMode, p.PreventsFailureModes)
	emitRefEdges(e, subj, rdf.PropBlocks, p.Blocks) // class-qualified forbidden_fix:/pattern_misuse:
	emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, p.Tests)
	emitBareEdges(e, subj, rdf.PropSupportedByEvidence, rdf.ClassEvidence, p.Evidence)
	emitSpineAnchors(e, subj, p.SourceFiles, p.CodeSymbols)
	emitUML(e, subj, p.Uml)

	return nil
}
