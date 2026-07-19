// SPDX-License-Identifier: Apache-2.0

package seedmeta

import (
	"bytes"
	"testing"
)

// TestMarkerTriples_MatchesAppendMarker locks the invariant that MarkerTriples
// emits exactly the marker lines AppendMarker appends — the scoped-update path
// inserts only these lines, so they must be byte-identical to the whole-artifact
// stamping, and the count must land on the marker's stated TripleCount.
func TestMarkerTriples_MatchesAppendMarker(t *testing.T) {
	base := []byte("<https://example.test/s> <https://example.test/p> \"v\" .\n")
	finalNT, marker := AppendMarker(base)

	mt := MarkerTriples(marker)
	if !bytes.HasSuffix(finalNT, mt) {
		t.Fatalf("AppendMarker output does not end with MarkerTriples:\n--- final ---\n%s\n--- marker ---\n%s", finalNT, mt)
	}
	if got := bytes.Count(mt, []byte("\n")); got != 6 {
		t.Fatalf("MarkerTriples lines=%d, want 6", got)
	}
	// The recomputed marker round-trips through ParseMarker (what the store's
	// verification reads back).
	parsed, ok := ParseMarker(append(append([]byte{}, base...), mt...))
	if !ok {
		t.Fatal("MarkerTriples output not parseable by ParseMarker")
	}
	if parsed.Digest != marker.Digest || parsed.TripleCount != marker.TripleCount {
		t.Fatalf("round-trip = %s/%d, want %s/%d", parsed.Digest, parsed.TripleCount, marker.Digest, marker.TripleCount)
	}
}
