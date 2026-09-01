// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// The gate, end to end, on the three specimen shapes -- through the same
// function runPromote now calls, with the same inputs it derives.
func TestThePromotionGateAdmitsOnlyIndependentlyVerifiedEvidence(t *testing.T) {
	head, older := headCommit(t), olderCommit(t)

	// A: real citation, material the claimant did not introduce.
	a := map[string]interface{}{"evidence_refs": []interface{}{map[string]interface{}{
		"kind": "source_fact", "commit": older, "file": "go.mod", "contains": "module github.com/globulario/sensei"}}}
	if got := verifyEvidenceRefs(context.Background(), "../..", evidenceRefsOf(a), head); got.Verdict != evidenceVerified {
		t.Fatalf("A: %+v", got)
	}

	// C: real citation, but every reference points at the commit that
	// introduced the candidate itself.
	c := map[string]interface{}{"evidence_refs": []interface{}{map[string]interface{}{
		"kind": "source_fact", "commit": head, "file": "go.mod", "contains": "module github.com/globulario/sensei"}}}
	if got := verifyEvidenceRefs(context.Background(), "../..", evidenceRefsOf(c), head); got.Verdict != evidenceClaimantControlled {
		t.Fatalf("C: self-introduced evidence was not refused: %+v", got)
	}

	// Free text only: absent. Refused unless the operator says so, and then
	// recorded -- never silently.
	free := map[string]interface{}{"evidence": "trust me, I read the code"}
	if got := verifyEvidenceRefs(context.Background(), "../..", evidenceRefsOf(free), head); got.Verdict != evidenceAbsent {
		t.Fatalf("free text was read as evidence: %+v", got)
	}

	// Fabricated: unverifiable.
	fake := map[string]interface{}{"evidence_refs": []interface{}{map[string]interface{}{
		"kind": "source_fact", "commit": older, "file": "go.mod", "contains": "module github.com/nobody/nothing"}}}
	if got := verifyEvidenceRefs(context.Background(), "../..", evidenceRefsOf(fake), head); got.Verdict != evidenceUnverifiable {
		t.Fatalf("a fabricated citation was verified: %+v", got)
	}
}

// The verdict must reach the canonical entry's provenance, so a reader of the
// YAML can see whether a rule's citations were ever checked.
func TestTheEvidenceVerdictIsRecordedInProvenance(t *testing.T) {
	candidate := map[string]interface{}{
		"id": "x.y", "class": "invariant", "status": "candidate", "confidence": "medium",
		"evidence": "see refs", "discovered_from": "test",
		evidenceVerificationKey: "EVIDENCE_VERIFIED: 1 of 1 reference(s) verified against material the claimant did not introduce",
	}
	entry := toCanonicalEntry(candidate)
	prov, _ := entry["provenance"].(map[string]interface{})
	got, _ := prov["evidence_verification"].(string)
	if !strings.HasPrefix(got, "EVIDENCE_VERIFIED") {
		t.Fatalf("provenance does not carry the verdict: %v", prov)
	}
	if _, leaked := entry[evidenceVerificationKey]; leaked {
		t.Fatal("the internal verdict key leaked into the canonical entry")
	}
}

// runPromote must actually call the gate. Asserted against the source, so the
// verifier cannot quietly become unreferenced again.
func TestRunPromoteCallsTheEvidenceGate(t *testing.T) {
	src := readSourceFile(t, "cmd_promote.go")
	i := strings.Index(src, "func runPromote(")
	if i < 0 {
		t.Fatal("runPromote not found")
	}
	body := src[i:]
	if !strings.Contains(body, "verifyEvidenceRefs(") {
		t.Fatal("runPromote does not call verifyEvidenceRefs; the verifier is built and unwired")
	}
	if strings.Index(body, "verifyEvidenceRefs(") > strings.Index(body, "toCanonicalEntry(") {
		t.Fatal("evidence is verified after the entry is already transformed for promotion")
	}
}

func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	b, err := exec.Command("cat", name).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
