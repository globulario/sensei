// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
)

// TestImportFixture_ProducesValidNTriples is the broad smoke test: feed
// the importer the three handcrafted fixtures under testdata/, validate
// the resulting N-Triples with the in-package validator, and report a
// hard fail with every offending line if validation surfaces anything.
// This is the test that would have caught both spike bugs
// (prose-in-IRI and {template}-in-IRI) at PR time instead of at
// container-load time.
func TestImportFixture_ProducesValidNTriples(t *testing.T) {
	var buf bytes.Buffer
	e, err := extractor.ImportAwarenessYAMLs("testdata", &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessYAMLs: %v", err)
	}
	if e.Triples == 0 {
		t.Fatal("expected triples to be emitted; got 0 — did testdata go missing?")
	}

	errs := extractor.ValidateNTriples(bytes.NewReader(buf.Bytes()))
	if len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("validation: %s", e)
		}
		t.Fatalf("%d N-Triples validation errors", len(errs))
	}
}

// TestImportFixture_VocabularyV01Migration is the regression guard for
// Patch 1's vocabulary corrections. If anyone re-introduces aw:source or
// aw:relatedInvariant — accidentally or via a partial revert — this test
// fails. Symmetrically, it asserts the v0.1 predicates ARE present.
//
// Specifically:
//   - aw:authoredIn MUST appear (replaces v0.0 aw:source).
//   - aw:source MUST NOT appear.
//   - aw:relatedInvariant MUST NOT appear (v0.0 ontology mistake).
//   - aw:affects MUST appear for the FM→Invariant case
//     (related_invariants on failure_modes.yaml moved from
//     aw:relatedInvariant to aw:affects in v0.1).
func TestImportFixture_VocabularyV01Migration(t *testing.T) {
	out := importFixtureToString(t)

	mustContain := []struct {
		needle string
		why    string
	}{
		{"awareness#authoredIn>", "aw:authoredIn predicate must appear (Patch 1 rename of aw:source)"},
		{"awareness#affects>", "aw:affects predicate must appear (Patch 1 unified FM→Inv under affects)"},
	}
	mustNotContain := []struct {
		needle string
		why    string
	}{
		{"awareness#source>", "aw:source predicate must NOT appear (Patch 1 removed it; superseded by aw:authoredIn)"},
		{"awareness#relatedInvariant>", "aw:relatedInvariant predicate must NOT appear (Patch 1 removed it; superseded by aw:affects + object-class filter)"},
	}

	for _, c := range mustContain {
		if !strings.Contains(out, c.needle) {
			t.Errorf("missing required predicate %q — %s", c.needle, c.why)
		}
	}
	for _, c := range mustNotContain {
		if strings.Contains(out, c.needle) {
			t.Errorf("forbidden predicate %q present — %s", c.needle, c.why)
		}
	}
}

// TestImportFixture_KnownTriplesPresent is the structural assertion:
// for each authored node in the fixtures, the importer emits the triples
// the agent surface depends on. These are not exhaustive — they're the
// triples whose absence would silently break Briefing/Impact composition.
func TestImportFixture_KnownTriplesPresent(t *testing.T) {
	out := importFixtureToString(t)

	// Each entry: substring that must appear at least once in the output.
	// The match is intentionally substring-based, not full-line, so a
	// future formatting change (e.g. trailing whitespace) won't break
	// the test for reasons unrelated to its intent.
	expectations := []struct {
		needle string
		why    string
	}{
		// Three authored nodes are typed.
		{`<https://globular.io/awareness#invariant/test.example.invariant> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Invariant> .`,
			"the fixture invariant must be typed as aw:Invariant"},
		{`<https://globular.io/awareness#failureMode/test.example.failure_mode> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#FailureMode> .`,
			"the fixture failure_mode must be typed as aw:FailureMode"},
		{`<https://globular.io/awareness#incidentPattern/pat.test.example> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#IncidentPattern> .`,
			"the fixture pattern must be typed as aw:IncidentPattern"},

		// File anchoring + reverse implements edge: the lever Briefing
		// uses to find direct-anchor patterns for a file.
		{`<https://globular.io/awareness#invariant/test.example.invariant> <https://globular.io/awareness#protects> <https://globular.io/awareness#sourceFile/test%2Fexample.go>`,
			"invariant must protect the named file (slash percent-encoded)"},
		{`<https://globular.io/awareness#sourceFile/test%2Fexample.go> <https://globular.io/awareness#implements> <https://globular.io/awareness#invariant/test.example.invariant>`,
			"reverse implements edge must exist so impact can find invariants from a file"},
		{`<https://globular.io/awareness#sourceFile/test%2Fexample.go> <https://globular.io/awareness#vulnerableTo> <https://globular.io/awareness#failureMode/test.example.failure_mode>`,
			"failure mode protects.files must materialize a direct source-file vulnerability edge for closure relevance"},

		// {template} placeholder in etcd key — regression guard for the
		// percent-encoding bug. The braces must be encoded as %7B / %7D.
		{`<https://globular.io/awareness#etcdKey/%2Ftest%2Fexample%2F%7Bid%7D%2Fconfig>`,
			"etcd key template placeholders must be percent-encoded (W3C IRIREF grammar disallows { })"},

		// IncidentPattern → FailureMode (exemplifies) — the singular
		// relationship that distinguishes the pattern's specific origin
		// from its many-to-many aw:affects edges.
		{`<https://globular.io/awareness#incidentPattern/pat.test.example> <https://globular.io/awareness#exemplifies> <https://globular.io/awareness#failureMode/test.example.failure_mode>`,
			"incident_pattern must exemplify its singular failure_mode via aw:exemplifies"},

		// Prose-vs-ID routing in wrong_fixes — the ID becomes an edge,
		// the prose entry becomes a rdfs:comment literal on the pattern.
		{`<https://globular.io/awareness#incidentPattern/pat.test.example> <https://globular.io/awareness#forbids> <https://globular.io/awareness#forbiddenFix/test_example_forbidden_fix>`,
			"stable-ID wrong_fix must become aw:forbids edge"},
		{`<https://globular.io/awareness#incidentPattern/pat.test.example> <http://www.w3.org/2000/01/rdf-schema#comment> "Don't ` + "`" + `pkill -f` + "`" + ` the binary`,
			"prose wrong_fix must become a rdfs:comment literal (not an invalid IRI)"},
	}

	for _, exp := range expectations {
		if !strings.Contains(out, exp.needle) {
			t.Errorf("missing triple — %s\n  expected to contain:\n    %s", exp.why, exp.needle)
		}
	}
}

// importFixtureToString is a small shared helper so each test reads the
// importer output the same way. Fails the test on importer error rather
// than returning one — every caller treats import failure as fatal.
func importFixtureToString(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := extractor.ImportAwarenessYAMLs("testdata", &buf); err != nil {
		t.Fatalf("ImportAwarenessYAMLs: %v", err)
	}
	return buf.String()
}
