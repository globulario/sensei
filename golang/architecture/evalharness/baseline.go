// SPDX-License-Identifier: AGPL-3.0-only

// Package evalharness runs one arm of the Phase 10.8 evaluation over the
// architectural mutant suite and reports, honestly, what that arm recovered.
//
// This file implements the first baseline arm issue #131 section 5 names:
// "deterministic extraction without investigation composition". It runs
// howextract over each mutant with the model disabled and no composition, which
// is the floor every richer configuration must beat to have earned its cost.
//
// # What this arm can and cannot report
//
// Deterministic HOW extraction produces topology, flow, boundary, contract and
// state OBSERVATIONS. It does not produce architectural verdicts, so it cannot
// name a defect, and this harness never claims it did. Reporting a detection
// rate for it would be the fabrication the whole evaluation exists to detect.
//
// What it can report is strictly weaker and genuinely useful: whether the arm
// recovered any grounded observation ANCHORED TO THE FILES THE DEFECT LIVES IN.
// That is a precondition for detection, not detection. An extractor that never
// produced evidence about the mutated file cannot support a downstream finding
// about it, however good the reasoning layered on top; one that did may still
// fail to reason correctly. Measuring the precondition separately keeps a
// later composition arm's score from silently inheriting credit for evidence
// this layer supplied, or blame for evidence it never had.
package evalharness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/evalmutant"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// ArmDeterministicExtraction is the arm identifier recorded in reports.
const ArmDeterministicExtraction = "deterministic_extraction_without_composition"

// SiteResult is one mutant's outcome under one arm.
type SiteResult struct {
	Defect evalmutant.Defect `json:"defect" yaml:"defect"`
	// DefectPresent is ground truth from the mutant's own witness, re-read from
	// the materialized tree. Recorded per-run rather than assumed, so a report
	// over a broken suite is visibly broken rather than quietly wrong.
	DefectPresent bool   `json:"defect_present" yaml:"defect_present"`
	WitnessDetail string `json:"witness_detail" yaml:"witness_detail"`
	// Statement is the reference answer a grader compares a detection against.
	Statement string `json:"statement" yaml:"statement"`

	// DefectPaths are the files the defect actually changed.
	DefectPaths []string `json:"defect_paths" yaml:"defect_paths"`
	// ObservedPaths are the files this arm produced observations for.
	ObservedPaths []string `json:"observed_paths" yaml:"observed_paths"`
	// CoveredDefectPaths is the intersection: defect sites this arm produced
	// evidence about.
	CoveredDefectPaths []string `json:"covered_defect_paths" yaml:"covered_defect_paths"`

	Observations     int `json:"observations" yaml:"observations"`
	EvidenceReceipts int `json:"evidence_receipts" yaml:"evidence_receipts"`
	Limitations      int `json:"limitations" yaml:"limitations"`

	// NamedTheDefect is always false for this arm, and is present precisely so
	// that fact is stated in the record rather than left to be inferred from
	// its absence. A later arm that CAN name defects sets it, and the two
	// become comparable.
	NamedTheDefect bool `json:"named_the_defect" yaml:"named_the_defect"`

	DocumentDigest string `json:"document_digest_sha256" yaml:"document_digest_sha256"`

	// observedRoot is where this site was materialized. Unexported so it never
	// reaches a report: a filesystem path is not a finding, and recording one
	// would make two runs of the same suite differ.
	observedRoot string
}

// SiteCovered reports whether this arm produced evidence about any file the
// defect touched.
func (r SiteResult) SiteCovered() bool { return len(r.CoveredDefectPaths) > 0 }

// Report is one arm's complete run over the suite.
type Report struct {
	SchemaVersion string `json:"schema_version" yaml:"schema_version"`
	Arm           string `json:"arm" yaml:"arm"`
	// CapturedAt is the caller's explicit timestamp, never wall clock read
	// here: a report that stamped itself would not be reproducible, and
	// reproducibility is a completion criterion of the evaluation.
	CapturedAt string       `json:"captured_at" yaml:"captured_at"`
	Baseline   SiteResult   `json:"baseline_control" yaml:"baseline_control"`
	Results    []SiteResult `json:"results" yaml:"results"`

	// Limitations are what this arm structurally could not do. They are part
	// of the report rather than a footnote, because an arm's ceiling is the
	// context that makes its numbers mean anything.
	Limitations []string `json:"limitations" yaml:"limitations"`
}

// SiteCoverageRate is the fraction of mutants whose defect site this arm
// produced any evidence about. It is deliberately NOT called a detection rate.
func (r Report) SiteCoverageRate() (covered, total int) {
	for _, res := range r.Results {
		total++
		if res.SiteCovered() {
			covered++
		}
	}
	return covered, total
}

// Options bind the run. Every field is explicit input; the harness reads no
// wall clock, no environment, and no repository state of its own.
type Options struct {
	// RepositoryDomain scopes the extraction. It is required, so the harness
	// never falls back to resolving identity from whatever checkout it happens
	// to be running inside.
	RepositoryDomain string
	// CapturedAt is the explicit RFC3339 capture time recorded in receipts.
	CapturedAt string
	// MaterializeInto returns a fresh empty directory for one tree. The caller
	// owns placement and cleanup, so the harness never invents temp paths that
	// a later run cannot reproduce.
	MaterializeInto func(name string) (string, error)
}

