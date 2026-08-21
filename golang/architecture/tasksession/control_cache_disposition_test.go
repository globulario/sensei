// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/identity"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/internal/resulttestkit"
)

var cacheGovernedPolicyFiles = []string{
	"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
	"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
}

func copyGovernedPolicyForCacheTest(t *testing.T, repo string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), "docs", "awareness")
	dst := filepath.Join(repo, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range cacheGovernedPolicyFiles {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// A cached control state was projected from the ledger as it stood when
// advance-task last ran. A dismissal recorded since then is invisible to it, so
// serving the cache shows an operator a demand the authority already
// terminated — the #230 defect one layer out, where the projection is correct
// and an older copy of it is returned instead.
func TestControlCacheIsNotServedAfterAGovernedDisposition(t *testing.T) {
	seeded, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Skipf("result seed unavailable: %v", err)
	}
	copyGovernedPolicyForCacheTest(t, seeded.Repo)
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(seeded.Repo), Now: time.Date(2026, 7, 16, 0, 1, 0, 0, time.UTC)}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	adv, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
		RepositoryRoot: seeded.Repo, TaskDirectory: seeded.TaskDir,
		RepositoryDomain: resulttestkit.Domain, ResultRevision: seeded.ResultRev,
	})
	if err != nil || adv.TransitionID == "" {
		t.Skipf("result transition unavailable: %v", err)
	}
	questions, err := qd.OpenQuestionsForLatestTransition(seeded.TaskDir)
	if err != nil || len(questions) == 0 {
		t.Skipf("seeded transition carries no architect question: %v", err)
	}

	if taskHasGovernedDisposition(seeded.TaskDir) {
		t.Fatal("no disposition has been recorded yet, but the cache is already treated as stale")
	}

	cand, err := qd.Prepare(qd.PrepareRequest{
		TaskDirectory: seeded.TaskDir, RepositoryRoot: seeded.Repo, IdentityRoot: identity.Root(seeded.Repo),
		QuestionID: questions[0].QuestionID, Disposition: qd.DispositionDismissed, Reusability: qd.ReusabilityNone,
		Rationale: "the architect decided no evidence will be sought for this question",
	})
	if err != nil {
		t.Fatalf("prepare dismissal: %v", err)
	}
	if _, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: seeded.TaskDir, Candidate: cand}); err != nil {
		t.Fatalf("record dismissal: %v", err)
	}

	if !taskHasGovernedDisposition(seeded.TaskDir) {
		t.Fatal("a dismissal is on the ledger, yet the cached control state would still be served")
	}
}

// A task with no ledger at all has no cached control state to serve either
// (control/latest.yaml cannot exist before the first advance), so an empty
// chain is not treated as staleness — it would rebuild anyway.
func TestAbsentLedgerIsNotTreatedAsStale(t *testing.T) {
	if taskHasGovernedDisposition(t.TempDir()) {
		t.Fatal("a task with no ledger was reported as carrying a disposition")
	}
}

// A ledger that exists but cannot be verified cannot prove the cache is
// current. The two errors are not symmetric: a needless rebuild costs time,
// while a wrongly served cache shows an operator a demand the authority has
// already terminated.
func TestCorruptLedgerInvalidatesTheControlCache(t *testing.T) {
	taskDir := t.TempDir()
	ledgerDir := filepath.Join(taskDir, "ledger")
	if err := os.MkdirAll(ledgerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ledgerDir, "HEAD.yaml"), []byte("not: [a, valid, head\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !taskHasGovernedDisposition(taskDir) {
		t.Fatal("an unverifiable ledger was treated as proof that the cache is current")
	}
}
