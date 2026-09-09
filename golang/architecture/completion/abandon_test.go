// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// seedWorldWithoutResult is a task that never recorded a result transition.
//
// seedWorld deliberately records one, because completion needs it. Abandonment is
// for the opposite case, and using seedWorld here was the defect Codex found: every
// abandonment test ran against a session that HAD produced a result, so nothing
// noticed the receipt claiming otherwise.
func seedWorldWithoutResult(t *testing.T) world {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       seedEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	copyGovernedPolicy(t, r.Repo)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: enrollNow}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	return world{Repo: r.Repo, TaskDir: r.TaskDir, IdentityRoot: identity.Root(r.Repo)}
}

// setActivePointer gives the seeded world an active-task pointer naming its task,
// which is the state a real repository is in while a task is live.
func setActivePointer(t *testing.T, w world, taskID string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(w.Repo, ".sensei", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := tasksession.WriteActivePointer(w.Repo, tasksession.ActivePointer{
		TaskID:                      taskID,
		RepositoryDomain:            resulttestkit.Domain,
		Revision:                    "0000000000000000000000000000000000000000",
		GraphDigestSHA256:           strings.Repeat("a", 64),
		LedgerPath:                  ".sensei/tasks/" + taskID + "/ledger",
		SessionPath:                 ".sensei/tasks/" + taskID + "/session.yaml",
		SessionDigestSHA256:         strings.Repeat("b", 64),
		LastTaskControlDigestSHA256: strings.Repeat("c", 64),
	}); err != nil {
		t.Fatalf("write active pointer: %v", err)
	}
}

func pointerExists(t *testing.T, repo string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(repo, ".sensei", "tasks", "active.yaml"))
	return err == nil
}

func taskID(t *testing.T, taskDir string) string {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	return chain.Entries[len(chain.Entries)-1].Entry.Task.ID
}

func abandonedEvents(t *testing.T, taskDir string) int {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	n := 0
	for _, e := range chain.Entries {
		if e.Entry.EventType == closureprotocol.LedgerEventAbandoned {
			n++
		}
	}
	return n
}

func abandon(t *testing.T, w world, head, reason string) AbandonResult {
	t.Helper()
	res, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: head, Reason: reason,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	return res
}

const whyAbandoned = "stage 3 landed outside this governed session through an ordinary PR"

// TestAbandonmentRecordsNoResult is the claim the whole transition exists to make.
//
// A task that stopped without producing anything must not end up with a record
// that a later reader can mistake for completion. The receipt has no field for a
// result, and it states the absence rather than leaving it to be inferred.
func TestAbandonmentRecordsNoResult(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)

	res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("outcome = %q (%s), want committed", res.Outcome, res.Detail)
	}
	if res.Receipt == nil {
		t.Fatal("committed with no receipt")
	}
	if res.Receipt.TerminalStatus != closureprotocol.TerminalAbandoned {
		t.Fatalf("terminal status = %q, want abandoned", res.Receipt.TerminalStatus)
	}
	if !res.Receipt.NoResultProduced {
		t.Fatal("receipt does not state that no result was produced")
	}
	if res.Receipt.Reason != whyAbandoned {
		t.Fatalf("reason = %q, want the caller's reason verbatim", res.Receipt.Reason)
	}
	// No result binding can appear in the durable event either: the payload has the
	// field, and an abandonment must leave it empty.
	chain, err := ledger.NewStore(w.TaskDir).VerifyChain()
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	last := chain.Entries[len(chain.Entries)-1]
	if last.Entry.EventType != closureprotocol.LedgerEventAbandoned {
		t.Fatalf("head event = %q, want abandoned", last.Entry.EventType)
	}
	data, rerr := ledger.ReadVerifiedPayload(last)
	if rerr != nil {
		t.Fatalf("payload: %v", rerr)
	}
	payload, perr := ledger.ParseTaskEventPayload(data)
	if perr != nil {
		t.Fatalf("parse payload: %v", perr)
	}
	if payload.ResultBinding != nil {
		t.Fatalf("the abandoned event carries a result binding %+v; the session produced none", *payload.ResultBinding)
	}
	if payload.Status != string(closureprotocol.TerminalAbandoned) {
		t.Fatalf("payload status = %q, want abandoned", payload.Status)
	}
}

