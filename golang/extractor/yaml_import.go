// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=extractor
// @awareness file_role=yaml_to_rdf_importer
// @awareness implements=globular.awareness_graph:intent.awareness.yaml_importer_does_not_silently_skip_in_strict_mode
// @awareness implements=globular.platform:intent.awareness.graph_is_compiled_context_not_authority

// Package extractor reads authored awareness sources and emits RDF triples.
//
// This file contains the three schema-specific importers for:
//
//	invariants.yaml, failure_modes.yaml, incident_patterns.yaml
//
// The recursive directory walker and schema classification live in
// yaml_import_dir.go (Phase A). Intent file support will land in
// intent_yaml_import.go (Phase C) once the intent ontology terms are settled.
//
// Output: N-Triples streamed to the provided io.Writer via rdf.Emitter.
// Counts are exposed on the returned emitter so callers can produce build
// summaries.
//
// Bug fixes carried forward from the /tmp/oxigraph-spike validation:
//
//  1. Incident pattern wrong_fixes is mixed: some entries are stable
//     forbidden_fix IDs, others are prose with em-dashes/backticks/quotes.
//     Naive emit produced invalid IRIs. The importer now routes prose to
//     rdfs:comment literals on the pattern and only emits aw:forbids edges
//     for entries that pass rdf.IsStableID.
//
//  2. Etcd state keys are templates like /globular/services/{id}/config.
//     The braces are explicitly disallowed in N-Triples IRIREFs (W3C
//     grammar). rdf.EncodeIRIPath percent-encodes them.
package extractor

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

// ─── YAML shapes — only the fields the importer maps ──────────────────────

type invariantProtects struct {
	State           []string `yaml:"state"`
	Files           []string `yaml:"files"`
	EnforcesFiles   []string `yaml:"enforces_files"`
	ConfiguresFiles []string `yaml:"configures_files"`
	ObservesFiles   []string `yaml:"observes_files"`
	MayAffectFiles  []string `yaml:"may_affect_files"`
	Symbols         []string `yaml:"symbols"`
	SystemdUnits    []string `yaml:"systemd_units"`
}

type yamlInvariant struct {
	ID                  string            `yaml:"id"`
	Title               string            `yaml:"title"`
	Severity            string            `yaml:"severity"`
	Status              string            `yaml:"status"`
	Protects            invariantProtects `yaml:"protects"`
	ForbiddenFixes      []string          `yaml:"forbidden_fixes"`
	RequiredTests       []string          `yaml:"required_tests"`
	RelatedFailureModes []string          `yaml:"related_failure_modes"`
	RelatedInvariants   []string          `yaml:"related_invariants"`
	Detect              detectRule        `yaml:"detect"`
	Uml                 umlProfile        `yaml:"uml"`
	domainScope         `yaml:",inline"`
}

type failureModeProtects struct {
	Files []string `yaml:"files"`
}

type yamlFailureMode struct {
	ID                string              `yaml:"id"`
	Title             string              `yaml:"title"`
	Severity          string              `yaml:"severity"`
	Protects          failureModeProtects `yaml:"protects"`
	RelatedInvariants []string            `yaml:"related_invariants"`
	// ViolatesContracts links a failure mode UP to the architectural contract(s)
	// it breaks — the spine that makes "not just a failing test, a violation of
	// contract X guarded by invariant Y" traversable.
	ViolatesContracts []string   `yaml:"violates_contracts"`
	RequiredTests     []string   `yaml:"required_tests"`
	Uml               umlProfile `yaml:"uml"`
}

type yamlIncidentPattern struct {
	ID                string   `yaml:"id"`
	Title             string   `yaml:"title"`
	Severity          string   `yaml:"severity"`
	FailureMode       string   `yaml:"failure_mode"`
	Files             []string `yaml:"files"`
	RelatedInvariants []string `yaml:"related_invariants"`
	WrongFixes        []string `yaml:"wrong_fixes"`
}

type invariantFile struct {
	Invariants []yamlInvariant `yaml:"invariants"`
}
type failureModesFile struct {
	FailureModes []yamlFailureMode `yaml:"failure_modes"`
}
type incidentPatternsFile struct {
	IncidentPatterns []yamlIncidentPattern `yaml:"incident_patterns"`
}

// ─── Importer entry point ────────────────────────────────────────────────

