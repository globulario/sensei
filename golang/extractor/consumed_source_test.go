package extractor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
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

// THE CHOKE POINT: an importable schema whose importer records nothing must
// yield NO digest, not the classifier's.
//
// The classifier's read is not evidence about the emitted triples. Falling back
// to it meant any importer that forgets readAndRecord silently reinstates the
// original lie -- a digest covering bytes the parser never saw. The policy
// lives here, once, instead of depending on 47 leaves staying correct.
func TestAnImporterThatRecordsNothingYieldsNoDigest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invariants.yaml")
	if err := os.WriteFile(path, []byte("invariants:\n  - id: x\n    statement: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The real importer reads through readAndRecord, so a digest IS recorded.
	var buf bytes.Buffer
	rep := classifyAndImport(rdf.NewEmitter(&buf), path)
	if rep.ConsumedDigest == "" {
		t.Fatalf("a wired importer produced no consumed digest: %+v", rep)
	}

	// Now the 48th importer, forgotten. An entry marked importable with no
	// case in the switch falls to default: no importer runs, nothing is
	// recorded, and the classifier's digest is the ONLY digest in hand -- the
	// exact situation the fallback used to paper over. Reached through the
	// existing schema table, because the property must hold for importers that
	// do not exist yet and no production seam should exist to reach it.
	saved := keySchemas
	defer func() { keySchemas = saved }()
	probe := saved[0]
	probe.key = "unwired_schema_probe"
	probe.entry = schemaEntry{"unwired_schema_probe", true, false, "A", "importable schema with no registered importer"}
	keySchemas = append(append(keySchemas[:0:0], probe), saved...)

	unwired := filepath.Join(dir, "unwired.yaml")
	if err := os.WriteFile(unwired, []byte("unwired_schema_probe:\n  - id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	rep = classifyAndImport(rdf.NewEmitter(&buf), unwired)
	if rep.Status != StatusInvalid || !strings.Contains(rep.Reason, "no importer registered") {
		t.Fatalf("the probe did not reach the unwired branch: %+v", rep)
	}
	if rep.ConsumedDigest != "" {
		t.Fatalf("an importer that recorded nothing still reported a digest %q: "+
			"the classifier's read would be published as evidence about triples it never produced", rep.ConsumedDigest)
	}
}