// TestTheDurableRecordIsWrittenBeforeThePointerIsCleared pins the ordering.
//
// It is proved by making the SECOND step fail: if the pointer cannot be retired
// and the terminal record is nonetheless durable, the record cannot have been
// written after it. The reverse ordering would leave nothing behind here.
func TestTheDurableRecordIsWrittenBeforeThePointerIsCleared(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)

	// Make removal impossible without making the pointer unreadable: a read-only
	// parent directory refuses the unlink while the file still parses.
	tasksDir := filepath.Join(w.Repo, ".sensei", "tasks")
	if err := os.Chmod(tasksDir, 0o500); err != nil {
		t.Skipf("cannot make the tasks directory read-only here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(tasksDir, 0o755) })

	res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if res.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("outcome = %q, want integrity_failure when the pointer cannot be retired", res.Outcome)
	}
	if !strings.Contains(res.Detail, "rerun") {
		t.Fatalf("detail does not tell the caller the residue is resumable: %q", res.Detail)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d, want 1: the durable record must survive a failed pointer retirement", n)
	}
}

// TestAnInterruptedAbandonmentIsResumable is the crash-recovery requirement.
//
// The residue the ordering deliberately produces -- terminal record written,
// pointer still set -- must be finishable by rerunning the same command, not by a
// separate repair nobody would think to invoke.
func TestAnInterruptedAbandonmentIsResumable(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)

	tasksDir := filepath.Join(w.Repo, ".sensei", "tasks")
	if err := os.Chmod(tasksDir, 0o500); err != nil {
		t.Skipf("cannot make the tasks directory read-only here: %v", err)
	}
	first := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if first.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("setup: outcome = %q, want the interrupted residue", first.Outcome)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("setup: the pointer was cleared, so there is no interruption to resume")
	}

	// The interruption ends. Rerunning finishes the job.
	if err := os.Chmod(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	second := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if second.Outcome != OutcomeExactReplay {
		t.Fatalf("outcome = %q (%s), want exact_replay on resume", second.Outcome, second.Detail)
	}
	if !second.ActivePointerCleared {
		t.Fatal("the resume did not retire the pointer, so the interruption is not recoverable by rerunning")
	}
	if pointerExists(t, w.Repo) {
		t.Fatal("pointer still present after resume")
	}
	if n := abandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d, want 1: the resume must not write a second terminal record", n)
	}
}

// TestRepeatedAbandonmentIsIdempotent covers the ordinary replay, with no
// interruption involved.
func TestRepeatedAbandonmentIsIdempotent(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))

	if res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned); res.Outcome != OutcomeCommitted {
		t.Fatalf("first: %q (%s)", res.Outcome, res.Detail)
	}
	second := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if second.Outcome != OutcomeExactReplay {
		t.Fatalf("second: outcome = %q, want exact_replay", second.Outcome)
	}
	if second.ActivePointerCleared {
		t.Fatal("the replay reported clearing a pointer that was already retired")
	}
	if n := abandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d, want exactly 1", n)
	}
}

// TestAStaleExpectedHeadIsRefusedWithoutMutation is the stale-binding refusal.
func TestAStaleExpectedHeadIsRefusedWithoutMutation(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))

	res := abandon(t, w, strings.Repeat("f", 64), whyAbandoned)
	if res.Outcome != OutcomeStaleExpectedHead {
		t.Fatalf("outcome = %q, want stale_expected_head", res.Outcome)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("a refused abandonment wrote %d terminal record(s)", n)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("a refused abandonment retired the active pointer")
	}
}

// TestAnUnenrolledActorIsRefusedWithoutMutation is the authority requirement.
func TestAnUnenrolledActorIsRefusedWithoutMutation(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	head := currentHead(t, w.TaskDir)

	res, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir,
		IdentityRoot:                   filepath.Join(t.TempDir(), "no-identity-here"),
		ExpectedLedgerHeadDigestSHA256: head, Reason: whyAbandoned,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if res.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("outcome = %q, want authority_refusal", res.Outcome)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("an unauthorized abandonment wrote %d terminal record(s)", n)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("an unauthorized abandonment retired the active pointer")
	}
}

