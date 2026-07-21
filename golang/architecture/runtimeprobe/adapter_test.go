// SPDX-License-Identifier: AGPL-3.0-only

package runtimeprobe

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	rb "github.com/globulario/sensei/golang/architecture/runtimeboundary"
	"github.com/globulario/sensei/golang/rdf"
)

const (
	tBoundaryIRI = rdf.AwNS + "boundary/boundary.seed_owner_path"
	tRepo        = "github.com/globulario/sensei"
	tEvidenceID  = "evidence:seedmeta_owner_path"
	tOwnerSvc    = "component.awareness_graph_service"
	tObsPath     = "owner:///seedmeta/graph-authority"
)

func tInput() ProbeObservationInput {
	return ProbeObservationInput{
		ResultID: "probe-result-1", ProbeID: "probe-1", ExecutedBy: "sensei static-probe-executor/v1",
		ObservedAt: "2026-07-21T00:00:00Z", EvidenceID: tEvidenceID, EvidenceStatus: "pass",
		EvidenceFreshness: "current", ObservationSource: tObsPath, OwnerService: tOwnerSvc,
		ResultStatus: "completed",
		Artifacts:    []ArtifactDigest{{Path: "docs/x.yaml", SHA256: "aaa", Size: 10}},
	}
}

func mustReceipt(t *testing.T, in ProbeObservationInput) closureprotocol.EvidenceReceipt {
	t.Helper()
	r, err := ToEvidenceReceipt(in)
	if err != nil {
		t.Fatalf("ToEvidenceReceipt: %v", err)
	}
	return r
}

func mustObs(t *testing.T, in ProbeObservationInput) rb.RuntimeObservation {
	t.Helper()
	o, err := ToRuntimeObservation(in, mustReceipt(t, in))
	if err != nil {
		t.Fatalf("ToRuntimeObservation: %v", err)
	}
	return o
}

// Proof: the honest mapping never fabricates caller/callee/contract.
func TestHonestMapping_NeverInventsCrossingIdentity(t *testing.T) {
	o := mustObs(t, tInput())
	if o.CallerIdentity != "" {
		t.Fatalf("a probe has no governed caller; mapper must leave it empty, got %q", o.CallerIdentity)
	}
	// The endpoint is the evidence anchor, NOT a contract identity.
	if o.EndpointOrContractIdentity != tEvidenceID {
		t.Fatalf("endpoint must be the evidence anchor, got %q", o.EndpointOrContractIdentity)
	}
	if o.InteractionKind != rb.InteractionRead || o.Direction != rb.DirectionUnknown {
		t.Fatalf("probe read must map to read/unknown-direction, got %s/%s", o.InteractionKind, o.Direction)
	}
	// It IS a well-formed observation (missing caller is honest absence, not malformed).
	if err := rb.ValidateObservation(o); err != nil {
		t.Fatalf("observation must be well-formed: %v", err)
	}
}

