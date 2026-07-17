// SPDX-License-Identifier: Apache-2.0

//go:build sensei_faultinject

package main

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// TestCLIAdvanceResultPostCommitRecovery is the CLI-level post-commit test. It runs
// only under sensei_faultinject (the non-shipping HEAD-write fault seam). The first
// invocation leaves a durable entry with an unreconciled HEAD: the CLI reports
// post_commit_incomplete with the committed identity + recovery action and exits 1.
// A later invocation of the exact same command reconciles and exits 0, appending no
// second transition event.
func TestCLIAdvanceResultPostCommitRecovery(t *testing.T) {
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-repo", r.Repo, "-task-dir", r.TaskDir, "-result-revision", r.ResultRev, "-format", "json"}

	ledger.InjectHeadWriteFaults(2)
	defer ledger.InjectHeadWriteFaults(0)

	out, code := captureAdvance(t, args)
	if code != 1 {
		t.Fatalf("post-commit exit %d, want 1: %s", code, out)
	}
	var o advanceResultOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.Outcome != "post_commit_incomplete" {
		t.Fatalf("outcome = %s, want post_commit_incomplete", o.Outcome)
	}
	if o.PostCommitRecoveryAction == "" || o.PostCommitEntryDigestSHA256 == "" {
		t.Fatal("post-commit must expose the recovery action and committed entry identity")
	}
	if o.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}

	// Exact retry after the fault clears: reconcile, exit 0.
	ledger.InjectHeadWriteFaults(0)
	out2, code2 := captureAdvance(t, args)
	if code2 != 0 {
		t.Fatalf("retry exit %d, want 0: %s", code2, out2)
	}
	var o2 advanceResultOutput
	if err := json.Unmarshal([]byte(out2), &o2); err != nil {
		t.Fatal(err)
	}
	if o2.Outcome != "recorded" {
		t.Fatalf("retry outcome = %s, want recorded", o2.Outcome)
	}
	if o2.TransitionDisposition == "recorded" {
		t.Fatal("retry must reconcile the durable entry, not perform a fresh record")
	}
}
