// SPDX-License-Identifier: AGPL-3.0-only

package reachability

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testRevision   = "5f999ba5354c2c9dcac57fba58dd841f61726cca"
	testGeneration = "fc45da2905b4f41d982b1d2c20b954b090713bc274be4be426210389004c5121"
)

func writeReceipt(t *testing.T, root, revision, generation string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"receipt":{"Domain":"github.com/globulario/sensei-code","Revision":"` + revision +
		`","State":"CLEAN_EXACT"},"graph_generation":"` + generation + `"}`
	if err := os.WriteFile(filepath.Join(root, ReceiptPath), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeMarker(t *testing.T, root, digest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"digest_sha256":"` + digest + `","triple_count":35234}`
	if err := os.WriteFile(filepath.Join(root, MarkerPath), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The published side of the comparison is the corpus revision the live
// generation was compiled from.
func TestThePublishedRevisionComesFromTheReceipt(t *testing.T) {
	root := t.TempDir()
	writeReceipt(t, root, testRevision, testGeneration)
	writeMarker(t, root, testGeneration)

	got, ok := PublishedCorpusRevisionFromRepo(root)
	if !ok || got != testRevision {
		t.Fatalf("resolved %q,%v; want %q,true", got, ok, testRevision)
	}
}

// A receipt describing a generation the store no longer serves resolves
// NOTHING. Returning its revision anyway would answer confidently about a
// generation that has been replaced -- a stale answer is worse than Unknown,
// because Unknown is a member of the state set and a wrong revision is not.
func TestAReceiptForAnotherGenerationResolvesNothing(t *testing.T) {
	root := t.TempDir()
	writeReceipt(t, root, testRevision, testGeneration)
	writeMarker(t, root, "0000000000000000000000000000000000000000000000000000000000000000")

	if got, ok := PublishedCorpusRevisionFromRepo(root); ok {
		t.Fatalf("a receipt for a superseded generation resolved %q", got)
	}
}

// Absence is Unknown, never a guess.
func TestAMissingReceiptOrMarkerResolvesNothing(t *testing.T) {
	for _, c := range []struct {
		name           string
		receipt, marks bool
	}{
		{"neither", false, false},
		{"marker only", false, true},
		{"receipt only", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			if c.receipt {
				writeReceipt(t, root, testRevision, testGeneration)
			}
			if c.marks {
				writeMarker(t, root, testGeneration)
			}
			if got, ok := PublishedCorpusRevisionFromRepo(root); ok {
				t.Fatalf("resolved %q from an incomplete pair", got)
			}
		})
	}
	if got, ok := PublishedCorpusRevisionFromRepo(""); ok {
		t.Fatalf("resolved %q from an empty root", got)
	}
}

// An abbreviation names a commit probabilistically and cannot be compared for
// equality with one written out in full. It is refused rather than accepted as
// a prefix -- the rule #345 established at the producer, applied at the reader.
func TestAnAbbreviatedOrMalformedRevisionIsRefused(t *testing.T) {
	for _, rev := range []string{
		"5f999ba5354c", // the abbreviation
		"5f999ba5354c2c9dcac57fba58dd841f61726cc",   // 39
		"5f999ba5354c2c9dcac57fba58dd841f61726ccaa", // 41
		"zzz99ba5354c2c9dcac57fba58dd841f61726cca",  // not hex
		"",
	} {
		root := t.TempDir()
		writeReceipt(t, root, rev, testGeneration)
		writeMarker(t, root, testGeneration)
		if got, ok := PublishedCorpusRevisionFromRepo(root); ok {
			t.Errorf("revision %q was accepted as %q", rev, got)
		}
	}
}

// Malformed files resolve nothing rather than panicking or half-reading.
func TestMalformedReceiptOrMarkerResolvesNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ReceiptPath), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, root, testGeneration)
	if got, ok := PublishedCorpusRevisionFromRepo(root); ok {
		t.Fatalf("a malformed receipt resolved %q", got)
	}

	root = t.TempDir()
	writeReceipt(t, root, testRevision, testGeneration)
	if err := os.WriteFile(filepath.Join(root, MarkerPath), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := PublishedCorpusRevisionFromRepo(root); ok {
		t.Fatalf("a malformed marker resolved %q", got)
	}
}

// Case is PRESENTATION, not ambiguity. Upper-case hex names the same object
// exactly, so it is normalized rather than refused -- unlike an abbreviation,
// which names a commit only probabilistically. Refusing it would reject a valid
// identity for the way it was written down.
func TestAnUpperCaseRevisionIsNormalizedNotRefused(t *testing.T) {
	root := t.TempDir()
	writeReceipt(t, root, "5F999BA5354C2C9DCAC57FBA58DD841F61726CCA", testGeneration)
	writeMarker(t, root, testGeneration)

	got, ok := PublishedCorpusRevisionFromRepo(root)
	if !ok {
		t.Fatal("an upper-case object id was refused")
	}
	if got != testRevision {
		t.Fatalf("normalized to %q, want %q", got, testRevision)
	}
}