// Proof: a real probe observation, composed through the frozen owner, is ADMITTED and correctly ruled
// INSUFFICIENT for a crossing — unknown/required_evidence_absent, never satisfied.
func TestInsufficiency_ProbeNeverSatisfiesCrossing(t *testing.T) {
	id, res, err := rb.BuildRuntimeBoundaryIdentity(tBoundaryIRI, []string{rdf.ClassBoundary}, "domain",
		tRepo, tRepo, "graph-authority-abc", "reg-1", []string{"contract.seed_owner_path"}, nil,
		"component.seedmeta", tOwnerSvc, "authority.seed", rb.LifecycleActive, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	pol, err := rb.BuildBoundaryPolicy(rb.BoundaryPolicy{
		PolicyID: "pol-1", BoundaryIRI: tBoundaryIRI, RuntimeProof: rb.ProofRequired, NextActionOwner: "architect",
	})
	if err != nil {
		t.Fatal(err)
	}
	bind, err := rb.BuildRuntimeArchitectureBinding(rb.RuntimeArchitectureBinding{
		BindingID: "bind-1", BoundaryIRI: tBoundaryIRI, RepositoryIdentity: tRepo,
		AuthorityGrantIdentity: "grant.seed-authority",
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := rb.AssessRuntimeBoundary(rb.AssessmentInput{
		Identity: id, IdentityResolution: res, Policy: &pol, Binding: &bind,
		Observations: []rb.RuntimeObservation{mustObs(t, tInput())}, CollectorAvailable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Verdict == rb.VerdictSatisfied || a.Verdict == rb.VerdictViolated {
		t.Fatalf("evidence-level probe must never satisfy/violate a crossing, got %s", a.Verdict)
	}
	if a.ResultKind != rb.KindRequiredEvidenceAbsent {
		t.Fatalf("want required_evidence_absent (no crossing evidence), got %s", a.ResultKind)
	}
	found := false
	for _, r := range a.RefusalReasons {
		if r == "ambiguous_identity" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the probe observation must be refused as ambiguous_identity, got %v", a.RefusalReasons)
	}
}

// Proof: the evidence receipt is immutable/content-addressed — a different payload is a different receipt.
func TestReceipt_ContentAddressed(t *testing.T) {
	r1 := mustReceipt(t, tInput())
	in2 := tInput()
	in2.Artifacts = []ArtifactDigest{{Path: "docs/x.yaml", SHA256: "bbb", Size: 10}}
	r2 := mustReceipt(t, in2)
	if r1.PayloadDigestSHA256 == r2.PayloadDigestSHA256 {
		t.Fatal("different read content must yield different payload digests")
	}
	if r1.EvidenceKind != closureprotocol.EvidenceRuntime {
		t.Fatalf("receipt must be kind=runtime, got %s", r1.EvidenceKind)
	}
	if err := closureprotocol.ValidateEvidenceReceipt(r1); err != nil {
		t.Fatalf("receipt must validate: %v", err)
	}
}

// Proof: replay is idempotent — the same probe result yields the same receipt → REPLAYED, no new event.
func TestIngest_ReplayIdempotent(t *testing.T) {
	r := mustReceipt(t, tInput())
	got, err := Ingest([]closureprotocol.EvidenceReceipt{r}, r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeReplayed {
		t.Fatalf("identical receipt must REPLAY, got %s", got.Outcome)
	}
}

// Proof: conflicting evidence is preserved and never first-row-wins — same owner-path subject,
// different payload → CONTESTED.
func TestIngest_ConflictContested(t *testing.T) {
	r1 := mustReceipt(t, tInput())
	in2 := tInput()
	in2.ResultID = "probe-result-2"                                                  // different observation
	in2.Artifacts = []ArtifactDigest{{Path: "docs/x.yaml", SHA256: "ccc", Size: 10}} // same subject, different payload
	r2 := mustReceipt(t, in2)
	got, err := Ingest([]closureprotocol.EvidenceReceipt{r1}, r2)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeContested {
		t.Fatalf("same subject / different payload must be CONTESTED, got %s", got.Outcome)
	}
	if len(got.ConflictingReceiptIDs) != 1 || got.ConflictingReceiptIDs[0] != r1.ReceiptID {
		t.Fatalf("the contested prior receipt must be preserved, got %v", got.ConflictingReceiptIDs)
	}
}

// Proof: a stale / unavailable / truncated probe maps honestly — never fresh/available.
func TestHonestMapping_StaleUnavailableTruncated(t *testing.T) {
	stale := tInput()
	stale.EvidenceFreshness = "stale"
	if o := mustObs(t, stale); o.Freshness != rb.FreshnessStale {
		t.Fatalf("stale probe must map to stale freshness, got %s", o.Freshness)
	}
	un := tInput()
	un.ResultStatus = "unavailable"
	o, err := ToRuntimeObservation(un, mustReceipt(t, un))
	if err != nil {
		t.Fatal(err)
	}
	if o.Availability != rb.SourceUnavailable || o.ReasonCode == "" {
		t.Fatalf("unavailable probe must map to unavailable + reason, got %s/%q", o.Availability, o.ReasonCode)
	}
	tr := tInput()
	tr.BudgetExhausted = true
	if o := mustObs(t, tr); !o.Truncated {
		t.Fatal("budget-exhausted probe must map to truncated")
	}
	// integrity is false for an incomplete probe
	inc := tInput()
	inc.ResultStatus = "inconclusive"
	o2, err := ToRuntimeObservation(inc, mustReceipt(t, inc))
	if err != nil {
		t.Fatal(err)
	}
	if o2.IntegrityVerified {
		t.Fatal("an incomplete probe must not claim integrity")
	}
}

// Proof: deterministic — artifact input order does not change the receipt payload digest.
func TestDeterminism_ArtifactOrder(t *testing.T) {
	in := tInput()
	in.Artifacts = []ArtifactDigest{{Path: "b", SHA256: "2", Size: 1}, {Path: "a", SHA256: "1", Size: 1}}
	rev := tInput()
	rev.Artifacts = []ArtifactDigest{{Path: "a", SHA256: "1", Size: 1}, {Path: "b", SHA256: "2", Size: 1}}
	if mustReceipt(t, in).PayloadDigestSHA256 != mustReceipt(t, rev).PayloadDigestSHA256 {
		t.Fatal("payload digest must not depend on artifact input order")
	}
}
