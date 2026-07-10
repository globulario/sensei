// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor/coldsource"
)

// fakeOnboardLLM is a deterministic coldsource.LLMClient for the drafter tests —
// no network. Mirrors the coldsource package's own fakeLLM seam.
type fakeOnboardLLM struct {
	reply string
	err   error
}

func (f fakeOnboardLLM) Complete(_ context.Context, _ coldsource.LLMRequest) (string, error) {
	return f.reply, f.err
}

func TestOnboardImport_ValidatesAndRoutesToReviewQueue(t *testing.T) {
	root := t.TempDir()
	drafts := `[
	  {"kind":"invariant","title":"Config from store not env","source_files":["a.go"],"related_failures":["fm.env_override"]},
	  {"kind":"failure_mode","title":"vague, no contract"}
	]`
	dpath := filepath.Join(root, "drafts.json")
	if err := os.WriteFile(dpath, []byte(drafts), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := onboardImport(root, dpath); code != 0 {
		t.Fatalf("exit %d, want 0 (1 accepted, 1 rejected)", code)
	}
	// The valid invariant must land in the review queue.
	got, _ := filepath.Glob(filepath.Join(root, "docs", "awareness", "candidates", "proposals", "invariant.*.yaml"))
	if len(got) != 1 {
		t.Fatalf("want 1 invariant candidate written, got %v", got)
	}
	body, _ := os.ReadFile(got[0])
	if !strings.Contains(string(body), "status: awaiting_review") {
		t.Errorf("candidate not marked awaiting review:\n%s", body)
	}
	// The invalid failure_mode must NOT be written.
	bad, _ := filepath.Glob(filepath.Join(root, "docs", "awareness", "candidates", "proposals", "failure_mode.*.yaml"))
	if len(bad) != 0 {
		t.Errorf("invalid draft must not be written: %v", bad)
	}
}

func TestOnboardImport_AllInvalidReturnsNonZero(t *testing.T) {
	root := t.TempDir()
	dpath := filepath.Join(root, "drafts.json")
	os.WriteFile(dpath, []byte(`[{"kind":"failure_mode","title":"no contract"}]`), 0o644)
	if code := onboardImport(root, dpath); code == 0 {
		t.Error("all-invalid import should return non-zero")
	}
}

func TestOnboardExport_ContainsSchemaAndTask(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "brief.md")
	if code := onboardExport(root, out); code != 0 {
		t.Fatalf("export exit %d", code)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Architecture", "Candidate schema", "contract-first", "invariant", "Your task", "awg onboard import"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("brief missing %q", want)
		}
	}
}

// The --drafter llm path must route the model's output through the SAME
// contract-first validator + review queue as a human agent's import: the valid
// candidate lands as awaiting_review, the invalid one is dropped.
func TestOnboardDraft_LLM_WritesCandidates(t *testing.T) {
	root := t.TempDir()
	reply := `{"candidates":[
	  {"kind":"invariant","title":"Config from store not env","source_files":["a.go"],"related_failures":["fm.env_override"]},
	  {"kind":"failure_mode","title":"vague, no contract"}
	]}`
	if code := onboardDraftWith(root, fakeOnboardLLM{reply: reply}, 15); code != 0 {
		t.Fatalf("exit %d, want 0 (1 accepted, 1 rejected)", code)
	}
	got, _ := filepath.Glob(filepath.Join(root, "docs", "awareness", "candidates", "proposals", "invariant.*.yaml"))
	if len(got) != 1 {
		t.Fatalf("want 1 invariant candidate written, got %v", got)
	}
	body, _ := os.ReadFile(got[0])
	if !strings.Contains(string(body), "status: awaiting_review") {
		t.Errorf("candidate not marked awaiting review:\n%s", body)
	}
	bad, _ := filepath.Glob(filepath.Join(root, "docs", "awareness", "candidates", "proposals", "failure_mode.*.yaml"))
	if len(bad) != 0 {
		t.Errorf("invalid draft must not be written: %v", bad)
	}
}

// Fairness guardrail: --drafter llm with no credential fails clearly (exit 2)
// and writes nothing — never a silent fallback to the keyless export.
func TestOnboardDraft_NoKey_FailsClear(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	root := t.TempDir()
	if code := onboardDraft(root, "llm", "", 15); code != 2 {
		t.Fatalf("no-credential --drafter llm should exit 2, got %d", code)
	}
	got, _ := filepath.Glob(filepath.Join(root, "docs", "awareness", "candidates", "proposals", "*.yaml"))
	if len(got) != 0 {
		t.Errorf("nothing should be written on fail-clear: %v", got)
	}
}

func TestOnboardDraft_UnknownDrafter_FailsClear(t *testing.T) {
	if code := onboardDraft(t.TempDir(), "bogus", "", 15); code != 2 {
		t.Fatalf("unknown --drafter should exit 2, got %d", code)
	}
}

// onboardDraftWith surfaces a client/transport error as a non-zero exit and
// writes nothing.
func TestOnboardDraft_ClientErrorFailsClean(t *testing.T) {
	root := t.TempDir()
	if code := onboardDraftWith(root, fakeOnboardLLM{err: context.Canceled}, 15); code == 0 {
		t.Fatal("client error should return non-zero")
	}
	got, _ := filepath.Glob(filepath.Join(root, "docs", "awareness", "candidates", "proposals", "*.yaml"))
	if len(got) != 0 {
		t.Errorf("nothing should be written on client error: %v", got)
	}
}
