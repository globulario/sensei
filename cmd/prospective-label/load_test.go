// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The blinding is structural. corpus.json, anchor-index.json and inventory.json
// hold anchors, materialization provenance, strata and accounting — Sensei's own
// account of what it knows and where it applies. The tool must be unable to open
// them, not merely uninclined to.
func TestTheToolCannotOpenWhatIsWithheldFromTheAdjudicator(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"corpus.json", "anchor-index.json", "inventory.json", "DIGESTS.txt", "README.md", "overlap-subset.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(`{"secret":"anchors"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := openAllowed(root, name); err == nil {
			t.Fatalf("%s was readable; an adjudicator could be shown what Sensei knows", name)
		} else if !strings.Contains(err.Error(), "refusing to read") {
			t.Fatalf("%s: unexpected error %v", name, err)
		}
	}
	for _, name := range []string{"sample-manifest.json", "blind-corpus.json", "packages/pr1-abc.json"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := openAllowed(root, name); err != nil {
			t.Fatalf("%s should be readable: %v", name, err)
		}
	}
	if _, err := openAllowed(root, "packages/../corpus.json"); err == nil {
		t.Fatal("a path escaping through packages/ was accepted")
	}
}

// The real frozen set loads, and its digests agree with one another.
func TestTheFrozenReferenceSetLoads(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "evaluation", "prospective-v1-reference-set")
	if _, err := os.Stat(root); err != nil {
		t.Skip("reference set not present")
	}
	rs, err := loadReferenceSet(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rs.ItemKeys) != 48 {
		t.Fatalf("loaded %d changes, want 48", len(rs.ItemKeys))
	}
	if len(rs.Corpus.Items) != 866 {
		t.Fatalf("loaded %d eligible items, want 866", len(rs.Corpus.Items))
	}
	if rs.Corpus.DigestSHA256 != rs.Manifest.BlindCorpusDigestSHA256 {
		t.Fatal("the blind corpus is not the one the manifest names")
	}
	for _, it := range rs.Corpus.Items {
		if it.Title == "" && it.Statement == "" {
			t.Fatalf("item %s is unjudgeable", it.ID)
		}
	}
	// Every package carries its change, and none carries a corpus.
	for k, p := range rs.Packages {
		if p.Change.Content == "" || len(p.Change.Paths) == 0 {
			t.Fatalf("package %s has no change to judge", k)
		}
	}
}

// The presentation order is a stable key order and carries no relevance.
func TestPresentationOrderIsClassThenTitleThenID(t *testing.T) {
	root := filepath.Join("..", "..", "docs", "evaluation", "prospective-v1-reference-set")
	if _, err := os.Stat(root); err != nil {
		t.Skip("reference set not present")
	}
	rs, err := loadReferenceSet(root)
	if err != nil {
		t.Fatal(err)
	}
	ids := rs.corpusIDs()
	if len(ids) != len(rs.Corpus.Items) {
		t.Fatalf("order lists %d of %d items; a dropped item is one nobody is asked about", len(ids), len(rs.Corpus.Items))
	}
	byID := map[string]struct{ class, title string }{}
	for _, it := range rs.Corpus.Items {
		byID[it.ID] = struct{ class, title string }{it.Class, it.Title}
	}
	for i := 1; i < len(ids); i++ {
		a, b := byID[ids[i-1]], byID[ids[i]]
		if a.class > b.class || (a.class == b.class && a.title > b.title) {
			t.Fatalf("order breaks at %d: %v then %v", i, a, b)
		}
	}
	// Deterministic across calls.
	again := rs.corpusIDs()
	for i := range ids {
		if ids[i] != again[i] {
			t.Fatal("the presentation order is not stable between calls")
		}
	}
}
