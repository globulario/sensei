// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/propose"
)

func prodDeps() promoteDeps { return promoteDeps{now: func() time.Time { return time.Now().UTC() }} }

func withNow(d promoteDeps) promoteDeps {
	d.now = func() time.Time { return time.Now().UTC() }
	return d
}

func promotionDirOf(p promotable, lineage string) string {
	return filepath.Join(p.Repo, ".sensei", "project", "promotions", lineage)
}

func countCommitted(t *testing.T, promotionDir string) int {
	t.Helper()
	chain, err := OpenJournal(promotionDir).Verify()
	if err != nil {
		t.Fatalf("journal verify: %v", err)
	}
	n := 0
	for _, e := range chain {
		if e.EventType == EventPromotionCommitted {
			n++
		}
	}
	return n
}

// crashThenRecover injects a crash via deps, asserts an incomplete outcome, then
// runs the production transaction and asserts exactly one authoritative commit.
func crashThenRecover(t *testing.T, name string, deps promoteDeps) {
	t.Helper()
	p := seedPromotable(t)
	first, err := promoteWith(context.Background(), p.request(), withNow(deps))
	if err != nil {
		t.Fatalf("%s crash run: %v", name, err)
	}
	if !strings.HasPrefix(string(first.Outcome), "promotion_incomplete") {
		t.Fatalf("%s: crash outcome = %s, want incomplete", name, first.Outcome)
	}
	// Recover.
	res, err := Promote(context.Background(), p.request())
	if err != nil {
		t.Fatalf("%s recover: %v", name, err)
	}
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("%s: recovered outcome = %s (%s), want committed", name, res.Outcome, res.Detail)
	}
	if n := countCommitted(t, promotionDirOf(p, res.PromotionLineageID)); n != 1 {
		t.Fatalf("%s: committed events = %d, want 1", name, n)
	}
}

// The seven asymmetric crash windows all recover deterministically to one commit.
func TestCrashWindow1_PreparedSourceAbsent(t *testing.T) {
	crashThenRecover(t, "prepared", promoteDeps{stopAfter: EventPrepared})
}
func TestCrashWindow2_SourceDurableEventAbsent(t *testing.T) {
	crashThenRecover(t, "source-no-event", promoteDeps{afterSourceApply: func() {}})
}
func TestCrashWindow3_SourceEventDurableGraphAbsent(t *testing.T) {
	crashThenRecover(t, "source_committed", promoteDeps{stopAfter: EventSourceCommitted})
}
func TestCrashWindow4_GraphDurableEventAbsent(t *testing.T) {
	crashThenRecover(t, "graph-no-event", promoteDeps{afterGraphBuild: func() {}})
}
func TestCrashWindow5_GraphEventDurableReceiptAbsent(t *testing.T) {
	crashThenRecover(t, "graph_verified", promoteDeps{stopAfter: EventGraphVerified})
}
func TestCrashWindow6_ReceiptDurableCommitAbsent(t *testing.T) {
	crashThenRecover(t, "receipt-no-commit", promoteDeps{afterReceiptPersist: func() {}})
}

// Window 7: commit durable, replay returns the same authoritative receipt and adds
// no event.
func TestCrashWindow7_CommittedReplay(t *testing.T) {
	p := seedPromotable(t)
	first, err := Promote(context.Background(), p.request())
	if err != nil || first.Outcome != OutcomeCommitted {
		t.Fatalf("first: %v (%s)", err, first.Outcome)
	}
	second, err := Promote(context.Background(), p.request())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if second.Outcome != OutcomeExactReplay {
		t.Fatalf("replay outcome = %s, want exact_replay", second.Outcome)
	}
	if second.ReceiptDigestSHA256 != first.ReceiptDigestSHA256 ||
		second.CommittedCausalIdentitySHA256 != first.CommittedCausalIdentitySHA256 {
		t.Fatal("replay produced a different authoritative identity")
	}
	if n := countCommitted(t, promotionDirOf(p, first.PromotionLineageID)); n != 1 {
		t.Fatalf("committed events = %d, want 1 after replay", n)
	}
}

// Two concurrent promotions for one lineage yield exactly one commit.
func TestConcurrentIdenticalSingleCommit(t *testing.T) {
	p := seedPromotable(t)
	var wg sync.WaitGroup
	results := make([]PromoteResult, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = Promote(context.Background(), p.request())
		}(i)
	}
	wg.Wait()
	committed, replay := 0, 0
	var lineage string
	for _, r := range results {
		switch r.Outcome {
		case OutcomeCommitted:
			committed++
			lineage = r.PromotionLineageID
		case OutcomeExactReplay:
			replay++
			lineage = r.PromotionLineageID
		}
	}
	if committed+replay == 0 {
		t.Fatal("no promotion succeeded")
	}
	if n := countCommitted(t, promotionDirOf(p, lineage)); n != 1 {
		t.Fatalf("committed events = %d, want exactly 1", n)
	}
}