// TestAnEmptyReasonIsRefused: a terminal state with no stated cause is not a record.
func TestAnEmptyReasonIsRefused(t *testing.T) {
	w := seedWorldWithoutResult(t)
	for _, reason := range []string{"", "   ", "\t\n"} {
		res := abandon(t, w, currentHead(t, w.TaskDir), reason)
		if res.Outcome != OutcomeInputInvalid {
			t.Fatalf("reason %q: outcome = %q, want input_invalid", reason, res.Outcome)
		}
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("a reasonless abandonment wrote %d terminal record(s)", n)
	}
}

// TestWrongRepositoryIsRefusedWithoutMutation: the root and the task must name one
// world, or authority is resolved against one and the ledger written in another.
func TestWrongRepositoryIsRefusedWithoutMutation(t *testing.T) {
	w := seedWorldWithoutResult(t)
	other := seedWorldWithoutResult(t)
	head := currentHead(t, w.TaskDir)

	res, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: other.Repo, TaskDirectory: w.TaskDir, IdentityRoot: other.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: head, Reason: whyAbandoned,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if res.Outcome != OutcomeInputInvalid {
		t.Fatalf("outcome = %q, want input_invalid for a cross-repository request", res.Outcome)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("a cross-repository abandonment wrote %d terminal record(s)", n)
	}
}

// TestACompletedTaskCannotBeAbandoned: abandonment may not weaken a stronger
// terminal. This is a refusal, not a replay.
func TestACompletedTaskCannotBeAbandoned(t *testing.T) {
	w := seedWorld(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	head := w.ready(t)
	if res := w.complete(t, head); res.Outcome != OutcomeCommitted {
		t.Fatalf("setup: completion = %q (%s)", res.Outcome, res.Detail)
	}

	res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if res.Outcome != OutcomeConflictingCompletion {
		t.Fatalf("outcome = %q, want conflicting_completion", res.Outcome)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("abandoning a completed task wrote %d terminal record(s)", n)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("a refused abandonment retired the active pointer")
	}
}

// TestClearingRefusesAnotherTasksPointer: the transition that abandoned task A
// must not retire task B's pointer.
func TestClearingRefusesAnotherTasksPointer(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, "task.defect.someone-else")

	res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if res.Outcome != OutcomeCommitted {
		t.Fatalf("outcome = %q (%s), want committed", res.Outcome, res.Detail)
	}
	if res.ActivePointerCleared {
		t.Fatal("this task's abandonment retired another task's pointer")
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("another task's pointer was removed")
	}
	// And the owner refuses it directly, not only through the caller's guard.
	if err := tasksession.ClearActivePointer(w.Repo, taskID(t, w.TaskDir)); err == nil {
		t.Fatal("ClearActivePointer removed a pointer naming a different task")
	}
}

