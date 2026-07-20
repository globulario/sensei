// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestDefaultRegistry_ValidAndDeterministic(t *testing.T) {
	r := DefaultRegistry()
	if err := r.Validate(); err != nil {
		t.Fatalf("default registry invalid: %v", err)
	}
	d1, err := r.Digest()
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := DefaultRegistry().Digest()
	if d1 != d2 || d1 == "" {
		t.Fatalf("registry digest not deterministic: %q vs %q", d1, d2)
	}
}

func TestRegistry_ExactlyFourAssessableClasses(t *testing.T) {
	r := DefaultRegistry()
	want := map[string]bool{
		rdf.ClassContract: true, rdf.ClassInvariant: true, rdf.ClassComponent: true, rdf.ClassBoundary: true,
	}
	got := map[string]bool{}
	for _, c := range r.Classes {
		if c.Coverage == CoverageAssessable {
			got[c.ClassIRI] = true
			if c.AssessmentPolicyID == "" {
				t.Errorf("assessable class %q has no assessment policy", c.ClassIRI)
			}
		}
	}
	if len(got) != 4 {
		t.Fatalf("assessable classes = %d, want 4", len(got))
	}
	for iri := range want {
		if !got[iri] {
			t.Errorf("expected assessable class %q missing", iri)
		}
	}
}

func TestRegistry_AllDashboardClassesRegistered(t *testing.T) {
	r := DefaultRegistry()
	// Every class the existing dashboard surfaces must be represented so migration hides nothing.
	for _, iri := range []string{
		rdf.ClassInvariant, rdf.ClassFailureMode, rdf.ClassIntent, rdf.ClassIncidentPattern, rdf.ClassForbiddenFix,
		rdf.ClassTest, rdf.ClassSourceFile, rdf.ClassComponent, rdf.ClassBoundary, rdf.ClassContract, rdf.ClassDecision,
		rdf.ClassEvidence, rdf.ClassMetaPrinciple, rdf.ClassDesignPattern, rdf.ClassImplementationPattern, rdf.ClassPatternMisuse,
		rdf.ClassArchitectureClaim, rdf.ClassOpenQuestion, rdf.ClassArchitectAnswer, rdf.ClassEvidenceProbe,
	} {
		if _, ok := r.classByIRI(iri); !ok {
			t.Errorf("dashboard class %q is not registered", iri)
		}
	}
}

func TestRegistry_UnsupportedIsNotNotApplicable(t *testing.T) {
	r := DefaultRegistry()
	// Decision is meaningful but has no reviewed policy → unsupported, NOT explicitly_not_applicable.
	dec, _ := r.classByIRI(rdf.ClassDecision)
	if dec.Coverage != CoverageUnsupported {
		t.Fatalf("Decision coverage = %q, want unsupported", dec.Coverage)
	}
	// A source file explicitly does not get closure → explicitly_not_applicable.
	sf, _ := r.classByIRI(rdf.ClassSourceFile)
	if sf.Coverage != CoverageExplicitlyNotApplicable {
		t.Fatalf("SourceFile coverage = %q, want explicitly_not_applicable", sf.Coverage)
	}
}

func TestRegistry_ValidationRejectsMalformed(t *testing.T) {
	// Two unclassified fallbacks.
	r := DefaultRegistry()
	r.Classes = append(r.Classes, ClassPolicy{ClassIRI: "x", Label: "x", Family: UnclassifiedFamilyID, Order: 99, Coverage: CoverageUnknown, Unclassified: true, ResolveCapable: true, DefaultVisible: true})
	if err := r.Validate(); err == nil {
		t.Fatal("two unclassified fallbacks must be rejected")
	}
	// Assessable without a policy.
	r2 := DefaultRegistry()
	for i := range r2.Classes {
		if r2.Classes[i].ClassIRI == rdf.ClassContract {
			r2.Classes[i].AssessmentPolicyID = ""
		}
	}
	if err := r2.Validate(); err == nil {
		t.Fatal("assessable class without a policy must be rejected")
	}
}
