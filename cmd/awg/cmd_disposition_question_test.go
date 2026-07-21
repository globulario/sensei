// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/internal/resulttestkit"
)

var (
	dispSeedEpoch = time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	dispEnrollNow = time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
)

func dispModuleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

type dispCLIEnv struct {
	Repo       string
	TaskDir    string
	QuestionID string
}

// seedDispositionCLI seeds a full task through result_transition_recorded with a
// real architect question, copies the governed authority policy, and enrolls a
// locally-trusted identity — the setup a real `disposition-question` run needs.
func seedDispositionCLI(t *testing.T) dispCLIEnv {
	t.Helper()
	r, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{
		Direction:   "evolve",
		Epoch:       dispSeedEpoch,
		ResultFiles: map[string]string{"src/model.go": "package src\n\n// evolve\nfunc Publish() {}\n"},
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := filepath.Join(dispModuleRoot(t), "docs", "awareness")
	dst := filepath.Join(r.Repo, "docs", "awareness")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"actor_roles.yaml", "mutation_paths.yaml", "observation_paths.yaml",
		"delegation_policies.yaml", "authority_grants.yaml", "authority_domains.yaml",
	} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := identity.Enroll(identity.EnrollOptions{Root: identity.Root(r.Repo), Now: dispEnrollNow}); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	adv, err := tasksession.AdvanceResultTransition(context.Background(), tasksession.AdvanceResultRequest{
		RepositoryRoot: r.Repo, TaskDirectory: r.TaskDir,
		RepositoryDomain: resulttestkit.Domain, ResultRevision: r.ResultRev,
	})
	if err != nil || adv.TransitionID == "" {
		t.Fatalf("advance: %v (outcome %s)", err, adv.Outcome)
	}
	return dispCLIEnv{Repo: r.Repo, TaskDir: r.TaskDir}
}

func captureDisposition(t *testing.T, args []string) (string, int) {
	t.Helper()
	old := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = pw
	code := runDispositionQuestion(args)
	_ = pw.Close()
	os.Stdout = old
	out, _ := io.ReadAll(pr)
	return string(out), code
}

func firstQuestionID(t *testing.T, env dispCLIEnv) string {
	t.Helper()
	out, code := captureDisposition(t, []string{"-repo", env.Repo, "-task-dir", env.TaskDir, "-list", "-format", "json"})
	if code != 0 {
		t.Fatalf("list exit %d: %s", code, out)
	}
	var qs []struct {
		QuestionID string `json:"question_id"`
	}
	if err := json.Unmarshal([]byte(out), &qs); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(qs) == 0 {
		t.Fatal("no questions listed")
	}
	return qs[0].QuestionID
}

// TestCLIDispositionHappyPath: `disposition-question` records an answered+reusable
// disposition and reports promote_reusable; exit 0, correctness_certified:false.
func TestCLIDispositionHappyPath(t *testing.T) {
	env := seedDispositionCLI(t)
	qid := firstQuestionID(t, env)
	answerFile := filepath.Join(t.TempDir(), "answer.txt")
	if err := os.WriteFile(answerFile, []byte("the intended basis is X"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := captureDisposition(t, []string{
		"-repo", env.Repo, "-task-dir", env.TaskDir, "-question", qid,
		"-disposition", "answered", "-reusability", "reusable_candidate",
		"-rationale", "the intended basis is X", "-answer-id", "answer.1",
		"-answer-file", answerFile, "-format", "json",
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	var o dispositionOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.Outcome != "recorded" {
		t.Fatalf("outcome = %s, want recorded", o.Outcome)
	}
	if o.NextAction != "promote_reusable" {
		t.Fatalf("next = %s, want promote_reusable", o.NextAction)
	}
	if o.EntryDigestSHA256 == "" || o.CorrectnessCertified {
		t.Fatalf("bad render: entry=%q certified=%v", o.EntryDigestSHA256, o.CorrectnessCertified)
	}
}

// TestCLIDispositionUnauthorizedFailsClosed: an unenrolled identity root yields a
// fail-closed refusal; exit 3.
func TestCLIDispositionUnauthorizedFailsClosed(t *testing.T) {
	env := seedDispositionCLI(t)
	qid := firstQuestionID(t, env)
	out, code := captureDisposition(t, []string{
		"-repo", env.Repo, "-task-dir", env.TaskDir, "-identity-root", t.TempDir(),
		"-question", qid, "-disposition", "answered", "-reusability", "reusable_candidate",
		"-rationale", "x", "-answer-id", "a", "-format", "json",
	})
	if code != 3 {
		t.Fatalf("exit = %d, want 3\n%s", code, out)
	}
	var o dispositionOutput
	if err := json.Unmarshal([]byte(out), &o); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if o.RefusalCode == "" || o.CorrectnessCertified {
		t.Fatalf("expected refusal render, got %+v", o)
	}
}