// TestClearingAnAbsentPointerSucceeds: absence is the goal state, so the resume
// path must not fail on a pointer somebody else already retired.
func TestClearingAnAbsentPointerSucceeds(t *testing.T) {
	w := seedWorldWithoutResult(t)
	if err := tasksession.ClearActivePointer(w.Repo, "task.defect.anything"); err != nil {
		t.Fatalf("clearing an absent pointer failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Reproductions of the five Codex findings on c96aced7. Each fails before the
// repair; the failure text is the reproduction.
// ---------------------------------------------------------------------------

// F1. TestAbandonmentIsRefusedWhenTheSessionProducedAResult
//
// A task that recorded a result transition HAS produced something. Stamping
// NoResultProduced on it writes a durable receipt that is simply false, and the
// falsehood is the one claim the receipt exists to make.
func TestAbandonmentIsRefusedWhenTheSessionProducedAResult(t *testing.T) {
	w := seedWorld(t) // seedWorld records a result transition
	setActivePointer(t, w, taskID(t, w.TaskDir))
	if _, ok := latestResultBinding(mustChain(t, w.TaskDir)); !ok {
		t.Fatal("setup: this world was supposed to have a result binding")
	}

	res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if res.Outcome != OutcomeConflictingCompletion {
		t.Fatalf("outcome = %q (%s), want a refusal: this session produced a result", res.Outcome, res.Detail)
	}
	if res.Receipt != nil {
		t.Fatalf("a receipt was written claiming no result was produced: %+v", *res.Receipt)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("abandoned events = %d, want 0: the refusal must not mutate", n)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("the refusal retired the active pointer")
	}
}

// F2. TestAnAbandonedLedgerReconstructsAsAbandoned
//
// classifyTerminalFacts counted only completed and revoked, so InspectTerminalState
// reported a successfully abandoned task as not_completed -- the state it had
// before anything happened to it.
func TestAnAbandonedLedgerReconstructsAsAbandoned(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	if res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned); res.Outcome != OutcomeCommitted {
		t.Fatalf("setup: %q (%s)", res.Outcome, res.Detail)
	}

	a, err := InspectTerminalState(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if a.State != TerminalAbandoned {
		t.Fatalf("terminal state = %q, want %q: an abandoned ledger must not reconstruct as untouched",
			a.State, TerminalAbandoned)
	}
}

// F2b. TestCompleteRefusesAnAbandonedTask
//
// PhaseAbandoned has no outgoing transitions. Completion that does not recognise
// the event would append a second, contradictory terminal.
func TestCompleteRefusesAnAbandonedTask(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	if res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned); res.Outcome != OutcomeCommitted {
		t.Fatalf("setup: %q (%s)", res.Outcome, res.Detail)
	}

	res, err := CompleteTask(context.Background(), CompleteRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: currentHead(t, w.TaskDir),
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if res.Outcome == OutcomeCommitted || res.Outcome == OutcomeExactReplay {
		t.Fatalf("completion outcome = %q: an abandoned task was completed, weakening a terminal", res.Outcome)
	}
	if res.Outcome != OutcomeConflictingCompletion {
		t.Fatalf("completion outcome = %q, want conflicting_completion", res.Outcome)
	}
}

// F3. TestReplayCleanupRequiresFreshnessAndAuthority
//
// The replay branch cleared the governed pointer before the expected-head check
// and before actor verification, so an unenrolled caller with an arbitrary head
// could finish the mutation on any interrupted task.
func TestReplayCleanupRequiresFreshnessAndAuthority(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)
	tasksDir := filepath.Join(w.Repo, ".sensei", "tasks")
	if err := os.Chmod(tasksDir, 0o500); err != nil {
		t.Skipf("cannot make the tasks directory read-only here: %v", err)
	}
	if res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned); res.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("setup: outcome = %q, want the interrupted residue", res.Outcome)
	}
	if err := os.Chmod(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("setup: nothing left to clean up")
	}

	// A stale head must not finish the cleanup.
	stale := abandon(t, w, strings.Repeat("e", 64), whyAbandoned)
	if stale.Outcome != OutcomeStaleExpectedHead {
		t.Fatalf("stale head: outcome = %q, want stale_expected_head", stale.Outcome)
	}
	if stale.ActivePointerCleared || !pointerExists(t, w.Repo) {
		t.Fatal("a stale-head caller completed the governed cleanup")
	}

	// Nor may an unenrolled actor.
	unenrolled, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir,
		IdentityRoot:                   filepath.Join(t.TempDir(), "no-identity-here"),
		ExpectedLedgerHeadDigestSHA256: currentHead(t, w.TaskDir), Reason: whyAbandoned,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if unenrolled.Outcome != OutcomeAuthorityRefusal {
		t.Fatalf("unenrolled: outcome = %q, want authority_refusal", unenrolled.Outcome)
	}
	if unenrolled.ActivePointerCleared || !pointerExists(t, w.Repo) {
		t.Fatal("an unenrolled actor completed the governed cleanup")
	}
}

