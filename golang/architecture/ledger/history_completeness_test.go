// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Contract tests for Family A of the capability-resurrection cluster, written
// against docs/audit/2026-09-11-ledger-resurrection-repair-contracts.md @ 98bc9ad.
//
//	A-INV  No authority-bearing reduction may interpret event ABSENCE until history
//	       completeness has been positively established against a monotonic witness
//	       that is not reconstructible from that history.
//
// Each negative case below is a reproducer that PASSES today. Each positive case
// is a behaviour the repair must not break.

// chainOf builds a task dir with n appended entries, under a sensei-style layout
// (<root>/.sensei/tasks/<id>) so an out-of-task witness has somewhere to live.
func chainOf(t *testing.T, n int) (root, taskDir string, store *Store) {
	t.Helper()
	root = t.TempDir()
	taskDir = filepath.Join(root, ".sensei", "tasks", "task.completeness")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store = NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	head := ""
	for i := 0; i < n; i++ {
		res, err := store.Append(context.Background(), AppendRequest{
			TaskID: "task.completeness", SessionID: "session.completeness",
			ExpectedHeadDigestSHA256: head,
			EventType:                closureprotocol.LedgerEventTaskPrepared,
			Payload:                  testPayload{SchemaVersion: "1", Message: "entry"},
			PayloadMediaType:         "application/yaml", ProducerID: "sensei.test",
			ProducedAt: time.Date(2026, 7, 15, 12, i, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("append %d: %v", i+1, err)
		}
		head = res.Entry.EntryDigestSHA256
	}
	return root, taskDir, store
}

func entryPaths(t *testing.T, taskDir string) []string {
	t.Helper()
	e, err := filepath.Glob(filepath.Join(taskDir, "ledger", "0*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(e)
	return e
}

func hasError(rep VerificationReport, code string) bool {
	for _, e := range rep.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

// A-N1. Deleting the highest-sequence entry leaves a chain that is a valid
// PREFIX: every per-entry check passes and only HEAD disagrees, as a warning.
func TestANegative1_TailTruncationIsRefused(t *testing.T) {
	_, taskDir, store := chainOf(t, 3)
	e := entryPaths(t, taskDir)
	if err := os.Remove(e[len(e)-1]); err != nil {
		t.Fatal(err)
	}
	rep, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Valid=%v errors=%d warnings=%d entries=%d", rep.Valid, len(rep.Errors), len(rep.Warnings), rep.EntryCount)
	if rep.Valid {
		t.Error("A-N1: a tail-truncated chain verified as valid; absence is indistinguishable from deletion")
	}
}

// A-N2. Destroying the whole ledger must not read as "this task has no chain yet".
func TestANegative2_DestroyedLedgerIsRefused(t *testing.T) {
	_, taskDir, store := chainOf(t, 3)
	if err := os.RemoveAll(filepath.Join(taskDir, "ledger")); err != nil {
		t.Fatal(err)
	}
	rep, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Valid=%v entries=%d errors=%d", rep.Valid, rep.EntryCount, len(rep.Errors))
	if rep.Valid && rep.EntryCount == 0 {
		t.Error("A-N2: a destroyed ledger verified valid with zero entries; a task with prior state read as a task with none")
	}
}

// A-N3. Artifacts referenced INSIDE a verified entry's payload are inside the
// integrity boundary. Today only entry.Payload.Path is digest-checked.
func TestANegative3_ReferencedArtifactDeletionIsRefused(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, ".sensei", "tasks", "task.nested")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(taskDir)

	// A governed record stored as its own artifact, then REFERENCED from inside a
	// task-event payload. This is the shape admission uses; only entry.Payload.Path
	// is digest-checked today, so this ref is outside the integrity boundary.
	ref, err := store.StoreArtifactBytes([]byte(`{"verdict":"refused"}`), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.nested", SessionID: "s", ExpectedHeadDigestSHA256: "",
		EventType: closureprotocol.LedgerEventAdmissionDecided,
		Payload: TaskEventPayload{
			SchemaVersion: EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventAdmissionDecided,
			TaskID: "task.nested", SessionID: "s",
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{"admission_decision": ref},
		},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
		ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(taskDir, filepath.FromSlash(ref.Path))
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("nested artifact not stored: %v", err)
	}

	t.Run("rewritten", func(t *testing.T) {
		if err := os.WriteFile(nested, []byte(`{"verdict":"admitted"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		rep, err := store.Verify()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Valid=%v errors=%d orphans=%v", rep.Valid, len(rep.Errors), rep.OrphanArtifacts)
		if rep.Valid {
			t.Error("A-N3: a REWRITTEN referenced artifact verified as valid; refused became admitted with the chain reporting clean")
		}
	})

	t.Run("deleted", func(t *testing.T) {
		if err := os.Remove(nested); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		rep, err := store.Verify()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("Valid=%v errors=%d", rep.Valid, len(rep.Errors))
		if rep.Valid {
			t.Error("A-N3: a DELETED referenced artifact verified as valid")
		}
	})
}

// A-N4. The one case that already works must STAY working.
func TestANegative4_MiddleDeletionStaysRefused(t *testing.T) {
	_, taskDir, store := chainOf(t, 3)
	e := entryPaths(t, taskDir)
	if err := os.Remove(e[1]); err != nil {
		t.Fatal(err)
	}
	rep, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Valid {
		t.Error("A-N4 REGRESSION: a middle deletion is no longer refused")
	}
	if !hasError(rep, "ledger.sequence_gap") {
		t.Errorf("A-N4: expected ledger.sequence_gap, got %+v", rep.Errors)
	}
}

// A-N5 / A-Q2. Losing HEAD.yaml alone is a lost CACHE, not lost history.
func TestANegative5_LosingHeadAloneIsRecoverable(t *testing.T) {
	_, taskDir, store := chainOf(t, 3)
	if err := os.Remove(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
		t.Fatal(err)
	}
	rep, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Valid=%v entries=%d errors=%+v", rep.Valid, rep.EntryCount, rep.Errors)
	if !rep.Valid {
		t.Errorf("A-N5: losing HEAD alone must not be an integrity failure: %+v", rep.Errors)
	}
	if rep.EntryCount != 3 {
		t.Errorf("A-N5: entry count must come from the entries, not from HEAD: got %d", rep.EntryCount)
	}
	if _, err := store.ReconcileDerivedState(); err != nil {
		t.Errorf("A-N5: the repair must REBUILD a lost HEAD: %v", err)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "ledger", "HEAD.yaml")); err != nil {
		t.Errorf("A-N5: HEAD was not rebuilt: %v", err)
	}
}

// A-P1. A task with genuinely no chain initializes normally. This is the
// legitimate absence A-INV exists to protect.
func TestAPositive1_GenuinelyEmptyTaskIsNotAnError(t *testing.T) {
	root := t.TempDir()
	taskDir := filepath.Join(root, ".sensei", "tasks", "task.fresh")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(taskDir, WithPayloadValidator(testPayloadValidator))
	rep, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Valid || rep.EntryCount != 0 {
		t.Errorf("A-P1: a fresh task must verify clean with zero entries: Valid=%v entries=%d errors=%+v", rep.Valid, rep.EntryCount, rep.Errors)
	}
	if _, err := store.Append(context.Background(), AppendRequest{
		TaskID: "task.fresh", SessionID: "s", ExpectedHeadDigestSHA256: "",
		EventType:        closureprotocol.LedgerEventTaskPrepared,
		Payload:          testPayload{SchemaVersion: "1", Message: "first"},
		PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
		ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Errorf("A-P1: a fresh task must accept its first append: %v", err)
	}
}

// A-P2. THE DISCRIMINATOR. HEAD may legitimately LAG the entries by the in-flight
// entry; it may never LEAD them. A repair that refuses both breaks recovery, and
// one that permits both closes nothing.
func TestAPositive2_HeadLaggingRecoversButHeadLeadingIsRefused(t *testing.T) {
	t.Run("lagging is recoverable", func(t *testing.T) {
		_, taskDir, store := chainOf(t, 2)
		// Roll HEAD back to entry 1 while both entries remain: the exact
		// durable-entry-but-stale-HEAD condition append.go documents.
		chain, err := store.VerifyChain()
		if err != nil {
			t.Fatal(err)
		}
		first := chain.Entries[0]
		if err := writeHead(filepath.Join(taskDir, "ledger", "HEAD.yaml"), Head{
			SchemaVersion: HeadSchemaVersion, TaskID: chain.TaskID, Sequence: first.Entry.Sequence,
			EntryDigestSHA256: first.Entry.EntryDigestSHA256,
			EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(first.EntryPath))),
		}); err != nil {
			t.Fatal(err)
		}
		rep, err := store.Verify()
		if err != nil {
			t.Fatal(err)
		}
		if !rep.Valid {
			t.Errorf("A-P2: HEAD lagging the entries must stay recoverable: %+v", rep.Errors)
		}
		if _, err := store.ReconcileDerivedState(); err != nil {
			t.Errorf("A-P2: reconcile must repair a lagging HEAD: %v", err)
		}
	})

	t.Run("leading is an integrity failure", func(t *testing.T) {
		_, taskDir, store := chainOf(t, 3)
		chain, err := store.VerifyChain()
		if err != nil {
			t.Fatal(err)
		}
		last := chain.Entries[len(chain.Entries)-1]
		e := entryPaths(t, taskDir)
		if err := os.Remove(e[len(e)-1]); err != nil {
			t.Fatal(err)
		}
		// HEAD still names the entry that no longer exists: evidence was lost.
		if err := writeHead(filepath.Join(taskDir, "ledger", "HEAD.yaml"), Head{
			SchemaVersion: HeadSchemaVersion, TaskID: chain.TaskID, Sequence: last.Entry.Sequence,
			EntryDigestSHA256: last.Entry.EntryDigestSHA256,
			EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(last.EntryPath))),
		}); err != nil {
			t.Fatal(err)
		}
		rep, err := store.Verify()
		if err != nil {
			t.Fatal(err)
		}
		if rep.Valid {
			t.Error("A-P2: HEAD LEADING the entries means evidence was lost and must be an integrity failure")
		}
		if _, err := store.ReconcileDerivedState(); err == nil {
			t.Error("A-P2: reconcile must REFUSE to normalise a leading HEAD down to a shorter chain")
		}
	})
}

// ---------------------------------------------------------------------------
// GUARD ISOLATION.
//
// The contract cases above are satisfied by SEVERAL guards at once: a truncated
// chain trips both the witness comparison and the HEAD-direction rule, so a test
// that only asserts "refused" cannot tell which guard did it, and removing either
// one leaves the other covering. Each test below disables every guard but one.
// ---------------------------------------------------------------------------

// removeWitness simulates a chain that predates this repair, so the HEAD-direction
// rule is the ONLY thing that can object.
func removeWitness(t *testing.T, taskDir string) {
	t.Helper()
	if err := os.Remove(witnessPath(taskDir)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// A-Q1, first prohibition: the witness is NEVER LOWERED. An advance to a sequence
// at or below the recorded one is a no-op, not a correction.
func TestWitnessIsNeverLowered(t *testing.T) {
	_, taskDir, _ := chainOf(t, 3)
	before, ok, err := readWitness(taskDir)
	if err != nil || !ok {
		t.Fatalf("no witness after three appends: ok=%v err=%v", ok, err)
	}
	if before.HighestSequence != 3 {
		t.Fatalf("witness = %d, want 3", before.HighestSequence)
	}
	// Exactly what a truncated chain would try to write back.
	if err := advanceWitness(taskDir, before.TaskID, Head{Sequence: 1, EntryDigestSHA256: "shorter"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	after, _, err := readWitness(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if after.HighestSequence != 3 {
		t.Errorf("the witness was LOWERED to %d; a shorter chain must never rewrite the completeness claim", after.HighestSequence)
	}
	if after.HeadDigestSHA256 == "shorter" {
		t.Error("the witness adopted the shorter chain's head")
	}
}

// A-Q1, second prohibition: the witness must not be destroyed by the same act that
// destroys the evidence it guards.
func TestWitnessLivesOutsideTheTaskDirectory(t *testing.T) {
	_, taskDir, _ := chainOf(t, 2)
	wp := witnessPath(taskDir)
	rel, err := filepath.Rel(taskDir, wp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.ToSlash(rel), "../") {
		t.Errorf("the witness is INSIDE the task at %s; one rm -rf would take the evidence and its witness together", rel)
	}

	// Behavioural form: destroy the whole task directory, then recreate it empty.
	if err := os.RemoveAll(taskDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rep, err := NewStore(taskDir, WithPayloadValidator(testPayloadValidator)).Verify()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Valid {
		t.Error("a destroyed-and-recreated task verified clean; the witness did not survive the destruction")
	}
	if !hasError(rep, "ledger.history_truncated") {
		t.Errorf("expected ledger.history_truncated, got %+v", rep.Errors)
	}
}

// The HEAD-direction rule, ISOLATED: no witness exists, so only the direction rule
// can object to a projection naming an entry that is gone.
func TestHeadLeadingIsRefusedWithoutAnyWitness(t *testing.T) {
	_, taskDir, store := chainOf(t, 3)
	e := entryPaths(t, taskDir)
	if err := os.Remove(e[len(e)-1]); err != nil {
		t.Fatal(err)
	}
	removeWitness(t, taskDir) // a chain predating this repair
	rep, err := store.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if rep.Valid {
		t.Error("with no witness, a HEAD naming a deleted entry still verified clean")
	}
	if !hasError(rep, "ledger.head_leads_entries") {
		t.Errorf("expected ledger.head_leads_entries as the sole objection, got %+v", rep.Errors)
	}
}

// Reconcile must never repair derived state over lost history. The refusal is
// enforced by verification, not by a second check inside reconcile: a duplicate
// there could never fire, and a guard that cannot fire reads as protection while
// providing none. This test pins the BEHAVIOUR, so moving the enforcement is
// allowed and removing it is not.
func TestReconcileRefusesToNormaliseDownwardWithoutAnyWitness(t *testing.T) {
	_, taskDir, store := chainOf(t, 3)
	e := entryPaths(t, taskDir)
	if err := os.Remove(e[len(e)-1]); err != nil {
		t.Fatal(err)
	}
	removeWitness(t, taskDir) // a chain predating this repair: only the HEAD rule can object
	if _, err := store.ReconcileDerivedState(); err == nil {
		t.Fatal("reconcile rewrote HEAD down to the shorter chain; the truncation now has no trace")
	}
	// The lagging case must still be repairable, or the refusal is indiscriminate.
	_, lagDir, lagStore := chainOf(t, 2)
	chain, err := lagStore.VerifyChain()
	if err != nil {
		t.Fatal(err)
	}
	first := chain.Entries[0]
	if err := writeHead(filepath.Join(lagDir, "ledger", "HEAD.yaml"), Head{
		SchemaVersion: HeadSchemaVersion, TaskID: chain.TaskID, Sequence: first.Entry.Sequence,
		EntryDigestSHA256: first.Entry.EntryDigestSHA256,
		EntryPath:         filepath.ToSlash(filepath.Join("ledger", filepath.Base(first.EntryPath))),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := lagStore.ReconcileDerivedState(); err != nil {
		t.Errorf("a LAGGING HEAD must still be repairable: %v", err)
	}
}

// The structural/referential split itself. Misclassifying truncation as referential
// would let a chain that LOST HISTORY load for "diagnosis" -- reopening the hole.
func TestTruncationIsStructuralAndNeverMerelyDiagnosable(t *testing.T) {
	t.Run("a truncated chain does not load, even for diagnosis", func(t *testing.T) {
		_, taskDir, store := chainOf(t, 3)
		e := entryPaths(t, taskDir)
		if err := os.Remove(e[len(e)-1]); err != nil {
			t.Fatal(err)
		}
		chain, rep, err := store.VerifyChainForDiagnosisCtx(context.Background())
		if err == nil || len(chain.Entries) > 0 {
			t.Errorf("lost history loaded for diagnosis: err=%v entries=%d", err, len(chain.Entries))
		}
		if rep.Valid {
			t.Error("a truncated chain reported Valid")
		}
	})

	t.Run("a referentially damaged chain DOES load, and is still invalid", func(t *testing.T) {
		root := t.TempDir()
		taskDir := filepath.Join(root, ".sensei", "tasks", "task.ref")
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			t.Fatal(err)
		}
		store := NewStore(taskDir)
		ref, err := store.StoreArtifactBytes([]byte(`{"v":1}`), "application/json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Append(context.Background(), AppendRequest{
			TaskID: "task.ref", SessionID: "s", ExpectedHeadDigestSHA256: "",
			EventType: closureprotocol.LedgerEventAdmissionDecided,
			Payload: TaskEventPayload{
				SchemaVersion: EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventAdmissionDecided,
				TaskID: "task.ref", SessionID: "s",
				Artifacts: map[string]closureprotocol.LedgerPayloadRef{"admission_decision": ref},
			},
			PayloadMediaType: "application/yaml", ProducerID: "sensei.test",
			ProducedAt: time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(taskDir, filepath.FromSlash(ref.Path))); err != nil {
			t.Fatal(err)
		}
		chain, rep, err := store.VerifyChainForDiagnosisCtx(context.Background())
		if err != nil || len(chain.Entries) == 0 {
			t.Errorf("referential damage blocked diagnosis: err=%v entries=%d", err, len(chain.Entries))
		}
		if rep.Valid {
			t.Error("a referentially damaged chain must still be Valid=false; diagnosable is not trustworthy")
		}
	})
}
