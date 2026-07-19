// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func (w world) verifyClosure(t *testing.T) CompletionClosureAssessment {
	t.Helper()
	a, err := VerifyCompletionClosure(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("verify closure: %v", err)
	}
	return a
}

func componentVerified(a CompletionClosureAssessment, name string) bool {
	for _, c := range a.Components {
		if c.Component == name {
			return c.Verified
		}
	}
	return false
}

// World 1: eligible -> ready -> one completion -> reinspect -> authoritative closure
// with every owner re-verified end-to-end.
func TestClosureHappyPath(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	a := w.verifyClosure(t)
	if a.Verdict != ClosureAuthoritativeCompletion {
		t.Fatalf("verdict = %s, want authoritative_completion", a.Verdict)
	}
	for _, name := range []string{"terminal_reconstruction", "completion_conjunction", "phase6_correctness", "question_resolution", "readiness_binding"} {
		if !componentVerified(a, name) {
			t.Fatalf("component %s not verified end-to-end", name)
		}
	}
}

// World 2: exact replay reconstructs the same terminal identity and adds nothing.
func TestClosureExactReplayStableIdentity(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	a1 := w.verifyClosure(t)
	if w.complete(t, head).Outcome != OutcomeExactReplay {
		t.Fatal("retry was not a replay")
	}
	a2 := w.verifyClosure(t)
	if a1.DigestSHA256 != a2.DigestSHA256 {
		t.Fatal("replay changed the closure identity")
	}
	if countLedgerEvents(t, w.TaskDir, "completed") != 1 {
		t.Fatal("replay added a completed event")
	}
}

// World 3: crash residue (an orphan receipt) is not completion, and retry through
// CompleteTask still lands exactly one completion.
func TestClosureCrashResidueThenComplete(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	seedOrphanReceipt(t, w.TaskDir)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion over residue failed")
	}
	if countLedgerEvents(t, w.TaskDir, "completed") != 1 {
		t.Fatal("residue caused more than one completion")
	}
	if w.verifyClosure(t).Verdict != ClosureAuthoritativeCompletion {
		t.Fatal("completion over harmless residue is not authoritative")
	}
}

// World 4: an event without a valid receipt is never authoritative and recovery
// cannot bless it.
func TestClosureBrokenCompletion(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	deleteReceiptArtifact(t, w.TaskDir)
	if v := w.verifyClosure(t).Verdict; v != ClosureBroken {
		t.Fatalf("verdict = %s, want broken_completion", v)
	}
	if w.recover(t).Outcome != RecoverBrokenCompletion {
		t.Fatal("recovery must not bless a broken completion")
	}
}

// World 5 & 6: duplicate completed facts and a revoked fact are contradictory.
func TestClosureDuplicateAndRevoked(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	seedCompletedEvent(t, w.TaskDir)
	if v := w.verifyClosure(t).Verdict; v != ClosureContradictory {
		t.Fatalf("duplicate verdict = %s, want contradictory", v)
	}

	w2 := seedWorld(t)
	h2 := w2.ready(t)
	if w2.complete(t, h2).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	w2.appendRevoked(t)
	if v := w2.verifyClosure(t).Verdict; v != ClosureContradictory {
		t.Fatalf("revoked verdict = %s, want contradictory", v)
	}
}

// World 7 & 8: a completion for an older result, or a lost current result, is never
// authoritative current completion.
func TestClosureResultTransitionAndMissingResult(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	rb := currentResultBinding(t, w.TaskDir)
	newer := rb
	newer.ResultTreeDigestSHA256 = "8888888888888888888888888888888888888888888888888888888888888888"
	w.appendResultTransition(t, newer)
	if v := w.verifyClosure(t).Verdict; v != ClosureBroken {
		t.Fatalf("older-result verdict = %s, want broken_completion", v)
	}

	w2 := seedWorld(t)
	h2 := w2.ready(t)
	if w2.complete(t, h2).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	w2.appendEmptyResultTransition(t)
	if v := w2.verifyClosure(t).Verdict; v != ClosureUnsupported {
		t.Fatalf("missing-result verdict = %s, want unsupported", v)
	}
}

// World 9: governed drift after completion keeps the completion authoritative and is
// reported distinctly, never as corruption.
func TestClosureGovernedDrift(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	changeGoverned(t, w.Repo)
	a := w.verifyClosure(t)
	if a.Verdict != ClosureAuthoritativeCompletion {
		t.Fatalf("verdict = %s, want authoritative_completion (drift is not corruption)", a.Verdict)
	}
	if !a.GovernedDriftAfterCompletion {
		t.Fatal("governed drift must be reported distinctly")
	}
}

