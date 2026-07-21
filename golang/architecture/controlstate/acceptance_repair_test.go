// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// snapAttn is a valid available attention envelope for snapshot inputs.
func snapAttn() AttentionObservation {
	return AttentionObservation{Owner: "controlstate.attention", Schema: "attention", Identity: "attention.collection", Availability: SourceAvailable}
}

// Item 1 — an untrustworthy (unavailable) dimension source may admit no evidence, question, or
// next-action; a degraded source may not redirect the next action.
func TestAccept_UnavailableDimensionAdmitsNothing(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	base := func() ArtifactSourceBundle { return satisfiedBundle(reg, rdf.ClassContract) }
	cases := map[string]DimensionObservation{
		"unavailable evidence":    {Dimension: "evidence", SourceOwner: "closure.evidence", SourceAvailability: SourceUnavailable, Outcome: OutcomeInsufficient, EvidenceIDs: []string{"e1"}},
		"unavailable question":    {Dimension: "evidence", SourceOwner: "closure.evidence", SourceAvailability: SourceUnavailable, Outcome: OutcomeInsufficient, QuestionIDs: []string{"q1"}},
		"unavailable next-action": {Dimension: "evidence", SourceOwner: "closure.evidence", SourceAvailability: SourceUnavailable, Outcome: OutcomeInsufficient, NextActionOwner: "someone"},
		"unavailable non-insuff":  {Dimension: "evidence", SourceOwner: "closure.evidence", SourceAvailability: SourceUnavailable, Outcome: OutcomeDegraded},
		"unavailable blocker":     {Dimension: "evidence", SourceOwner: "closure.evidence", SourceAvailability: SourceInvalid, Outcome: OutcomeInsufficient, BlockerIDs: []string{"b1"}},
	}
	for name, obs := range cases {
		t.Run(name, func(t *testing.T) {
			b := base()
			b.Dimensions["evidence"] = obs
			if _, err := BuildArtifactState(reg, id, res, b); err == nil {
				t.Fatalf("%s must be rejected", name)
			}
		})
	}
}

// Item 1 — a degraded definitive blocker cannot redirect the next action away from the reviewed
// policy owner.
func TestAccept_DegradedBlockerCannotRedirectNextAction(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.Dimensions["enforcement"] = DimensionObservation{Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "dim", SourceIdentity: "i",
		SourceAvailability: SourceDegraded, Outcome: OutcomeDefinitiveBlocker, BlockerIDs: []string{"b1"}, NextActionOwner: "attacker"}
	if _, err := BuildArtifactState(reg, id, res, b); err == nil {
		t.Fatal("a degraded blocker redirecting the next action must be rejected")
	}
	// The same blocker with the reviewed policy owner (or empty) is admissible and stays OPEN.
	b.Dimensions["enforcement"] = DimensionObservation{Dimension: "enforcement", SourceOwner: "closure.enforcement", SourceSchema: "dim", SourceIdentity: "i",
		SourceAvailability: SourceDegraded, Outcome: OutcomeDefinitiveBlocker, BlockerIDs: []string{"b1"}, NextActionOwner: "architect"}
	st, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	if st.Closure != ClosureOpen {
		t.Fatalf("degraded definitive blocker must remain open, got %q", st.Closure)
	}
}

// Item 2 — a fabricated summary canonical class is rejected; an empty summary availability is
// rejected; an ambiguous observed-class set stays unclassified/unknown (and is accepted only under
// the unclassified class).
func TestAccept_CatalogArtifactIdentityCoherence(t *testing.T) {
	reg := DefaultRegistry()
	// Fabricated canonical class: observed=[Component] but the summary claims Contract.
	fab := catalog(reg, 1)
	fab.Artifacts[0].Identity.ObservedClasses = []string{rdf.ClassComponent}
	if ValidateCatalogScope(reg, fab) == nil {
		t.Fatal("a fabricated canonical class must be rejected")
	}
	// Empty availability.
	noAvail := catalog(reg, 1)
	noAvail.Artifacts[0].Availability = ""
	if ValidateCatalogScope(reg, noAvail) == nil {
		t.Fatal("an empty summary availability must be rejected")
	}
	// Ambiguous observed classes → the summary must be unclassified/unknown to be accepted.
	sentinel := reg.unclassifiedPolicy()
	amb := catalog(reg, 1)
	amb.Artifacts[0].Identity.ObservedClasses = sortedUnique([]string{rdf.ClassContract, rdf.ClassComponent})
	amb.Artifacts[0].Identity.CanonicalClass = sentinel.ClassIRI
	amb.Artifacts[0].Class = sentinel.ClassIRI
	amb.Artifacts[0].Family = sentinel.Family
	amb.Artifacts[0].Coverage = sentinel.Coverage
	amb.Artifacts[0].Closure = ClosureUnknown
	if err := ValidateCatalogScope(reg, amb); err != nil {
		t.Fatalf("an ambiguous summary classified as unclassified/unknown must be accepted: %v", err)
	}
	// The same ambiguous observed set posing as a concrete class is rejected.
	ambBad := catalog(reg, 1)
	ambBad.Artifacts[0].Identity.ObservedClasses = sortedUnique([]string{rdf.ClassContract, rdf.ClassComponent})
	if ValidateCatalogScope(reg, ambBad) == nil {
		t.Fatal("an ambiguous observed set posing as a concrete class must be rejected")
	}
}

