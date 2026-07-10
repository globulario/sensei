// SPDX-License-Identifier: Apache-2.0

package seedmeta

import (
	"strings"
	"testing"
)

func TestAppendMarker_IsIdempotent(t *testing.T) {
	base := []byte("<https://globular.io/awareness#invariant/foo> <http://www.w3.org/2000/01/rdf-schema#label> \"Foo\" .\n")
	first, want := AppendMarker(base)
	second, got := AppendMarker(first)

	if string(first) != string(second) {
		t.Fatal("append marker must be idempotent")
	}
	if want != got {
		t.Fatalf("marker mismatch: %#v vs %#v", want, got)
	}
	if want.TripleCount != 7 {
		t.Fatalf("triple count=%d, want 7", want.TripleCount)
	}
	if count := strings.Count(string(first), want.IRI); count != 6 {
		t.Fatalf("expected 6 marker triples, got %d", count)
	}
}

func TestParseMarker_FindsDigestAndIRI(t *testing.T) {
	stamped, want := AppendMarker([]byte("<https://globular.io/awareness#invariant/foo> <http://www.w3.org/2000/01/rdf-schema#label> \"Foo\" .\n"))
	got, ok := ParseMarker(stamped)
	if !ok {
		t.Fatal("expected marker")
	}
	if got != want {
		t.Fatalf("parsed marker mismatch: %#v want %#v", got, want)
	}
}

func TestParseMarker_ComputesLegacyTripleCount(t *testing.T) {
	legacy := strings.Join([]string{
		"<https://globular.io/awareness#invariant/foo> <http://www.w3.org/2000/01/rdf-schema#label> \"Foo\" .",
		"<https://globular.io/awareness#seedBuild/sha256-abc> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#SeedBuild> .",
		"<https://globular.io/awareness#seedBuild/sha256-abc> <http://www.w3.org/2000/01/rdf-schema#label> \"Embedded seed sha256 abc\" .",
		"<https://globular.io/awareness#seedBuild/sha256-abc> <https://globular.io/awareness#seedDigestSha256> \"abc\" .",
		"<https://globular.io/awareness#seedBuild/sha256-abc> <https://globular.io/awareness#seedMarkerVersion> \"v1\" .",
		"<https://globular.io/awareness#seedBuild/sha256-abc> <https://globular.io/awareness#authoredIn> \"generated:seed_marker\" .",
	}, "\n") + "\n"
	got, ok := ParseMarker([]byte(legacy))
	if !ok {
		t.Fatal("expected legacy marker to parse")
	}
	if got.TripleCount != 6 {
		t.Fatalf("legacy triple count=%d, want 6", got.TripleCount)
	}
}
