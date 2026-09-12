// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package questiondisposition_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"gopkg.in/yaml.v3"
)

// TestDurableEntryHeadFailReconciles: the append's HEAD write fails once;
// Store.Append's bounded publication retry absorbs it under its lock and the call
// succeeds with exactly one event.
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

// TestPostCommitErrorDoesNotRepairHead: every HEAD publication attempt
// Store.Append makes fails, yielding a PostCommitError carrying the durable entry
// identity. Neither this call nor a retry of the SAME candidate repairs HEAD or
// reads the chain through it: HEAD publication recovery belongs to Store.Append
// alone (#352).
func TestPostCommitErrorDoesNotRepairHead(t *testing.T) {
	env := seedDisposable(t)
	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	ledger.InjectHeadWriteFaults(ledger.HeadPublicationAttempts())
	defer ledger.InjectHeadWriteFaults(0)
	_, err = qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	var pce *qd.PostCommitError
	if !errors.As(err, &pce) {
		t.Fatalf("err = %v, want *qd.PostCommitError", err)
	}
	if n := ledger.PendingHeadWriteFaults(); n != 0 {
		t.Fatalf("%d faults never fired; the retry bound was not exhausted", n)
	}
	if pce.EntryDigestSHA256 == "" || pce.RecoveryAction == "" {
		t.Fatal("post-commit error missing durable identity/recovery")
	}
	ledger.InjectHeadWriteFaults(0)
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand}); err == nil {
		t.Fatal("a retry read through the unpublished HEAD")
	}
	if report, _ := ledger.NewStore(env.TaskDir).Verify(); report.Valid {
		t.Fatal("HEAD was republished outside Store.Append")
	}
	if n := durableDispositionEvents(t, env.TaskDir); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

// durableDispositionEvents counts disposition entry files WITHOUT verifying the
// chain, which is the only way to observe a durable entry whose HEAD is unpublished.
func durableDispositionEvents(t *testing.T, taskDir string) int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(taskDir, "ledger", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range files {
		if filepath.Base(f) == "HEAD.yaml" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var e closureprotocol.LedgerEntry
		if err := yaml.Unmarshal(data, &e); err != nil {
			t.Fatal(err)
		}
		if e.EventType == closureprotocol.LedgerEventQuestionDispositionRecorded {
			n++
		}
	}
	return n
}
