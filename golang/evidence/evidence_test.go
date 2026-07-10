// SPDX-License-Identifier: Apache-2.0

package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAggregate(t *testing.T) {
	events := []Event{
		{TS: "2026-07-01T10:00:00Z", Tool: "gate", Repo: "repoA", Decision: DecisionBlock, Enforced: true, BlockedRules: []string{"rule.x"}},
		{TS: "2026-07-02T10:00:00Z", Tool: "gate", Repo: "repoA", Decision: DecisionAllow},
		{TS: "2026-07-03T10:00:00Z", Tool: "edit-guard", Repo: "repoB", Decision: DecisionWouldBlock, BlockedRules: []string{"rule.x", "rule.y"}},
		{TS: "2026-07-04T10:00:00Z", Tool: "gate", Repo: "repoB", Decision: DecisionWarn, WarnedRules: []string{"rule.z"}},
		{TS: "2026-07-05T10:00:00Z", Tool: "gate", Repo: "repoC", Decision: DecisionCannotVerify},
	}
	s := Aggregate(events)

	if s.Events != 5 {
		t.Errorf("events = %d, want 5", s.Events)
	}
	if s.Blocks != 2 { // block + would_block
		t.Errorf("blocks = %d, want 2", s.Blocks)
	}
	if s.HardBlocks != 1 { // only the enforced block
		t.Errorf("hard_blocks = %d, want 1", s.HardBlocks)
	}
	if s.Warns != 1 || s.Allows != 1 || s.CannotVerify != 1 {
		t.Errorf("warns/allows/cannot = %d/%d/%d, want 1/1/1", s.Warns, s.Allows, s.CannotVerify)
	}
	if len(s.Repos) != 3 {
		t.Errorf("repos = %v, want 3 distinct", s.Repos)
	}
	if s.CatchesByRule["rule.x"] != 2 || s.CatchesByRule["rule.y"] != 1 {
		t.Errorf("catches by rule wrong: %v", s.CatchesByRule)
	}
	if s.FirstTS != "2026-07-01T10:00:00Z" || s.LastTS != "2026-07-05T10:00:00Z" {
		t.Errorf("window = %s..%s", s.FirstTS, s.LastTS)
	}
	// repoA: 2 events, 1 catch.
	var repoA *RepoStat
	for i := range s.ByRepo {
		if s.ByRepo[i].Repo == "repoA" {
			repoA = &s.ByRepo[i]
		}
	}
	if repoA == nil || repoA.Events != 2 || repoA.Blocks != 1 {
		t.Errorf("repoA stat wrong: %+v", repoA)
	}
}

func TestAggregate_Empty(t *testing.T) {
	s := Aggregate(nil)
	if s.Events != 0 || s.Blocks != 0 || len(s.Repos) != 0 {
		t.Errorf("empty aggregate not zero: %+v", s)
	}
}

func TestAppendLoadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ledger.jsonl") // dir must be created
	evs := []Event{
		{TS: "2026-07-01T00:00:00Z", Tool: "gate", Decision: DecisionBlock, BlockedRules: []string{"r1"}},
		{TS: "2026-07-02T00:00:00Z", Tool: "edit-guard", Decision: DecisionAllow},
	}
	for _, e := range evs {
		if err := Append(path, e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 || got[0].Decision != DecisionBlock || got[1].Tool != "edit-guard" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestAppend_EmptyPathIsNoOp(t *testing.T) {
	if err := Append("", Event{Decision: DecisionBlock}); err != nil {
		t.Errorf("empty path should be a silent no-op, got %v", err)
	}
}

func TestLoad_MissingFileIsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || got != nil {
		t.Errorf("missing ledger should be empty/no-error, got %v / %v", got, err)
	}
}

func TestLoad_SkipsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.jsonl")
	content := `{"ts":"t1","decision":"block"}` + "\n" +
		`not json — a torn write` + "\n" +
		`{"ts":"t2","decision":"allow"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("malformed line must be skipped, kept %d: %+v", len(got), got)
	}
}
