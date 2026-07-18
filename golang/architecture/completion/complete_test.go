// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"sync"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// expectRefusal completes and asserts a typed refusal that wrote no completed event.
func (w world) expectRefusal(t *testing.T, expectedHead string, want Outcome) CompleteResult {
	t.Helper()
	res := w.complete(t, expectedHead)
	if res.Outcome != want {
		t.Fatalf("outcome = %s (%s), want %s", res.Outcome, res.Detail, want)
	}
	if hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("a refused completion must not append a completed event")
	}
	if res.Receipt != nil && res.Outcome != OutcomeExactReplay {
		t.Fatal("a refusal must not return a committed receipt")
	}
	return res
}

// Item 2/6: a tampered current correctness artifact → not ready, zero writes.
func TestCompleteRefusesTamperedCorrectness(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	tamperCurrentCorrectness(t, w.TaskDir)
	w.expectRefusal(t, head, OutcomeNotReady)
}

// Item 7: a tampered current question-resolution certificate → not ready.
func TestCompleteRefusesTamperedQR(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	tamperQRCert(t, w.Repo)
	w.expectRefusal(t, head, OutcomeNotReady)
}

// Item 5: governed world changes between observation and mutation → refusal.
func TestCompleteRefusesGovernedDrift(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	changeGoverned(t, w.Repo)
	w.expectRefusal(t, head, OutcomeNotReady)
}

// Item 2 (missing obligation): no question-resolution certificate → not ready.
func TestCompleteRefusesMissingQR(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	w.seedCorrectness(t, rb, closureprotocol.Certified)
	// no runQRCert
	head := currentHead(t, w.TaskDir)
	w.expectRefusal(t, head, OutcomeNotReady)
}

// Item 8: a correctness certificate bound only to a different result → not ready.
func TestCompleteRefusesWrongResult(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t)
	rb := currentResultBinding(t, w.TaskDir)
	other := rb
	other.ResultTreeDigestSHA256 = "4444444444444444444444444444444444444444444444444444444444444444"
	w.seedCorrectness(t, other, closureprotocol.Certified)
	w.runQRCert(t)
	head := currentHead(t, w.TaskDir)
	w.expectRefusal(t, head, OutcomeNotReady)
}

// Item 4: a stale expected head → refusal.
func TestCompleteRefusesStaleExpectedHead(t *testing.T) {
	w := seedWorld(t)
	w.ready(t)
	stale := "0000000000000000000000000000000000000000000000000000000000000000"
	w.expectRefusal(t, stale, OutcomeStaleExpectedHead)
}

// Item 9: an un-enrolled completion actor → authority refusal.
func TestCompleteRefusesMissingAuthority(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	res, err := CompleteTask(context.Background(), CompleteRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: t.TempDir(),
		ExpectedLedgerHeadDigestSHA256: head,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("outcome = %s, want authority_refusal", res.Outcome)
	}
	if hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("authority refusal must not append a completed event")
	}
}

// Item 3: no caller field can manufacture completion — the request carries no
// readiness; an unready task never completes.
func TestCompleteNoCallerCanManufacture(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t) // resolved but never certified: correctness + qr missing
	head := currentHead(t, w.TaskDir)
	w.expectRefusal(t, head, OutcomeNotReady)
}

// Item 10: exact retry → replay, no duplicate receipt or event.
func TestCompleteExactReplay(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	first := w.complete(t, head)
	if first.Outcome != OutcomeCommitted {
		t.Fatalf("first = %s (%s)", first.Outcome, first.Detail)
	}
	second := w.complete(t, head) // same expected head — a genuine retry
	if second.Outcome != OutcomeExactReplay {
		t.Fatalf("second = %s (%s), want exact_replay", second.Outcome, second.Detail)
	}
	if n := countLedgerEvents(t, w.TaskDir, "completed"); n != 1 {
		t.Fatalf("completed events = %d, want 1", n)
	}
}

// Item 11: concurrent completion attempts → at most one authoritative completion.
func TestCompleteConcurrentSingleWinner(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	const n = 4
	var wg sync.WaitGroup
	outcomes := make([]Outcome, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := CompleteTask(context.Background(), CompleteRequest{
				RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
				ExpectedLedgerHeadDigestSHA256: head,
			})
			if err != nil {
				outcomes[idx] = Outcome("error:" + err.Error())
				return
			}
			outcomes[idx] = res.Outcome
		}(i)
	}
	wg.Wait()
	committed := 0
	for _, o := range outcomes {
		switch o {
		case OutcomeCommitted:
			committed++
		case OutcomeExactReplay:
		default:
			t.Fatalf("unexpected concurrent outcome %q", o)
		}
	}
	if committed != 1 {
		t.Fatalf("committed = %d, want exactly 1", committed)
	}
	if got := countLedgerEvents(t, w.TaskDir, "completed"); got != 1 {
		t.Fatalf("completed events = %d, want 1", got)
	}
}

