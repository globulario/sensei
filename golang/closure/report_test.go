// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The authority contract these tests pin:
//
//	authoritative = publication_current AND closure_proven
//
// Freshness alone was the old definition, and on 2026-08-05 it reported
// authoritative=true for a store that certified services commit d7c1a87c while
// containing sensei's corpus. The publication was genuinely current. The
// knowledge was the wrong repository's.

func writeReport(t *testing.T, dir string, r *Report) string {
	t.Helper()
	marker := filepath.Join(dir, "graph-authority.json")
	if err := os.WriteFile(marker, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if r != nil {
		if err := r.Write(marker); err != nil {
			t.Fatal(err)
		}
	}
	return marker
}

func provenCensus() *ClosureCensus {
	return &ClosureCensus{
		SourceRoot:        "/repo/services/docs/awareness",
		SourceIdentities:  []string{"a", "b"},
		ExpectedToProject: []string{"a", "b"},
		Projected:         []string{"a", "b"},
	}
}

// TestProvenReportYieldsAuthority is the positive control.
func TestProvenReportYieldsAuthority(t *testing.T) {
	dir := t.TempDir()
	marker := writeReport(t, dir, NewReport("globular", "abc123", 120010, provenCensus()))

	state, detail, r := Evaluate(marker, "abc123")
	if state != SemanticClosureProven {
		t.Fatalf("state = %s (%s), want PROVEN", state, detail)
	}
	if r == nil || !r.ClosureProven || r.Projected != 2 {
		t.Errorf("report = %+v", r)
	}
}

// TestIncidentReplay_FreshPublicationOfWrongCorpusIsNotAuthoritative is the
// negative control the directive names.
//
//	certified services revision
//	+ sensei corpus published into the services domain
//	→ freshness may be current
//	→ authority must be false
func TestIncidentReplay_FreshPublicationOfWrongCorpusIsNotAuthoritative(t *testing.T) {
	dir := t.TempDir()
	// The census a wrong-workspace build actually produces: the services
	// identities are absent, and what IS present was authored in the sensei repo.
	wrong := &ClosureCensus{
		SourceRoot:        "/repo/services/docs/awareness",
		SourceIdentities:  []string{"four_layer.layer_has_single_writing_actor"},
		ExpectedToProject: []string{"four_layer.layer_has_single_writing_actor"},
		Missing:           []string{"four_layer.layer_has_single_writing_actor"},
		Unexpected: []Subject{{
			IRI:        NS + "invariant/awareness.briefing.deterministic_compact_context",
			Class:      "Invariant",
			AuthoredIn: []string{"/repo/sensei/docs/awareness/invariants.yaml"},
		}},
	}
	// The marker matches the store exactly — the publication really is current.
	marker := writeReport(t, dir, NewReport("globular", "freshdigest", 145491, wrong))

	state, detail, _ := Evaluate(marker, "freshdigest")
	if state == SemanticClosureProven {
		t.Fatal("a fresh publication of the WRONG repository's corpus was reported as " +
			"closure-proven — this is the 2026-08-05 incident, where every freshness " +
			"lamp stayed green while the graph described a different repository")
	}
	if state != SemanticClosureFailed {
		t.Errorf("state = %s, want FAILED", state)
	}
	if !strings.Contains(detail, "missing") || !strings.Contains(detail, "outside") {
		t.Errorf("detail must name BOTH directions (missing + foreign provenance); got %q", detail)
	}
}

// TestMissingReportIsUnprovenNotPassing. Absence of a verdict is not a passing
// verdict — an older publication, or a build that never ran the check, must not
// inherit authority by default.
func TestMissingReportIsUnprovenNotPassing(t *testing.T) {
	dir := t.TempDir()
	marker := writeReport(t, dir, nil) // marker present, no closure report

	state, detail, _ := Evaluate(marker, "abc123")
	if state != SemanticClosureUnproven {
		t.Fatalf("state = %s, want UNPROVEN: a publication with no closure report has "+
			"proven nothing and must not be trusted by default", state)
	}
	if !strings.Contains(detail, "no closure report") {
		t.Errorf("detail = %q", detail)
	}
}

// TestStaleReportDoesNotVouchForNewContent. A report left beside a newer marker
// describes a different publication. This is how a system talks itself into
// believing an old proof covers new content.
func TestStaleReportDoesNotVouchForNewContent(t *testing.T) {
	dir := t.TempDir()
	marker := writeReport(t, dir, NewReport("globular", "OLDdigest", 100, provenCensus()))

	state, detail, _ := Evaluate(marker, "NEWdigest")
	if state == SemanticClosureProven {
		t.Fatal("a closure report for a DIFFERENT publication vouched for the live one")
	}
	if state != SemanticClosureUnproven {
		t.Errorf("state = %s, want UNPROVEN", state)
	}
	if !strings.Contains(detail, "different content") {
		t.Errorf("detail must say the report describes different content; got %q", detail)
	}
}

// TestMalformedReportIsUnproven: an unreadable verdict is not a verdict.
func TestMalformedReportIsUnproven(t *testing.T) {
	dir := t.TempDir()
	marker := writeReport(t, dir, nil)
	if err := os.WriteFile(ReportPath(marker), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := Evaluate(marker, "abc"); state != SemanticClosureUnproven {
		t.Fatalf("state = %s, want UNPROVEN", state)
	}
}

// TestReportRoundTripsFailureReasons keeps the diagnosis durable: a reader who
// was not present at build time still learns why closure failed.
func TestReportRoundTripsFailureReasons(t *testing.T) {
	dir := t.TempDir()
	failing := &ClosureCensus{
		SourceRoot:        "/repo/services/docs/awareness",
		ExpectedToProject: []string{"x"},
		Missing:           []string{"x"},
	}
	marker := writeReport(t, dir, NewReport("globular", "d", 1, failing))

	_, _, r := Evaluate(marker, "d")
	if r == nil || len(r.FailureReasons) == 0 {
		t.Fatal("failure reasons must survive into the persisted report")
	}
	if !strings.Contains(strings.Join(r.FailureReasons, " "), "ABSENT") {
		t.Errorf("reasons = %v", r.FailureReasons)
	}
}