// F4. TestAnInvalidAbandonmentReceiptIsAnIntegrityFailure
//
// A missing, unparseable or digest-mismatched receipt cannot reconstruct the
// reason or the actor. Reporting exact_replay and clearing the pointer blesses a
// broken event as terminal and removes the default route back to the task.
func TestAnInvalidAbandonmentReceiptIsAnIntegrityFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		damage func(t *testing.T, path string)
	}{
		{"missing", func(t *testing.T, p string) {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed", func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"digest-mismatched", func(t *testing.T, p string) {
			b, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, append(b, ' '), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := seedWorldWithoutResult(t)
			id := taskID(t, w.TaskDir)
			setActivePointer(t, w, id)
			first := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
			if first.Outcome != OutcomeCommitted {
				t.Fatalf("setup: %q (%s)", first.Outcome, first.Detail)
			}
			setActivePointer(t, w, id) // the pointer is what the replay must preserve
			tc.damage(t, filepath.Join(w.TaskDir, filepath.FromSlash(first.ReceiptPath)))

			res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
			if res.Outcome == OutcomeExactReplay || res.Outcome == OutcomeCommitted {
				t.Fatalf("outcome = %q: a %s receipt was accepted as a valid terminal", res.Outcome, tc.name)
			}
			if res.Outcome != OutcomeIntegrityFailure {
				t.Fatalf("outcome = %q, want integrity_failure for a %s receipt", res.Outcome, tc.name)
			}
			if res.ActivePointerCleared || !pointerExists(t, w.Repo) {
				t.Fatalf("a %s receipt caused the active pointer to be retired", tc.name)
			}
		})
	}
}

// F5. TestAnUnreadableActivePointerIsNotAbsence
//
// LoadActivePointer fails for absence AND for malformed, inaccessible or
// unsafe-path files. Treating every error as "nothing to retire" reports the
// abandonment committed while leaving a pointer that breaks active-task
// resolution afterwards.
func TestAnUnreadableActivePointerIsNotAbsence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(t *testing.T, path string)
	}{
		{"malformed yaml", func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("architecture_active_task: [this is not a mapping\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe session path", func(t *testing.T, p string) {
			body := "architecture_active_task:\n" +
				"    schema_version: \"1\"\n" +
				"    task_id: task.defect.whatever\n" +
				"    session_path: ../../escape/session.yaml\n" +
				"    session_digest_sha256: " + strings.Repeat("b", 64) + "\n"
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := seedWorldWithoutResult(t)
			pointer := filepath.Join(w.Repo, ".sensei", "tasks", "active.yaml")
			if err := os.MkdirAll(filepath.Dir(pointer), 0o755); err != nil {
				t.Fatal(err)
			}
			tc.corrupt(t, pointer)

			res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
			if res.Outcome == OutcomeCommitted {
				t.Fatalf("outcome = committed with an unreadable active pointer (%s); "+
					"the pointer survives and will break active-task resolution", tc.name)
			}
			if res.Outcome != OutcomeIntegrityFailure {
				t.Fatalf("outcome = %q, want integrity_failure for an unreadable pointer (%s)", res.Outcome, tc.name)
			}
			if _, err := os.Stat(pointer); err != nil {
				t.Fatalf("the unreadable pointer was removed anyway: %v", err)
			}
		})
	}
}

func mustChain(t *testing.T, taskDir string) ledger.VerifiedChain {
	t.Helper()
	chain, err := ledger.NewStore(taskDir).VerifyChain()
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	return chain
}

// ---------------------------------------------------------------------------
// Reproductions of the five Codex findings on 29a12987. Round two: three of the
// five are consequences of the round-one repairs, which is the useful part.
// ---------------------------------------------------------------------------