// Item 13/14: a completed event whose receipt is tampered fails verification.
func TestCompleteTamperedReceiptFailsVerification(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	tamperCompletionReceipt(t, w.TaskDir)
	// A retry now finds a completed event whose receipt no longer verifies.
	res := w.complete(t, head)
	if res.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("outcome = %s, want integrity_failure", res.Outcome)
	}
	if _, err := verifyDurableConjunction(w.TaskDir, currentResultBinding(t, w.TaskDir)); err == nil {
		t.Fatal("durable conjunction must fail on a tampered receipt")
	}
}

// Item 15: a completed fact for an older result must not silently complete a newer
// result.
func TestCompleteOlderResultDoesNotCompleteNewer(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("setup completion failed")
	}
	// Post-completion re-work: a new result transition binds a different result.
	rb := currentResultBinding(t, w.TaskDir)
	newer := rb
	newer.ResultTreeDigestSHA256 = "5555555555555555555555555555555555555555555555555555555555555555"
	w.appendResultTransition(t, newer)
	newHead := currentHead(t, w.TaskDir)
	res := w.complete(t, newHead)
	if res.Outcome != OutcomeConflictingCompletion {
		t.Fatalf("outcome = %s, want conflicting_completion", res.Outcome)
	}
}

// Item 16: a revoked fact fails closed.
func TestCompleteRevokedFailsClosed(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	_ = head
	w.appendRevoked(t)
	newHead := currentHead(t, w.TaskDir)
	res := w.complete(t, newHead)
	if res.Outcome != OutcomeConflictingCompletion {
		t.Fatalf("outcome = %s, want conflicting_completion", res.Outcome)
	}
	if hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("must not complete over a revocation")
	}
}

// Item 17: completion mutates no correctness, disposition, promotion, question-
// resolution, or governed-source truth.
func TestCompleteMutatesNoUpstreamTruth(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	governedBefore := treeDigest(t, w.Repo+"/docs/awareness")
	promotionsBefore := treeDigest(t, w.Repo+"/.sensei/project/promotions")
	qrBefore := treeDigest(t, w.Repo+"/.sensei/project/question-resolution-certifications")
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	if treeDigest(t, w.Repo+"/docs/awareness") != governedBefore {
		t.Fatal("completion mutated governed source")
	}
	if treeDigest(t, w.Repo+"/.sensei/project/promotions") != promotionsBefore {
		t.Fatal("completion mutated promotions")
	}
	if treeDigest(t, w.Repo+"/.sensei/project/question-resolution-certifications") != qrBefore {
		t.Fatal("completion mutated question-resolution certificates")
	}
}

// Item 18: deterministic causal identity on unchanged evidence; changed evidence
// changes it.
func TestCompleteCausalIdentityDeterminism(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.x", SessionID: "session.x"}
	rb := closureprotocol.ResultBinding{BaseRevision: "r0", ResultTreeDigestSHA256: "tree", GraphDigestSHA256: "g"}
	a := causalIdentity(task, rb, "cd", "qd", "gm", GrantTerminalCompletion, "role.x")
	b := causalIdentity(task, rb, "cd", "qd", "gm", GrantTerminalCompletion, "role.x")
	if a != b {
		t.Fatal("causal identity must be deterministic on identical evidence")
	}
	c := causalIdentity(task, rb, "cd", "DIFFERENT", "gm", GrantTerminalCompletion, "role.x")
	if a == c {
		t.Fatal("changed evidence must change the causal identity")
	}
}

// Item 1: a fully ready current world → one receipt + one completed event.
func TestCompleteHappyPath(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	res := w.complete(t, head)
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("outcome = %s (%s), want committed", res.Outcome, res.Detail)
	}
	if !hasLedgerEvent(t, w.TaskDir, "completed") {
		t.Fatal("no completed event appended")
	}
	if res.Receipt == nil {
		t.Fatal("committed result must carry a receipt")
	}
	if err := ValidateTerminalReceipt(*res.Receipt); err != nil {
		t.Fatalf("committed receipt invalid: %v", err)
	}
	// exactly one completed event
	if n := countLedgerEvents(t, w.TaskDir, "completed"); n != 1 {
		t.Fatalf("completed events = %d, want 1", n)
	}
	// the durable conjunction verifies independently.
	if _, err := verifyDurableConjunction(w.TaskDir, currentResultBinding(t, w.TaskDir)); err != nil {
		t.Fatalf("durable conjunction: %v", err)
	}
}

func countLedgerEvents(t *testing.T, taskDir, eventType string) int {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	n := 0
	for _, e := range chain.Entries {
		if string(e.Entry.EventType) == eventType {
			n++
		}
	}
	return n
}
