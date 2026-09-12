// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package tasksession

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// Scenario 6 — post-commit recovery. Requires the non-shipping HEAD-write fault
// seam (ledger.InjectHeadWriteFaults), so it is compiled only under the
// sensei_faultinject build tag and is absent from every normal build. Run:
//
//	go test -tags sensei_faultinject ./golang/architecture/tasksession/ -run TestE2EPostCommit
//
// The transition entry becomes durable but HEAD publication fails twice, so the
// orchestrator surfaces the committed identity and a recovery action instead of
// a false success.
//
// The retry used to finish the job by rebuilding HEAD from the entries on disk.
// It no longer does, and that is the repair for issue #352 arriving at the
// orchestrator. An unpublished HEAD and a deleted highest-sequence entry leave
// the same thing behind, so rebuilding HEAD from the survivors republishes a
// truncated history as whole and a consumed admission comes back. The evidence
// that tells the two apart is the ErrEntryDurable identity the append produced,
// and it does not survive into a later call. So: the current state is honestly
// UNAVAILABLE while the chain does not verify -- not guessed from the entries --
// and the retry refuses without appending a second event.
func TestE2EPostCommitRecoveryRefusesWithoutSecondEvent(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{})
	taskDir := r.TaskDir
	req := AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
	}

	// Fail the append's HEAD write AND the reconciliation's HEAD write.
	ledger.InjectHeadWriteFaults(2)
	defer ledger.InjectHeadWriteFaults(0)

	res, err := AdvanceResultTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("a post-commit condition must be a result, not a hard error: %v", err)
	}
	if res.Outcome != OutcomePostCommitIncomplete {
		t.Fatalf("outcome = %s, want post_commit_incomplete", res.Outcome)
	}
	if res.PostCommitEntryDigestSHA256 == "" || res.PostCommitRecoveryAction == "" {
		t.Fatal("post-commit must expose the committed entry identity and a recovery action")
	}
	// The durable entry is real, but HEAD was never published, so there is no
	// verified chain to reconstruct a current state from. Typed absence, not a
	// phase inferred from an unverifiable ledger.
	if res.CurrentStateAvailable {
		t.Fatalf("current state was reported from a ledger that does not verify: phase=%s status=%s", res.TaskPhase, res.OperationalStatus)
	}
	if res.CurrentStateDetail == "" {
		t.Fatal("an unavailable current state must say why")
	}
	if res.TaskPhase != "" || res.OperationalStatus != "" {
		t.Fatalf("phase/status must be left empty when current state is unavailable, got %s/%s", res.TaskPhase, res.OperationalStatus)
	}
	if e2eCountTransitionEntryFiles(t, taskDir) != 1 {
		t.Fatal("the durable entry must exist exactly once")
	}

	// Exact retry after the obstruction clears: it cannot prove the unpublished
	// HEAD belongs to this append rather than to a deleted tail, so it refuses.
	ledger.InjectHeadWriteFaults(0)
	retry, err := AdvanceResultTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.Outcome == OutcomeRecorded {
		t.Fatal("the retry rebuilt an unpublished HEAD; a truncated tail produces the same state and would be laundered the same way")
	}
	report, verr := ledger.NewStore(taskDir).Verify()
	if verr != nil {
		t.Fatal(verr)
	}
	if report.Valid {
		t.Fatal("the refusal repaired the ledger on its way out")
	}
	if e2eCountTransitionEntryFiles(t, taskDir) != 1 {
		t.Fatal("retry appended a second transition event")
	}
}

// e2eCountTransitionEntryFiles counts transition entries off the ledger
// directory rather than off a verified chain, because this scenario runs
// against a ledger that deliberately does not verify.
func e2eCountTransitionEntryFiles(t *testing.T, taskDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(taskDir, "ledger"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), string(closureprotocol.LedgerEventResultTransitionRecorded)) {
			n++
		}
	}
	return n
}
