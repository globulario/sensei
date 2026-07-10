// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.loader
// @awareness file_role=yaml_loader_for_invariant_and_failure_mode_structured_metadata
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────────────────────────────────────────────────────
// principle-check v1 — graph-driven pattern loading
// ──────────────────────────────────────────────────────────────────────────
//
// v0 hardcoded the exception / conformant / hidden-workflow file patterns
// directly in main.go. Adding or reclassifying a file required editing
// Go code, even though the architectural claim ("this file is observer-
// only self-state") belongs in the awareness YAML.
//
// v1 reads the patterns from the services repo's authored YAML:
//
//   docs/awareness/invariants.yaml
//     → workflow.every_state_mutation_belongs_to_a_workflow_instance
//       . actor_writer_dirs              (which dirs to sweep)
//       . exception_files[] {file, category, reason}
//       . workflow_step_handler_files[] {file, reason}
//
//   docs/awareness/failure_modes.yaml
//     → every failure_mode whose id starts "hidden_workflow." AND
//       whose status is "active" contributes its protects.files
//       to the HIDDEN_WORKFLOW set. status: lifted entries are
//       excluded automatically.
//
// Reclassifying a file is now a YAML edit. The scanner code stays
// principle-agnostic — adding a scanner for another meta-principle
// is a YAML schema decision, not a Go change.

// principleSchema is the subset of the per-instance invariant the
// scanner needs. The per-instance invariant — the one carrying the
// structured pattern metadata — is linked to its parent meta-principle
// via related_invariants OR meta_principle_instances.
type principleSchema struct {
	ID                       string                `yaml:"id"`
	RelatedInvariants        []string              `yaml:"related_invariants"`
	MetaPrincipleInstances   []metaInstance        `yaml:"meta_principle_instances"`
	ActorWriterDirs          []string              `yaml:"actor_writer_dirs"`
	ScanPattern              string                `yaml:"scan_pattern"`
	ExceptionFiles           []exceptionFileEntry  `yaml:"exception_files"`
	WorkflowStepHandlerFiles []conformantFileEntry `yaml:"workflow_step_handler_files"`
	UnknownHelperFiles       []conformantFileEntry `yaml:"unknown_helper_files"`
	// AnalysisMode picks the scanning engine. Empty defaults to "regex"
	// (the original line-by-line regex sweep). "ruleguard" shells out to
	// the ruleguard CLI with a rule file colocated with the invariant —
	// useful for patterns that need AST shape + type info (e.g.
	// "connection-class err absorbed in a return path").
	AnalysisMode string `yaml:"analysis_mode"`
	// RuleguardRulesFile is the repo-relative path to the ruleguard rules
	// file when AnalysisMode = "ruleguard". The path is relative to the
	// awareness-graph repo (where the rules live alongside the scanner),
	// not the services repo (which is the scan TARGET).
	RuleguardRulesFile string `yaml:"ruleguard_rules_file"`
}

type metaInstance struct {
	Parent   string `yaml:"parent"`
	Relation string `yaml:"relation"`
}

type exceptionFileEntry struct {
	File     string `yaml:"file"`
	Category string `yaml:"category"`
	Reason   string `yaml:"reason"`
}

type conformantFileEntry struct {
	File   string `yaml:"file"`
	Reason string `yaml:"reason"`
}

type invariantsDoc struct {
	Invariants []principleSchema `yaml:"invariants"`
}

type failureModeEntry struct {
	ID       string             `yaml:"id"`
	Status   string             `yaml:"status"`
	Protects failureModeProtect `yaml:"protects"`
	// lifted_files is populated only on entries with status: lifted —
	// the file is now conformant; ignored by the active hidden-
	// workflow scan.
	LiftedFiles []string `yaml:"lifted_files"`
}

type failureModeProtect struct {
	Files []string `yaml:"files"`
}

type failureModesDoc struct {
	FailureModes []failureModeEntry `yaml:"failure_modes"`
}

// loadedPrinciple is the resolved pattern set the classifier uses.
type loadedPrinciple struct {
	PrincipleID        string
	ActorWriterDirs    []string
	ScanPattern        *regexp.Regexp
	AnalysisMode       string
	RuleguardRulesFile string
	// Each map: file-path-suffix-regex → reason+bucket.
	Conformant     []patternEntry
	Exception      []patternEntry
	HiddenWorkflow []patternEntry
	Unknown        []patternEntry
}

type patternEntry struct {
	re     *regexp.Regexp
	reason string
}

