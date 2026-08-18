// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

// A decision's decision_digest_sha256 is its identity. `sensei admit-change
// --output <file> --format json` marshals ONE decision twice -- once to the
// file it stores, once to the stream it prints -- and the two must agree about
// what decision they are. They did not: the stored decision declared one digest
// and the printed decision another, so an application receipt binding the
// stored decision could never be matched against the printed one.
//
// The cause was normalizePath being non-idempotent for the empty string
// (filepath.Clean("") == "."), combined with normalizeDecision rewriting path
// slices in place on a backing array shared with the caller's Decision.
func TestDecisionDigestIsStableAcrossRepeatedMarshals(t *testing.T) {
	// "." is what a projection contributes when it names the repository root.
	// It normalizes away to nothing on the first pass; the defect was that the
	// emptied slot came back as "." on the second.
	d := Decision{
		SchemaVersion: SchemaVersion,
		Decision:      DecisionAdmitted,
		FilesToRead:   []string{".", "golang/architecture", "docs/awareness"},
	}

	first, err := MarshalCanonicalDecisionJSON(d)
	if err != nil {
		t.Fatalf("first marshal: %v", err)
	}
	second, err := MarshalCanonicalDecisionJSON(d)
	if err != nil {
		t.Fatalf("second marshal: %v", err)
	}

	digestOf := func(data []byte) string {
		var env decisionEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("decode decision: %v", err)
		}
		return env.ArchitectureAdmissionDecision.DecisionDigestSHA256
	}
	if a, b := digestOf(first), digestOf(second); a != b {
		t.Fatalf("the same decision declared two identities: %s then %s", a, b)
	}

	// And the two serializations of one decision must agree, which is the shape
	// the CLI actually emits.
	yamlBytes, err := MarshalCanonicalDecisionYAML(d)
	if err != nil {
		t.Fatalf("marshal yaml: %v", err)
	}
	jsonBytes, err := MarshalCanonicalDecisionJSON(d)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	var env decisionEnvelope
	if err := yaml.Unmarshal(yamlBytes, &env); err != nil {
		t.Fatalf("reload yaml: %v", err)
	}
	if stored := env.ArchitectureAdmissionDecision.DecisionDigestSHA256; stored != digestOf(jsonBytes) {
		t.Fatalf("stored decision declares %s, printed decision declares %s", stored, digestOf(jsonBytes))
	}
}

func TestNormalizePathIsIdempotent(t *testing.T) {
	for _, in := range []string{"", ".", "./", "./a/b", "a/b/", "a//b", "  a/b  "} {
		once := normalizePath(in)
		twice := normalizePath(once)
		if once != twice {
			t.Errorf("normalizePath(%q) = %q, but normalizePath(%q) = %q", in, once, once, twice)
		}
	}
}
