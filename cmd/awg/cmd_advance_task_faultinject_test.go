// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package main

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// TestCLIAdvanceResultPostCommitRefusal is the CLI-level post-commit test. It
// runs only under sensei_faultinject (the non-shipping HEAD-write fault seam).
// The first invocation leaves a durable entry with HEAD unpublished: the CLI
// reports post_commit_incomplete with the committed identity + recovery action
// and exits 1.
//
// A later invocation of the same command used to reconcile and exit 0. It now
// refuses, because a HEAD that does not name the last entry is also what
// deleting the highest-sequence entry leaves behind, and rebuilding HEAD from
// the survivors would republish a truncated history as whole (issue #352). The
// evidence that separates the two is the durable-append identity from the call
// that made the append, and it is gone by the next process. The refusal is
// typed, non-zero, and leaves the ledger visibly damaged rather than repaired.
func TestCLIAdvanceResultPostCommitRefusal(t *testing.T) {
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

	// Exact retry after the fault clears: it cannot prove the unpublished HEAD
	// belongs to this append rather than to a deleted tail, so it refuses.
	ledger.InjectHeadWriteFaults(0)
	out2, code2 := captureAdvance(t, args)
	if code2 == 0 {
		t.Fatalf("retry exit 0: the CLI rebuilt an unpublished HEAD, which is the same operation that launders a deleted tail entry: %s", out2)
	}
	var o2 advanceResultOutput
	if err := json.Unmarshal([]byte(out2), &o2); err != nil {
		t.Fatal(err)
	}
	if o2.Outcome == "recorded" {
		t.Fatalf("retry outcome = recorded, want a refusal: %s", out2)
	}
	if o2.CurrentStateAvailable {
		t.Fatal("current state must be reported unavailable while the chain does not verify")
	}
	if o2.CurrentStateDetail == "" {
		t.Fatal("an unavailable current state must say why")
	}
	if o2.CorrectnessCertified {
		t.Fatal("correctness_certified must be false")
	}
}