// RunDeterministicExtraction runs the baseline arm over the whole suite plus
// the clean control.
//
// The control is run through the identical path, not skipped: an arm whose
// numbers on a clean tree are indistinguishable from its numbers on a mutated
// one has told you something important, and only running both reveals it.
func RunDeterministicExtraction(opts Options) (Report, error) {
	if strings.TrimSpace(opts.RepositoryDomain) == "" {
		return Report{}, fmt.Errorf("evalharness: RepositoryDomain is required; the arm must not resolve identity from its own checkout")
	}
	if strings.TrimSpace(opts.CapturedAt) == "" {
		return Report{}, fmt.Errorf("evalharness: CapturedAt is required; a self-stamped report is not reproducible")
	}
	if opts.MaterializeInto == nil {
		return Report{}, fmt.Errorf("evalharness: MaterializeInto is required")
	}

	report := Report{
		SchemaVersion: "sensei.evalharness.report.v1",
		Arm:           ArmDeterministicExtraction,
		CapturedAt:    opts.CapturedAt,
		Limitations: []string{
			"deterministic HOW extraction produces observations, not architectural verdicts; this arm cannot name a defect and no detection rate is reported for it",
			"site coverage is a precondition for detection, never detection itself: evidence about the right file does not imply a correct finding about it",
		},
	}

	control, err := runOne(opts, "baseline", evalmutant.Baseline(), nil)
	if err != nil {
		return Report{}, fmt.Errorf("evalharness: baseline control: %w", err)
	}
	report.Baseline = control

	for _, d := range evalmutant.Defects() {
		m, err := evalmutant.Build(d)
		if err != nil {
			return Report{}, fmt.Errorf("evalharness: build %s: %w", d, err)
		}
		witness, err := evalmutant.WitnessFor(d)
		if err != nil {
			return Report{}, fmt.Errorf("evalharness: witness %s: %w", d, err)
		}
		res, err := runOne(opts, string(d), m, witness)
		if err != nil {
			return Report{}, fmt.Errorf("evalharness: run %s: %w", d, err)
		}
		report.Results = append(report.Results, res)
	}
	return report, nil
}

func runOne(opts Options, name string, m evalmutant.Mutant, witness evalmutant.Witness) (SiteResult, error) {
	root, err := opts.MaterializeInto(name)
	if err != nil {
		return SiteResult{}, err
	}
	if err := evalmutant.Materialize(root, m); err != nil {
		return SiteResult{}, err
	}

	res := SiteResult{
		Defect:      m.Defect,
		Statement:   m.Statement,
		DefectPaths: normalizePaths(m.TouchedPaths),
	}
	if witness != nil {
		present, detail, werr := witness(root)
		if werr != nil {
			return SiteResult{}, fmt.Errorf("witness: %w", werr)
		}
		res.DefectPresent, res.WitnessDetail = present, detail
	}

	doc, err := howextract.Extract(root, howextract.Options{
		CapturedAt: opts.CapturedAt,
		Repository: architecture.ClaimDocumentBinding{
			RepositoryDomain: opts.RepositoryDomain,
			// The tree digest identifies the exact content without requiring a
			// git history the mutant does not have. Revision stays unresolved
			// and says so, rather than borrowing the harness's own checkout.
			TreeDigestSHA256:  treeDigest(m),
			RevisionStatus:    "unavailable",
			GraphDigestStatus: "not_requested",
		},
		ResourceLimits: map[string]string{"arm": ArmDeterministicExtraction},
	})
	if err != nil {
		return SiteResult{}, fmt.Errorf("extract: %w", err)
	}

	observed := map[string]bool{}
	for _, obs := range doc.Observations {
		if p := strings.TrimSpace(obs.Evidence.SourceFile); p != "" {
			observed[p] = true
		}
	}
	res.ObservedPaths = sortedKeys(observed)
	for _, p := range res.DefectPaths {
		if observed[p] {
			res.CoveredDefectPaths = append(res.CoveredDefectPaths, p)
		}
	}
	sort.Strings(res.CoveredDefectPaths)

	res.Observations = len(doc.Observations)
	res.EvidenceReceipts = len(doc.RawEvidence)
	res.Limitations = len(doc.Limitations)
	res.DocumentDigest = doc.Receipt.OutputDocumentDigestSHA256
	// Stated, not inferred: this arm has no mechanism to name a defect.
	res.NamedTheDefect = false
	return res, nil
}

// normalizePaths drops the "(removed)" annotation Build uses for deleted files,
// so a path can be compared against an observation's source file.
func normalizePaths(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		out = append(out, strings.TrimSuffix(p, " (removed)"))
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// treeDigest identifies a mutant's exact content, computed from the files
// themselves so two runs of the same mutant bind to the same tree.
func treeDigest(m evalmutant.Mutant) string {
	paths := make([]string, 0, len(m.Files))
	for p := range m.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&b, "%s\x00%s\x00", p, m.Files[p])
	}
	return investigation.SHA256String(b.String())
}
