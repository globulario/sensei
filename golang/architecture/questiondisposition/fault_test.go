// SPDX-License-Identifier: Apache-2.0

//go:build sensei_faultinject

package questiondisposition_test

import (
	"context"
	"errors"
	"testing"

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

// TestPostCommitErrorThenRetryRecovers: both the append and reconcile HEAD writes
// fail, yielding a PostCommitError carrying the durable entry identity; clearing
// the fault and retrying the SAME candidate recovers with no second event.
func TestPostCommitErrorThenRetryRecovers(t *testing.T) {
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
	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if res.Outcome != qd.OutcomeReplayed && res.Outcome != qd.OutcomeReconciled {
		t.Fatalf("retry outcome = %s, want replayed/reconciled", res.Outcome)
	}
	if n := countDispositionEvents(t, env.TaskDir); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}
