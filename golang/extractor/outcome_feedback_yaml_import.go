// SPDX-License-Identifier: AGPL-3.0-only

// Importer for OutcomeFeedback YAML files (Phase 2).
//
// An OutcomeFeedback record captures how a decision / change / remediation
// turned out — success, failure, blocked, or reverted — and links to the
// authority nodes (invariants, failure modes, implementation patterns, tests)
// that informed or were involved in it. It is compiled/indexed knowledge: the
// importer emits ONE OutcomeFeedback node plus link edges to EXISTING nodes.
// It never types those targets, so an outcome that names invariant:foo can
// never make foo an active invariant — only the invariant importer does that.
//
// Schema (composite-key detected by id + class:OutcomeFeedback):
//
//	id:                     outcome.<slug>
//	class:                  OutcomeFeedback         (discriminator — required)
//	label:                  Human-readable title
//	status:                 active | draft          (optional)
//	decision:               applied | rejected | deferred | reverted (optional)
//	outcome_status:         success | failure | blocked | reverted   (optional)
//	failure_class:          string                  (optional)
//	reason_code:            string                  (optional)
//	observed_at:            ISO date/time literal    (optional)
//	for_task:               task text               (optional)
//	for_finding:            FindingID                (optional)
//	for_workflow_run:       workflow run id          (optional)
//	for_step:               workflow step id         (optional)
//	used_preflight_status:  OK | DEGRADED | EMPTY    (optional)
//	used_risk_class:        risk class               (optional)
//	used_knowledge_nodes:   []class-qualified id     (optional) e.g. invariant:foo.bar,
//	                        failure_mode:baz, implementation_pattern:globular.pattern.x,
//	                        required_test:TestFoo
//	suggests_candidate:     candidate id (inert)     (optional)
//	promoted_from_incident: incident id              (optional)
//	notes:                  free text → rdfs:comment (optional)
//
// Empty id is a soft skip — file parsed, no triples, no error.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type yamlOutcomeFeedback struct {
	ID                   string   `yaml:"id"`
	Class                string   `yaml:"class"`
	Label                string   `yaml:"label"`
	Status               string   `yaml:"status"`
	Decision             string   `yaml:"decision"`
	OutcomeStatus        string   `yaml:"outcome_status"`
	FailureClass         string   `yaml:"failure_class"`
	ReasonCode           string   `yaml:"reason_code"`
	ObservedAt           string   `yaml:"observed_at"`
	ForTask              string   `yaml:"for_task"`
	ForFinding           string   `yaml:"for_finding"`
	ForWorkflowRun       string   `yaml:"for_workflow_run"`
	ForStep              string   `yaml:"for_step"`
	UsedPreflightStatus  string   `yaml:"used_preflight_status"`
	UsedRiskClass        string   `yaml:"used_risk_class"`
	UsedKnowledgeNodes   []string `yaml:"used_knowledge_nodes"`
	SuggestsCandidate    string   `yaml:"suggests_candidate"`
	PromotedFromIncident string   `yaml:"promoted_from_incident"`
	Notes                string   `yaml:"notes"`
}

