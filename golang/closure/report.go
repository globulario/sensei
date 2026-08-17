// SPDX-License-Identifier: AGPL-3.0-only

package closure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReportFileName is the closure report written beside the graph marker.
//
// The marker answers "does the store match the last publication?". This answers
// the question the marker cannot: "does that publication represent the source it
// claims to certify?". Keeping them as separate artifacts is deliberate — the
// 2026-08-05 incident was a store that matched its marker perfectly and
// contained the wrong repository's corpus.
const ReportFileName = "graph-closure-report.json"

// Report is the durable verdict a publication leaves behind, so a reader that
// was not present at build time can still tell whether the slice is
// semantically valid.
type Report struct {
	// Domain the slice was published under.
	Domain string `json:"domain"`
	// CertifiedSourceRoot is the corpus the publication claims to represent.
	CertifiedSourceRoot string `json:"certified_source_root"`
	// MarkerDigest binds this report to one exact publication. A report whose
	// digest does not match the live marker describes a DIFFERENT publication
	// and must never be used to vouch for the current one.
	MarkerDigest string `json:"marker_digest_sha256"`

	SourceIdentities  int      `json:"source_identities"`
	ExpectedToProject int      `json:"expected_to_project"`
	Projected         int      `json:"identities_projected"`
	Missing           int      `json:"identities_missing"`
	Excluded          int      `json:"identities_explicitly_excluded"`
	Unexpected        int      `json:"unexpected_foreign_provenance"`
	Unproven          int      `json:"unresolved_attribution"`
	ProvenanceNotEmit int      `json:"class_emits_no_provenance"`
	DuplicateSubjects int      `json:"duplicate_canonical_subjects"`
	PublishedTriples  int      `json:"published_triples"`
	ClosureProven     bool     `json:"closure_proven"`
	FailureReasons    []string `json:"failure_reasons,omitempty"`
}

// SemanticState is the explicit vocabulary for "the publication is fresh but
// the knowledge may not be valid". Freshness and semantic validity are separate
// dimensions and must never be collapsed into one flag again.
type SemanticState string

const (
	// SemanticClosureProven — the slice represents its certified source, and
	// contains nothing authored outside it.
	SemanticClosureProven SemanticState = "GRAPH_DOMAIN_CLOSURE_PROVEN"
	// SemanticClosureFailed — the slice is internally consistent and
	// semantically wrong.
	SemanticClosureFailed SemanticState = "GRAPH_DOMAIN_CLOSURE_FAILED"
	// SemanticClosureUnproven — no report, an unreadable report, or a report
	// describing a different publication. Fail-closed: absence of a verdict is
	// not a passing verdict.
	SemanticClosureUnproven SemanticState = "GRAPH_DOMAIN_CLOSURE_UNPROVEN"
)

// NewReport builds a durable report from a census.
func NewReport(domain, markerDigest string, publishedTriples int, c *ClosureCensus) *Report {
	proven, reasons := c.Authoritative()
	return &Report{
		Domain:              domain,
		CertifiedSourceRoot: c.SourceRoot,
		MarkerDigest:        markerDigest,
		SourceIdentities:    len(c.SourceIdentities),
		ExpectedToProject:   len(c.ExpectedToProject),
		Projected:           len(c.Projected),
		Missing:             len(c.Missing),
		Excluded:            len(c.Excluded),
		Unexpected:          len(c.Unexpected),
		Unproven:            len(c.Unproven),
		ProvenanceNotEmit:   len(c.ProvenanceNotEmitted),
		DuplicateSubjects:   len(c.Duplicates),
		PublishedTriples:    publishedTriples,
		ClosureProven:       proven,
		FailureReasons:      reasons,
	}
}

// ReportPath returns the closure report path for a given graph marker path.
func ReportPath(markerPath string) string {
	return filepath.Join(filepath.Dir(markerPath), ReportFileName)
}

// Write persists the report beside the marker.
func (r *Report) Write(markerPath string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ReportPath(markerPath), append(b, '\n'), 0o644)
}

// Evaluate reads the closure report for a publication and returns the semantic
// state plus a human-readable detail.
//
// FAIL-CLOSED. Every path that cannot affirmatively prove closure returns
// UNPROVEN, never PROVEN:
//
//   - no report at all (an older publication, or a build that never ran the
//     check) — absence of a verdict is not a passing verdict;
//   - an unreadable or malformed report;
//   - a report whose MarkerDigest does not match the live marker, which means
//     it describes a different publication and vouches for nothing here.
//
// That last case is the one that matters most in practice: a stale report left
// beside a fresh marker is exactly how a system talks itself into believing an
// old proof covers new content.
func Evaluate(markerPath, liveMarkerDigest string) (SemanticState, string, *Report) {
	path := ReportPath(markerPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return SemanticClosureUnproven, fmt.Sprintf(
			"no closure report beside the graph marker (%s): the publication has not proven that it represents the source it certifies", path), nil
	}
	var r Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return SemanticClosureUnproven, fmt.Sprintf("closure report unreadable (%s): %v", path, err), nil
	}
	state, detail := EvaluateReport(&r, liveMarkerDigest)
	return state, detail, &r
}

// EvaluateReport is the verdict half of Evaluate, for callers that already hold
// the report.
//
// Kept separate so that a report loaded from the store-scoped proof set and a
// report read from beside a marker file are judged by exactly the same rules.
// Two verdict implementations would eventually disagree about what "proven"
// means, and the disagreement would surface as authority.
func EvaluateReport(r *Report, liveMarkerDigest string) (SemanticState, string) {
	if r == nil {
		return SemanticClosureUnproven, "no closure report: absence of a verdict is not a passing verdict"
	}
	if liveMarkerDigest != "" && r.MarkerDigest != "" && !strings.EqualFold(r.MarkerDigest, liveMarkerDigest) {
		return SemanticClosureUnproven, fmt.Sprintf(
			"closure report describes publication %s but the live marker is %s — the report vouches for different content",
			shortDigest(r.MarkerDigest), shortDigest(liveMarkerDigest))
	}
	if !r.ClosureProven {
		detail := fmt.Sprintf(
			"domain %q: %d required identities missing, %d authored outside the certified source root, %d unattributed",
			r.Domain, r.Missing, r.Unexpected, r.Unproven)
		if len(r.FailureReasons) > 0 {
			detail += " — " + strings.Join(r.FailureReasons, "; ")
		}
		return SemanticClosureFailed, detail
	}
	return SemanticClosureProven, fmt.Sprintf(
		"domain %q: %d/%d identities projected, 0 missing, 0 foreign, 0 unattributed",
		r.Domain, r.Projected, r.ExpectedToProject)
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
