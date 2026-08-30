package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// F5: the digest must cover the buffer the IMPORTER parsed.
//
// classifyAndImport reads a file to detect its schema, then the importer
// reopens the path and parses a SECOND buffer. Digesting the classifier's read
// proves nothing about the emitted triples: a file changed between the two
// reads and restored afterwards leaves the classifier digest matching the
// revision while the graph came from bytes no revision holds.
func TestTheRecordedDigestCoversTheImporterRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	first := []byte("a: 1\n")
	if err := os.WriteFile(path, first, 0o644); err != nil {
		t.Fatal(err)
	}
	forgetConsumed(path)
	if _, ok := consumedDigestFor(path); ok {
		t.Fatal("a forgotten path still reports a digest")
	}

	// The importer's read is what gets recorded.
	got, err := readAndRecord(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(first)
	d, ok := consumedDigestFor(path)
	if !ok || d != hex.EncodeToString(want[:]) {
		t.Fatalf("digest = %q ok=%v, want the bytes just read", d, ok)
	}
	if string(got) != string(first) {
		t.Fatal("readAndRecord returned different bytes than it digested")
	}

	// The file changes; a SECOND importer read records the second buffer, not
	// the first. That is the property: the digest follows the parse.
	second := []byte("a: 999\n")
	if err := os.WriteFile(path, second, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAndRecord(path); err != nil {
		t.Fatal(err)
	}
	sum2 := sha256.Sum256(second)
	if d, _ := consumedDigestFor(path); d != hex.EncodeToString(sum2[:]) {
		t.Fatalf("the digest did not follow the importer's read: %q", d)
	}
}
