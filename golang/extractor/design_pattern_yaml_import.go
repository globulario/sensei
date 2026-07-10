// SPDX-License-Identifier: Apache-2.0

// Design-pattern awareness importers (the "how" layer).
//
// DesignPattern  — the general project-grounded shape (when to use, when NOT to,
//
//	what it prevents, what misuse is forbidden).
//
// PatternMisuse  — a dangerous misuse, linked to a ForbiddenFix where one exists.
// (ImplementationPattern is extended in implementation_pattern_yaml_import.go.)
//
// All three are single-entity docs detected by `id` + `class:` (the established
// ImplementationPattern convention). Linking never types the target. Many edges
// also emit a reverse aw:relatedPattern from the referenced node, so resolving
// an invariant/component/boundary/contract surfaces the patterns connected to it.
package extractor

import (
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

// emitPatternEdges emits the forward edge subj→prop→target for each bare id and
// a reverse <target> aw:relatedPattern subj so the target surfaces the pattern.
func emitPatternEdges(e *rdf.Emitter, subj, prop, classIRI string, ids []string) {
	for _, id := range ids {
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		tgt := rdf.MintIRI(classIRI, id)
		e.Triple(subj, rdf.IRI(prop), tgt)
		e.Triple(tgt, rdf.IRI(rdf.PropRelatedPattern), subj)
	}
}

// ── DesignPattern ──────────────────────────────────────────────────────────

type yamlDesignPattern struct {
	ID                    string     `yaml:"id"`
	Class                 string     `yaml:"class"`
	Name                  string     `yaml:"name"`
	Category              string     `yaml:"category"`
	Description           string     `yaml:"description"`
	AppliesWhen           string     `yaml:"applies_when"`
	DoesNotApplyWhen      string     `yaml:"does_not_apply_when"`
	ForcesOrTradeoffs     string     `yaml:"forces_or_tradeoffs"`
	Confidence            string     `yaml:"confidence"`
	FailureModesPrevented []string   `yaml:"failure_modes_prevented"`
	ForbiddenMisuses      []string   `yaml:"forbidden_misuses"`
	RelatedMetaPrinciples []string   `yaml:"related_meta_principles"`
	RelatedInvariants     []string   `yaml:"related_invariants"`
	RelatedComponents     []string   `yaml:"related_components"`
	RelatedBoundaries     []string   `yaml:"related_boundaries"`
	RelatedContracts      []string   `yaml:"related_contracts"`
	RelatedDecisions      []string   `yaml:"related_decisions"`
	Tests                 []string   `yaml:"tests"`
	Evidence              []string   `yaml:"evidence"`
	ExampleFiles          []string   `yaml:"example_files"`
	ExampleCodeSymbols    []string   `yaml:"example_code_symbols"`
	Uml                   umlProfile `yaml:"uml"`
}

func importDesignPattern(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var p yamlDesignPattern
	if err := yaml.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if p.ID == "" {
		return nil
	}
	subj := rdf.MintIRI(rdf.ClassDesignPattern, p.ID)
	e.Typed(subj, rdf.ClassDesignPattern)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(p.Name, p.ID)))
	emitOptLit(e, subj, rdf.PropComment, p.Description)
	emitOptLit(e, subj, rdf.PropKind, p.Category)
	emitOptLit(e, subj, rdf.PropAppliesWhen, p.AppliesWhen)
	emitOptLit(e, subj, rdf.PropDoesNotApplyWhen, p.DoesNotApplyWhen)
	emitOptLit(e, subj, rdf.PropTradeoffs, p.ForcesOrTradeoffs)
	e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(p.Confidence)))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	emitPatternEdges(e, subj, rdf.PropMitigates, rdf.ClassFailureMode, p.FailureModesPrevented)
	emitBareEdges(e, subj, rdf.PropForbids, rdf.ClassPatternMisuse, p.ForbiddenMisuses)
	emitBareEdges(e, subj, rdf.PropSatisfiesMetaPrinciple, rdf.ClassInvariant, p.RelatedMetaPrinciples)
	emitPatternEdges(e, subj, rdf.PropSatisfiesInvariant, rdf.ClassInvariant, p.RelatedInvariants)
	emitPatternEdges(e, subj, rdf.PropAppliesTo, rdf.ClassComponent, p.RelatedComponents)
	emitPatternEdges(e, subj, rdf.PropProtects, rdf.ClassBoundary, p.RelatedBoundaries)
	emitPatternEdges(e, subj, rdf.PropShapes, rdf.ClassContract, p.RelatedContracts)
	emitBareEdges(e, subj, rdf.PropChosenBy, rdf.ClassDecision, p.RelatedDecisions)
	emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, p.Tests)
	emitBareEdges(e, subj, rdf.PropSupportedByEvidence, rdf.ClassEvidence, p.Evidence)
	emitSpineAnchors(e, subj, p.ExampleFiles, p.ExampleCodeSymbols)
	emitUML(e, subj, p.Uml)
	return nil
}

