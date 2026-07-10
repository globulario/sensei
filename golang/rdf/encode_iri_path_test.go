// SPDX-License-Identifier: AGPL-3.0-only

package rdf

import "testing"

// EncodeIRIPath/DecodeIRIPath must be inverses so a node id survives the
// round-trip id → IRI segment → id. The SourceFile/CodeSymbol double-encoding
// bug came from decoding being absent, so this pins the property.
func TestEncodeDecodeIRIPath_RoundTrip(t *testing.T) {
	cases := []string{
		"cmd/loadnt/main.go",
		"editor/vscode/src/awgRunner.ts",
		"state.runtime_not_desired",          // slash-free: encode is a no-op
		"globular.awareness_graph:code.go.x", // colon is kept verbatim
		"a b\tc",                             // control/space chars
		"100%/done",                          // literal percent + slash
		"",
	}
	for _, raw := range cases {
		enc := EncodeIRIPath(raw)
		if got := DecodeIRIPath(enc); got != raw {
			t.Errorf("round-trip %q: encode=%q decode=%q, want %q", raw, enc, got, raw)
		}
	}
}

// DecodeIRIPath is a no-op on input with no escapes (already-decoded ids), and
// leaves a malformed trailing escape verbatim rather than panicking.
func TestDecodeIRIPath_NoOpAndMalformed(t *testing.T) {
	if got := DecodeIRIPath("no-escapes-here"); got != "no-escapes-here" {
		t.Errorf("no-op decode = %q", got)
	}
	if got := DecodeIRIPath("trailing%2"); got != "trailing%2" {
		t.Errorf("malformed decode = %q, want verbatim", got)
	}
}
