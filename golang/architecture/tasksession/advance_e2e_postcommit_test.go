// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package tasksession

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
	"gopkg.in/yaml.v3"
)

// Scenario 6 — post-commit. Requires the non-shipping HEAD-write fault seam
// (ledger.InjectHeadWriteFaults), so it is compiled only under the
// sensei_faultinject build tag and is absent from every normal build. Run:
//
//	go test -tags sensei_faultinject ./golang/architecture/tasksession/ -run TestE2EPostCommit
//
// Every HEAD publication attempt Store.Append makes fails, so the transition entry
// is durable and HEAD is unpublished. The orchestrator surfaces the committed
// identity instead of a false success, and neither it nor a retry reconstructs
// state through the unpublished HEAD or repairs it: HEAD publication recovery
// belongs to Store.Append alone (#352).
func TestE2EPostCommitSurfacesTheDurableEntryWithoutRepairingHead(t *testing.T) {
	r := e2eSeed(t, resulttestkit.Options{})
	taskDir := r.TaskDir
	req := AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir, RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
	}

	ledger.InjectHeadWriteFaults(ledger.HeadPublicationAttempts())
	defer ledger.InjectHeadWriteFaults(0)

	res, err := AdvanceResultTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("a post-commit condition must be a result, not a hard error: %v", err)
	}
	if n := ledger.PendingHeadWriteFaults(); n != 0 {
		t.Fatalf("%d faults never fired; the retry bound was not exhausted", n)
	}
	if res.Outcome != OutcomePostCommitIncomplete {
		t.Fatalf("outcome = %s, want post_commit_incomplete", res.Outcome)
	}
	if res.PostCommitEntryDigestSHA256 == "" || res.PostCommitRecoveryAction == "" {
		t.Fatal("post-commit must expose the committed entry identity and a recovery action")
	}
	if res.CurrentStateAvailable {
		t.Fatalf("current state (phase %s) was reconstructed through an unpublished HEAD", res.TaskPhase)
	}
	if e2eCountTransitions(e2eDurableEvents(t, taskDir)) != 1 {
		t.Fatal("the durable entry must exist exactly once")
	}

	ledger.InjectHeadWriteFaults(0)
	retry, err := AdvanceResultTransition(context.Background(), req)
	if err == nil && retry.Outcome == OutcomeRecorded {
		t.Fatal("a retry read through the unpublished HEAD")
	}
	if report, _ := ledger.NewStore(taskDir).Verify(); report.Valid {
		t.Fatal("HEAD was republished outside Store.Append")
	}
	if e2eCountTransitions(e2eDurableEvents(t, taskDir)) != 1 {
		t.Fatal("retry appended a second transition event")
	}
}

// e2eDurableEvents lists entry event types WITHOUT verifying the chain, which is
// the only way to observe a durable entry whose HEAD is unpublished.
func e2eDurableEvents(t *testing.T, taskDir string) []closureprotocol.LedgerEventType {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(taskDir, "ledger", "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var out []closureprotocol.LedgerEventType
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
		out = append(out, e.EventType)
	}
	return out
}