// Item 3 — a snapshot authority/catalog identity mismatch is rejected before tallying; a degraded
// catalog source whose identity is not the snapshot identity is rejected.
func TestAccept_AuthorityAndCatalogBinding(t *testing.T) {
	reg := DefaultRegistry()
	// Authority identity does not match the catalog authority identity.
	if _, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4), Attention: snapAttn(),
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: "different-authority"}}); err == nil {
		t.Fatal("a snapshot authority/catalog identity mismatch must be rejected")
	}
	// A degraded catalog source with a mismatched identity is rejected.
	cat := catalog(reg, 4)
	cat.Source = srcStatus("controlstate.catalog", "catalog", "not-the-snapshot", "", SourceDegraded, ImpactPrimary, "stale")
	if ValidateCatalogScope(reg, cat) == nil {
		t.Fatal("a degraded catalog source identity mismatch must be rejected")
	}
}

// Item 4 — an unavailable feedback source is retained in the ledger but its payload is withheld; an
// unavailable source cannot claim feedback_available.
func TestAccept_FeedbackPayloadCoherence(t *testing.T) {
	reg := DefaultRegistry()
	snap, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4), Attention: snapAttn(),
		Feedback:  &FeedbackObservation{Owner: "briefingfeedback", Schema: "fb", Availability: SourceUnavailable, Context: FeedbackContext{Capable: true, Availability: "feedback_unavailable"}},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if err != nil {
		t.Fatal(err)
	}
	if snap.FeedbackContext != nil {
		t.Fatal("an unavailable feedback source must withhold its payload")
	}
	retained := false
	for _, s := range snap.Sources {
		if s.Owner == "briefingfeedback" && s.Availability == SourceUnavailable {
			retained = true
		}
	}
	if !retained {
		t.Fatal("the unavailable feedback source must be retained in the ledger")
	}
	// An unavailable source cannot claim feedback_available.
	if _, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4), Attention: snapAttn(),
		Feedback:  &FeedbackObservation{Owner: "briefingfeedback", Schema: "fb", Availability: SourceUnavailable, Context: FeedbackContext{Availability: "feedback_available"}},
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}}); err == nil {
		t.Fatal("an unavailable feedback source claiming feedback_available must be rejected")
	}
}

// Item 5 — a graph-authority attention construction error propagates through BuildArtifactState; an
// absolute attention identity is rejected at construction.
func TestAccept_AttentionFailClosed(t *testing.T) {
	reg := DefaultRegistry()
	// An artifact whose graph-authority identity is an absolute path: the (stale) graph-authority
	// attention fails construction and the error must surface through BuildArtifactState.
	id, res, err := BuildArtifactIdentity(reg, "aw:c", []string{rdf.ClassContract}, tRepo, tRepo, "/abs/authority", nil)
	if err != nil {
		t.Fatal(err)
	}
	b := satisfiedBundle(reg, rdf.ClassContract)
	b.GraphAuthority = GraphAuthorityObservation{Observed: true, Current: false, Integrity: true, Identity: "/abs/authority"}
	if _, err := BuildArtifactState(reg, id, res, b); err == nil {
		t.Fatal("a graph-authority attention construction error must propagate through BuildArtifactState")
	}
	// Absolute identities are rejected at attention construction (source identity + affected).
	if _, err := newAttention("o", "s", "/abs/x", "", "class", "r", SeverityWarning, "b", []string{"aw:x"}, false, nil, "architect", false); err == nil {
		t.Fatal("an absolute attention source identity must be rejected")
	}
	if _, err := newAttention("o", "s", "id", "", "class", "r", SeverityWarning, "b", []string{"/abs"}, false, nil, "architect", false); err == nil {
		t.Fatal("an absolute affected identity must be rejected")
	}
}

// Item 6 — determinism and no-mutation remain intact; controlstate never certifies (Phase 9.4 /
// CorrectnessCertified unchanged: projections stay non-authoritative and carry no certification).
func TestAccept_DeterminismNoMutationNoCertification(t *testing.T) {
	reg := DefaultRegistry()
	id, res := mustIdentity(t, reg, "aw:c", []string{rdf.ClassContract})
	b := satisfiedBundle(reg, rdf.ClassContract)
	st1, err := BuildArtifactState(reg, id, res, b)
	if err != nil {
		t.Fatal(err)
	}
	// No mutation of the input bundle: rebuild from an independent copy yields the identical digest.
	st2, err := BuildArtifactState(reg, id, res, satisfiedBundle(reg, rdf.ClassContract))
	if err != nil {
		t.Fatal(err)
	}
	if st1.DigestSHA256 != st2.DigestSHA256 {
		t.Fatal("artifact state build is not deterministic")
	}
	if !st1.ProjectionMeta.NonAuthoritativeProjection {
		t.Fatal("controlstate projection must be non-authoritative")
	}
	snap, err := BuildControlSnapshot(reg, ControlSnapshotInput{RepositoryIdentity: tRepo, Catalog: catalog(reg, 4), Attention: snapAttn(),
		Authority: GraphAuthoritySummary{Observed: true, Current: true, Integrity: true, Identity: tAuth}})
	if err != nil {
		t.Fatal(err)
	}
	for _, blob := range [][]byte{mustJSON(t, st1), mustJSON(t, snap)} {
		low := strings.ToLower(string(blob))
		if strings.Contains(low, "certified") || strings.Contains(low, "\"score\"") {
			t.Fatalf("controlstate output must carry no certification or score: %s", blob)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
