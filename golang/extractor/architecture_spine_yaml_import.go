// SPDX-License-Identifier: Apache-2.0

// Architectural-spine importers (Stage A).
//
// These add the "architectural nervous system" over the existing safety nodes:
// Component (units of ownership), Boundary (places architecture can be
// violated), Contract (API/proto/schema/CLI contracts), Evidence (proof a rule
// is alive), and the meta-principle links that connect the existing meta.*
// invariants to the rules / decisions / components they drive. Decision reuses
// the Phase-B importer (extended in yaml_import_phaseB.go); MetaPrinciple nodes
// are dual-typed by importInvariants — this file only attaches their outgoing
// edges.
//
// Each importer follows the Phase-B contract: take an *rdf.Emitter + path,
// skip missing files, error only on read/parse failure, and link to existing
// nodes via MintIRI without typing them (the defining file owns the node).
package extractor

import (
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

// ── shared helpers ─────────────────────────────────────────────────────────

// emitBareEdges emits one object edge subj→prop→MintIRI(classIRI, id) per bare
// id. Linking is not authoring: the target is not typed here, so a reference to
// an undefined node does not inflate that class's count (dangling references are
// caught by `awg validate`, not papered over).
func emitBareEdges(e *rdf.Emitter, subj, prop, classIRI string, ids []string) {
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			e.Triple(subj, rdf.IRI(prop), rdf.MintIRI(classIRI, id))
		}
	}
}

// metaPrincipleRef mints the IRI of a meta-principle. Meta-principles are
// dual-typed meta.* invariants, so the node lives at the invariant IRI.
func metaPrincipleRef(id string) string {
	return rdf.MintIRI(rdf.ClassInvariant, strings.TrimSpace(id))
}

// emitSpineAnchors wires a spine node to its code anchors: aw:anchoredIn →
// SourceFile/CodeSymbol plus the reverse aw:implements edge so briefing-by-file
// surfaces the spine node as a Direct anchor.
func emitSpineAnchors(e *rdf.Emitter, subj string, files, symbols []string) {
	for _, f := range files {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		ensureNode(e, rdf.ClassSourceFile, f)
		e.Triple(subj, rdf.IRI(rdf.PropAnchoredIn), rdf.MintIRI(rdf.ClassSourceFile, f))
		e.Triple(rdf.MintIRI(rdf.ClassSourceFile, f), rdf.IRI(rdf.PropImplements), subj)
	}
	for _, s := range symbols {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		ensureNode(e, rdf.ClassCodeSymbol, s)
		e.Triple(subj, rdf.IRI(rdf.PropAnchoredIn), rdf.MintIRI(rdf.ClassCodeSymbol, s))
		e.Triple(rdf.MintIRI(rdf.ClassCodeSymbol, s), rdf.IRI(rdf.PropImplements), subj)
	}
}

// assertionOrDefault returns the node's assertion method, defaulting to
// "declared" (hand-authored corpus). Stage B proto extraction would set
// "inferred".
func assertionOrDefault(v string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return "declared"
}

// ── Component ──────────────────────────────────────────────────────────────

type yamlComponent struct {
	ID                      string     `yaml:"id"`
	Name                    string     `yaml:"name"`
	Description             string     `yaml:"description"`
	Kind                    string     `yaml:"kind"`
	Owner                   string     `yaml:"owner"`
	Assertion               string     `yaml:"assertion"`
	OwnsInvariants          []string   `yaml:"owns_invariants"`
	ImplementsIntents       []string   `yaml:"implements_intents"`
	ExposesContracts        []string   `yaml:"exposes_contracts"`
	DependsOn               []string   `yaml:"depends_on"`
	ReadsFrom               []string   `yaml:"reads_from"`
	WritesTo                []string   `yaml:"writes_to"`
	ProtectedBy             []string   `yaml:"protected_by"`
	SatisfiesMetaPrinciples []string   `yaml:"satisfies_meta_principles"`
	ViolatesMetaPrinciples  []string   `yaml:"violates_meta_principles"`
	Tests                   []string   `yaml:"tests"`
	SupportedByEvidence     []string   `yaml:"supported_by_evidence"`
	SourceFiles             []string   `yaml:"source_files"`
	CodeSymbols             []string   `yaml:"code_symbols"`
	Uml                     umlProfile `yaml:"uml"`
	domainScope             `yaml:",inline"`
}

