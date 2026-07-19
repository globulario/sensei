// SPDX-License-Identifier: AGPL-3.0-only

package graphsnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestSharedNTriplesParserPreservesPlaneBehavior(t *testing.T) {
	triples, err := Read(strings.NewReader(`<s> <p> "literal with spaces" .
<s> <p2> <o> .
`))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(triples) != 2 || triples[0].Object != "literal with spaces" || triples[1].Object != "o" || !triples[1].ObjectIsIRI {
		t.Fatalf("unexpected triples: %#v", triples)
	}
}

func TestClosureGraphIndexRejectsMalformedNTriples(t *testing.T) {
	if _, err := Read(strings.NewReader(`<s> <p> "unterminated .`)); err == nil {
		t.Fatal("expected malformed literal to be rejected")
	}
}

func TestVerifyDigestResolved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.nt")
	data := []byte(`<s> <p> "o" .
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	receipt, err := Verify(path, hex.EncodeToString(sum[:]), architecture.GraphDigestResolved)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !receipt.Verified || receipt.DigestSHA256 == "" {
		t.Fatalf("receipt not verified: %#v", receipt)
	}
}

func TestVerifyDigestMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.nt")
	if err := os.WriteFile(path, []byte(`<s> <p> "o" .
`), 0o644); err != nil {
		t.Fatal(err)
	}
	receipt, err := Verify(path, strings.Repeat("0", 64), architecture.GraphDigestResolved)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if receipt.Verified || len(receipt.Reasons) == 0 || receipt.Reasons[0].Code != "graphsnapshot.digest_mismatch" {
		t.Fatalf("expected mismatch receipt: %#v", receipt)
	}
}
