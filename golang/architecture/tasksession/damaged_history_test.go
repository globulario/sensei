// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// ledgerEntryFiles lists a task's chain entries, newest last.
func ledgerEntryFiles(t *testing.T, taskDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(taskDir, "ledger"))
	if err != nil {
		t.Fatalf("read ledger dir: %v", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "HEAD.yaml" || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		files = append(files, filepath.Join(taskDir, "ledger", e.Name()))
	}
	sort.Strings(files)
	return files
}

// a governed task carried to a SPENT mutation capability.
func spentCapabilityTask(t *testing.T) (string, string) {
	t.Helper()
	repo, taskDir := enrolledPreparedTask(t)
	decision := recordAdmissionDecision(t, taskDir, time.Now().UTC())
	recordCapabilityConsumption(t, taskDir, decision, time.Now().UTC().Add(time.Minute))
	rebindActivePointer(t, repo, taskDir)
	return repo, taskDir
}

// #352. TAIL TRUNCATION MUST NOT RESURRECT A SPENT MUTATION CAPABILITY.
//
// One file deleted -- the highest-sequence entry, admission_consumed -- and no tampering,
// forgery or privileged access. The truncated chain is a valid PREFIX, so every per-entry
// check passes; HEAD's disagreement was a WARNING, and report.Valid ignores warnings.
// foldGovernance then saw genuine ABSENCE of admission_consumed and re-granted the
// single-use capability.
func TestTruncatingTheChainDoesNotRegrantASpentCapability(t *testing.T) {
	repo, taskDir := spentCapabilityTask(t)

	before, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir})
	if err != nil {
		t.Skipf("advance-task unavailable in fixture: %v", err)
	}
	if before.Control.Permission.Modify == admission.CapabilityAdmitted {
		t.Skipf("fixture did not reach a spent capability (modify=%q)", before.Control.Permission.Modify)
	}

	files := ledgerEntryFiles(t, taskDir)
	if len(files) < 2 {
		t.Skipf("fixture chain too short to truncate (%d entries)", len(files))
	}
	if err := os.Remove(files[len(files)-1]); err != nil {
		t.Fatalf("truncate chain: %v", err)
	}
	rebindActivePointer(t, repo, taskDir)

	// THE ASSERTION IS THE REFUSAL, not the absence of a grant.
	//
	// Asserting only "modify != admitted" could not fail here: #351's replay repair already
	// returns replay_no_new_iteration on this path, so the truncated task reported modify
	// ="waiting" whether or not the chain was checked for length. That witness passed against
	// a build with the length check mutated away -- a control that cannot fail is not evidence,
	// and it is recorded here rather than quietly rewritten.
	//
	// What the length check actually buys is that the chain is refused BEFORE any fold reads
	// it, so no rule that moves a task to an earlier phase on absence is ever consulted.
	after, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir})
	if err == nil {
		t.Fatalf("#352: a truncated chain was ACCEPTED (modify=%q, disposition=%v); "+
			"the fold was allowed to read absence from a history that is merely incomplete",
			after.Control.Permission.Modify, after.Disposition)
	}
	if !strings.Contains(err.Error(), "invalid ledger chain") {
		t.Errorf("#352: truncation was refused with %v, not as an unverifiable chain", err)
	}
	if after.Control.Permission.Modify == admission.CapabilityAdmitted {
		t.Fatalf("#352: deleting one entry regranted a SPENT mutation capability (modify=%q)",
			after.Control.Permission.Modify)
	}
}

// #353. DESTROYING THE HISTORY MUST NOT ERASE WHAT IT RECORDED.
//
// `rm -rf <taskDir>/ledger` leaves listLedgerEntryFiles returning (nil, nil): the chain
// verifies as valid with zero entries, indistinguishable from a task that has no chain yet.
// The terminal is not bypassed, it is REMOVED, and the task returns to waiting_governance
// where fresh authority can be issued.
func TestDestroyingTheLedgerDoesNotReturnTheTaskToWaitingGovernance(t *testing.T) {
	repo, taskDir := spentCapabilityTask(t)
	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir}); err != nil {
		t.Skipf("advance-task unavailable in fixture: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(taskDir, "ledger")); err != nil {
		t.Fatalf("destroy ledger: %v", err)
	}
	rebindActivePointer(t, repo, taskDir)

	after, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir})
	if err != nil {
		return // refusing is the correct outcome
	}
	if after.Control.Permission.Modify == admission.CapabilityAdmitted {
		t.Fatalf("#353: destroying the ledger produced a GRANTED capability (modify=%q); "+
			"earlier authority was reconstructed from a history whose completeness nothing established",
			after.Control.Permission.Modify)
	}
	t.Fatalf("#353: a task whose history was destroyed still reported a state (%+v) instead of "+
		"refusing; absence is indistinguishable from destruction", after.Disposition)
}