// R2-F1. An unreadable result transition is not evidence of no result.
//
// The round-one repair asked latestResultBinding, which answers "is there a
// current result binding". A result_transition_recorded event whose payload
// carries no binding makes that false while the durable transition fact stands,
// so the receipt claims the session produced nothing on a chain that records it
// producing something.
func TestAnUnreadableResultTransitionIsNotNoResult(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	w.appendEmptyResultTransition(t)

	res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if res.Outcome == OutcomeCommitted || res.Outcome == OutcomeExactReplay {
		t.Fatalf("outcome = %q: a recorded result transition whose binding cannot be read "+
			"was treated as proof that no result was produced", res.Outcome)
	}
	if res.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("outcome = %q, want integrity_failure: the transition fact exists and cannot be resolved", res.Outcome)
	}
	if n := abandonedEvents(t, w.TaskDir); n != 0 {
		t.Fatalf("abandoned events = %d, want 0", n)
	}
	if !pointerExists(t, w.Repo) {
		t.Fatal("the refusal retired the active pointer")
	}
}

// R2-F2. The canonical projection vocabulary must contain the state.
//
// Teaching the classifier a new terminal without adding it to the bound set left
// every abandoned task's projection failing its own canonical contract.
func TestAbandonedIsInTheCanonicalProjectionVocabulary(t *testing.T) {
	found := false
	for _, s := range AssessmentBoundStates() {
		if s == TerminalAbandoned {
			found = true
		}
	}
	if !found {
		t.Fatalf("AssessmentBoundStates() omits %q, so every abandoned projection is rejected as off-vocabulary: %v",
			TerminalAbandoned, AssessmentBoundStates())
	}
	if !validCompletionTerminalState(TerminalAbandoned) {
		t.Fatalf("validCompletionTerminalState(%q) = false", TerminalAbandoned)
	}
}

// R2-F2b. The real projection path, not just the intermediate classifier.
func TestAnAbandonedProjectionValidates(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	if res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned); res.Outcome != OutcomeCommitted {
		t.Fatalf("setup: %q (%s)", res.Outcome, res.Detail)
	}
	a, err := InspectTerminalState(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	// Built through the real producer, not hand-assembled: the point is that the
	// path task-status actually takes accepts an abandoned task.
	p, perr := BuildCompletionProjection(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if perr != nil {
		t.Fatalf("build projection: %v", perr)
	}
	if p.TerminalState != TerminalAbandoned {
		t.Fatalf("projection terminal state = %q, want %q", p.TerminalState, TerminalAbandoned)
	}
	if verr := ValidateCanonicalCompletionProjection(p); verr != nil {
		t.Fatalf("an abandoned task's canonical projection does not validate: %v", verr)
	}
	_ = a
}

// R2-F4. The stored receipt must carry its own digest.
//
// CanonicalJSON ran before ReceiptDigestSHA256 was assigned, so the durable
// artifact never contained the identity, and every replay reconstructed a
// receipt whose digest was empty while a fresh success reported one.
func TestTheStoredAbandonmentReceiptCarriesItsDigest(t *testing.T) {
	w := seedWorldWithoutResult(t)
	id := taskID(t, w.TaskDir)
	setActivePointer(t, w, id)

	fresh := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if fresh.Outcome != OutcomeCommitted || fresh.Receipt == nil {
		t.Fatalf("setup: %q (%s)", fresh.Outcome, fresh.Detail)
	}
	if strings.TrimSpace(fresh.Receipt.ReceiptDigestSHA256) == "" {
		t.Fatal("the fresh receipt reports no digest")
	}
	setActivePointer(t, w, id)
	replay := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned)
	if replay.Outcome != OutcomeExactReplay || replay.Receipt == nil {
		t.Fatalf("replay: %q (%s)", replay.Outcome, replay.Detail)
	}
	if got := strings.TrimSpace(replay.Receipt.ReceiptDigestSHA256); got == "" {
		t.Fatal("the replayed receipt has an empty digest: the stored artifact never carried its identity")
	}
	if replay.Receipt.ReceiptDigestSHA256 != fresh.Receipt.ReceiptDigestSHA256 {
		t.Fatalf("replay digest %q != fresh digest %q", replay.Receipt.ReceiptDigestSHA256, fresh.Receipt.ReceiptDigestSHA256)
	}
}

