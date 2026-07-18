// SPDX-License-Identifier: Apache-2.0

package questionresolution

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/architecture/questionpromotion"
	"github.com/globulario/sensei/golang/propose"
)

func invariantY() propose.Request {
	return propose.Request{
		Kind: "invariant", ID: "invariant.promoted.serve_after_reload",
		Title: "Serve only after reload", Description: "promoted from a second accepted answer",
		SourceFiles: []string{"golang/server/serve.go"}, RelatedFailures: []string{"failure.y"},
		Domain: testDomain,
	}
}

func evidenceFor(c *QuestionResolutionCertificate, id string) (QuestionEvidence, bool) {
	for _, q := range c.QuestionEvidence {
		if q.QuestionID == id {
			return q, true
		}
	}
	return QuestionEvidence{}, false
}

// Item 1: no architectural questions → bounded gate passes with an explicit empty
// evidence set.
func TestGatePassesWithNoQuestions(t *testing.T) {
	w := seedWorldDir(t, "not_applicable")
	if len(w.Questions) != 0 {
		t.Fatalf("expected no questions, got %d", len(w.Questions))
	}
	res := w.certify(t)
	if res.Outcome != OutcomeSatisfied {
		t.Fatalf("outcome = %s (%s), want satisfied", res.Outcome, res.Detail)
	}
	if res.Certificate == nil || len(res.Certificate.QuestionEvidence) != 0 {
		t.Fatalf("expected certificate with empty evidence set")
	}
	if err := ValidateCertificate(*res.Certificate); err != nil {
		t.Fatalf("certificate invalid: %v", err)
	}
}

// Items 2 & 4: a valid committed in-scope promotion satisfies the reusable
// obligation; a valid answered task-local question satisfies the current task but is
// never rendered as governed repository truth.
func TestGateSatisfiedByPromotionAndTaskLocal(t *testing.T) {
	w := seedWorld(t)
	qA, qB := w.Questions[0].QuestionID, w.Questions[1].QuestionID
	dA := w.dispose2(t, qA, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	dB := w.dispose2(t, qB, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")

	res := w.certify(t)
	if res.Outcome != OutcomeSatisfied {
		t.Fatalf("outcome = %s (%s), want satisfied", res.Outcome, res.Detail)
	}
	c := res.Certificate
	evA, _ := evidenceFor(c, qA)
	evB, _ := evidenceFor(c, qB)
	if evA.State != StateReusablePromoted {
		t.Fatalf("qA state = %s, want reusable_promoted", evA.State)
	}
	if evB.State != StateAnsweredTaskLocal {
		t.Fatalf("qB state = %s, want answered_task_local", evB.State)
	}
	// Exactly the promoted answer carries promotion evidence; the task-local answer
	// never appears as governed truth.
	if len(c.PromotionEvidence) != 1 || c.PromotionEvidence[0].DispositionReceiptDigestSHA256 != dA {
		t.Fatalf("promotion evidence = %+v, want exactly qA's disposition", c.PromotionEvidence)
	}
	for _, p := range c.PromotionEvidence {
		if p.DispositionReceiptDigestSHA256 == dB {
			t.Fatal("task-local disposition surfaced as governed promotion truth")
		}
	}
}

// Item 7: an unresolved binding question fails the gate closed and writes nothing.
func TestGateBlockedOnUnresolvedBindingQuestion(t *testing.T) {
	w := seedWorld(t)
	w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "A")
	// w.Questions[1] left undisposed.
	res := w.certify(t)
	if res.Outcome != OutcomeUnresolvedQuestion {
		t.Fatalf("outcome = %s, want blocked_unresolved_question", res.Outcome)
	}
	if res.Certificate != nil {
		t.Fatal("a refused gate must not produce a certificate")
	}
	if certFileCount(t, w.Repo) != 0 {
		t.Fatal("a refused gate must not write a certificate file")
	}
}

// Item 7 (deferred is non-terminal): a deferred binding disposition fails the gate.
func TestGateBlockedOnDeferredBindingQuestion(t *testing.T) {
	w := seedWorld(t)
	w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionDeferred, qd.ReusabilityNone, "A")
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	res := w.certify(t)
	if res.Outcome != OutcomeUnresolvedQuestion {
		t.Fatalf("outcome = %s, want blocked_unresolved_question", res.Outcome)
	}
}