// THE NEGATIVE CONTROL. An intact governed task still advances, or the two refusals above
// would be satisfied by refusing every task.
func TestAnIntactGovernedTaskStillAdvances(t *testing.T) {
	repo, taskDir := enrolledPreparedTask(t)
	recordAdmissionDecision(t, taskDir, time.Now().UTC())
	rebindActivePointer(t, repo, taskDir)

	if _, err := AdvanceTask(AdvanceTaskOptions{RepoRoot: repo, TaskDir: taskDir}); err != nil {
		t.Fatalf("an intact governed task was refused: %v", err)
	}
}

// #354. AN APPENDED ARTIFACT-LESS EVENT MUST NOT HIDE AN EXISTING RECORD.
//
// Nothing is deleted. The chain stays complete, valid and append-only as designed, so this
// survives any integrity anchor that establishes history COMPLETENESS -- and it needs only
// APPEND rights, not delete rights. Two predicates were conflated:
//
//	latest event of a type   treated as   the current fact
//	a malformed record       treated as   an absent record
//
// latestArtifactFromChain scanned backwards for the latest event of the type and returned
// (false, nil) -- "not found, no error" -- the moment that event lacked the artifact key,
// never consulting the earlier event that carries the real record. ValidateTaskEventPayload
// required an artifact for only 2 of 19 event types, and admission_consumed was not one, so
// such an event could be appended at all.
func TestAnArtifactLessAdmissionConsumedCannotBeAppended(t *testing.T) {
	repo, taskDir := spentCapabilityTask(t)
	_ = repo

	before, err := admission.LoadRecordedConsumption(taskDir)
	if err != nil {
		t.Skipf("fixture has no recorded consumption to hide: %v", err)
	}
	if strings.TrimSpace(before.CapabilityID) == "" {
		t.Skip("fixture consumption carries no capability id")
	}

	// Append a SECOND admission_consumed carrying no artifact at all.
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	appendErr := appendArtifactLessConsumed(t, taskDir, head, before.Task.ID, before.Task.SessionID)
	if appendErr == nil {
		// If the write is permitted, the read must still not lose the record.
		after, readErr := admission.LoadRecordedConsumption(taskDir)
		if readErr == nil && strings.TrimSpace(after.CapabilityID) == strings.TrimSpace(before.CapabilityID) {
			return // the earlier record still answers; acceptable
		}
		t.Fatalf("#354: an appended artifact-less admission_consumed hid the recorded consumption "+
			"(before=%q after=%q err=%v); a malformed record was read as an absent one",
			before.CapabilityID, after.CapabilityID, readErr)
	}
	// Refusing the append is the stronger repair: the record can never be shadowed.
	if !strings.Contains(appendErr.Error(), "capability_consumption") {
		t.Errorf("#354: the append was refused with %v, which does not name the artifact the event "+
			"contract requires", appendErr)
	}

	// And the real record still reads back, or the refusal would have been bought by breaking
	// the ordinary path.
	after, err := admission.LoadRecordedConsumption(taskDir)
	if err != nil {
		t.Fatalf("#354: the recorded consumption became unreadable after a REFUSED append: %v", err)
	}
	if strings.TrimSpace(after.CapabilityID) != strings.TrimSpace(before.CapabilityID) {
		t.Errorf("#354: capability id changed from %q to %q", before.CapabilityID, after.CapabilityID)
	}
}

// appendArtifactLessConsumed appends an admission_consumed event whose payload carries NO
// artifacts, the way an append-only adversary would.
func appendArtifactLessConsumed(t *testing.T, taskDir, head, taskID, sessionID string) error {
	t.Helper()
	payload := ledger.TaskEventPayload{
		SchemaVersion: ledger.EventPayloadSchemaVersion,
		EventType:     closureprotocol.LedgerEventAdmissionConsumed,
		TaskID:        taskID,
		SessionID:     sessionID,
	}
	_, err := taskLedgerStore(taskDir).Append(context.Background(), ledger.AppendRequest{
		TaskID:                   taskID,
		SessionID:                sessionID,
		ExpectedHeadDigestSHA256: head,
		EventType:                closureprotocol.LedgerEventAdmissionConsumed,
		Payload:                  payload,
		PayloadMediaType:         "application/yaml",
		ProducerID:               "sensei.test.adversary",
		ProducedAt:               time.Now().UTC().Add(2 * time.Minute),
	})
	return err
}