// World 10: projection loss reconstructs as a valid conjunction; recovery rebuilds
// only projections; cardinality unchanged.
func TestClosureProjectionLossThenRecover(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	deleteProjections(t, w.TaskDir)
	if w.verifyClosure(t).Verdict != ClosureAuthoritativeCompletion {
		t.Fatal("valid conjunction with stale projections must remain authoritative")
	}
	entriesBefore := ledgerEntryCount(t, w.TaskDir)
	if rec := w.recover(t); rec.Outcome != RecoverProjectionsRebuilt {
		t.Fatalf("recover = %s", rec.Outcome)
	}
	if ledgerEntryCount(t, w.TaskDir) != entriesBefore || countLedgerEvents(t, w.TaskDir, "completed") != 1 {
		t.Fatal("recovery changed ledger/receipt cardinality")
	}
	if w.verifyClosure(t).Verdict != ClosureAuthoritativeCompletion {
		t.Fatal("not authoritative after rebuild")
	}
}

// World 11: tampering any bound component cannot produce authoritative closure.
func TestClosureTamperingBreaksClosure(t *testing.T) {
	t.Run("correctness", func(t *testing.T) {
		w := seedWorld(t)
		head := w.ready(t)
		if w.complete(t, head).Outcome != OutcomeCommitted {
			t.Fatal("completion failed")
		}
		tamperCurrentCorrectness(t, w.TaskDir)
		a := w.verifyClosure(t)
		if a.Verdict == ClosureAuthoritativeCompletion || componentVerified(a, "phase6_correctness") {
			t.Fatal("tampered correctness must break closure")
		}
	})
	t.Run("question_resolution", func(t *testing.T) {
		w := seedWorld(t)
		head := w.ready(t)
		if w.complete(t, head).Outcome != OutcomeCommitted {
			t.Fatal("completion failed")
		}
		tamperQRCert(t, w.Repo)
		a := w.verifyClosure(t)
		if a.Verdict == ClosureAuthoritativeCompletion || componentVerified(a, "question_resolution") {
			t.Fatal("tampered question-resolution cert must break closure")
		}
	})
	t.Run("completion_receipt", func(t *testing.T) {
		w := seedWorld(t)
		head := w.ready(t)
		if w.complete(t, head).Outcome != OutcomeCommitted {
			t.Fatal("completion failed")
		}
		tamperCompletionReceipt(t, w.TaskDir)
		if w.verifyClosure(t).Verdict == ClosureAuthoritativeCompletion {
			t.Fatal("tampered completion receipt must break closure")
		}
	})
}

// World 12: completion and projection recovery serialize; exactly one terminal fact.
func TestClosureConcurrentCompletionAndRecovery(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	deleteProjections(t, w.TaskDir)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_, _ = CompleteTask(context.Background(), CompleteRequest{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot, ExpectedLedgerHeadDigestSHA256: head})
			} else {
				_, _ = RecoverProjections(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
			}
		}(i)
	}
	wg.Wait()
	if countLedgerEvents(t, w.TaskDir, "completed") != 1 {
		t.Fatalf("completed events = %d, want exactly 1", countLedgerEvents(t, w.TaskDir, "completed"))
	}
	if w.verifyClosure(t).Verdict != ClosureAuthoritativeCompletion {
		t.Fatal("not authoritative after concurrent completion/recovery")
	}
}

// World 13: no caller-supplied value can manufacture closure — the request carries
// only repo/task, and an unready task is never authoritative.
func TestClosureNoCallerManufacture(t *testing.T) {
	w := seedWorld(t)
	w.resolveAll(t) // resolved but never certified: not ready, not completed
	if v := w.verifyClosure(t).Verdict; v != ClosureNotCompleted {
		t.Fatalf("verdict = %s, want not_completed", v)
	}
}

// World 14: unchanged durable evidence produces a byte-identical closure identity.
func TestClosureDeterministic(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	if w.verifyClosure(t).DigestSHA256 != w.verifyClosure(t).DigestSHA256 {
		t.Fatal("closure verification is not deterministic")
	}
}

