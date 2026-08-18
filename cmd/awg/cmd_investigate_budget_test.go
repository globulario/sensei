// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/extractbudget"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// The CLI is the surface an operator actually holds, so the enforcement has to
// be provable from there -- not only from the package that implements it. A
// limit that binds in a unit test and is dropped on the way through a flag is
// the same defect wearing a different hat.
func investigateFixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	write := func(rel, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/budgetfixture\n\ngo 1.22\n")
	write("alpha/alpha.go", "package alpha\n\n// Alpha is exported.\nfunc Alpha() int { return 1 }\n")
	write("omega/omega.go", "package omega\n\n// Omega is exported.\nfunc Omega() int { return 2 }\n")
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", "https://example.com/budgetfixture.git"},
		{"config", "user.email", "fixture@example.invalid"},
		{"config", "user.name", "fixture"},
		{"add", "-A"},
		{"commit", "-q", "-m", "base"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func runInvestigateHowToFile(t *testing.T, root string, extra ...string) investigation.Document {
	t.Helper()
	out := filepath.Join(t.TempDir(), "how.json")
	args := append([]string{
		"--repo", root,
		"--captured-at", "2026-08-17T00:00:00Z",
		"--out", out,
		"--format", "json",
	}, extra...)
	if code := runInvestigateHow(args); code != 0 {
		t.Fatalf("sensei investigate how exited %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc investigation.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	return doc
}

func TestInvestigateHowFileCeilingBindsThroughTheCLI(t *testing.T) {
	root := investigateFixtureRepo(t)

	full := runInvestigateHowToFile(t, root)
	if full.Receipt.ResourceBudget == nil {
		t.Fatal("no budget receipt on an unbounded run")
	}
	if full.Receipt.ResourceBudget.Status != extractbudget.StatusCompleted {
		t.Fatalf("unbounded status = %q", full.Receipt.ResourceBudget.Status)
	}
	if full.Receipt.ResourceBudget.Consumption.Files < 2 {
		t.Fatalf("fixture searched %d files; the ceiling below would not be meaningful", full.Receipt.ResourceBudget.Consumption.Files)
	}

	bounded := runInvestigateHowToFile(t, root, "--max-files", "1")
	rb := bounded.Receipt.ResourceBudget
	if rb == nil {
		t.Fatal("no budget receipt on a bounded run")
	}
	if rb.Status != extractbudget.StatusBudgetExhausted {
		t.Fatalf("status = %q, want budget_exhausted", rb.Status)
	}
	if rb.Consumption.Files != 1 {
		t.Errorf("--max-files 1 searched %d files", rb.Consumption.Files)
	}
	if rb.Budget.MaxFiles != 1 {
		t.Errorf("the flag did not reach the recorded budget: %+v", rb.Budget)
	}
	if len(bounded.Observations) >= len(full.Observations) {
		t.Errorf("a bound that reduced the searched set did not reduce the observations: %d vs %d",
			len(bounded.Observations), len(full.Observations))
	}
}

// A scope narrows what the document describes, and is recorded so a later
// reader can tell a deliberately narrow search from an exhausted one.
func TestInvestigateHowScopeFlagsBindThroughTheCLI(t *testing.T) {
	root := investigateFixtureRepo(t)
	doc := runInvestigateHowToFile(t, root, "--exclude", "omega")

	rb := doc.Receipt.ResourceBudget
	if rb == nil || len(rb.ExcludePaths) != 1 || rb.ExcludePaths[0] != "omega" {
		t.Fatalf("the scope in force was not recorded: %+v", rb)
	}
	for _, obs := range doc.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "omega/") {
			t.Fatalf("an excluded directory produced an observation: %s", obs.Evidence.SourceFile)
		}
	}
	var found bool
	for _, obs := range doc.Observations {
		if strings.HasPrefix(obs.Evidence.SourceFile, "alpha/") {
			found = true
		}
	}
	if !found {
		t.Error("the included directory produced nothing; the scope cut too much")
	}
}

// A budget that cannot be honoured as written is refused before any work,
// rather than normalized into one that searches somewhere else.
func TestInvestigateHowRefusesAnUnhonourableScope(t *testing.T) {
	root := investigateFixtureRepo(t)
	out := filepath.Join(t.TempDir(), "how.json")
	code := runInvestigateHow([]string{
		"--repo", root, "--captured-at", "2026-08-17T00:00:00Z",
		"--out", out, "--include", "/etc",
	})
	if code == 0 {
		t.Fatal("an absolute include scope was accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("a refused run still wrote an artifact")
	}
}
