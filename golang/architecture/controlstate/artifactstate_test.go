// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

const (
	tRepo = "github.com/globulario/sensei"
	tAuth = "auth-digest-abc"
)

func mustIdentity(t *testing.T, reg Registry, iri string, classes []string) (ArtifactIdentity, ClassResolution) {
	t.Helper()
	id, res, err := BuildArtifactIdentity(reg, iri, classes, tRepo, tRepo, tAuth, nil)
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	return id, res
}

// dimSatisfied builds a valid satisfied source-bound observation for a policy dimension.
func dimSatisfied(dp dimensionPolicy) DimensionObservation {
	return DimensionObservation{Dimension: dp.Dimension, SourceOwner: dp.Owner, SourceSchema: "dim", SourceIdentity: "s." + dp.Dimension, SourceAvailability: SourceAvailable, Outcome: OutcomeSatisfied}
}

// satisfiedBundle: every non-contradiction dimension satisfied, current authority, available
// contradiction source with no relevant findings.
func satisfiedBundle(reg Registry, class string) ArtifactSourceBundle {
	p, _ := reg.classByIRI(class)
	ap := assessmentPolicies()[p.AssessmentPolicyID]
	dims := map[string]DimensionObservation{}
	for _, dp := range ap.Dimensions {
		if dp.Dimension == "contradiction" {
			continue
		}
		dims[dp.Dimension] = dimSatisfied(dp)
	}
	return ArtifactSourceBundle{
		GraphAuthority: GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: tAuth},
		Contradiction:  ContradictionSource{Owner: "extractor.contradiction", Schema: "contradiction", Identity: "contra.src", Availability: SourceAvailable},
		Dimensions:     dims,
		// A governed lifecycle source (for classes with a lifecycle policy) so the projection is
		// fully available; classes without a lifecycle policy ignore it.
		Lifecycle: LifecycleSource{Observed: true, Availability: SourceAvailable, Owner: "governed", Schema: "governed_status", Identity: "gs", Status: "governed"},
	}
}

func TestArtifactState_AssessableClosesWhenAllSatisfied(t *testing.T) {
	reg := DefaultRegistry()
	for _, cls := range []string{rdf.ClassContract, rdf.ClassInvariant, rdf.ClassComponent, rdf.ClassBoundary} {
		id, res := mustIdentity(t, reg, "aw:x/"+cls, []string{cls})
		st, err := BuildArtifactState(reg, id, res, satisfiedBundle(reg, cls))
		if err != nil {
			t.Fatalf("%s: %v", cls, err)
		}
		if st.Closure != ClosureClosed {
			t.Fatalf("%s: closure = %q, want closed (reason %q)", cls, st.Closure, st.ClosureReason)
		}
		if st.Availability != AvailabilityAvailable {
			t.Fatalf("%s: availability = %q, want available", cls, st.Availability)
		}
	}
}

func TestArtifactState_OpenBlockerDominatesDegraded(t *testing.T) {
	reg := DefaultRegistry()
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Dimensions["evidence"] = DimensionObservation{Dimension: "evidence", SourceOwner: "closure.evidence", SourceSchema: "dim", SourceIdentity: "s.ev", SourceAvailability: SourceDegraded, Outcome: OutcomeDegraded}
	b.Dimensions["enforcement"] = DimensionObservation{Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "dim", SourceIdentity: "s.en", SourceAvailability: SourceAvailable, Outcome: OutcomeDefinitiveBlocker, BlockerIDs: []string{"blk.1"}}
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	st, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	if st.Closure != ClosureOpen {
		t.Fatalf("open blocker must dominate degraded, got %q", st.Closure)
	}
}

func TestArtifactState_StaleAuthorityCannotClose(t *testing.T) {
	reg := DefaultRegistry()
	b := satisfiedBundle(reg, rdf.ClassInvariant)
	b.GraphAuthority.Current = false
	id, res := mustIdentity(t, reg, "aw:i", []string{rdf.ClassInvariant})
	st, _ := BuildArtifactState(reg, id, res, b)
	if st.Closure == ClosureClosed {
		t.Fatal("stale graph authority must not yield closed")
	}
	if st.Closure != ClosureUnknown {
		t.Fatalf("stale authority → unknown, got %q", st.Closure)
	}
}

func TestArtifactState_MissingSourceIsUnknownNotClosed(t *testing.T) {
	reg := DefaultRegistry()
	b := satisfiedBundle(reg, rdf.ClassComponent)
	delete(b.Dimensions, "ownership")
	id, res := mustIdentity(t, reg, "aw:comp", []string{rdf.ClassComponent})
	st, _ := BuildArtifactState(reg, id, res, b)
	if st.Closure != ClosureUnknown || st.ClosureReason == "" {
		t.Fatalf("missing source → unknown with reason, got %q/%q", st.Closure, st.ClosureReason)
	}
	// The missing dimension contributes an unavailable required source → projection partial.
	if st.Availability != AvailabilityPartial {
		t.Fatalf("missing required source → partial, got %q", st.Availability)
	}
}