// ── PatternMisuse ──────────────────────────────────────────────────────────

type yamlPatternMisuse struct {
	ID                 string     `yaml:"id"`
	Class              string     `yaml:"class"`
	Name               string     `yaml:"name"`
	Description        string     `yaml:"description"`
	WhyDangerous       string     `yaml:"why_dangerous"`
	Status             string     `yaml:"status"`
	Confidence         string     `yaml:"confidence"`
	MisusedPattern     string     `yaml:"misused_pattern"`
	MisusedPatterns    []string   `yaml:"misused_patterns"`
	SaferPattern       string     `yaml:"safer_pattern"`
	ForbiddenBy        []string   `yaml:"forbidden_by"` // class-qualified refs (forbidden_fix:.., meta_principle:..)
	ViolatesInvariants []string   `yaml:"violates_invariants"`
	CausesFailureModes []string   `yaml:"causes_failure_modes"`
	AvoidedBy          []string   `yaml:"avoided_by"` // implementation_pattern ids
	DetectedIn         []string   `yaml:"detected_in"`
	Tests              []string   `yaml:"tests"`
	Evidence           []string   `yaml:"evidence"`
	Uml                umlProfile `yaml:"uml"`
}

func importPatternMisuse(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var m yamlPatternMisuse
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if m.ID == "" {
		return nil
	}
	subj := rdf.MintIRI(rdf.ClassPatternMisuse, m.ID)
	e.Typed(subj, rdf.ClassPatternMisuse)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(m.Name, m.ID)))
	emitOptLit(e, subj, rdf.PropComment, m.Description)
	emitOptLit(e, subj, rdf.PropComment, m.WhyDangerous)
	emitOptLit(e, subj, rdf.PropStatus, m.Status)
	e.Triple(subj, rdf.IRI(rdf.PropAssertionMethod), rdf.Lit(assertionOrDefault(m.Confidence)))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	misused := m.MisusedPatterns
	if m.MisusedPattern != "" {
		misused = append(misused, m.MisusedPattern)
	}
	emitBareEdges(e, subj, rdf.PropMisuses, rdf.ClassDesignPattern, misused)
	if sp := strings.TrimSpace(m.SaferPattern); sp != "" {
		e.Triple(subj, rdf.IRI(rdf.PropSaferPattern), rdf.MintIRI(rdf.ClassDesignPattern, sp))
	}
	emitRefEdges(e, subj, rdf.PropForbiddenBy, m.ForbiddenBy)
	emitPatternEdges(e, subj, rdf.PropViolatesInvariant, rdf.ClassInvariant, m.ViolatesInvariants)
	emitPatternEdges(e, subj, rdf.PropCauses, rdf.ClassFailureMode, m.CausesFailureModes)
	emitBareEdges(e, subj, rdf.PropAvoidedBy, rdf.ClassImplementationPattern, m.AvoidedBy)
	emitBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, m.Tests)
	emitBareEdges(e, subj, rdf.PropSupportedByEvidence, rdf.ClassEvidence, m.Evidence)
	emitSpineAnchors(e, subj, m.DetectedIn, nil)
	emitUML(e, subj, m.Uml)
	return nil
}
