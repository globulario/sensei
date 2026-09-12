// SPDX-License-Identifier: AGPL-3.0-only

//go:build sensei_faultinject

package completion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"gopkg.in/yaml.v3"
)

// These tests need a post-commit HEAD publication fault. That seam exists ONLY
// under the sensei_faultinject build tag (ledger's faultinject.go), so it ships in
// no normal build; the tests are compiled under the same tag. Run:
//
//	go test -tags sensei_faultinject ./golang/architecture/completion/ -run HeadPublication

// A transient HEAD publication failure is recovered inside Store.Append, so
// abandonment completes on its ordinary path.
func TestAbandonmentCompletesThroughATransientHeadPublicationFault(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	before := currentHead(t, w.TaskDir)

	ledger.InjectHeadWriteFaults(ledger.HeadPublicationAttempts() - 1)
	defer ledger.InjectHeadWriteFaults(0)
	res := abandon(t, w, before, whyAbandoned)
	if n := ledger.PendingHeadWriteFaults(); n != 0 {
		t.Fatalf("%d faults never fired; the publication retry was not exercised", n)
	}
	if res.Outcome != OutcomeCommitted || !res.ActivePointerCleared || pointerExists(t, w.Repo) {
		t.Fatalf("outcome = %q (%s) cleared=%v: Append-owned recovery did not complete the abandonment",
			res.Outcome, res.Detail, res.ActivePointerCleared)
	}
	a, ierr := InspectTerminalState(context.Background(), Request{RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir})
	if ierr != nil || a.State != TerminalAbandoned {
		t.Fatalf("terminal state = %q err=%v, want abandoned", a.State, ierr)
	}
}

// Once Append exhausts its bounded retry, AbandonTask performs no recovery of its
// own: the entry is durable, the ledger fails closed, and the pointer stays.
func TestAbandonmentDoesNotRecoverAnUnpublishedHeadItself(t *testing.T) {
	w := seedWorldWithoutResult(t)
	setActivePointer(t, w, taskID(t, w.TaskDir))
	before := currentHead(t, w.TaskDir)

	ledger.InjectHeadWriteFaults(ledger.HeadPublicationAttempts())
	defer ledger.InjectHeadWriteFaults(0)
	res, err := AbandonTask(context.Background(), AbandonRequest{
		RepositoryRoot: w.Repo, TaskDirectory: w.TaskDir, IdentityRoot: w.IdentityRoot,
		ExpectedLedgerHeadDigestSHA256: before, Reason: whyAbandoned,
	})
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if n := ledger.PendingHeadWriteFaults(); n != 0 {
		t.Fatalf("%d faults never fired; the retry bound was not exhausted", n)
	}
	ledger.InjectHeadWriteFaults(0)

	if n := durableAbandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d, want 1: the fault must fire AFTER the entry is durable", n)
	}
	if res.Outcome != OutcomeIntegrityFailure || res.ActivePointerCleared || !pointerExists(t, w.Repo) {
		t.Fatalf("outcome = %q cleared=%v: a caller retired the pointer over an unpublished HEAD", res.Outcome, res.ActivePointerCleared)
	}
	if report, _ := ledger.NewStore(w.TaskDir).Verify(); report.Valid || report.HeadDigestSHA256 != before {
		t.Fatalf("ledger = %+v: want invalid, still publishing the pre-abandonment head", report)
	}

	// A caller retry cannot read through the unpublished HEAD either.
	retry := abandon(t, w, before, whyAbandoned)
	if retry.Outcome != OutcomeLedgerInvalid || !pointerExists(t, w.Repo) {
		t.Fatalf("retry = %q (%s): want ledger_invalid with the pointer in place", retry.Outcome, retry.Detail)
	}
	if n := durableAbandonedEvents(t, w.TaskDir); n != 1 {
		t.Fatalf("abandoned events = %d after retry, want exactly 1", n)
	}
}

// durableAbandonedEvents counts abandoned entry files WITHOUT verifying the chain,
// which is the only way to observe a durable entry whose HEAD was never published.
func durableAbandonedEvents(t *testing.T, taskDir string) int {
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
		if e.EventType == closureprotocol.LedgerEventAbandoned {
			n++
		}
	}
	return n
}