type componentsFile struct {
	Components []yamlComponent `yaml:"components"`
}

func importComponents(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f componentsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, c := range f.Components {
		if c.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassComponent, c.ID)
		e.Typed(subj, rdf.ClassComponent)
		emitDomainScope(e, subj, c.domainScope)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(c.Name, c.ID)))
		emitOptLit(e, subj, rdf.PropComment, c.Description)
		emitOptLit(e, subj, rdf.PropKind, c.Kind)
		emitOptLit(e, subj, rdf.PropOwnerService, c.Owner)
		e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(c.Assertion)))
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitBareEdges(e, subj, rdf.PropOwnsInvariant, rdf.ClassInvariant, c.OwnsInvariants)
		emitBareEdges(e, subj, rdf.PropImplementsIntent, rdf.ClassIntent, c.ImplementsIntents)
		emitBareEdges(e, subj, rdf.PropExposesContract, rdf.ClassContract, c.ExposesContracts)
		emitBareEdges(e, subj, rdf.PropDependsOn, rdf.ClassComponent, c.DependsOn)
		emitBareEdges(e, subj, rdf.PropReadsFrom, rdf.ClassComponent, c.ReadsFrom)
		emitBareEdges(e, subj, rdf.PropWritesTo, rdf.ClassComponent, c.WritesTo)
		emitBareEdges(e, subj, rdf.PropProtectedByBoundary, rdf.ClassBoundary, c.ProtectedBy)
		emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, c.Tests)
		emitBareEdges(e, subj, rdf.PropSupportedByEvidence, rdf.ClassEvidence, c.SupportedByEvidence)
		for _, mp := range c.SatisfiesMetaPrinciples {
			if mp = strings.TrimSpace(mp); mp != "" {
				e.Triple(subj, rdf.IRI(rdf.PropSatisfiesMetaPrinciple), metaPrincipleRef(mp))
			}
		}
		for _, mp := range c.ViolatesMetaPrinciples {
			if mp = strings.TrimSpace(mp); mp != "" {
				e.Triple(subj, rdf.IRI(rdf.PropViolatesMetaPrinciple), metaPrincipleRef(mp))
			}
		}
		emitSpineAnchors(e, subj, c.SourceFiles, c.CodeSymbols)
		emitUML(e, subj, c.Uml)
	}
	return nil
}

// ── Boundary ───────────────────────────────────────────────────────────────

type yamlBoundary struct {
	ID               string     `yaml:"id"`
	Name             string     `yaml:"name"`
	Title            string     `yaml:"title"`
	Description      string     `yaml:"description"`
	Kind             string     `yaml:"kind"`
	Status           string     `yaml:"status"`
	Assertion        string     `yaml:"assertion"`
	Separates        []string   `yaml:"separates"`
	Protects         []string   `yaml:"protects"`
	ExposesContracts []string   `yaml:"exposes_contracts"`
	VulnerableTo     []string   `yaml:"vulnerable_to"`
	Forbids          []string   `yaml:"forbids"`
	Tests            []string   `yaml:"tests"`
	SourceFiles      []string   `yaml:"source_files"`
	CodeSymbols      []string   `yaml:"code_symbols"`
	Uml              umlProfile `yaml:"uml"`
	domainScope      `yaml:",inline"`
}

type boundariesFile struct {
	Boundaries []yamlBoundary `yaml:"boundaries"`
}

func importBoundaries(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f boundariesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, b := range f.Boundaries {
		if b.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassBoundary, b.ID)
		e.Typed(subj, rdf.ClassBoundary)
		emitDomainScope(e, subj, b.domainScope)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(b.Name, b.Title, b.ID)))
		emitOptLit(e, subj, rdf.PropComment, b.Description)
		emitOptLit(e, subj, rdf.PropKind, b.Kind)
		emitOptLit(e, subj, rdf.PropStatus, b.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(b.Assertion)))
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitBareEdges(e, subj, rdf.PropSeparates, rdf.ClassComponent, b.Separates)
		emitBareEdges(e, subj, rdf.PropProtects, rdf.ClassInvariant, b.Protects)
		emitBareEdges(e, subj, rdf.PropExposesContract, rdf.ClassContract, b.ExposesContracts)
		emitBareEdges(e, subj, rdf.PropVulnerableTo, rdf.ClassFailureMode, b.VulnerableTo)
		emitBareEdges(e, subj, rdf.PropForbids, rdf.ClassForbiddenFix, b.Forbids)
		emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, b.Tests)
		emitSpineAnchors(e, subj, b.SourceFiles, b.CodeSymbols)
		emitUML(e, subj, b.Uml)
	}
	return nil
}