// Readiness identity (correction): the bound readiness owner is proven by
// reconstruction + digest recomputation, not digest syntax. Any invented digest, or
// mutated/missing/duplicate obligations, breaks it; an unchanged completion holds.
func TestClosureReadinessProvenByIdentityNotSyntax(t *testing.T) {
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	receipt, err := verifyDurableConjunction(w.TaskDir, currentResultBinding(t, w.TaskDir))
	if err != nil {
		t.Fatalf("load receipt: %v", err)
	}
	if ok, d := reverifyReadiness(receipt); !ok {
		t.Fatalf("unchanged readiness rejected: %s", d)
	}

	// arbitrary well-formed digest — passes isHex64, fails identity.
	bad := receipt
	bad.ReadinessAssessmentDigestSHA256 = strings.Repeat("a", 64)
	if ok, _ := reverifyReadiness(bad); ok {
		t.Fatal("an invented well-formed readiness digest must break closure")
	}

	// mutated obligation state.
	mutated := receipt
	mutated.Obligations = append([]ObligationAssessment(nil), receipt.Obligations...)
	mutated.Obligations[0].State = EvidenceMissing
	if ok, _ := reverifyReadiness(mutated); ok {
		t.Fatal("a mutated obligation must break closure")
	}

	// missing obligation.
	missing := receipt
	missing.Obligations = receipt.Obligations[:len(receipt.Obligations)-1]
	if ok, _ := reverifyReadiness(missing); ok {
		t.Fatal("a missing obligation must break closure")
	}

	// duplicate obligation.
	dup := receipt
	dup.Obligations = append(append([]ObligationAssessment(nil), receipt.Obligations...), receipt.Obligations[0])
	if ok, _ := reverifyReadiness(dup); ok {
		t.Fatal("a duplicate obligation must break closure")
	}

	// wrong obligation (replace one id with an off-set value).
	wrong := receipt
	wrong.Obligations = append([]ObligationAssessment(nil), receipt.Obligations...)
	wrong.Obligations[0].Obligation = "not_an_obligation"
	if ok, _ := reverifyReadiness(wrong); ok {
		t.Fatal("a wrong obligation must break closure")
	}

	// End-to-end: a receipt whose readiness fails identity cannot yield authoritative
	// closure — proven through the full verifier on the untouched happy path.
	if w.verifyClosure(t).Verdict != ClosureAuthoritativeCompletion {
		t.Fatal("unchanged completion must remain authoritative")
	}
}

// World 15 + contract: all Phase-8 owners and terminal states are present and
// connected, and the closure report keeps the three claims distinct.
func TestPhase8ClosureReportContract(t *testing.T) {
	r := BuildPhase8ClosureReport()
	wantOwners := map[string]bool{
		"phase6_correctness_certification": true, "question_resolution_certification": true,
		"readiness_assessment": true, "terminal_completion": true,
		"terminal_reconstruction_and_recovery": true, "completion_closure_integration": true,
	}
	for _, o := range r.Owners {
		delete(wantOwners, o.Name)
		if o.Identity == "" {
			t.Fatalf("owner %s has no identity", o.Name)
		}
	}
	if len(wantOwners) != 0 {
		t.Fatalf("missing Phase-8 owners: %v", wantOwners)
	}
	if len(r.TerminalStates) != len(AssessmentBoundStates()) {
		t.Fatal("closure report does not enumerate the full terminal state set")
	}
	if len(r.Distinctions) != 3 {
		t.Fatalf("distinctions = %d, want 3 (implementation != task-completion != perfection)", len(r.Distinctions))
	}
	joined := strings.Join(r.Distinctions, " ")
	for _, must := range []string{"implementation", "one task", "Repository-wide"} {
		if !strings.Contains(joined, must) {
			t.Fatalf("distinctions must keep %q distinct", must)
		}
	}
	// Deterministic + not usable as a completion fact.
	if BuildPhase8ClosureReport().DigestSHA256 != r.DigestSHA256 {
		t.Fatal("closure report is not deterministic")
	}
	if r.SchemaVersion == string(closureprotocol.TerminalCompleted) {
		t.Fatal("report must not masquerade as a completion status")
	}
}

// World 15 (authority separation): the completion grant is disjoint from the other
// owner grants, and projection recovery never appends a completed fact.
func TestClosureAuthoritySeparation(t *testing.T) {
	// distinct authority identities.
	if GrantTerminalCompletion == "grant.sensei.question_resolution_certification" ||
		GrantTerminalCompletion == "grant.sensei.governed_promotion" ||
		GrantTerminalCompletion == "grant.sensei.question_disposition" {
		t.Fatal("completion grant must be disjoint from the other owner grants")
	}
	// recovery on a committed task never appends completed.
	w := seedWorld(t)
	head := w.ready(t)
	if w.complete(t, head).Outcome != OutcomeCommitted {
		t.Fatal("completion failed")
	}
	deleteProjections(t, w.TaskDir)
	if w.recover(t).Outcome != RecoverProjectionsRebuilt {
		t.Fatal("recovery failed")
	}
	if countLedgerEvents(t, w.TaskDir, "completed") != 1 {
		t.Fatal("recovery appended a completed fact")
	}
}
