// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// 9.4a: the advisory completion gate reports availability + the closure verdict + the
// three distinctions, exits 0, and mutates nothing.
func TestGateCompletion_AdvisoryReportsVerdictAndExitsZero(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := context.Background()
	req := completion.Request{RepositoryRoot: seed.Repo, TaskDirectory: seed.TaskDir}
	before, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect before: %v", err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir})
	})
	if code != 0 {
		t.Fatalf("advisory completion gate must exit 0, got %d", code)
	}
	if !strings.Contains(out, "Availability: available") || !strings.Contains(out, "verdict=not_completed") {
		t.Fatalf("must report availability + verdict verbatim: %q", out)
	}
	if !strings.Contains(out, "Distinctions:") {
		t.Fatalf("an available projection must report its three distinctions: %q", out)
	}
	// Read-only: authoritative state is untouched.
	after, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect after: %v", err)
	}
	if after.State != before.State {
		t.Fatalf("completion gate must mutate nothing; state %s -> %s", before.State, after.State)
	}
}

// The gate emits the canonical typed publication union as JSON and a note-level SARIF
// result (advisory never surfaces as an error/warning).
func TestGateCompletion_JSONAndSARIF(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := captureStdout(t, func() {
		if code := runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--json"}); code != 0 {
			t.Fatalf("json completion gate exit %d", code)
		}
	})
	var pub map[string]any
	if err := json.Unmarshal([]byte(out), &pub); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, out)
	}
	if pub["schema_version"] != "completion.projection_publication/v1" || pub["canonical"] != true {
		t.Fatalf("json must be the canonical publication union, got %v", pub)
	}

	sarif := filepath.Join(t.TempDir(), "completion.sarif")
	if code := runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--sarif", sarif}); code != 0 {
		t.Fatalf("sarif completion gate exit %d", code)
	}
	data, err := os.ReadFile(sarif)
	if err != nil {
		t.Fatalf("read sarif: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"level": "note"`) || !strings.Contains(s, "sensei.completion_gate.advisory") {
		t.Fatalf("sarif must be a single note-level advisory result:\n%s", s)
	}
}

func TestGateCompletion_RequiresTaskDir(t *testing.T) {
	if code := runGate([]string{"--completion", "--repo-root", t.TempDir()}); code != 2 {
		t.Fatalf("missing --task-dir must exit 2, got %d", code)
	}
}