// ── Contract (architectural) ───────────────────────────────────────────────
//
// Distinct from importContracts (versioned_doc schema, routed on the `version:`
// key). This importer handles a top-level `contracts:` list of API/proto/schema/
// CLI contracts with read/write semantics. Both emit aw:Contract nodes.

type yamlArchContract struct {
	ID                      string     `yaml:"id"`
	Name                    string     `yaml:"name"`
	Description             string     `yaml:"description"`
	Kind                    string     `yaml:"kind"`
	Stability               string     `yaml:"stability"`
	ReadOrWrite             string     `yaml:"read_or_write"`
	Status                  string     `yaml:"status"`
	Assertion               string     `yaml:"assertion"`
	ExposedBy               []string   `yaml:"exposed_by"`
	ConsumedBy              []string   `yaml:"consumed_by"`
	ConstrainedByInvariants []string   `yaml:"constrained_by_invariants"`
	SatisfiesMetaPrinciples []string   `yaml:"satisfies_meta_principles"`
	Tests                   []string   `yaml:"tests"`
	SupportedByEvidence     []string   `yaml:"supported_by_evidence"`
	SourceFiles             []string   `yaml:"source_files"`
	CodeSymbols             []string   `yaml:"code_symbols"`
	Uml                     umlProfile `yaml:"uml"`
	domainScope             `yaml:",inline"`
}

type archContractsFile struct {
	Contracts []yamlArchContract `yaml:"contracts"`
}

func importArchitectureContracts(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f archContractsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, c := range f.Contracts {
		if c.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassContract, c.ID)
		e.Typed(subj, rdf.ClassContract)
		emitDomainScope(e, subj, c.domainScope)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(c.Name, c.ID)))
		emitOptLit(e, subj, rdf.PropComment, c.Description)
		emitOptLit(e, subj, rdf.PropKind, c.Kind)
		emitOptLit(e, subj, rdf.PropStability, c.Stability)
		emitOptLit(e, subj, rdf.PropReadOrWrite, c.ReadOrWrite)
		emitOptLit(e, subj, rdf.PropStatus, c.Status)
		e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(c.Assertion)))
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitBareEdges(e, subj, rdf.PropExposedBy, rdf.ClassComponent, c.ExposedBy)
		emitBareEdges(e, subj, rdf.PropConsumedBy, rdf.ClassComponent, c.ConsumedBy)
		emitBareEdges(e, subj, rdf.PropConstrainedByInvariant, rdf.ClassInvariant, c.ConstrainedByInvariants)
		emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, c.Tests)
		emitBareEdges(e, subj, rdf.PropSupportedByEvidence, rdf.ClassEvidence, c.SupportedByEvidence)
		for _, mp := range c.SatisfiesMetaPrinciples {
			if mp = strings.TrimSpace(mp); mp != "" {
				e.Triple(subj, rdf.IRI(rdf.PropSatisfiesMetaPrinciple), metaPrincipleRef(mp))
			}
		}
		emitSpineAnchors(e, subj, c.SourceFiles, c.CodeSymbols)
		emitUML(e, subj, c.Uml)
	}
	return nil
}

// ── Evidence ───────────────────────────────────────────────────────────────

type yamlEvidence struct {
	ID                  string     `yaml:"id"`
	Name                string     `yaml:"name"`
	Summary             string     `yaml:"summary"`
	Description         string     `yaml:"description"`
	Kind                string     `yaml:"kind"`
	Status              string     `yaml:"status"`
	Assertion           string     `yaml:"assertion"`
	Command             string     `yaml:"command"`
	Timestamp           string     `yaml:"timestamp"`
	Source              string     `yaml:"source"`
	Supports            []string   `yaml:"supports"` // class-qualified refs
	ValidatesComponents []string   `yaml:"validates_components"`
	Confirms            []string   `yaml:"confirms"` // failure_modes
	ProducedByTests     []string   `yaml:"produced_by_tests"`
	StaleFor            []string   `yaml:"stale_for"` // class-qualified refs
	SourceFiles         []string   `yaml:"source_files"`
	Uml                 umlProfile `yaml:"uml"`
	domainScope         `yaml:",inline"`
}

