// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// hashTaskDir is a content+path snapshot of every file under dir — the strongest
// available proof that a read-only surface mutated no authoritative task state.
func hashTaskDir(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		fmt.Fprintf(h, "%s\x00%x\n", filepath.ToSlash(rel), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatalf("hash task dir: %v", err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Available not_completed: reports availability + verdict + all three distinctions, exits
// 0, and mutates NOTHING — proven by a full file-content snapshot, the assessment digest,
// and the ledger head, not merely the terminal-state enum.
func TestGateCompletion_AdvisoryReportsVerdictAndZeroMutation(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ctx := context.Background()
	req := completion.Request{RepositoryRoot: seed.Repo, TaskDirectory: seed.TaskDir}
	beforeInspect, err := completion.InspectTerminalState(ctx, req)
	if err != nil {
		t.Fatalf("inspect before: %v", err)
	}
	beforeHash := hashTaskDir(t, seed.TaskDir)
	beforeLedger, err := ledger.VerifyTaskLedger(seed.TaskDir)
	if err != nil {
		t.Fatalf("ledger before: %v", err)
	}

	var code int
	out := captureStdout(t, func() {
		code = runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir})
	})
	if code != 0 {
		t.Fatalf("advisory completion gate must exit 0, got %d", code)
	}
	if !strings.Contains(out, "Availability: available") || !strings.Contains(out, "verdict=not_completed") {
		t.Fatalf("must report availability + verdict: %q", out)
	}
	env := completion.BuildCompletionProjectionEnvelope(ctx, req)
	if env.Projection == nil || len(env.Projection.Distinctions) != 3 {
		t.Fatalf("fixture must have 3 distinctions")
	}
	for _, d := range env.Projection.Distinctions {
		if !strings.Contains(out, d) {
			t.Fatalf("text must report each distinction verbatim; missing %q", d)
		}
	}

	// Zero authoritative mutation — hash snapshot + assessment digest + ledger head.
	if afterHash := hashTaskDir(t, seed.TaskDir); afterHash != beforeHash {
		t.Fatalf("task directory files changed: gate mutated authoritative state")
	}
	afterInspect, _ := completion.InspectTerminalState(ctx, req)
	if afterInspect.DigestSHA256 != beforeInspect.DigestSHA256 {
		t.Fatalf("assessment digest changed: %s -> %s", beforeInspect.DigestSHA256, afterInspect.DigestSHA256)
	}
	afterLedger, _ := ledger.VerifyTaskLedger(seed.TaskDir)
	if afterLedger.HeadDigestSHA256 != beforeLedger.HeadDigestSHA256 {
		t.Fatalf("ledger head changed: %s -> %s", beforeLedger.HeadDigestSHA256, afterLedger.HeadDigestSHA256)
	}
}

// JSON is the typed publication union with the nested available envelope, exact verdict,
// terminal state, authoritative boolean, and exactly three distinctions.
func TestGateCompletion_JSONNestedEnvelope(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := captureStdout(t, func() {
		if code := runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--json"}); code != 0 {
			t.Fatalf("json exit %d", code)
		}
	})
	var pub completion.CompletionProjectionPublication
	if err := json.Unmarshal([]byte(out), &pub); err != nil {
		t.Fatalf("json invalid: %v\n%s", err, out)
	}
	if pub.SchemaVersion != "completion.projection_publication/v1" || !pub.Canonical || pub.Envelope == nil {
		t.Fatalf("json must be the canonical publication union with an envelope: %+v", pub)
	}
	if pub.Envelope.Availability != completion.CompletionAvailable {
		t.Fatalf("availability = %s, want available", pub.Envelope.Availability)
	}
	p := pub.Envelope.Projection
	if p == nil {
		t.Fatal("available envelope must carry a projection")
	}
	if p.ClosureVerdict != completion.ClosureNotCompleted {
		t.Fatalf("verdict = %s, want not_completed", p.ClosureVerdict)
	}
	if p.TerminalState != completion.TerminalNotCompleted {
		t.Fatalf("terminal_state = %s, want not_completed", p.TerminalState)
	}
	if p.AuthoritativeCompletion {
		t.Fatal("authoritative_completion must be false for not_completed")
	}
	if len(p.Distinctions) != 3 {
		t.Fatalf("must carry exactly three distinctions, got %d", len(p.Distinctions))
	}
}

