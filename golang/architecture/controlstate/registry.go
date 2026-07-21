// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"

	"github.com/globulario/sensei/golang/rdf"
)

// AssessmentCoverage is the CLOSED per-class assessment-coverage vocabulary.
type AssessmentCoverage string

const (
	// CoverageAssessable: a reviewed artifact-assessment policy exists for the class.
	CoverageAssessable AssessmentCoverage = "assessable"
	// CoverageExplicitlyNotApplicable: the class policy states artifact closure does not apply.
	CoverageExplicitlyNotApplicable AssessmentCoverage = "explicitly_not_applicable"
	// CoverageUnsupported: architecturally meaningful, but no reviewed artifact-closure policy
	// exists yet — closure is unknown, never not_applicable.
	CoverageUnsupported AssessmentCoverage = "unsupported"
	// CoverageUnknown: the unclassified fallback / an unknown graph class.
	CoverageUnknown AssessmentCoverage = "unknown"
)

func validCoverage(c AssessmentCoverage) bool {
	switch c {
	case CoverageAssessable, CoverageExplicitlyNotApplicable, CoverageUnsupported, CoverageUnknown:
		return true
	}
	return false
}

// UnclassifiedFamilyID / UnclassifiedClassSentinel identify the mandatory fallback.
const (
	UnclassifiedFamilyID       = "unclassified"
	UnclassifiedClassSentinel  = "controlstate.unclassified"
	NavigationDescriptorSchema = "ontology.navigation_descriptor/v1"
)

// OntologyFamily is a grouped taxonomy family for the left rail.
type OntologyFamily struct {
	ID    string `json:"id" yaml:"id"`
	Label string `json:"label" yaml:"label"`
	Order int    `json:"order" yaml:"order"`
}