type evidenceFile struct {
	Evidence []yamlEvidence `yaml:"evidence"`
}

func importEvidence(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f evidenceFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, ev := range f.Evidence {
		if ev.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassEvidence, ev.ID)
		e.Typed(subj, rdf.ClassEvidence)
		emitDomainScope(e, subj, ev.domainScope)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(ev.Name, ev.Summary, ev.ID)))
		emitOptLit(e, subj, rdf.PropComment, ev.Description)
		emitOptLit(e, subj, rdf.PropKind, ev.Kind)
		emitOptLit(e, subj, rdf.PropStatus, ev.Status)
		emitOptLit(e, subj, rdf.PropCommand, ev.Command)
		emitOptLit(e, subj, rdf.PropObservedAt, ev.Timestamp)
		emitOptLit(e, subj, rdf.PropSourcePath, ev.Source)
		e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(ev.Assertion)))
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitRefEdges(e, subj, rdf.PropSupports, ev.Supports)
		emitRefEdges(e, subj, rdf.PropStaleFor, ev.StaleFor)
		emitBareEdges(e, subj, rdf.PropValidatesComponent, rdf.ClassComponent, ev.ValidatesComponents)
		emitBareEdges(e, subj, rdf.PropConfirms, rdf.ClassFailureMode, ev.Confirms)
		emitBareEdges(e, subj, rdf.PropProducedByTest, rdf.ClassTest, ev.ProducedByTests)
		emitSpineAnchors(e, subj, ev.SourceFiles, nil)
		emitUML(e, subj, ev.Uml)
	}
	return nil
}

// ── MetaPrinciple links ────────────────────────────────────────────────────
//
// Meta-principles are the existing meta.* invariants (dual-typed by
// importInvariants). This importer attaches their OUTGOING architectural edges
// — generates / constrains / appliesTo / explains — to the meta.* invariant IRI
// so an agent resolving a principle sees the rules, decisions, and components it
// drives. It never re-types or re-authors the principle.

type yamlMetaPrincipleLink struct {
	ID                       string   `yaml:"id"` // meta.<slug>
	Assertion                string   `yaml:"assertion"`
	GeneratesInvariants      []string `yaml:"generates_invariants"`
	ConstrainsDecisions      []string `yaml:"constrains_decisions"`
	AppliesToComponents      []string `yaml:"applies_to_components"`
	AppliesToBoundaries      []string `yaml:"applies_to_boundaries"`
	AppliesToContracts       []string `yaml:"applies_to_contracts"`
	ExplainsIntents          []string `yaml:"explains_intents"`
	RecommendsDesignPatterns []string `yaml:"recommends_design_patterns"`
}

type metaPrincipleLinksFile struct {
	MetaPrincipleLinks []yamlMetaPrincipleLink `yaml:"meta_principle_links"`
}

func importMetaPrincipleLinks(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f metaPrincipleLinksFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, l := range f.MetaPrincipleLinks {
		if strings.TrimSpace(l.ID) == "" {
			continue
		}
		// Attach to the meta.* invariant IRI; do not type/author the node.
		subj := metaPrincipleRef(l.ID)
		e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(l.Assertion)))
		emitBareEdges(e, subj, rdf.PropGenerates, rdf.ClassInvariant, l.GeneratesInvariants)
		emitBareEdges(e, subj, rdf.PropConstrains, rdf.ClassDecision, l.ConstrainsDecisions)
		emitBareEdges(e, subj, rdf.PropAppliesTo, rdf.ClassComponent, l.AppliesToComponents)
		emitBareEdges(e, subj, rdf.PropAppliesTo, rdf.ClassBoundary, l.AppliesToBoundaries)
		emitBareEdges(e, subj, rdf.PropAppliesTo, rdf.ClassContract, l.AppliesToContracts)
		emitBareEdges(e, subj, rdf.PropExplains, rdf.ClassIntent, l.ExplainsIntents)
		emitBareEdges(e, subj, rdf.PropRecommends, rdf.ClassDesignPattern, l.RecommendsDesignPatterns)
	}
	return nil
}