// Item 3: a reusable-candidate answer with no committed promotion blocks the gate.
func TestGateBlockedOnReusableWithoutPromotion(t *testing.T) {
	w := seedWorld(t)
	w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	res := w.certify(t)
	if res.Outcome != OutcomeIncompletePromotion {
		t.Fatalf("outcome = %s, want blocked_incomplete_promotion", res.Outcome)
	}
	if res.Certificate != nil {
		t.Fatal("must not certify a reusable candidate without a committed promotion")
	}
}

// Boundedness (correction): an unrelated broken promotion elsewhere in the
// repository must NOT block a fully-resolved current task.
func TestUnrelatedBrokenPromotionDoesNotBlock(t *testing.T) {
	w := seedWorld(t)
	qA, qB := w.Questions[0].QuestionID, w.Questions[1].QuestionID
	dA := w.dispose2(t, qA, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	w.dispose2(t, qB, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	// Debris: a broken promotion binding a disposition of no current-task question.
	writeUnrelatedBrokenPromotion(t, w.Repo, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")

	res := w.certify(t)
	if res.Outcome != OutcomeSatisfied {
		t.Fatalf("outcome = %s (%s); unrelated debris must not veto a bounded certificate", res.Outcome, res.Detail)
	}
	if len(res.Summary.IntegrityFindings) != 0 {
		t.Fatalf("unrelated debris leaked into findings: %v", res.Summary.IntegrityFindings)
	}
}

// Boundedness (correction, requirement 7): two verified promotions binding the same
// disposition fail closed as contradictory evidence — never map-order selection.
func TestReusableClassificationFailsClosedOnCollision(t *testing.T) {
	two := []questionpromotion.VerifiedPromotion{{PromotionLineageID: "lineage.a"}, {PromotionLineageID: "lineage.b"}}
	if st, finding := classifyReusable(two, ""); st != StateEvidenceIntegrityFailure || finding == "" {
		t.Fatalf("two verified promotions: state=%s finding=%q, want evidence_integrity_failure + finding", st, finding)
	}
	if st, _ := classifyReusable(two[:1], ""); st != StateReusablePromoted {
		t.Fatalf("one verified promotion state = %s, want reusable_promoted", st)
	}
	if st, _ := classifyReusable(nil, "broken"); st != StateEvidenceIntegrityFailure {
		t.Fatalf("relevant integrity state = %s, want evidence_integrity_failure", st)
	}
	if st, _ := classifyReusable(nil, ""); st != StateReusableUnpromoted {
		t.Fatalf("no evidence state = %s, want reusable_candidate_unpromoted", st)
	}
}

// Item 5: a tampered promotion tied to a current reusable disposition is excluded
// with a typed integrity finding and fails the gate closed.
func TestGateBlockedOnTamperedPromotion(t *testing.T) {
	w := seedWorld(t)
	dA := w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	tamperGraph(t, w.Repo)

	res := w.certify(t)
	if res.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("outcome = %s, want blocked_integrity_failure", res.Outcome)
	}
	if len(res.Summary.IntegrityFindings) == 0 {
		t.Fatal("a tampered promotion must surface a typed integrity finding")
	}
	if res.Certificate != nil {
		t.Fatal("must not certify over tampered evidence")
	}
}

// Item 6: a contested disposition fails the gate.
func TestGateBlockedOnContestedQuestion(t *testing.T) {
	w := seedWorld(t)
	qA := w.Questions[0].QuestionID
	w.dispose2(t, qA, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "first")
	w.dispose2(t, qA, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "second")
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	res := w.certify(t)
	if res.Outcome != OutcomeContestedQuestion {
		t.Fatalf("outcome = %s, want blocked_contested_question", res.Outcome)
	}
}

// Item 6 (stale): a mismatched expected ledger head refuses the gate — no stale
// certification.
func TestGateRefusesStaleExpectedHead(t *testing.T) {
	w := seedWorld(t)
	w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "A")
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	stale := "0000000000000000000000000000000000000000000000000000000000000000"
	res, err := Certify(context.Background(), CertifyRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: stale,
	})
	if err != nil {
		t.Fatalf("certify: %v", err)
	}
	if res.Outcome != OutcomeStaleExpectedHead {
		t.Fatalf("outcome = %s, want stale_expected_head", res.Outcome)
	}
	if certFileCount(t, w.Repo) != 0 {
		t.Fatal("a stale gate must not write a certificate")
	}
}

