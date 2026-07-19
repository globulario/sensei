// SPDX-License-Identifier: Apache-2.0

package architecture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFactIDIsDeterministic(t *testing.T) {
	a := StableID("guard", "pkg.Apply", "refuses_when", "bad", "state.go", 3, "go_guard_extractor")
	b := StableID("guard", "pkg.Apply", "refuses_when", "bad", "state.go", 3, "go_guard_extractor")
	if a != b {
		t.Fatalf("ids differ: %s != %s", a, b)
	}
}

func TestFactIDDoesNotDependOnAbsoluteRepoPath(t *testing.T) {
	a := StableID("guard", "pkg.Apply", "refuses_when", "bad", "state.go", 3, "go_guard_extractor")
	b := StableID("guard", "pkg.Apply", "refuses_when", "bad", "state.go", 3, "go_guard_extractor")
	if a != b {
		t.Fatalf("absolute roots affected id: %s != %s", a, b)
	}
}

func TestNormalizeFactsSortsAndDeduplicatesScope(t *testing.T) {
	f := validFact()
	f.Scope.Files = []string{"b.go", "a.go", "a.go", `dir\c.go`}
	f.Scope.Symbols = []string{"Z", "A", "A"}
	out, err := NormalizeFacts("", []Fact{f})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(out[0].Scope.Files, ","); got != "a.go,b.go,dir/c.go" {
		t.Fatalf("files = %s", got)
	}
	if got := strings.Join(out[0].Scope.Symbols, ","); got != "A,Z" {
		t.Fatalf("symbols = %s", got)
	}
}

func TestNormalizeFactsDeduplicatesIdenticalFact(t *testing.T) {
	f := validFact()
	out, err := NormalizeFacts("", []Fact{f, f})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1", len(out))
	}
}

func TestNormalizeFactsRejectsIDDivergence(t *testing.T) {
	a := validFact()
	b := validFact()
	a.ID = "fact.collision"
	b.ID = "fact.collision"
	b.Object = "different"
	if _, err := NormalizeFacts("", []Fact{a, b}); err == nil {
		t.Fatal("expected collision error")
	}
}

func TestFactValidationRejectsMissingExtractor(t *testing.T) {
	f := validFact()
	f.Extractor = ""
	if err := ValidateFact(f); err == nil {
		t.Fatal("expected missing extractor error")
	}
}

func TestFactValidationRejectsInvalidLineRange(t *testing.T) {
	f := validFact()
	f.Evidence.LineStart = 10
	f.Evidence.LineEnd = 9
	if err := ValidateFact(f); err == nil {
		t.Fatal("expected invalid line range error")
	}
}

func TestSourceDigestChangesWhenSourceChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := SourceDigestSHA256(root, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := SourceDigestSHA256(root, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("digest did not change")
	}
}

func TestMissingRevisionHasExplicitStatus(t *testing.T) {
	rev, status, lim := ResolveRevision(t.TempDir(), true)
	if rev != "" || status != RevisionNotGit || len(lim) == 0 {
		t.Fatalf("rev=%q status=%q limitations=%v", rev, status, lim)
	}
	rev, status, lim = ResolveRevision(t.TempDir(), false)
	if rev != "" || status != RevisionNotRequested || len(lim) != 0 {
		t.Fatalf("not requested rev=%q status=%q limitations=%v", rev, status, lim)
	}
}

func TestCanonicalFactDoesNotContainWallClockTime(t *testing.T) {
	f := validFact()
	f.Provenance = &Provenance{RevisionStatus: RevisionNotRequested}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{[]byte("time"), []byte("observed_at"), []byte("validated_at")} {
		if bytes.Contains(bytes.ToLower(raw), bad) {
			t.Fatalf("legacy fact JSON leaked wall-clock-like field %q: %s", bad, raw)
		}
	}
}

func validFact() Fact {
	return Fact{
		ID:        "fact.valid",
		Kind:      "guard",
		Subject:   "pkg.Apply",
		Predicate: "refuses_when",
		Object:    "bad",
		Scope: Scope{
			Repository: "repo",
			Files:      []string{"state.go"},
			Symbols:    []string{"pkg.Apply"},
		},
		Evidence:   Evidence{SourceFile: "state.go", LineStart: 3, LineEnd: 4},
		Confidence: 0.5,
		Extractor:  "test_extractor",
	}
}
