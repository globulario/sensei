// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package questiondisposition_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// TestDurableEntryHeadFailReconciles: the append's HEAD write fails once; the
// entry is durable and the same call reconciles HEAD and succeeds with exactly
// one event.
func TestDurableEntryHeadFailReconciles(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	ledger.InjectHeadWriteFaults(1)
	defer ledger.InjectHeadWriteFaults(0)
	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if res.Outcome != qd.OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded", res.Outcome)
	}
	if n := countDispositionEvents(t, env.TaskDir); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

// TestPostCommitErrorThenRetryRefuses: both the append and the recovery HEAD
// writes fail, yielding a PostCommitError carrying the durable entry identity.
// Clearing the fault and retrying the SAME candidate no longer recovers.
//
// It used to, by rebuilding HEAD from the entries that were there. That repair
// could not tell what it was repairing: an entry durable with HEAD unpublished
// and a chain whose highest-sequence entry was deleted are the same state on
// disk, and rebuilding HEAD over the second one republishes a truncated history
// as whole -- which is how a consumed admission came back as ready_for_mutation
// (issue #352). The evidence that separates them is the ErrEntryDurable identity
// the append itself produced, and it does not survive into a later call. So the
// retry refuses, the ledger stays invalid, and no second event is appended.
func TestPostCommitErrorThenRetryRefuses(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	ledger.InjectHeadWriteFaults(2)
	_, err = qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	var pce *qd.PostCommitError
	if !errors.As(err, &pce) {
		t.Fatalf("err = %v, want *qd.PostCommitError", err)
	}
	if pce.EntryDigestSHA256 == "" || pce.RecoveryAction == "" {
		t.Fatal("post-commit error missing durable identity/recovery")
	}
	ledger.InjectHeadWriteFaults(0)
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand}); err == nil {
		t.Fatal("a later call rebuilt an unpublished HEAD: it holds no evidence that the missing HEAD came from this append rather than from a deleted tail entry")
	}
	report, verr := ledger.NewStore(env.TaskDir).Verify()
	if verr != nil {
		t.Fatal(verr)
	}
	if report.Valid {
		t.Fatal("the refusal repaired the ledger on its way out")
	}
	if n := countDispositionEntryFiles(t, env.TaskDir); n != 1 {
		t.Fatalf("entry files = %d, want 1: the refused retry appended an event", n)
	}
}

// countDispositionEntryFiles counts disposition entries off the directory rather
// than off a verified chain, because this test runs against a ledger that
// deliberately does not verify.
func countDispositionEntryFiles(t *testing.T, taskDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(taskDir, "ledger"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), string(closureprotocol.LedgerEventQuestionDispositionRecorded)) {
			n++
		}
	}
	return n
}