// loadPrinciple reads the structured metadata from the services repo's
// awareness YAMLs and resolves it for the named principle.
//
// The v0 hardcoded list was indexed by file-path suffix regex. v1 keeps
// the same indexing — each YAML file entry maps to a regex anchored at
// path end. Suffix matching keeps the patterns compatible with the
// current scanner walk (which works from repo-relative paths).
func loadPrinciple(repoRoot, principleID string) (*loadedPrinciple, error) {
	invariantsPath := filepath.Join(repoRoot, "docs", "awareness", "invariants.yaml")
	failuresPath := filepath.Join(repoRoot, "docs", "awareness", "failure_modes.yaml")

	invariantsBytes, err := os.ReadFile(invariantsPath)
	if err != nil {
		return nil, fmt.Errorf("read invariants.yaml: %w", err)
	}
	var inv invariantsDoc
	if err := yaml.Unmarshal(invariantsBytes, &inv); err != nil {
		return nil, fmt.Errorf("parse invariants.yaml: %w", err)
	}

	failuresBytes, err := os.ReadFile(failuresPath)
	if err != nil {
		return nil, fmt.Errorf("read failure_modes.yaml: %w", err)
	}
	var fm failureModesDoc
	if err := yaml.Unmarshal(failuresBytes, &fm); err != nil {
		return nil, fmt.Errorf("parse failure_modes.yaml: %w", err)
	}

	// Find the invariant whose structured pattern metadata answers for
	// this principle. Two cases:
	//
	//   (1) principleID names the invariant directly — match by ID.
	//   (2) principleID names a meta-principle (meta.*) — find an
	//       invariant that links to it via related_invariants OR
	//       meta_principle_instances AND declares actor_writer_dirs.
	//
	// Case (2) is the common path. The CLI flag is named -principle so
	// operators reach for the meta-principle ID; the per-instance
	// invariant is an implementation detail.
	var found *principleSchema
	// Pass 1: direct ID match.
	for i := range inv.Invariants {
		if inv.Invariants[i].ID == principleID {
			found = &inv.Invariants[i]
			break
		}
	}
	// Pass 2: meta-principle lookup — invariants that link to the meta
	// AND declare actor_writer_dirs (= they're scanner-targets).
	if found == nil {
		for i := range inv.Invariants {
			cand := &inv.Invariants[i]
			if len(cand.ActorWriterDirs) == 0 {
				continue
			}
			if invariantLinksToMeta(cand, principleID) {
				found = cand
				break
			}
		}
	}
	if found == nil {
		return nil, fmt.Errorf("principle %q not found in invariants.yaml (looked for direct ID match and meta-principle parent lookup; need an invariant with actor_writer_dirs declared)", principleID)
	}

	mode := found.AnalysisMode
	if mode == "" {
		mode = "regex"
	}

	out := &loadedPrinciple{
		PrincipleID:        principleID,
		ActorWriterDirs:    found.ActorWriterDirs,
		AnalysisMode:       mode,
		RuleguardRulesFile: found.RuleguardRulesFile,
	}

	switch mode {
	case "regex":
		if found.ScanPattern == "" {
			return nil, fmt.Errorf("principle %q invariant %q analysis_mode=regex but no scan_pattern declared", principleID, found.ID)
		}
		scanRe, err := regexp.Compile(found.ScanPattern)
		if err != nil {
			return nil, fmt.Errorf("compile scan_pattern for invariant %q: %w", found.ID, err)
		}
		out.ScanPattern = scanRe
	case "ruleguard":
		if found.RuleguardRulesFile == "" {
			return nil, fmt.Errorf("principle %q invariant %q analysis_mode=ruleguard but no ruleguard_rules_file declared", principleID, found.ID)
		}
	default:
		return nil, fmt.Errorf("principle %q invariant %q has unsupported analysis_mode=%q (supported: regex, ruleguard)", principleID, found.ID, mode)
	}

	// Exception patterns from the invariant's exception_files list.
	for _, e := range found.ExceptionFiles {
		out.Exception = append(out.Exception, patternEntry{
			re:     fileSuffixRegex(e.File),
			reason: e.Category + ": " + e.Reason,
		})
	}

	// Conformant patterns from the invariant's workflow_step_handler_files list.
	for _, e := range found.WorkflowStepHandlerFiles {
		out.Conformant = append(out.Conformant, patternEntry{
			re:     fileSuffixRegex(e.File),
			reason: e.Reason,
		})
	}

	// Unknown helper files — generic helpers whose classification
	// depends on the caller. Reported as UNKNOWN, not DRIFT.
	for _, e := range found.UnknownHelperFiles {
		out.Unknown = append(out.Unknown, patternEntry{
			re:     fileSuffixRegex(e.File),
			reason: e.Reason,
		})
	}

	// HIDDEN_WORKFLOW patterns from active hidden_workflow.* failure_modes.
	// Lifted entries are excluded; their files (now conformant via the
	// new workflow step handlers) appear in the invariant's
	// workflow_step_handler_files list.
	for _, f := range fm.FailureModes {
		if !isHiddenWorkflowID(f.ID) {
			continue
		}
		if f.Status != "" && f.Status != "active" {
			// "lifted", "resolved", anything-other-than-active: skip.
			continue
		}
		for _, file := range f.Protects.Files {
			out.HiddenWorkflow = append(out.HiddenWorkflow, patternEntry{
				re:     fileSuffixRegex(file),
				reason: "HIDDEN_WORKFLOW: " + f.ID + " (lift to a declarative workflow definition; see failure_modes.yaml entry for lift_to_workflow remediation)",
			})
		}
	}

	return out, nil
}

// fileSuffixRegex builds a regex that matches a file path ending in the
// given relative path. Anchors at path-component boundary so
// "foo/bar.go" matches "/abs/foo/bar.go" but not "/abs/notfoo/bar.go".
func fileSuffixRegex(relPath string) *regexp.Regexp {
	// Quote regex meta-chars in path, then anchor with leading "/" or
	// start-of-string, and end-of-string.
	quoted := regexp.QuoteMeta(relPath)
	return regexp.MustCompile(`(?:^|/)` + quoted + `$`)
}

// isHiddenWorkflowID reports whether the given failure_mode ID names
// a hidden-workflow finding. Convention: id starts with "hidden_workflow.".
func isHiddenWorkflowID(id string) bool {
	const prefix = "hidden_workflow."
	return len(id) >= len(prefix) && id[:len(prefix)] == prefix
}

// invariantLinksToMeta reports whether the invariant declares a link
// to the given meta-principle via related_invariants OR meta_principle_instances.
func invariantLinksToMeta(inv *principleSchema, metaID string) bool {
	for _, r := range inv.RelatedInvariants {
		if r == metaID {
			return true
		}
	}
	for _, m := range inv.MetaPrincipleInstances {
		if m.Parent == metaID {
			return true
		}
	}
	return false
}