// ImportAwarenessYAMLs walks docsDir recursively and imports every YAML file
// whose schema has a registered importer. It delegates to ImportAwarenessDir
// and drops the ImportReport; use ImportAwarenessDir directly when you need
// the per-file coverage report or strict-mode enforcement.
//
// Returns the emitter so callers can read .Triples, .ByClass, .ByPredicate
// for build summaries.
// ImportAwarenessYAMLs is the legacy single-directory entry point.
// It delegates to ImportAwarenessDir which implements the strict-mode contract:
// every skipped file is recorded in the ImportReport; --strict callers see them all.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=extractor
// @awareness implements=globular.awareness_graph:intent.awareness.yaml_importer_does_not_silently_skip_in_strict_mode
// @awareness tested_by=golang/extractor/yaml_import_dir_test.go:TestImportDir_NoSilentSkip
func ImportAwarenessYAMLs(docsDir string, w io.Writer) (*rdf.Emitter, error) {
	e, _, err := ImportAwarenessDir(docsDir, w)
	return e, err
}

// ─── Per-source importers ────────────────────────────────────────────────

func importInvariants(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f invariantFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, inv := range f.Invariants {
		subj := rdf.MintIRI(rdf.ClassInvariant, inv.ID)
		e.Typed(subj, rdf.ClassInvariant)
		// Architectural spine: meta.* invariants are reusable architectural
		// laws. Dual-type them aw:MetaPrinciple (in addition to aw:Invariant, on
		// the same node) so they are queryable as first-class architecture
		// drivers without duplicating their definitions. Existing invariant
		// queries are unaffected.
		if strings.HasPrefix(inv.ID, "meta.") {
			e.Typed(subj, rdf.ClassMetaPrinciple)
		}
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(inv.Title)))
		if inv.Severity != "" {
			e.Triple(subj, rdf.IRI(rdf.PropSeverity), rdf.Lit(inv.Severity))
		}
		if inv.Status != "" {
			e.Triple(subj, rdf.IRI(rdf.PropStatus), rdf.Lit(inv.Status))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
		emitUML(e, subj, inv.Uml)

		// Domain scope + provenance: tags a repo-scoped (pilot) invariant with
		// aw:repo/aw:domain so the scope filter isolates it. Untagged
		// (home-domain) invariants emit nothing here.
		emitDomainScope(e, subj, inv.domainScope)
		// Detect block: advisory bad-shape patterns for warning-level EditCheck.
		emitDetect(e, subj, inv.Detect)

		// Each anchored file gets:
		//   1. A typed source_file node (idempotent at the RDF layer).
		//   2. A forward edge invariant → role → file (carries the role
		//      distinction: protects / enforces / configures / observes /
		//      mayAffect).
		//   3. A reverse edge file → aw:implements → invariant. This is
		//      what the impact partition follows to land Direct anchors
		//      starting from a file; without it, an agent editing a file
		//      cannot find the invariants the file is part of without
		//      scanning every invariant.
		//
		// mayAffect is the deliberate exception — it is the weakest
		// indirect connection ("edits to this file MAY affect the
		// invariant") and we don't want it to land as a Direct anchor in
		// impact output. No reverse implements edge is emitted for
		// may_affect_files.
		for _, f := range inv.Protects.Files {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, f))
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
		for _, f := range inv.Protects.EnforcesFiles {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropEnforces), rdf.MintIRI(rdf.ClassSourceFile, f))
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
		for _, f := range inv.Protects.ConfiguresFiles {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropConfigures), rdf.MintIRI(rdf.ClassSourceFile, f))
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
		for _, f := range inv.Protects.ObservesFiles {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropObserves), rdf.MintIRI(rdf.ClassSourceFile, f))
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
		for _, f := range inv.Protects.MayAffectFiles {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropMayAffect), rdf.MintIRI(rdf.ClassSourceFile, f))
			// No reverse implements — see comment above.
		}
		for _, s := range inv.Protects.Symbols {
			ensureNode(e, rdf.ClassSymbol, s)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSymbol, s))
		}
		for _, k := range inv.Protects.State {
			ensureNode(e, rdf.ClassEtcdKey, k)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassEtcdKey, k))
		}
		for _, u := range inv.Protects.SystemdUnits {
			ensureNode(e, rdf.ClassSystemdUnit, u)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSystemdUnit, u))
		}

		for _, fx := range inv.ForbiddenFixes {
			// Same prose-vs-ID split the incident_patterns importer uses.
			// Authors mix stable IDs (resolvable to definitions in
			// forbidden_fixes.yaml) with free-text reminders ("don't add
			// X without Y"). Without this guard:
			//   - prose mints a typed-stub node whose IRI contains
			//     percent-encoded spaces — valid but unusable;
			//   - every such stub trips the reference validator as
			//     "dangling" because no schema ever defines it.
			// Routing prose to rdfs:comment preserves the authoring
			// intent ("here's something not to do") while keeping the
			// typed-anchor surface honest.
			if rdf.IsStableID(fx) {
				ensureNode(e, rdf.ClassForbiddenFix, fx)
				e.Triple(subj, rdf.IRI(rdf.PropForbids), rdf.MintIRI(rdf.ClassForbiddenFix, fx))
			} else {
				e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(fx))
			}
		}
		for _, t := range inv.RequiredTests {
			ensureNode(e, rdf.ClassTest, t)
			e.Triple(subj, rdf.IRI(rdf.PropRequiresTest), rdf.MintIRI(rdf.ClassTest, t))
		}
		for _, fm := range inv.RelatedFailureModes {
			// Don't ensure — failure_modes.yaml owns FM typing. Edges to
			// missing FMs become "dangling references" surfaced by the drift
			// query. This is intentional: the importer does not paper over
			// authoring gaps by minting orphan typed nodes.
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassFailureMode, fm))
		}
		for _, rel := range inv.RelatedInvariants {
			// Invariant → Invariant linkage (e.g. a concrete ui.* invariant
			// citing its generative meta.* parent). Same don't-ensure rule:
			// a reference to an undefined invariant surfaces as dangling.
			e.Triple(subj, rdf.IRI(rdf.PropRelatedTo), rdf.MintIRI(rdf.ClassInvariant, rel))
		}
	}
	return nil
}