// R2-F5a. Substituted receipt bytes are refused by the content-addressed ref.
//
// This is what the end-to-end path actually enforces, and it is worth pinning
// separately: overwriting the artifact changes its sha256, and the ledger's
// payload ref no longer matches. It does NOT exercise the event/receipt binding
// -- see the unit test below, and the reply on that thread.
func TestSubstitutedAbandonmentReceiptBytesAreRefused(t *testing.T) {
	victim := seedWorldWithoutResult(t)
	other := seedWorldWithoutResult(t)
	vid := taskID(t, victim.TaskDir)
	setActivePointer(t, victim, vid)
	setActivePointer(t, other, taskID(t, other.TaskDir))

	v := abandon(t, victim, currentHead(t, victim.TaskDir), whyAbandoned)
	o := abandon(t, other, currentHead(t, other.TaskDir), "a different task stopped for its own reasons")
	if v.Outcome != OutcomeCommitted || o.Outcome != OutcomeCommitted {
		t.Fatalf("setup: victim=%q other=%q", v.Outcome, o.Outcome)
	}
	otherBytes, err := os.ReadFile(filepath.Join(other.TaskDir, filepath.FromSlash(o.ReceiptPath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(victim.TaskDir, filepath.FromSlash(v.ReceiptPath)), otherBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	setActivePointer(t, victim, vid)

	res := abandon(t, victim, currentHead(t, victim.TaskDir), whyAbandoned)
	if res.Outcome != OutcomeIntegrityFailure {
		t.Fatalf("outcome = %q, want integrity_failure for substituted receipt bytes", res.Outcome)
	}
	if res.ActivePointerCleared || !pointerExists(t, victim.Repo) {
		t.Fatal("substituted receipt bytes caused this task's pointer to be retired")
	}
	a, ierr := InspectTerminalState(context.Background(), Request{RepositoryRoot: victim.Repo, TaskDirectory: victim.TaskDir})
	if ierr != nil {
		t.Fatalf("inspect: %v", ierr)
	}
	if a.State == TerminalAbandoned {
		t.Fatal("InspectTerminalState reports a clean abandonment on substituted bytes")
	}
}

// R2-F5b. The event/receipt binding itself, tested where it is reachable.
//
// The end-to-end substitution above is stopped earlier, by the content-addressed
// ref. That makes the binding check defence-in-depth rather than the only guard
// -- so it is tested directly, because a guard whose failure mode is unreachable
// through the caller is exactly the kind that rots unnoticed.
func TestAbandonedEventMatchesRefusesAnotherTasksReceipt(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	if res := abandon(t, w, currentHead(t, w.TaskDir), whyAbandoned); res.Outcome != OutcomeCommitted {
		t.Fatalf("setup: %q", res.Outcome)
	}
	chain := mustChain(t, w.TaskDir)
	tf := classifyTerminalFacts(chain)
	if tf.abandonedCount != 1 {
		t.Fatalf("abandoned events = %d", tf.abandonedCount)
	}
	receipt, ref, err := loadAbandonmentReceipt(w.TaskDir, tf.abandoned)
	if err != nil {
		t.Fatalf("the honest receipt does not load: %v", err)
	}
	if merr := abandonedEventMatches(tf.abandoned, receipt, ref); merr != nil {
		t.Fatalf("the matching receipt was refused: %v", merr)
	}

	for _, tc := range []struct {
		name   string
		mutate func(r *closureprotocol.AbandonmentReceipt)
	}{
		{"another task", func(r *closureprotocol.AbandonmentReceipt) { r.Task.ID = "task.defect.someone-else" }},
		{"another session", func(r *closureprotocol.AbandonmentReceipt) { r.Task.SessionID = "session.elsewhere" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			foreign := receipt
			tc.mutate(&foreign)
			if merr := abandonedEventMatches(tf.abandoned, foreign, ref); merr == nil {
				t.Fatalf("a receipt for %s was accepted for this event", tc.name)
			}
		})
	}
	t.Run("no referenced digest", func(t *testing.T) {
		if merr := abandonedEventMatches(tf.abandoned, receipt, closureprotocol.LedgerPayloadRef{Path: ref.Path}); merr == nil {
			t.Fatal("an event referencing no receipt digest was accepted")
		}
	})
}