func TestArtifactState_UnsupportedIsUnknownNotNotApplicable(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:d", []string{rdf.ClassDecision})
	st, _ := BuildArtifactState(reg, id, res, ArtifactSourceBundle{GraphAuthority: GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if st.Closure != ClosureUnknown {
		t.Fatalf("unsupported class → unknown, got %q", st.Closure)
	}
}

func TestArtifactState_ExplicitlyNotApplicable(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:f.go", []string{rdf.ClassSourceFile})
	st, _ := BuildArtifactState(reg, id, res, ArtifactSourceBundle{GraphAuthority: GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if st.Closure != ClosureNotApplicable {
		t.Fatalf("explicitly-not-applicable class → not_applicable, got %q", st.Closure)
	}
}

func TestArtifactState_UnknownAndAmbiguousClassesVisibleUnknown(t *testing.T) {
	reg := DefaultRegistry()
	uid, ures := mustIdentity(t, reg, "aw:mystery", []string{"https://globular.io/awareness#Mystery"})
	if ures.CanonicalClass != UnclassifiedClassSentinel || !ures.Unknown {
		t.Fatalf("unknown class must resolve to unclassified/unknown: %+v", ures)
	}
	ust, _ := BuildArtifactState(reg, uid, ures, ArtifactSourceBundle{GraphAuthority: GraphAuthorityObservation{Identity: tAuth}})
	if ust.Closure != ClosureUnknown || ust.Coverage != CoverageUnknown {
		t.Fatalf("unknown class must be visible + unknown, got %q/%q", ust.Closure, ust.Coverage)
	}
	aid, ares := mustIdentity(t, reg, "aw:amb", []string{rdf.ClassContract, rdf.ClassComponent})
	if !ares.Ambiguous || ares.ReasonCode != "artifact_class_ambiguous" {
		t.Fatalf("incompatible multi-typed must be ambiguous: %+v", ares)
	}
	ast, _ := BuildArtifactState(reg, aid, ares, ArtifactSourceBundle{GraphAuthority: GraphAuthorityObservation{Identity: tAuth}})
	if ast.Closure != ClosureUnknown {
		t.Fatalf("ambiguous artifact must be unknown, got %q", ast.Closure)
	}
}

func TestResolveCanonicalClass_CompatibleMostSpecificWins(t *testing.T) {
	reg := DefaultRegistry()
	res := reg.ResolveCanonicalClass([]string{rdf.ClassInvariant, rdf.ClassMetaPrinciple})
	if res.CanonicalClass != rdf.ClassMetaPrinciple || res.Ambiguous || res.Unknown {
		t.Fatalf("compatible dual-type must resolve to meta-principle: %+v", res)
	}
}

func TestArtifactState_LifecycleNeverDefaultsActive(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:comp2", []string{rdf.ClassComponent})
	st, _ := BuildArtifactState(reg, id, res, satisfiedBundle(reg, rdf.ClassComponent))
	if st.Lifecycle.State == LifecycleActive {
		t.Fatal("absent lifecycle must not synthesize active")
	}
	if st.Lifecycle.State != LifecycleUnknown {
		t.Fatalf("component lifecycle → unknown, got %q", st.Lifecycle.State)
	}
	b := satisfiedBundle(reg, rdf.ClassInvariant)
	b.Lifecycle = LifecycleSource{Observed: true, Availability: SourceAvailable, Owner: "governed", Schema: "governed_status", Identity: "gs.1", Status: "superseded"}
	iid, ires := mustIdentity(t, reg, "aw:i2", []string{rdf.ClassInvariant})
	ist, _ := BuildArtifactState(reg, iid, ires, b)
	if ist.Lifecycle.State != LifecycleSuperseded {
		t.Fatalf("observed superseded status → superseded, got %q", ist.Lifecycle.State)
	}
	// An ownerless available status cannot label a Contract active.
	b2 := satisfiedBundle(reg, rdf.ClassContract)
	b2.Lifecycle = LifecycleSource{Observed: true, Availability: SourceAvailable, Status: "governed"} // no owner/schema/identity
	cid, cres := mustIdentity(t, reg, "aw:c9", []string{rdf.ClassContract})
	cst, _ := BuildArtifactState(reg, cid, cres, b2)
	if cst.Lifecycle.State == LifecycleActive {
		t.Fatal("ownerless status must not label a Contract active")
	}
}