// importOutcomeFeedback imports a single OutcomeFeedback YAML file.
func importOutcomeFeedback(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var o yamlOutcomeFeedback
	if err := yaml.Unmarshal(data, &o); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if o.ID == "" {
		return nil
	}

	subj := rdf.MintIRI(rdf.ClassOutcomeFeedback, o.ID)
	e.Typed(subj, rdf.ClassOutcomeFeedback)

	// Core + scalar context. Each emitted only when present so a sparse
	// outcome record produces no empty literals.
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(o.Label, o.ID)))
	emitOptLit(e, subj, rdf.PropStatus, o.Status)
	emitOptLit(e, subj, rdf.PropDecision, o.Decision)
	emitOptLit(e, subj, rdf.PropOutcomeStatus, o.OutcomeStatus)
	emitOptLit(e, subj, rdf.PropFailureClass, o.FailureClass)
	emitOptLit(e, subj, rdf.PropReasonCode, o.ReasonCode)
	emitOptLit(e, subj, rdf.PropObservedAt, o.ObservedAt)
	emitOptLit(e, subj, rdf.PropForTask, o.ForTask)
	emitOptLit(e, subj, rdf.PropForFinding, o.ForFinding)
	emitOptLit(e, subj, rdf.PropForWorkflowRun, o.ForWorkflowRun)
	emitOptLit(e, subj, rdf.PropForStep, o.ForStep)
	emitOptLit(e, subj, rdf.PropUsedPreflightStatus, o.UsedPreflightStatus)
	emitOptLit(e, subj, rdf.PropUsedRiskClass, o.UsedRiskClass)
	emitOptLit(e, subj, rdf.PropSuggestsCandidate, o.SuggestsCandidate)
	emitOptLit(e, subj, rdf.PropPromotedFromIncident, o.PromotedFromIncident)
	if notes := strings.TrimSpace(o.Notes); notes != "" {
		e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(notes))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	// Knowledge-node links — object edges to EXISTING authority nodes. We mint
	// the target IRI from its class-qualified id and emit usedKnowledgeNode.
	// We do NOT type the target: linking is not authoring. An unresolvable
	// class prefix is skipped (it cannot be a real graph node).
	for _, ref := range o.UsedKnowledgeNodes {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		iri, ok := knowledgeRefToIRI(ref)
		if !ok {
			continue
		}
		e.Triple(subj, rdf.IRI(rdf.PropUsedKnowledgeNode), iri)
	}

	return nil
}

// emitOptLit emits a single literal triple only when val is non-empty (after
// trimming). Keeps sparse outcome records free of empty-string triples.
func emitOptLit(e *rdf.Emitter, subj, prop, val string) {
	if val = strings.TrimSpace(val); val != "" {
		e.Triple(subj, rdf.IRI(prop), rdf.Lit(val))
	}
}

// knowledgeRefToIRI resolves a class-qualified id (the form used by the
// awareness Query/Resolve API, e.g. "invariant:foo.bar") to the minted node
// IRI. The slug after the first colon is preserved verbatim (it may contain
// dots), matching how the per-class importers mint their nodes. Returns false
// for an unknown class prefix or empty slug. Shared by the outcome-feedback
// and repair-plan importers.
func knowledgeRefToIRI(ref string) (string, bool) {
	colon := strings.IndexByte(ref, ':')
	if colon < 0 {
		return "", false
	}
	class := ref[:colon]
	slug := strings.TrimSpace(ref[colon+1:])
	if slug == "" {
		return "", false
	}
	var classIRI string
	switch class {
	case "invariant":
		classIRI = rdf.ClassInvariant
	case "failure_mode":
		classIRI = rdf.ClassFailureMode
	case "incident_pattern":
		classIRI = rdf.ClassIncidentPattern
	case "intent":
		classIRI = rdf.ClassIntent
	case "forbidden_fix":
		classIRI = rdf.ClassForbiddenFix
	case "implementation_pattern":
		classIRI = rdf.ClassImplementationPattern
	case "authority_domain":
		classIRI = rdf.ClassAuthorityDomain
	case "repair_plan":
		classIRI = rdf.ClassRepairPlan
	case "test", "required_test":
		classIRI = rdf.ClassTest
	case "component":
		classIRI = rdf.ClassComponent
	case "boundary":
		classIRI = rdf.ClassBoundary
	case "contract":
		classIRI = rdf.ClassContract
	case "decision":
		classIRI = rdf.ClassDecision
	case "evidence":
		classIRI = rdf.ClassEvidence
	case "meta_principle":
		// Meta-principles are dual-typed meta.* invariants: the node lives at
		// the invariant IRI, so a meta_principle ref resolves there.
		classIRI = rdf.ClassInvariant
	case "source_file":
		classIRI = rdf.ClassSourceFile
	case "code_symbol":
		classIRI = rdf.ClassCodeSymbol
	case "symbol":
		classIRI = rdf.ClassSymbol
	case "design_pattern":
		classIRI = rdf.ClassDesignPattern
	case "pattern_misuse":
		classIRI = rdf.ClassPatternMisuse
	default:
		return "", false
	}
	return rdf.MintIRI(classIRI, slug), true
}
