// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package main

import (
	"encoding/json"
	"testing"

	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// TestCLIAdvanceResultPostCommitDoesNotRepairHead is the CLI-level post-commit
// test. It runs only under sensei_faultinject (the non-shipping HEAD-write fault
// seam). Every HEAD publication attempt Store.Append makes fails, so the first
// invocation leaves a durable entry with an unpublished HEAD: the CLI reports
// post_commit_incomplete with the committed identity + recovery action and exits
// 1. A later invocation of the same command cannot read through that HEAD and does
// not exit 0: HEAD publication recovery belongs to Store.Append alone (#352).
func TestCLIAdvanceResultPostCommitDoesNotRepairHead(t *testing.T) {
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-repo", r.Repo, "-task-dir", r.TaskDir, "-result-revision", r.ResultRev, "-format", "json"}

	ledger.InjectHeadWriteFaults(ledger.HeadPublicationAttempts())
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

	// Retry after the fault clears: the unpublished HEAD is refused, not repaired.
	ledger.InjectHeadWriteFaults(0)
	out2, code2 := captureAdvance(t, args)
	if code2 == 0 {
		t.Fatalf("retry exit 0 over an unpublished HEAD: %s", out2)
	}
	if report, _ := ledger.NewStore(r.TaskDir).Verify(); report.Valid {
		t.Fatal("HEAD was republished outside Store.Append")
	}
}
