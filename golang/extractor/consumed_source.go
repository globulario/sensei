// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
)

// consumedDigests records, per path, the digest of the bytes an IMPORTER
// actually parsed.
//
// WHY NOT THE CLASSIFIER'S READ. classifyAndImport reads a file to detect its
// schema, then the schema's importer reopens the same path and parses a SECOND
// buffer. Digesting the classifier's read proves nothing about the triples: a
// file changed between the two reads and restored afterwards leaves the
// classifier digest matching the revision while the emitted graph came from
// bytes no revision holds.
//
// The digest must cover the read that FEEDS THE PARSER, so importers read
// through readAndRecord and the digest is taken from that buffer.
var consumedDigests = struct {
	sync.Mutex
	byPath map[string]string
}{byPath: map[string]string{}}

// readAndRecord reads a source file and records the digest of exactly those
// bytes. Importers use it in place of os.ReadFile.
func readAndRecord(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	consumedDigests.Lock()
	consumedDigests.byPath[path] = hex.EncodeToString(sum[:])
	consumedDigests.Unlock()
	return data, nil
}

// consumedDigestFor returns the digest of the bytes an importer parsed for
// path, and whether an importer read it at all.
func consumedDigestFor(path string) (string, bool) {
	consumedDigests.Lock()
	defer consumedDigests.Unlock()
	d, ok := consumedDigests.byPath[path]
	return d, ok
}

// forgetConsumed clears the record for a path before a walk re-reads it, so a
// stale digest from an earlier build cannot be mistaken for this one's.
func forgetConsumed(path string) {
	consumedDigests.Lock()
	delete(consumedDigests.byPath, path)
	consumedDigests.Unlock()
}