func importFailureModes(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f failureModesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, fm := range f.FailureModes {
		subj := rdf.MintIRI(rdf.ClassFailureMode, fm.ID)
		e.Typed(subj, rdf.ClassFailureMode)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(fm.Title)))
		if fm.Severity != "" {
			e.Triple(subj, rdf.IRI(rdf.PropSeverity), rdf.Lit(fm.Severity))
		}
		emitUML(e, subj, fm.Uml)
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		for _, inv := range fm.RelatedInvariants {
			// FM → Invariant uses aw:affects, same predicate as
			// Invariant → FM and IncidentPattern → Invariant. The v0.0
			// ontology had a separate PropRelatedInvariant; v0.1 unifies
			// under PropAffects with object-class type-filtering for
			// directional queries.
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, inv))
		}
		for _, t := range fm.RequiredTests {
			ensureNode(e, rdf.ClassTest, t)
			e.Triple(subj, rdf.IRI(rdf.PropRequiresTest), rdf.MintIRI(rdf.ClassTest, t))
		}
		// Spine ligament: FM → architectural contract it violates, plus the
		// reverse Contract → violatedBy → FM so the contract side (and the
		// verification gate) can find its known violations.
		for _, c := range fm.ViolatesContracts {
			contractIRI := rdf.MintIRI(rdf.ClassContract, c)
			e.Triple(subj, rdf.IRI(rdf.PropViolatesContract), contractIRI)
			e.Triple(contractIRI, rdf.IRI(rdf.PropViolatedBy), subj)
		}
		for _, f := range fm.Protects.Files {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, f))
			// Reverse edge — lets briefing-by-file surface failure modes as Direct anchors.
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
	}
	return nil
}

func importIncidentPatterns(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f incidentPatternsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, p := range f.IncidentPatterns {
		subj := rdf.MintIRI(rdf.ClassIncidentPattern, p.ID)
		e.Typed(subj, rdf.ClassIncidentPattern)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(p.Title)))
		if p.Severity != "" {
			e.Triple(subj, rdf.IRI(rdf.PropSeverity), rdf.Lit(p.Severity))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		if p.FailureMode != "" {
			e.Triple(subj, rdf.IRI(rdf.PropExemplifies), rdf.MintIRI(rdf.ClassFailureMode, p.FailureMode))
		}
		for _, f := range p.Files {
			ensureNode(e, rdf.ClassSourceFile, f)
			e.Triple(subj, rdf.IRI(rdf.PropProtects), rdf.MintIRI(rdf.ClassSourceFile, f))
			// Reverse implements edge — same rationale as the invariant
			// case: lets impact-by-file land patterns as Direct anchors.
			e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
		}
		for _, inv := range p.RelatedInvariants {
			e.Triple(subj, rdf.IRI(rdf.PropAffects), rdf.MintIRI(rdf.ClassInvariant, inv))
		}
		for _, fix := range p.WrongFixes {
			// Bug-fix-from-spike: route prose to rdfs:comment, IDs to
			// aw:forbids edges. See package docstring.
			if rdf.IsStableID(fix) {
				ensureNode(e, rdf.ClassForbiddenFix, fix)
				e.Triple(subj, rdf.IRI(rdf.PropForbids), rdf.MintIRI(rdf.ClassForbiddenFix, fix))
			} else {
				e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(fix))
			}
		}
	}
	return nil
}

// ensureNode emits the rdf:type triple for a referenced node. Idempotent
// at the RDF level (store dedupes); but we still call it conditionally so
// the emitter's per-class counter reflects unique nodes rather than
// reference occurrences.
func ensureNode(e *rdf.Emitter, cls, id string) {
	e.Typed(rdf.MintIRI(cls, id), cls)
}