// SARIF is a single note-level result carrying all three distinctions in stable
// properties.
func TestGateCompletion_SARIFPreservesDistinctions(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	want := completion.BuildCompletionProjectionEnvelope(context.Background(), completion.Request{RepositoryRoot: seed.Repo, TaskDirectory: seed.TaskDir}).Projection.Distinctions

	sarif := filepath.Join(t.TempDir(), "c.sarif")
	if code := runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--sarif", sarif}); code != 0 {
		t.Fatalf("sarif exit %d", code)
	}
	data, err := os.ReadFile(sarif)
	if err != nil {
		t.Fatalf("read sarif: %v", err)
	}
	var log struct {
		Runs []struct {
			Results []struct {
				Level      string `json:"level"`
				Properties struct {
					Distinctions []string `json:"distinctions"`
				} `json:"properties"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatalf("sarif invalid: %v", err)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 {
		t.Fatalf("sarif must have exactly one result")
	}
	r := log.Runs[0].Results[0]
	if r.Level != "note" {
		t.Fatalf("advisory result level = %q, want note", r.Level)
	}
	if len(r.Properties.Distinctions) != 3 {
		t.Fatalf("sarif must preserve all three distinctions, got %d", len(r.Properties.Distinctions))
	}
	for _, d := range want {
		if !strings.Contains(string(data), d) {
			t.Fatalf("sarif dropped a distinction: %q", d)
		}
	}
}

// A canonical UNAVAILABLE envelope (owner could not establish the projection) reports its
// typed class and still exits 0.
func TestGateCompletion_UnavailableReportsClassAndExitsZero(t *testing.T) {
	a, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	b, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed b: %v", err)
	}
	// Repo A paired with repo B's task directory: the owner refuses (one-world law), so
	// the envelope is a typed unavailable projection_owner_identity_error (an identity
	// cause). Advisory still exits 0 and reports the class.
	var code int
	out := captureStdout(t, func() {
		code = runGate([]string{"--completion", "--repo-root", a.Repo, "--task-dir", b.TaskDir})
	})
	if code != 0 {
		t.Fatalf("unavailable must still exit 0 (advisory), got %d", code)
	}
	if !strings.Contains(out, "Availability: unavailable") || !strings.Contains(out, "projection_owner_identity_error") {
		t.Fatalf("must report the typed unavailable class: %q", out)
	}
}

// An unwritable SARIF path is a warning, not a gate failure — advisory still exits 0.
func TestGateCompletion_SARIFWriteFailureStaysAdvisory(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	badSarif := filepath.Join(t.TempDir(), "no-such-dir", "c.sarif") // parent does not exist
	if code := runGate([]string{"--completion", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--sarif", badSarif}); code != 0 {
		t.Fatalf("a SARIF write failure must stay advisory (exit 0), got %d", code)
	}
}

// 9.4b routing: enforce requests reach the enforce path (and, with no completion policy
// adopted, pass — enforcement never activates without opt-in); advisory requests are
// unchanged and still consult no policy.
func TestGateCompletion_EnforceRoutingAndPolicyGuards(t *testing.T) {
	repo := t.TempDir()
	td := filepath.Join(repo, ".sensei", "tasks", "task.x")
	if err := os.MkdirAll(td, 0o755); err != nil {
		t.Fatal(err)
	}
	base := []string{"--completion", "--repo-root", repo, "--task-dir", td}

	// Enforce variants with NO completion policy adopted → not enforced → exit 0.
	for _, extra := range [][]string{{"--enforce"}, {"--mode", "enforce"}, {"--mode", "block"}} {
		if code := runGate(append(append([]string{}, base...), extra...)); code != 0 {
			t.Fatalf("enforce with no adopted policy %v must pass (exit 0), got %d", extra, code)
		}
	}
	// Enforce with a missing EXPLICIT policy path is a loud config error (exit 2), never
	// silently treated as absent/advisory.
	if code := runGate(append(append([]string{}, base...), "--enforce", "--policy", filepath.Join(repo, "nope.yaml"))); code != 2 {
		t.Fatalf("enforce with a missing explicit policy must exit 2, got %d", code)
	}
	// Advisory (no --enforce) still rejects --policy — advisory consults no policy.
	if code := runGate(append(append([]string{}, base...), "--policy", "x.yaml")); code != 2 {
		t.Fatalf("advisory --completion --policy must exit 2, got %d", code)
	}
	// Advisory-shaped requests are unchanged (exit 0 on an evaluated outcome).
	for _, extra := range [][]string{{}, {"--report-only"}, {"--mode", "advisory"}} {
		if code := runGate(append(append([]string{}, base...), extra...)); code != 0 {
			t.Fatalf("advisory request %v must exit 0, got %d", extra, code)
		}
	}
}

func TestGateCompletion_RequiresTaskDir(t *testing.T) {
	if code := runGate([]string{"--completion", "--repo-root", t.TempDir()}); code != 2 {
		t.Fatalf("missing --task-dir must exit 2, got %d", code)
	}
}
