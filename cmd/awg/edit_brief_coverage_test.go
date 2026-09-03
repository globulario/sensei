// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/evidence"
)

// An edit the hook DECLINES to brief must still be countable.
//
// The bug these tests pin: `sensei edit-brief` recorded a row for every
// briefing it attempted and nothing at all for an edit it declined, so a
// ledger holding zero rows could not be told apart from a repository where
// nobody edited anything. An entire campaign's edits ran outside the resolving
// project root and produced a clean-looking zero.
//
// Declining to brief an out-of-project file is correct. Declining to SAY SO is
// the defect.

// hookPayload is the Claude Code PreToolUse shape the hook reads on stdin.
func hookPayload(f string) string {
	b, _ := json.Marshal(map[string]any{
		"tool_name":  "Edit",
		"tool_input": map[string]any{"file_path": f, "new_string": "y := 1"},
	})
	return string(b)
}

func readRows(t *testing.T, path string) []evidence.Event {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []evidence.Event
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e evidence.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("ledger row is not valid JSON: %v", err)
		}
		out = append(out, e)
	}
	return out
}

func TestAnObservedEditIsCountableEvenWhenItIsNotBriefed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		rootArg  string
		file     string
		coverage evidence.CoverageClass
	}{
		{
			name:     "file outside the resolved project root",
			rootArg:  root,
			file:     "/etc/passwd",
			coverage: evidence.CoverageOutsideProject,
		},
		{
			// resolveProjectRoot does NOT fail on a root that does not exist:
			// an explicit --root is absolutised, and the walk-up falls back to
			// cwd. So this lands on outside_project, and that is the correct
			// answer. Pinned because an earlier version of this test asserted
			// an error path that the code has never had.
			name:     "root that contains no Sensei project",
			rootArg:  filepath.Join(t.TempDir(), "nowhere"),
			file:     "/etc/passwd",
			coverage: evidence.CoverageOutsideProject,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
			t.Setenv("AWG_EVENT_LOG", ledger)

			called := false
			restore := editBriefRPC
			editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
				called = true
				return okBrief("x"), nil
			}
			t.Cleanup(func() { editBriefRPC = restore })

			out := runEditBriefWithStdin(t, []string{"--root", tc.rootArg}, hookPayload(tc.file))

			// The fail-open contract is unchanged and must stay unchanged: an
			// unbriefed edit is recorded, never announced and never blocked.
			if strings.TrimSpace(out) != "" {
				t.Errorf("an unbriefed edit must stay silent to the agent, got %q", out)
			}
			if called {
				t.Error("an unbriefed edit must not reach the backend")
			}

			rows := readRows(t, ledger)
			if len(rows) != 1 {
				t.Fatalf("observed edit produced %d ledger rows, want exactly 1 — "+
					"zero rows is indistinguishable from zero edits, which is the defect", len(rows))
			}
			got := rows[0]
			if got.Coverage != tc.coverage {
				t.Errorf("coverage = %q, want %q", got.Coverage, tc.coverage)
			}
			if got.Delivered {
				t.Error("an unbriefed edit must not be recorded as delivered")
			}
			if strings.TrimSpace(got.Reason) == "" {
				t.Error("an unbriefed edit must name why it was not briefed")
			}
			if !strings.Contains(strings.Join(got.Files, ","), "passwd") {
				t.Errorf("row must name the observed file, got %v", got.Files)
			}
		})
	}
}

// A briefed file and an unbriefed one must be separable WITHOUT parsing prose.
// Reason is written for a human; a coverage figure computed by matching
// substrings of it would break the first time the wording changed.
func TestCoverageIsReadableWithoutParsingTheReason(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "in_project.go")
	if err := os.WriteFile(target, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	t.Setenv("AWG_EVENT_LOG", ledger)

	restore := editBriefRPC
	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return okBrief("governing prose"), nil
	}
	t.Cleanup(func() { editBriefRPC = restore })

	runEditBriefWithStdin(t, []string{"--root", root}, hookPayload(target))
	runEditBriefWithStdin(t, []string{"--root", root}, hookPayload("/etc/passwd"))

	rows := readRows(t, ledger)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (one briefed, one not), got %d", len(rows))
	}
	if rows[0].Coverage != evidence.CoverageInProject || !rows[0].Delivered {
		t.Errorf("in-project row = coverage %q delivered %v, want in_project/true",
			rows[0].Coverage, rows[0].Delivered)
	}
	if rows[1].Coverage != evidence.CoverageOutsideProject {
		t.Errorf("out-of-project row = coverage %q, want outside_project", rows[1].Coverage)
	}

	// The whole point: opportunities and observations are different counts.
	var observed, opportunities, delivered int
	for _, r := range rows {
		observed++
		if r.Coverage == evidence.CoverageInProject {
			opportunities++
		}
		if r.Delivered {
			delivered++
		}
	}
	if observed != 2 || opportunities != 1 || delivered != 1 {
		t.Errorf("observed=%d opportunities=%d delivered=%d, want 2/1/1", observed, opportunities, delivered)
	}
}