// ClassPolicy is the immutable per-class registry policy. It is authored + code-reviewed, never
// inferred from the graph.
type ClassPolicy struct {
	ClassIRI           string             `json:"class_iri" yaml:"class_iri"`
	Label              string             `json:"label" yaml:"label"`
	Family             string             `json:"family" yaml:"family"`
	Order              int                `json:"order" yaml:"order"`
	Coverage           AssessmentCoverage `json:"coverage" yaml:"coverage"`
	Aliases            []string           `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Precedence         int                `json:"precedence" yaml:"precedence"`
	QueryCapable       bool               `json:"query_capable" yaml:"query_capable"`
	ResolveCapable     bool               `json:"resolve_capable" yaml:"resolve_capable"`
	InspectorCapable   bool               `json:"inspector_capable" yaml:"inspector_capable"`
	QuestionCapable    bool               `json:"question_capable" yaml:"question_capable"`
	DefaultVisible     bool               `json:"default_visible" yaml:"default_visible"`
	OverviewVisible    bool               `json:"overview_visible" yaml:"overview_visible"`
	LifecyclePolicyID  string             `json:"lifecycle_policy_id,omitempty" yaml:"lifecycle_policy_id,omitempty"`
	AssessmentPolicyID string             `json:"assessment_policy_id,omitempty" yaml:"assessment_policy_id,omitempty"`
	Unclassified       bool               `json:"unclassified,omitempty" yaml:"unclassified,omitempty"`
}

// Registry is the immutable typed ontology + policy registry. It is the single canonical source
// for both the navigation descriptor and the assessment engine.
type Registry struct {
	Families []OntologyFamily `json:"families" yaml:"families"`
	Classes  []ClassPolicy    `json:"classes" yaml:"classes"`
}

const (
	famKnowledge    = "knowledge"
	famArchitecture = "architecture"
	famRealization  = "realization"
	famPatterns     = "patterns"
	famDialogue     = "dialogue_closure"
)

// DefaultRegistry returns the immutable Phase 9.5 v1 registry. Every ontology class currently
// surfaced by the VS Code dashboard is registered so migration hides nothing; classes outside
// the reviewed set map to the unclassified fallback.
func DefaultRegistry() Registry {
	fam := []OntologyFamily{
		{famKnowledge, "Knowledge", 10},
		{famArchitecture, "Architecture", 20},
		{famRealization, "Realization", 30},
		{famPatterns, "Patterns", 40},
		{famDialogue, "Dialogue & closure", 50},
		{UnclassifiedFamilyID, "Unclassified", 900},
	}
	// helpers for common capability shapes.
	navOnly := func(iri, label, family string, order int, cov AssessmentCoverage) ClassPolicy {
		return ClassPolicy{ClassIRI: iri, Label: label, Family: family, Order: order, Coverage: cov,
			Precedence: 100, QueryCapable: true, ResolveCapable: true, InspectorCapable: true,
			DefaultVisible: true, OverviewVisible: family == famKnowledge || family == famArchitecture}
	}
	assessable := func(iri, label, family string, order, prec int, policyID, lifecycleID string) ClassPolicy {
		return ClassPolicy{ClassIRI: iri, Label: label, Family: family, Order: order,
			Coverage: CoverageAssessable, Precedence: prec, QueryCapable: true, ResolveCapable: true,
			InspectorCapable: true, QuestionCapable: true, DefaultVisible: true, OverviewVisible: true,
			LifecyclePolicyID: lifecycleID, AssessmentPolicyID: policyID}
	}

	classes := []ClassPolicy{
		// Knowledge
		assessable(rdf.ClassInvariant, "Invariants", famKnowledge, 10, 20, "invariant.v1", "governed_status"),
		navOnly(rdf.ClassIntent, "Intents", famKnowledge, 20, CoverageUnsupported),
		navOnly(rdf.ClassFailureMode, "Failure modes", famKnowledge, 30, CoverageUnsupported),
		navOnly(rdf.ClassIncidentPattern, "Incident patterns", famKnowledge, 40, CoverageUnsupported),
		navOnly(rdf.ClassForbiddenFix, "Forbidden fixes", famKnowledge, 50, CoverageUnsupported),
		// Architecture
		assessable(rdf.ClassComponent, "Components", famArchitecture, 10, 30, "component.v1", ""),
		assessable(rdf.ClassBoundary, "Boundaries", famArchitecture, 20, 30, "boundary.v1", ""),
		assessable(rdf.ClassContract, "Contracts", famArchitecture, 30, 20, "contract.v1", "governed_status"),
		navOnly(rdf.ClassDecision, "Decisions", famArchitecture, 40, CoverageUnsupported),
		// Realization — proof/realization artifacts: closure does not apply.
		navOnly(rdf.ClassSourceFile, "Source files", famRealization, 10, CoverageExplicitlyNotApplicable),
		navOnly(rdf.ClassTest, "Tests", famRealization, 20, CoverageExplicitlyNotApplicable),
		navOnly(rdf.ClassEvidence, "Evidence", famRealization, 30, CoverageExplicitlyNotApplicable),
		// Patterns
		metaPrinciplePolicy(),
		navOnly(rdf.ClassDesignPattern, "Design patterns", famPatterns, 20, CoverageUnsupported),
		navOnly(rdf.ClassImplementationPattern, "Implementation patterns", famPatterns, 30, CoverageUnsupported),
		navOnly(rdf.ClassPatternMisuse, "Pattern misuses", famPatterns, 40, CoverageUnsupported),
		// Dialogue & closure
		navOnly(rdf.ClassArchitectureClaim, "Claims", famDialogue, 10, CoverageUnsupported),
		questionClass(),
		navOnly(rdf.ClassArchitectAnswer, "Answers", famDialogue, 30, CoverageUnsupported),
		navOnly(rdf.ClassEvidenceProbe, "Probes", famDialogue, 40, CoverageExplicitlyNotApplicable),
		// Mandatory unclassified fallback (exactly once).
		{ClassIRI: UnclassifiedClassSentinel, Label: "Unclassified", Family: UnclassifiedFamilyID,
			Order: 10, Coverage: CoverageUnknown, Precedence: 1000, QueryCapable: true,
			ResolveCapable: true, InspectorCapable: true, DefaultVisible: true, Unclassified: true},
	}
	return Registry{Families: fam, Classes: classes}
}

func metaPrinciplePolicy() ClassPolicy {
	// A meta.* node is dual-typed as an Invariant; MetaPrinciple is the more specific canonical
	// class (lower precedence wins), and it is compatible with Invariant.
	return ClassPolicy{ClassIRI: rdf.ClassMetaPrinciple, Label: "Meta-principles", Family: famPatterns,
		Order: 10, Coverage: CoverageUnsupported, Aliases: []string{rdf.ClassInvariant}, Precedence: 10,
		QueryCapable: true, ResolveCapable: true, InspectorCapable: true, DefaultVisible: true}
}

func questionClass() ClassPolicy {
	return ClassPolicy{ClassIRI: rdf.ClassOpenQuestion, Label: "Questions", Family: famDialogue, Order: 20,
		Coverage: CoverageUnsupported, Precedence: 100, QueryCapable: true, ResolveCapable: true,
		InspectorCapable: true, QuestionCapable: true, DefaultVisible: true}
}

// Validate enforces the registry structural laws.
func (r Registry) Validate() error {
	if len(r.Families) == 0 || len(r.Classes) == 0 {
		return fmt.Errorf("registry is empty")
	}
	famIDs := map[string]bool{}
	famOrders := map[int]bool{}
	hasUnclassifiedFamily := false
	for _, f := range r.Families {
		if f.ID == "" || f.Label == "" {
			return fmt.Errorf("family missing id/label")
		}
		if famIDs[f.ID] {
			return fmt.Errorf("duplicate family id %q", f.ID)
		}
		if famOrders[f.Order] {
			return fmt.Errorf("duplicate family order %d", f.Order)
		}
		famIDs[f.ID] = true
		famOrders[f.Order] = true
		if f.ID == UnclassifiedFamilyID {
			hasUnclassifiedFamily = true
		}
	}
	if !hasUnclassifiedFamily {
		return fmt.Errorf("registry has no unclassified family")
	}

	classIRIs := map[string]ClassPolicy{}
	orderInFamily := map[string]bool{}
	unclassifiedCount := 0
	for _, c := range r.Classes {
		if c.ClassIRI == "" || c.Label == "" {
			return fmt.Errorf("class missing iri/label")
		}
		if !famIDs[c.Family] {
			return fmt.Errorf("class %q references unknown family %q", c.ClassIRI, c.Family)
		}
		if !validCoverage(c.Coverage) {
			return fmt.Errorf("class %q coverage %q is off-vocabulary", c.ClassIRI, c.Coverage)
		}
		if _, dup := classIRIs[c.ClassIRI]; dup {
			return fmt.Errorf("duplicate class iri %q", c.ClassIRI)
		}
		ordKey := c.Family + "\x00" + fmt.Sprint(c.Order)
		if orderInFamily[ordKey] {
			return fmt.Errorf("duplicate class order %d in family %q", c.Order, c.Family)
		}
		orderInFamily[ordKey] = true
		classIRIs[c.ClassIRI] = c
		// Capability coherence: an assessable class references an assessment policy; an
		// explicitly-not-applicable class must not.
		switch c.Coverage {
		case CoverageAssessable:
			if c.AssessmentPolicyID == "" {
				return fmt.Errorf("assessable class %q has no assessment policy", c.ClassIRI)
			}
		case CoverageExplicitlyNotApplicable:
			if c.AssessmentPolicyID != "" {
				return fmt.Errorf("explicitly-not-applicable class %q references an assessment policy", c.ClassIRI)
			}
		}
		if c.Unclassified {
			unclassifiedCount++
			if c.Coverage != CoverageUnknown {
				return fmt.Errorf("unclassified fallback must have unknown coverage")
			}
		}
		// Capability coherence: a class cannot be inspector/question capable while not resolvable.
		if (c.InspectorCapable || c.QuestionCapable) && !c.ResolveCapable {
			return fmt.Errorf("class %q is inspector/question capable but not resolvable", c.ClassIRI)
		}
	}
	if unclassifiedCount != 1 {
		return fmt.Errorf("registry must have exactly one unclassified fallback, got %d", unclassifiedCount)
	}
	// Aliases must reference known classes (no alias owned by an unknown/incompatible class).
	for _, c := range r.Classes {
		for _, a := range c.Aliases {
			if _, ok := classIRIs[a]; !ok {
				return fmt.Errorf("class %q aliases unknown class %q", c.ClassIRI, a)
			}
			if a == c.ClassIRI {
				return fmt.Errorf("class %q aliases itself", c.ClassIRI)
			}
		}
	}
	// Lifecycle policy id is from the closed vocabulary; unsupported / explicitly-not-applicable
	// classes carry no assessment policy.
	for _, c := range r.Classes {
		switch c.LifecyclePolicyID {
		case "", lifecyclePolicyNotApplicable, "governed_status":
		default:
			return fmt.Errorf("class %q lifecycle policy %q is off-vocabulary", c.ClassIRI, c.LifecyclePolicyID)
		}
		if (c.Coverage == CoverageUnsupported || c.Coverage == CoverageUnknown) && c.AssessmentPolicyID != "" {
			return fmt.Errorf("%s class %q must carry no assessment policy", c.Coverage, c.ClassIRI)
		}
	}

	// Every assessable class references an EXISTING policy bound to that exact class; policies are
	// internally closed (unique dimension ids, non-empty owners, exactly one contradiction dim).
	policies := assessmentPolicies()
	policyIDs := map[string]bool{}
	for _, c := range r.Classes {
		if c.Coverage != CoverageAssessable {
			continue
		}
		ap, ok := policies[c.AssessmentPolicyID]
		if !ok {
			return fmt.Errorf("assessable class %q references unknown assessment policy %q", c.ClassIRI, c.AssessmentPolicyID)
		}
		if ap.ID != c.AssessmentPolicyID {
			return fmt.Errorf("assessment policy id %q disagrees with its key %q", ap.ID, c.AssessmentPolicyID)
		}
		if policyIDs[ap.ID] {
			return fmt.Errorf("duplicate assessment policy id %q", ap.ID)
		}
		policyIDs[ap.ID] = true
		if ap.ClassIRI != c.ClassIRI {
			return fmt.Errorf("assessment policy %q is bound to %q, not %q", ap.ID, ap.ClassIRI, c.ClassIRI)
		}
		if err := validateAssessmentPolicy(ap); err != nil {
			return err
		}
	}
	return nil
}

// validateAssessmentPolicy enforces per-policy closure.
func validateAssessmentPolicy(ap assessmentPolicy) error {
	if len(ap.Dimensions) == 0 {
		return fmt.Errorf("assessment policy %q has no dimensions", ap.ID)
	}
	dimSeen := map[string]bool{}
	contradictions := 0
	for _, dp := range ap.Dimensions {
		if dp.Dimension == "" {
			return fmt.Errorf("assessment policy %q has an unnamed dimension", ap.ID)
		}
		if dimSeen[dp.Dimension] {
			return fmt.Errorf("assessment policy %q has a duplicate dimension %q", ap.ID, dp.Dimension)
		}
		dimSeen[dp.Dimension] = true
		if dp.Owner == "" || dp.NextAction == "" {
			return fmt.Errorf("assessment policy %q dimension %q missing owner/next-action", ap.ID, dp.Dimension)
		}
		if dp.Dimension == "contradiction" {
			contradictions++
		}
	}
	if contradictions != 1 {
		return fmt.Errorf("assessment policy %q must have exactly one contradiction dimension, got %d", ap.ID, contradictions)
	}
	return nil
}

// Digest is the deterministic self-describing registry digest.
func (r Registry) Digest() (string, error) { return digestOf(r) }

// classByIRI returns the policy for an exact class IRI.
func (r Registry) classByIRI(iri string) (ClassPolicy, bool) {
	for _, c := range r.Classes {
		if c.ClassIRI == iri {
			return c, true
		}
	}
	return ClassPolicy{}, false
}

// unclassifiedPolicy returns the fallback policy.
func (r Registry) unclassifiedPolicy() ClassPolicy {
	for _, c := range r.Classes {
		if c.Unclassified {
			return c
		}
	}
	return ClassPolicy{}
}

// familyByID returns a family.
func (r Registry) familyByID(id string) (OntologyFamily, bool) {
	for _, f := range r.Families {
		if f.ID == id {
			return f, true
		}
	}
	return OntologyFamily{}, false
}
