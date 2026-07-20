// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// Item 1 — an anonymous available/degraded SourceStatus is rejected; a degraded source without a
// typed reason is rejected; an available source with an identity and no reason is accepted.
func TestClosure_AnonymousSourceRejected(t *testing.T) {
	if validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Availability: SourceAvailable, Impact: ImpactPrimary}) == nil {
		t.Fatal("available source without an identity must be rejected")
	}
	if validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Identity: "i", Availability: SourceDegraded, Impact: ImpactRelevant}) == nil {
		t.Fatal("degraded source without a typed reason must be rejected")
	}
	if validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Availability: SourceDegraded, Impact: ImpactRelevant, ReasonCode: "stale"}) == nil {
		t.Fatal("degraded source without an identity must be rejected")
	}
	if validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Availability: SourceUnavailable, Impact: ImpactRelevant, ReasonCode: "gone"}) != nil {
		t.Fatal("unavailable source may omit an identity when it carries a typed reason")
	}
	if err := validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Identity: "i", Availability: SourceAvailable, Impact: ImpactPrimary}); err != nil {
		t.Fatalf("available source with identity and no reason must be accepted: %v", err)
	}
	// An available source that carries a failure reason is rejected.
	if validateSourceStatus(SourceStatus{Owner: "o", Schema: "s", Identity: "i", Availability: SourceAvailable, Impact: ImpactPrimary, ReasonCode: "stale"}) == nil {
		t.Fatal("available source carrying a failure reason must be rejected")
	}
}

// Item 2 — Current/Integrity cannot be asserted while unobserved (artifact bundle + snapshot).
func TestClosure_UnobservedAuthorityRejected(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.GraphAuthority = GraphAuthorityObservation{Observed: false, Current: true, Identity: tAuth}
	if _, err := BuildArtifactState(reg, id, res, b); err == nil {
		t.Fatal("artifact: current-while-unobserved authority must be rejected")
	}
	snapBad := ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		Authority: GraphAuthoritySummary{Observed: false, Integrity: true}}
	if _, err := BuildControlSnapshot(reg, snapBad); err == nil {
		t.Fatal("snapshot: integrity-while-unobserved authority must be rejected")
	}
}

// Item 2 — an observed authority missing its identity is rejected.
func TestClosure_AuthorityIdentityOmission(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.GraphAuthority = GraphAuthorityObservation{Observed: true, Current: true, Integrity: true, Identity: ""}
	if _, err := BuildArtifactState(reg, id, res, b); err == nil {
		t.Fatal("observed authority without an identity must be rejected")
	}
}

// Item 3 — a contradiction source whose owner is not the policy owner is rejected (no fallback).
func TestClosure_ContradictionOwnerMismatch(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Contradiction = ContradictionSource{Owner: "not.the.policy.owner", Schema: "c", Identity: "s", Availability: SourceAvailable}
	if _, err := BuildArtifactState(reg, id, res, b); err == nil {
		t.Fatal("contradiction owner not matching the policy owner must be rejected")
	}
}

// Item 3 — a degraded contradiction source carrying a relevant finding stays OPEN while the
// projection is PARTIAL (a degraded relevant contradiction is a definitive blocker).
func TestClosure_DegradedRelevantContradictionOpenPartial(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Contradiction = ContradictionSource{Owner: "extractor.contradiction", Schema: "c", Identity: "s", Availability: SourceDegraded, ReasonCode: "stale",
		Findings: []ContradictionObservation{{Identity: "contra.1", Relevant: true}}}
	st, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	if st.Closure != ClosureOpen {
		t.Fatalf("degraded relevant contradiction must remain open, got %q", st.Closure)
	}
	if st.Availability != AvailabilityPartial {
		t.Fatalf("degraded contradiction source must make the projection partial, got %q", st.Availability)
	}
}

// Item 4 — registry policy closure: duplicate policy id, unknown lifecycle policy, and per-policy
// closure (duplicate dimensions, missing owner/next-action, exactly one contradiction dimension).
func TestClosure_RegistryPolicyClosure(t *testing.T) {
	dupPolicy := DefaultRegistry()
	for i := range dupPolicy.Classes {
		if dupPolicy.Classes[i].ClassIRI == rdf.ClassBoundary {
			dupPolicy.Classes[i].AssessmentPolicyID = "contract.v1" // already bound to Contract
		}
	}
	if dupPolicy.Validate() == nil {
		t.Fatal("two classes sharing one assessment policy id must be rejected")
	}
	badLifecycle := DefaultRegistry()
	for i := range badLifecycle.Classes {
		if badLifecycle.Classes[i].ClassIRI == rdf.ClassContract {
			badLifecycle.Classes[i].LifecyclePolicyID = "not_a_real_policy"
		}
	}
	if badLifecycle.Validate() == nil {
		t.Fatal("an off-vocabulary lifecycle policy id must be rejected")
	}
	// Per-policy closure (validated directly on synthetic policies).
	dup := assessmentPolicy{ID: "p", ClassIRI: "c", Dimensions: []dimensionPolicy{
		{Dimension: "contradiction", Owner: "o", NextAction: "a"},
		{Dimension: "evidence", Owner: "o", NextAction: "a"},
		{Dimension: "evidence", Owner: "o", NextAction: "a"},
	}}
	if validateAssessmentPolicy(dup) == nil {
		t.Fatal("a policy with a duplicate dimension must be rejected")
	}
	noContra := assessmentPolicy{ID: "p", ClassIRI: "c", Dimensions: []dimensionPolicy{{Dimension: "evidence", Owner: "o", NextAction: "a"}}}
	if validateAssessmentPolicy(noContra) == nil {
		t.Fatal("a policy without a contradiction dimension must be rejected")
	}
	noOwner := assessmentPolicy{ID: "p", ClassIRI: "c", Dimensions: []dimensionPolicy{{Dimension: "contradiction", NextAction: "a"}}}
	if validateAssessmentPolicy(noOwner) == nil {
		t.Fatal("a dimension without an owner must be rejected")
	}
}