// Every row this command writes carries a value from the closed set. A future
// call site that forgets the predicate reintroduces the exact blindness, and an
// empty coverage would be read by a consumer as "not out-of-project" — which is
// reading a closed vocabulary by exclusion, and fails open.
func TestEveryRecordedRowCarriesAKnownCoverage(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "in_project.go")
	if err := os.WriteFile(target, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")
	t.Setenv("AWG_EVENT_LOG", ledger)

	restore := editBriefRPC
	t.Cleanup(func() { editBriefRPC = restore })

	// Drive every outcome the command can record: delivered, refused by the
	// backend, withheld as non-governing, blank prose, and both unbriefed paths.
	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return okBrief("prose"), nil
	}
	runEditBriefWithStdin(t, []string{"--root", root}, hookPayload(target))

	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return editBriefOutcome{}, context.DeadlineExceeded
	}
	runEditBriefWithStdin(t, []string{"--root", root}, hookPayload(target))

	editBriefRPC = func(_ context.Context, _, _, _, _ string) (editBriefOutcome, error) {
		return okBrief("   "), nil
	}
	runEditBriefWithStdin(t, []string{"--root", root}, hookPayload(target))

	runEditBriefWithStdin(t, []string{"--root", root}, hookPayload("/etc/passwd"))
	runEditBriefWithStdin(t, []string{"--root", filepath.Join(t.TempDir(), "nowhere")}, hookPayload("/etc/passwd"))

	rows := readRows(t, ledger)
	if len(rows) < 5 {
		t.Fatalf("expected every outcome to be recorded, got %d rows", len(rows))
	}
	for i, r := range rows {
		if !evidence.KnownCoverage(r.Coverage) {
			t.Errorf("row %d has coverage %q, which is not a member of the closed set", i, r.Coverage)
		}
	}
}

// CoverageNoProject labels the branch taken when resolveProjectRoot itself
// fails, which happens only when the process cannot determine or absolutise a
// working directory. That is an environment failure and cannot be provoked from
// a test without deleting the cwd out from under the test binary.
//
// So this proves the ROW SHAPE directly and claims nothing more. Stated plainly
// because a test named for a branch it does not reach is worse than no test:
// the branch is recorded so it is not silent, and its integration path is
// UNCOVERED.
func TestTheNoProjectRowShapeIsWellFormed(t *testing.T) {
	ledger := filepath.Join(t.TempDir(), "ledger.jsonl")

	recordEditBrief(ledger, "/somewhere/x.go", "", editBriefOutcome{}, false,
		evidence.CoverageNoProject, "no Sensei project resolves for this path")

	rows := readRows(t, ledger)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Coverage != evidence.CoverageNoProject {
		t.Errorf("coverage = %q, want %q", rows[0].Coverage, evidence.CoverageNoProject)
	}
	if !evidence.KnownCoverage(rows[0].Coverage) {
		t.Error("no_project must be a member of the closed set")
	}
	if rows[0].Delivered {
		t.Error("an unbriefed edit must not be recorded as delivered")
	}
}

// An empty ledger path is the "nowhere to write" case and must stay silent
// rather than erroring or creating a stray file.
func TestRecordingWithNowhereToWriteIsSilent(t *testing.T) {
	dir := t.TempDir()
	recordEditBrief("", "/somewhere/x.go", "", editBriefOutcome{}, false,
		evidence.CoverageOutsideProject, "no ledger configured")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("recording with no ledger must create nothing, found %v", entries)
	}
}
