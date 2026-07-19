// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/changebinding"
	"github.com/globulario/sensei/internal/resulttestkit"
	"gopkg.in/yaml.v3"
)

const composeDomain = "github.com/globulario/sensei"

// composeWorld seeds a completion world, gives it a github origin, and returns the repo,
// task dir, and the trusted subject values (repo/head/base/task/session/completion-digest).
func composeWorld(t *testing.T) (repo, taskDir, taskID, sessionID, headSHA, baseSHA, completionDigest string) {
	t.Helper()
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", seed.Repo}, args...)...).Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("remote", "add", "origin", "git@github.com:globulario/sensei.git")
	head := git("rev-parse", "HEAD")
	base := strings.Repeat("b", 40)
	return seed.Repo, seed.TaskDir, seed.TaskID, seed.SessionID, head, base, strings.Repeat("c", 64)
}

// writeEventAndPolicy writes a pull_request event payload + a require_completion policy and
// returns their paths.
func writeEventAndPolicy(t *testing.T, head, base string) (eventPath, policyPath string) {
	t.Helper()
	dir := t.TempDir()
	eventPath = filepath.Join(dir, "event.json")
	ev := map[string]any{"number": 81, "pull_request": map[string]any{"base": map[string]any{"sha": base}, "head": map[string]any{"sha": head}}}
	data, _ := json.Marshal(ev)
	if err := os.WriteFile(eventPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	policyPath = filepath.Join(dir, "gate-policy.yaml")
	pol := "completion:\n  domains:\n    " + composeDomain + ":\n      mode: enforce\n      require_completion: true\n"
	if err := os.WriteFile(policyPath, []byte(pol), 0o644); err != nil {
		t.Fatal(err)
	}
	return eventPath, policyPath
}

func writeBinding(t *testing.T, path, repoID, changeID, head, base, taskDir, taskID, sessionID, completionDigest, issuer, tool string) {
	t.Helper()
	b := changebinding.ChangeTaskBinding{
		SchemaVersion:                changebinding.SchemaVersion,
		Repository:                   changebinding.RepositoryIdentity{Provider: "github", Identity: repoID},
		Change:                       changebinding.ChangeIdentity{Provider: "github", ID: changeID, HeadSHA: head, BaseSHA: base},
		Task:                         changebinding.TaskIdentity{Directory: taskDir, ID: taskID, SessionID: sessionID},
		CompletionResultDigestSHA256: completionDigest,
		Issuer:                       issuer,
		Publication:                  changebinding.PublicationIdentity{ID: "pub.compose"},
		Provenance:                   changebinding.Provenance{EventSource: "github_pull_request", Checkout: "actions_checkout_v4", Tool: tool, ToolVersion: "1.1.0"},
	}
	d, _ := changebinding.BindingDigest(b)
	b.DigestSHA256 = d
	data, _ := yaml.Marshal(b)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCompose(t *testing.T, repo, taskDir, taskID, sessionID, completionDigest, bindingPath, policyPath string, extra ...string) (int, combinedEnforceAudit) {
	t.Helper()
	var audit combinedEnforceAudit
	args := append([]string{
		"--completion", "--enforce", "--require-binding", "--json",
		"--repo-root", repo, "--task-dir", taskDir, "--domain", composeDomain,
		"--task-id", taskID, "--session-id", sessionID, "--completion-digest", completionDigest,
		"--binding", bindingPath, "--policy", policyPath,
	}, extra...)
	var code int
	out := captureStdout(t, func() { code = runGate(args) })
	if strings.TrimSpace(out) != "" {
		_ = json.Unmarshal([]byte(out), &audit)
	}
	return code, audit
}

// Binding accepted → the 9.4b completion evaluation runs; a not_completed world under
// require_completion blocks (completion stage).
func TestCompose_AcceptedBindingRunsCompletion(t *testing.T) {
	repo, taskDir, taskID, sessionID, head, base, cd := composeWorld(t)
	eventPath, policyPath := writeEventAndPolicy(t, head, base)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "globulario/sensei")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	bindingPath := filepath.Join(t.TempDir(), "binding.yaml")
	writeBinding(t, bindingPath, composeDomain, "81", head, base, taskDir, taskID, sessionID, cd, "sensei.ci", "sensei")

	code, audit := runCompose(t, repo, taskDir, taskID, sessionID, cd, bindingPath, policyPath)
	if audit.BindingValidity != "authoritative_binding_accepted" {
		t.Fatalf("binding must be accepted, got %s", audit.BindingValidity)
	}
	if !audit.CompletionRan || audit.CompletionResult != "block" {
		t.Fatalf("accepted binding must run completion (not_completed → block): %+v", audit)
	}
	if code != 1 || audit.FinalStage != "completion" {
		t.Fatalf("final must block at completion stage, exit 1; got exit %d %+v", code, audit)
	}
}

// A mismatched binding (wrong task-id in the subject vs the binding) blocks at the BINDING
// stage — completion never runs.
func TestCompose_MismatchedBindingBlocksBeforeCompletion(t *testing.T) {
	repo, taskDir, taskID, sessionID, head, base, cd := composeWorld(t)
	eventPath, policyPath := writeEventAndPolicy(t, head, base)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "globulario/sensei")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	bindingPath := filepath.Join(t.TempDir(), "binding.yaml")
	// The binding names a DIFFERENT task than the current subject.
	writeBinding(t, bindingPath, composeDomain, "81", head, base, taskDir, "task.other", sessionID, cd, "sensei.ci", "sensei")

	code, audit := runCompose(t, repo, taskDir, taskID, sessionID, cd, bindingPath, policyPath)
	if audit.BindingValidity != "binding_task_mismatch" {
		t.Fatalf("mismatched task must block at binding, got %s", audit.BindingValidity)
	}
	if audit.CompletionRan {
		t.Fatal("completion must NOT run when the binding is rejected")
	}
	if code != 1 || audit.FinalStage != "binding" {
		t.Fatalf("final must block at the binding stage, exit 1; got exit %d %+v", code, audit)
	}
}

// A missing binding artifact under require_completion blocks as absent (never degrades).
func TestCompose_AbsentBindingBlocks(t *testing.T) {
	repo, taskDir, taskID, sessionID, head, base, cd := composeWorld(t)
	eventPath, policyPath := writeEventAndPolicy(t, head, base)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "globulario/sensei")
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	code, audit := runCompose(t, repo, taskDir, taskID, sessionID, cd, filepath.Join(t.TempDir(), "nope.yaml"), policyPath)
	if audit.BindingValidity != "binding_absent" || audit.CompletionRan || code != 1 {
		t.Fatalf("absent required binding must block at binding stage: exit %d %+v", code, audit)
	}
}

// Missing mandatory 9.4c inputs → exit 2 (invocation error), never a silent pass.
func TestCompose_MissingMandatoryInputExits2(t *testing.T) {
	repo, taskDir, _, _, _, _, _ := composeWorld(t)
	_, policyPath := writeEventAndPolicy(t, strings.Repeat("a", 40), strings.Repeat("b", 40))
	code := runGate([]string{
		"--completion", "--enforce", "--require-binding",
		"--repo-root", repo, "--task-dir", taskDir, "--domain", composeDomain, "--policy", policyPath,
		"--binding", filepath.Join(t.TempDir(), "b.yaml"),
		// missing --task-id / --session-id / --completion-digest
	})
	if code != 2 {
		t.Fatalf("missing mandatory 9.4c inputs must exit 2, got %d", code)
	}
}