// Refusals occur before mutation.
func TestUnenrolledPromotionActorRefused(t *testing.T) {
	p := seedPromotable(t)
	req := p.request()
	req.IdentityRoot = t.TempDir() // empty — not enrolled
	res, _ := Promote(context.Background(), req)
	if res.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("outcome = %s, want authority_refusal", res.Outcome)
	}
	assertNoGovernedRecord(t, p)
}

func TestWrongDispositionRefusedBeforeMutation(t *testing.T) {
	p := seedPromotable(t)
	req := p.request()
	req.QuestionDispositionReceiptDigestSHA256 = strings.Repeat("0", 64)
	res, _ := Promote(context.Background(), req)
	if res.Outcome != OutcomeIneligibleDisposition {
		t.Fatalf("outcome = %s, want ineligible_disposition", res.Outcome)
	}
	assertNoGovernedRecord(t, p)
}

// A second promotion proposing a DIFFERENT body for the same canonical id is a
// contradiction refusal (never overwrite).
func TestContradictoryProposalRefused(t *testing.T) {
	p := seedPromotable(t)
	if res, _ := Promote(context.Background(), p.request()); res.Outcome != OutcomeCommitted {
		t.Fatalf("first: %s", res.Outcome)
	}
	req := p.request()
	conflicting := proposedInvariant()
	conflicting.Description = "a genuinely different governed body"
	req.Proposal = conflicting
	res, _ := Promote(context.Background(), req)
	if res.Outcome != OutcomeContradiction {
		t.Fatalf("outcome = %s, want contradiction", res.Outcome)
	}
}

// Tampering the persisted graph after a commit fails closed on replay.
func TestGraphTamperFailsClosedOnReplay(t *testing.T) {
	p := seedPromotable(t)
	first, _ := Promote(context.Background(), p.request())
	if first.Outcome != OutcomeCommitted {
		t.Fatalf("first: %s", first.Outcome)
	}
	graphPath := filepath.Join(p.Repo, ".sensei", "project", "graph.nt")
	data, _ := os.ReadFile(graphPath)
	if err := os.WriteFile(graphPath, append(data, []byte("\n<x> <y> <z> .\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := Promote(context.Background(), p.request())
	if res.Outcome == OutcomeExactReplay || res.Outcome == OutcomeCommitted {
		t.Fatalf("tampered graph must fail closed, got %s", res.Outcome)
	}
}

// Tampering the journal fails closed.
func TestJournalTamperFailsClosed(t *testing.T) {
	p := seedPromotable(t)
	first, _ := Promote(context.Background(), p.request())
	promotionDir := promotionDirOf(p, first.PromotionLineageID)
	entry := filepath.Join(promotionDir, "journal", "000000.json")
	data, _ := os.ReadFile(entry)
	// Corrupt a payload value so the recomputed payload/entry digest no longer matches.
	tampered := strings.Replace(string(data), "\"pre_manifest_digest_sha256\": \"", "\"pre_manifest_digest_sha256\": \"deadbeef", 1)
	if tampered == string(data) {
		t.Fatal("tamper had no effect")
	}
	if err := os.WriteFile(entry, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := Promote(context.Background(), p.request())
	if res.Outcome != OutcomeTamperedJournal {
		t.Fatalf("outcome = %s, want tampered_journal", res.Outcome)
	}
}

// The promotion never mutates the task ledger head.
func TestPromotionDoesNotMutateTaskLedger(t *testing.T) {
	p := seedPromotable(t)
	before, _ := admission.TaskLedgerHead(p.TaskDir)
	if _, err := Promote(context.Background(), p.request()); err != nil {
		t.Fatal(err)
	}
	after, _ := admission.TaskLedgerHead(p.TaskDir)
	if before != after {
		t.Fatal("promotion mutated the task ledger head")
	}
}

func assertNoGovernedRecord(t *testing.T, p promotable) {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(p.Repo, "docs", "awareness", "invariants.yaml"))
	if strings.Contains(string(data), "invariant.promoted.reload_validates") {
		t.Fatal("a refused promotion mutated governed source")
	}
}

var _ = propose.Request{}