// Item 5 — a catalog source whose identity is not the snapshot identity is rejected.
func TestClosure_CatalogSourceIdentityMismatch(t *testing.T) {
	reg := DefaultRegistry()
	cat := catalog(reg, 4)
	cat.Source.Identity = "not-the-snapshot"
	if ValidateCatalogScope(reg, cat) == nil {
		t.Fatal("catalog source identity not equal to the snapshot identity must be rejected")
	}
}

// Item 5 — a degraded/unavailable catalog is never trusted to expose or tally rows.
func TestClosure_UnavailableCatalogRowsNotTallied(t *testing.T) {
	reg := DefaultRegistry()
	cat := catalog(reg, 4)
	cat.Source = srcStatus("controlstate.catalog", "catalog", "snap-1", "", SourceDegraded, ImpactPrimary, "stale")
	// Index: an empty page, no trusted rows.
	idx, err := BuildArtifactIndex(reg, indexReq(10), cat)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Page) != 0 {
		t.Fatalf("degraded catalog must expose no rows, got %d", len(idx.Page))
	}
	// Snapshot: no tallies, partial availability.
	snap, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: cat,
		Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.CountsByClass != nil || snap.ClosureCounts != nil || snap.LifecycleUnknown != nil {
		t.Fatal("degraded catalog must not be tallied")
	}
	if snap.Availability == AvailabilityAvailable {
		t.Fatalf("degraded primary catalog must degrade snapshot availability, got %q", snap.Availability)
	}
}

// Item 6 — a supplied but unavailable optional (task/completion) source is retained in the ledger
// even though its payload is withheld.
func TestClosure_UnavailableOptionalSourceRetained(t *testing.T) {
	reg := DefaultRegistry()
	snap, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention:  AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable},
		Completion: &CompletionObservation{Owner: "certification", Schema: "completion", Availability: SourceUnavailable},
		Authority:  GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Completion != nil {
		t.Fatal("an unavailable completion source must withhold its payload")
	}
	found := false
	for _, s := range snap.Sources {
		if s.Owner == "certification" && s.Availability == SourceUnavailable {
			found = true
		}
	}
	if !found {
		t.Fatal("the unavailable completion source must be retained in the ledger")
	}
}

// Item 6/7 — a malformed attention item after the truncation cap (index 50) still rejects the
// snapshot (validated before dedup/truncation, never silently dropped).
func TestClosure_InvalidAttentionAfter50Rejected(t *testing.T) {
	reg := DefaultRegistry()
	var items []AttentionItem
	for i := 0; i < 50; i++ {
		a, err := AttentionForOpenQuestion("q."+string(rune('a'+i%26))+string(rune('a'+i/26)), []string{"aw:x"})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, a)
	}
	items = append(items, AttentionItem{}) // index 50: malformed
	_, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4),
		Attention: AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable, Items: items},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if err == nil {
		t.Fatal("a malformed attention item after the cap must reject the snapshot")
	}
}

// Item 7 — the attention builder is error-returning and fails closed: a relevant contradiction
// finding with no identity fails construction rather than being silently omitted. BuildArtifactState
// wires this error through (see line 202: `return ArtifactState{}, fmt.Errorf("attention ...")`).
func TestClosure_AttentionBuilderFailsClosed(t *testing.T) {
	reg := DefaultRegistry()
	id, _ := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	policy, _ := reg.classByIRI(rdf.ClassContract)
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Contradiction = ContradictionSource{Owner: "extractor.contradiction", Schema: "c", Identity: "s", Availability: SourceAvailable,
		Findings: []ContradictionObservation{{Identity: "", Relevant: true}}}
	if _, err := buildArtifactAttention(id, policy, b); err == nil {
		t.Fatal("a relevant contradiction finding without an identity must fail attention construction")
	}
	// A well-formed bundle builds attention without error (positive control).
	if _, err := buildArtifactAttention(id, policy, satisfiedBundle(reg, rdf.ClassContract)); err != nil {
		t.Fatalf("a well-formed bundle must build attention: %v", err)
	}
}

// Item 8 — colon-bearing identities are accepted; Unix/Windows-drive/UNC absolute forms rejected.
func TestClosure_IdentityColonFormsAcceptedAbsoluteRejected(t *testing.T) {
	for _, id := range []string{"aw:x", "contract:example", "invariant:example", "questiondisposition:q.1"} {
		if isAbsoluteIdentity(id) {
			t.Fatalf("colon-bearing identity %q must be accepted", id)
		}
	}
	for _, id := range []string{"/etc/passwd", `\abs`, `\\host\share\x`, `C:\Users\x`, `C:/Users/x`, `d:/x`} {
		if !isAbsoluteIdentity(id) {
			t.Fatalf("absolute identity %q must be rejected", id)
		}
	}
}