// Item 8: a promotion satisfies ONLY the exact disposition it committed; it can
// never be mis-credited to a different (out-of-scope) question.
func TestPromotionSatisfiesOnlyItsExactDisposition(t *testing.T) {
	w := seedWorld(t)
	qA, qB := w.Questions[0].QuestionID, w.Questions[1].QuestionID
	dA := w.dispose2(t, qA, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	// qB is also a reusable candidate but is NOT promoted — the qA promotion must
	// not satisfy it.
	w.dispose2(t, qB, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "B")
	res := w.certify(t)
	if res.Outcome != OutcomeIncompletePromotion {
		t.Fatalf("outcome = %s, want blocked_incomplete_promotion", res.Outcome)
	}
}

// Item 10: repeated evaluation on an unchanged world yields a byte-identical
// certificate, an explicit replay, and no additional file or ledger event.
func TestCertifyIsDeterministicAndIdempotent(t *testing.T) {
	w := seedWorld(t)
	dA := w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")

	before := ledgerEntryCount(t, w.TaskDir)
	res1 := w.certify(t)
	if res1.Outcome != OutcomeSatisfied {
		t.Fatalf("first outcome = %s (%s)", res1.Outcome, res1.Detail)
	}
	n1 := certFileCount(t, w.Repo)
	res2 := w.certify(t)
	if res2.Outcome != OutcomeReplay {
		t.Fatalf("second outcome = %s, want exact_replay", res2.Outcome)
	}
	if res1.Certificate.DigestSHA256 != res2.Certificate.DigestSHA256 {
		t.Fatal("identical world produced different certificate identities")
	}
	if certFileCount(t, w.Repo) != n1 {
		t.Fatal("replay wrote an additional certificate file")
	}
	if after := ledgerEntryCount(t, w.TaskDir); after != before {
		t.Fatalf("certification appended a task-ledger event (%d -> %d)", before, after)
	}
}

// Item 11: changed evidence produces a different certification identity.
func TestChangedEvidenceChangesIdentity(t *testing.T) {
	// World X: both binding questions task-local (no promotion).
	wx := seedWorld(t)
	wx.dispose2(t, wx.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "A")
	wx.dispose2(t, wx.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	rx := wx.certify(t)
	if rx.Outcome != OutcomeSatisfied {
		t.Fatalf("world X outcome = %s (%s)", rx.Outcome, rx.Detail)
	}

	// World Y: identical seed, but qA is a promoted reusable candidate.
	wy := seedWorld(t)
	dA := wy.dispose2(t, wy.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	wy.promote(t, dA, proposedInvariant())
	wy.dispose2(t, wy.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")
	ry := wy.certify(t)
	if ry.Outcome != OutcomeSatisfied {
		t.Fatalf("world Y outcome = %s (%s)", ry.Outcome, ry.Detail)
	}

	if rx.Certificate.DigestSHA256 == ry.Certificate.DigestSHA256 {
		t.Fatal("changed evidence must produce a different certification identity")
	}
}

// Item 13: the certificate asserts only the bounded obligation — never correctness
// or completion — and certification appends no Phase-6 certified/completed event.
func TestGateNeverAssertsCorrectnessOrCompletion(t *testing.T) {
	w := seedWorld(t)
	dA := w.dispose2(t, w.Questions[0].QuestionID, qd.DispositionAnswered, qd.ReusabilityReusableCandidate, "A")
	w.promote(t, dA, proposedInvariant())
	w.dispose2(t, w.Questions[1].QuestionID, qd.DispositionAnswered, qd.ReusabilityTaskLocal, "B")

	res := w.certify(t)
	if res.Outcome != OutcomeSatisfied {
		t.Fatalf("outcome = %s (%s)", res.Outcome, res.Detail)
	}
	if hasLedgerEvent(t, w.TaskDir, "certified") || hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("bounded gate must not append a Phase-6 certified/completed task-ledger event")
	}
	raw, err := os.ReadFile(filepath.Join(w.Repo, filepath.FromSlash(CertificationsRelDir), res.Certificate.DigestSHA256, "certificate.json"))
	if err != nil {
		t.Fatalf("read certificate: %v", err)
	}
	blob := string(raw)
	if strings.Contains(blob, "correctness_certified") {
		t.Fatal("certificate must carry no correctness-certified assertion")
	}
	if strings.Contains(blob, "awareness.nt") || strings.Contains(blob, "embeddata") {
		t.Fatal("certificate must make no combined-embedded-seed claim")
	}
	// The bound disclaimer is part of the content.
	var c QuestionResolutionCertificate
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range c.Bound {
		if strings.Contains(b, "does not assert overall correctness") {
			found = true
		}
	}
	if !found {
		t.Fatal("certificate must disclaim correctness/closure/completion in its bound statement")
	}
}
