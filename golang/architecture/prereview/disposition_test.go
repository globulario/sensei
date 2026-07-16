// SPDX-License-Identifier: Apache-2.0

package prereview

import "testing"

func TestDispositionCannotClaimCertifiedWithoutReceipt(t *testing.T) {
	d := governedDraft()
	d.Coverage.Level = CoverageProofBound
	d.Proof.Certification = &CertificationView{Verdict: "certified"} // no receipt digest

	if got := DeriveDisposition(d); got == DispositionCertified {
		t.Fatal("derived certified without a certification receipt")
	}
	if _, err := Finalize(d); err == nil {
		t.Fatal("finalized a certified claim without a receipt")
	}

	// With a receipt at proof_bound coverage, certified is legitimate.
	d.Proof.Certification.ReceiptDigestSHA256 = "cert00000000000000000000000000000000000000000000000000000000eeee"
	r := mustFinalize(t, d)
	if r.Disposition != DispositionCertified {
		t.Fatalf("disposition = %q, want certified", r.Disposition)
	}
}

func TestDispositionCannotClaimTerminalWithoutCompletion(t *testing.T) {
	d := governedDraft()
	d.Coverage.Level = CoverageTerminal
	d.Proof.Certification = &CertificationView{Verdict: "certified", ReceiptDigestSHA256: "cert00000000000000000000000000000000000000000000000000000000eeee"}
	d.Result = ResultArchitectureSummary{
		Available:               true,
		BaseGraphDigestSHA256:   "base0000000000000000000000000000000000000000000000000000000ffff",
		ResultGraphDigestSHA256: "res00000000000000000000000000000000000000000000000000000000f1f1",
		// No completion receipt.
	}
	if got := DeriveDisposition(d); got == DispositionTerminallyClosed {
		t.Fatal("derived terminally_closed without a completion receipt")
	}
	r := mustFinalize(t, d)
	if r.Disposition != DispositionCertified {
		t.Fatalf("disposition = %q, want certified (not terminal without completion)", r.Disposition)
	}

	// With a completion receipt, terminal closure is legitimate.
	d.Result.Completion = &CompletionView{ReceiptDigestSHA256: "done0000000000000000000000000000000000000000000000000000000f2f2"}
	r = mustFinalize(t, d)
	if r.Disposition != DispositionTerminallyClosed {
		t.Fatalf("disposition = %q, want terminally_closed", r.Disposition)
	}
}

func TestScopeVerifiedDoesNotImplyCorrectness(t *testing.T) {
	// A governed change whose scope is verified but with no certification must
	// never read as certified or terminally closed.
	r := mustFinalize(t, governedDraft())
	if r.Governance.ScopeStatus != "scope_verified" {
		t.Fatalf("precondition: scope status = %q", r.Governance.ScopeStatus)
	}
	if r.Disposition == DispositionCertified || r.Disposition == DispositionTerminallyClosed {
		t.Fatalf("scope verification implied correctness: disposition = %q", r.Disposition)
	}
	if r.Disposition != DispositionReadyForHumanReview {
		t.Fatalf("disposition = %q, want ready_for_human_review", r.Disposition)
	}
}

func TestMissingEvidenceCannotPass(t *testing.T) {
	d := baseDraft()
	d.Protection.Invariants = []ProtectionItem{
		{ID: "inv.owner", Title: "Single owner", Severity: SeverityHigh, Status: "pass", Epistemic: EpistemicGoverned},
	}
	if _, err := Finalize(d); err == nil {
		t.Fatal("finalized a pass status without evidence")
	}
	d.Protection.Invariants[0].EvidenceRefs = []string{"receipt:owner"}
	if _, err := Finalize(d); err != nil {
		t.Fatalf("evidence-backed pass rejected: %v", err)
	}
}

func TestDispositionPriorityBlockerBeatsPositive(t *testing.T) {
	// A scope violation must dominate even a receipt-backed certification.
	d := governedDraft()
	d.Coverage.Level = CoverageProofBound
	d.Change.OutOfEnvelopeChanges = []string{"secret.go"}
	d.Proof.Certification = &CertificationView{Verdict: "certified", ReceiptDigestSHA256: "cert00000000000000000000000000000000000000000000000000000000eeee"}
	r := mustFinalize(t, d)
	if r.Disposition != DispositionScopeViolation {
		t.Fatalf("disposition = %q, want scope_violation to dominate certification", r.Disposition)
	}
}
