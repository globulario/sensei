// SPDX-License-Identifier: AGPL-3.0-only

package main

// The wiring witness for the construction-confinement derivation.
//
// The derivation's own tests call Derive directly. That proves the analyzer and says
// nothing about whether the command a governed run actually reaches invokes it -- and
// this front has now produced seven findings of exactly that shape: a property proven in
// a helper while the caller bypassed it. sensei-code's coverage path shells out to
// `sensei derive` (internal/workflow/engine.go, senseiBinary + derived.AnchorsFor), so
// runDerive is the real entry point and is what these exercise.
//
// They assert the OUTCOME the CLI reports, in both directions, and the proposition
// sentence it prints: a refusal that came from a usage error rather than a counterexample
// would otherwise look identical from the exit status alone.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wireGoMod = "module example.com/m\n\ngo 1.22\n"

const wireOwner = `package exchange

import "time"

type Record struct {
	TaskID   string
	Deadline time.Time
}

func Open(id string, d time.Duration) *Record {
	return &Record{TaskID: id, Deadline: time.Now().Add(d)}
}
`

// wireRepo commits a fixture tree and returns its root. derive.NewGitSource reads the
// pinned revision out of git rather than the worktree, so the files must be committed.
func wireRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"add", "-A"},
		{"-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable in this environment: %v: %s", err, out)
		}
	}
	return root
}

// runDeriveCapturing runs the real command and returns its exit code with stdout.
func runDeriveCapturing(t *testing.T, args []string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	code := runDerive(args)
	os.Stdout = saved
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return code, sb.String()
}

func wireArgs(root string) []string {
	return []string{
		"--repo-root", root, "--repo", "example.com/m", "--revision", "HEAD",
		"--kind", "construction_confined_to_owner",
		"--dir", "exchange", "--type", "Record", "--field", "Deadline",
		"--search", "exchange", "--search", "transport",
	}
}

// THE WIRING CLAIM. An outsider minting a Deadline through an ELIDED literal must reach
// the command as a counterexample: exit 1, REFUTED, naming the site.
func TestTheDeriveCommandRefusesAnElidedConstructionOutsideTheOwner(t *testing.T) {
	root := wireRepo(t, map[string]string{
		"go.mod":               wireGoMod,
		"exchange/exchange.go": wireOwner,
		"transport/transport.go": `package transport

import (
	"time"

	"example.com/m/exchange"
)

func Forge() []exchange.Record { return []exchange.Record{{Deadline: time.Now()}} }
`,
	})
	code, out := runDeriveCapturing(t, wireArgs(root))
	if code != 1 {
		t.Fatalf("exit=%d, want 1 (REFUTED): the command did not carry the elided construction to a counterexample\n%s", code, out)
	}
	if !strings.Contains(out, "REFUTED") {
		t.Errorf("the command did not report REFUTED:\n%s", out)
	}
	if !strings.Contains(out, "transport/transport.go") {
		t.Errorf("the command did not name the construction site it refused on:\n%s", out)
	}
}

// The other direction, so the test above cannot pass because the command refuses
// everything. A zero-value construction outside the owner mints no authority and the
// command must still report DERIVED.
func TestTheDeriveCommandStillDerivesWhenNoOutsiderMintsTheField(t *testing.T) {
	root := wireRepo(t, map[string]string{
		"go.mod":               wireGoMod,
		"exchange/exchange.go": wireOwner,
		"transport/transport.go": `package transport

import "example.com/m/exchange"

func Zero() exchange.Record { var r exchange.Record; return r }
`,
	})
	code, out := runDeriveCapturing(t, wireArgs(root))
	if code != 0 {
		t.Fatalf("exit=%d, want 0 (DERIVED): a zero-value construction was treated as minting the field\n%s", code, out)
	}
	if !strings.Contains(out, "DERIVED") {
		t.Errorf("the command did not report DERIVED:\n%s", out)
	}
}

// The command must state the proposition it derived. It printed a lock-discipline
// sentence for this family until Proposition.String matched every kind by name.
func TestTheDeriveCommandStatesAConstructionPropositionNotALockOne(t *testing.T) {
	root := wireRepo(t, map[string]string{
		"go.mod":                 wireGoMod,
		"exchange/exchange.go":   wireOwner,
		"transport/transport.go": "package transport\n\nimport \"example.com/m/exchange\"\n\nfunc Use() *exchange.Record { return exchange.Open(\"t\", 0) }\n",
	})
	_, out := runDeriveCapturing(t, wireArgs(root))
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "proposition:") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("the command printed no proposition line:\n%s", out)
	}
	if !strings.Contains(line, "construction of Record initializing Deadline") {
		t.Errorf("the command states a proposition that is not the one derived: %q", line)
	}
	if strings.Contains(line, "is held") {
		t.Errorf("the command states a lock-discipline claim over a construction derivation: %q", line)
	}
}
