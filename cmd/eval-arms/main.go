// SPDX-License-Identifier: AGPL-3.0-only

// Command eval-arms runs the landed evaluation arms of issue #131 section 5
// over the architectural mutant suite and writes their reports.
//
// It exists because the arms were reachable only from Go tests, and #131's
// completion evidence asks for reports that are reproducible and
// content-addressed — which a test that prints to a log is not. This produces
// the artifacts: each arm's report as canonical JSON, its SHA-256, and a
// summary index binding both to the exact revision the run was made at.
//
// Two comparison SETS, not one rectangular matrix.
//
// Arms 1 and 2 are offline evaluators: they run against an arbitrary checkout
// and an onboarded-but-offline repository respectively. Arm 4 is the operational
// surface pair, and those REQUIRE an admitted, current, published graph. That is
// a capability and authority distinction between the things being compared, not
// harness friction, so the report carries the mutant suite and the
// published-domain comparison separately rather than forcing every arm through
// every subject.
//
// Arm 4 over the mutant suite is therefore recorded as
// UNAVAILABLE_BY_AUTHORITY_MODEL and kept in the report. It is a measured fact
// about the surfaces — briefing and impact are not defined over unpublished
// graphs — and it must not vanish once arm 4 runs somewhere it is defined.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/benchmark"
	"github.com/globulario/sensei/golang/architecture/evalharness"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/client"
)

type armArtifact struct {
	Arm           string `json:"arm"`
	Subject       string `json:"subject"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	ReportFile    string `json:"report_file,omitempty"`
	ReportDigest  string `json:"report_digest_sha256,omitempty"`
	SiteCoverage  string `json:"site_coverage,omitempty"`
	CandidateRate string `json:"candidate_rate,omitempty"`
	// CandidateGrounding is how many produced candidates cite only identities
	// that exist in the documents they were composed from — #131's candidate
	// grounding, in the half that needs no adjudicator.
	CandidateGrounding string `json:"candidate_grounding,omitempty"`
}

const (
	subjectMutantSuite     = "mutant_suite"
	subjectPublishedDomain = "published_domain"

	statusRan = "ran"
	// statusUnavailableByAuthority is not "not_run". The arm was attempted and
	// the system refused it, on purpose: the operational surfaces are not
	// defined over a graph nobody published. Recording it as a plain absence
	// would lose the finding.
	statusUnavailableByAuthority = "unavailable_by_authority_model"
	statusNotRun                 = "not_run"
	// statusNotImplemented is not "not_run" either. not_run means the arm could
	// run and this environment did not let it; this means the evaluated path
	// has no such behaviour to measure, so running it would produce a column
	// comparing a recorded label against a capability.
	statusNotImplemented = "not_implemented_in_evaluated_path"
	statusFailed         = "failed"
)

type index struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedBy   string        `json:"generated_by"`
	Revision      string        `json:"revision"`
	RevisionState string        `json:"revision_status"`
	CapturedAt    string        `json:"captured_at"`
	Domain        string        `json:"repository_domain"`
	Arms          []armArtifact `json:"arms"`
	// RunEnvelopeFile holds elapsed time, memory and machine identity. It is a
	// SEPARATE artifact and is deliberately NOT covered by any report digest:
	// replay identity is about the semantic result, and folding a millisecond
	// count into it would turn deterministic replay into a benchmark of the
	// scheduler.
	RunEnvelopeFile string `json:"run_envelope_file"`

	// Authority identifies the SENSEI SIDE of the run: the evaluator's own
	// revision and tree state, the seed and authored-corpus it consulted, the
	// build transaction stamp, and the provenance of the executing binary.
	//
	// Revision above already binds the checkout (#216). That is not the same
	// claim: a checkout can be advanced without rebuilding, and it says nothing
	// about which compiled graph answered. #131 requires every run to bind
	// "revision/tree, graph digest/status, policy/profile, provider versions",
	// and this supplies the half a target-repository binding cannot.
	//
	// It lives in the index rather than in any arm report on purpose. The index
	// carries no digest of its own, so recording it here cannot perturb the
	// per-arm replay identity that CI compares. It can only ever REDUCE what a
	// run claims — an unbound or drifted authority makes a run non-certifiable
	// and never makes a measurement look better — which is why adding it does
	// not violate the rule against benchmark-driven modification.
	Authority *benchmark.AuthorityState `json:"authority,omitempty"`
}

// runEnvelope is the volatile half: what a run cost, never what it concluded.
type runEnvelope struct {
	SchemaVersion  string            `json:"schema_version"`
	Revision       string            `json:"revision"`
	CapturedAt     string            `json:"captured_at"`
	StartedAtUnix  int64             `json:"started_at_unix"`
	ElapsedMsByArm map[string]int64  `json:"elapsed_ms_by_arm"`
	PeakHeapBytes  uint64            `json:"peak_heap_bytes"`
	TotalAllocated uint64            `json:"total_allocated_bytes"`
	Machine        map[string]string `json:"machine"`
	Note           string            `json:"note"`
}

func main() {
	out := flag.String("out", "", "directory to write reports into (required)")
	capturedAt := flag.String("captured-at", "", "explicit RFC3339 capture timestamp (required; a self-stamped report is not reproducible)")
	domain := flag.String("repo-domain", "example.com/eval", "repository domain the synthetic mutants are attributed to")
	publishedDomain := flag.String("published-domain", "", "an ADMITTED, PUBLISHED domain to baseline the operational briefing/impact surfaces against (arm 4); empty skips that comparison")
	addr := flag.String("addr", "localhost:10120", "Sensei server serving the published domain")
	var publishedFiles repeatable
	flag.Var(&publishedFiles, "published-file", "repo-relative file in the published domain to query; repeatable, and part of the run's pinned inputs")
	var worlds repeatable
	flag.Var(&worlds, "world", "an evaluation world as name=domain=path, e.g. world2_globular=github.com/globulario/Globular=/path/to/checkout; repeatable")
	flag.Parse()

	if strings.TrimSpace(*out) == "" || strings.TrimSpace(*capturedAt) == "" {
		fmt.Fprintln(os.Stderr, "sensei eval-arms: --out and --captured-at are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "sensei eval-arms: %v\n", err)
		os.Exit(1)
	}

	// The suite is materialized under --out so the trees a run measured are
	// kept beside the reports that describe them, rather than in a temp
	// directory the reader cannot inspect afterwards.
	workdir := filepath.Join(*out, "mutants")
	started := time.Now()
	elapsed := map[string]int64{}
	opts := evalharness.Options{
		RepositoryDomain: *domain,
		CapturedAt:       *capturedAt,
		MaterializeInto: func(name string) (string, error) {
			path := filepath.Join(workdir, name)
			return path, os.MkdirAll(path, 0o755)
		},
	}
	idx := newIndex(*capturedAt, *domain)

	armStart := time.Now()
	if report, err := evalharness.RunDeterministicExtraction(opts); err != nil {
		elapsed[evalharness.ArmDeterministicExtraction] = time.Since(armStart).Milliseconds()
		idx.Arms = append(idx.Arms, armArtifact{Arm: evalharness.ArmDeterministicExtraction, Subject: subjectMutantSuite, Status: statusFailed, Reason: err.Error()})
	} else {
		elapsed[evalharness.ArmDeterministicExtraction] = time.Since(armStart).Milliseconds()
		covered, total := report.SiteCoverageRate()
		art := writeReport(*out, evalharness.ArmDeterministicExtraction, report)
		art.Subject = subjectMutantSuite
		art.SiteCoverage = fmt.Sprintf("%d/%d", covered, total)
		idx.Arms = append(idx.Arms, art)
	}

	armStart = time.Now()
	if report, err := evalharness.RunCompositionArm(opts); err != nil {
		elapsed[evalharness.ArmCompositionModelDisabled] = time.Since(armStart).Milliseconds()
		idx.Arms = append(idx.Arms, armArtifact{Arm: evalharness.ArmCompositionModelDisabled, Subject: subjectMutantSuite, Status: statusFailed, Reason: err.Error()})
	} else {
		elapsed[evalharness.ArmCompositionModelDisabled] = time.Since(armStart).Milliseconds()
		produced, total := report.CandidateRate()
		grounded, candidates, dangling, groundingFailures := report.CandidateGrounding()
		art := writeReport(*out, evalharness.ArmCompositionModelDisabled, report)
		art.Subject = subjectMutantSuite
		art.CandidateRate = fmt.Sprintf("%d/%d", produced, total)
		if candidates > 0 || groundingFailures > 0 {
			art.CandidateGrounding = fmt.Sprintf("%d/%d post-validation", grounded, candidates)
			if dangling > 0 {
				art.CandidateGrounding += fmt.Sprintf(", %d dangling refs", dangling)
			}
			if groundingFailures > 0 {
				// Where a dangling reference actually surfaces: Compose rejects
				// it and the site fails, so it never reaches the rate above.
				art.CandidateGrounding += fmt.Sprintf(", %d site(s) did not compose", groundingFailures)
			}
		}
		idx.Arms = append(idx.Arms, art)
	}

	// Arm 4 over the mutant suite: attempted, and refused by the authority
	// model. Kept as a measured result rather than an absence — briefing and
	// impact are not defined over a graph nobody published, and that is a fact
	// about the surfaces worth carrying into the comparison.
	idx.Arms = append(idx.Arms, armArtifact{
		Arm: armBriefingImpactSurfaces, Subject: subjectMutantSuite, Status: statusUnavailableByAuthority,
		Reason: "the operational surfaces require an admitted, current, published graph: reads fail closed on graph freshness, and publishing a synthetic mutant domain is refused by registry admission. Neither wall was weakened to obtain a number.",
	})

	// Arm 4 where it IS defined: an admitted, published domain.
	idx.Arms = append(idx.Arms, runPublishedSurfaces(*out, *addr, *publishedDomain, publishedFiles, elapsed))

	idx.Arms = append(idx.Arms, runWorlds(*out, worlds, *capturedAt, elapsed)...)

	idx.Arms = append(idx.Arms,
		armArtifact{Arm: "phase10_composition_model_bound", Subject: subjectMutantSuite, Status: statusNotImplemented,
			Reason: "the investigation path carries a model binding but never invokes a model: investigator copies Binding.Model into the receipt and nothing else reads it, no model provider is reachable from howextract, investigator or investigation, and nothing in the tree sets ModelStatusResolved. Binding a model today would change one recorded field and no observation, so an arm-3 column would compare a label against a capability."},
	)

	// The volatile half, written as its own artifact and covered by no digest.
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	envelope := runEnvelope{
		SchemaVersion:  "sensei.eval_run_envelope.v1",
		Revision:       idx.Revision,
		CapturedAt:     *capturedAt,
		StartedAtUnix:  started.Unix(),
		ElapsedMsByArm: elapsed,
		PeakHeapBytes:  mem.HeapAlloc,
		TotalAllocated: mem.TotalAlloc,
		Machine: map[string]string{
			"goos": runtime.GOOS, "goarch": runtime.GOARCH, "go_version": runtime.Version(),
			"num_cpu": fmt.Sprint(runtime.NumCPU()),
		},
		Note: "elapsed, memory and machine identity are NOT part of any report digest: replay identity is the semantic result, and folding a millisecond count into it would make deterministic replay a benchmark of the scheduler",
	}
	idx.RunEnvelopeFile = "run_envelope.json"
	if data, err := json.MarshalIndent(envelope, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(*out, idx.RunEnvelopeFile), append(data, '\n'), 0o644)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei eval-arms: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(*out, "index.json"), data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei eval-arms: %v\n", err)
		os.Exit(1)
	}

	failures := 0
	for _, a := range idx.Arms {
		if a.Status == statusFailed {
			failures++
		}
		line := fmt.Sprintf("%-34s %-18s %s", a.Arm, a.Subject, a.Status)
		if a.SiteCoverage != "" {
			line += "  site_coverage=" + a.SiteCoverage
		}
		if a.CandidateRate != "" {
			line += "  candidates=" + a.CandidateRate
		}
		if a.CandidateGrounding != "" {
			line += "  grounded=" + a.CandidateGrounding
		}
		if a.ReportDigest != "" {
			line += "  digest=" + a.ReportDigest[:12]
		}
		if a.Reason != "" {
			line += "  (" + a.Reason + ")"
		}
		fmt.Println(line)
	}
	if idx.Authority != nil {
		fmt.Printf("\nevaluating authority: %s\n", benchmark.AuthoritySummary(idx.Authority))
		if idx.Authority.CaptureState != benchmark.AuthorityCaptureBound {
			fmt.Printf("  this run is NOT certifiable against another: %s\n", idx.Authority.CaptureReason)
		}
	}
	fmt.Printf("\nindex: %s\n", filepath.Join(*out, "index.json"))

	// An arm that was SUPPOSED to run and did not is a broken evaluation, not a
	// result. It exits non-zero so a CI run cannot publish an index of failures
	// as though it were evidence — while not_run and
	// unavailable_by_authority_model, which are findings, leave the exit code
	// alone. The index is written first either way, because the artifact
	// explaining the failure is the useful thing to keep.
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\nsensei eval-arms: %d arm(s) failed to run; the index records each failure\n", failures)
		os.Exit(1)
	}
}

// armBriefingImpactSurfaces is #131 section 5's fourth comparison: the EXISTING
// operational surfaces, which are the only arm requiring publication authority.
const armBriefingImpactSurfaces = "briefing_and_impact_surfaces"

// repeatable collects a flag given more than once.
type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, ",") }
func (r *repeatable) Set(v string) error {
	*r = append(*r, strings.TrimSpace(v))
	return nil
}

// publishedSurfaceReport is arm 4 where it is defined: the operational surfaces
// over an admitted, published domain, at a pinned file list.
//
// The file list is an INPUT and is recorded, because a baseline over a set the
// runner chose at random is not comparable with the next one.
type publishedSurfaceReport struct {
	SchemaVersion string                  `json:"schema_version"`
	Arm           string                  `json:"arm"`
	CapturedAt    string                  `json:"captured_at"`
	Domain        string                  `json:"repository_domain"`
	Address       string                  `json:"server_address"`
	Files         []string                `json:"pinned_files"`
	Results       []publishedSurfaceEntry `json:"results"`
	Limitations   []string                `json:"limitations"`
}

type publishedSurfaceEntry struct {
	File            string `json:"file"`
	ImpactNodes     int    `json:"impact_nodes"`
	BriefingStatus  string `json:"briefing_status"`
	BriefingRefused string `json:"briefing_refused,omitempty"`
	ImpactRefused   string `json:"impact_refused,omitempty"`
}

// runPublishedSurfaces baselines briefing and impact over a published domain.
//
// It never publishes anything, never registers a domain, and never relaxes a
// freshness check. If the server declines, the decline is the result.
func runPublishedSurfaces(out, addr, domain string, files []string, elapsed map[string]int64) armArtifact {
	art := armArtifact{Arm: armBriefingImpactSurfaces, Subject: subjectPublishedDomain}
	if strings.TrimSpace(domain) == "" || len(files) == 0 {
		art.Status = statusNotRun
		art.Reason = "no --published-domain and --published-file given; the operational surfaces are only defined over an admitted, published graph, so there is nothing to baseline"
		return art
	}
	start := time.Now()
	conn, err := client.Dial(addr)
	if err != nil {
		art.Status = statusNotRun
		art.Reason = fmt.Sprintf("no server at %s: %v", addr, err)
		return art
	}
	defer conn.Close()

	report := publishedSurfaceReport{
		SchemaVersion: "sensei.eval_published_surfaces.v1",
		Arm:           armBriefingImpactSurfaces,
		CapturedAt:    "",
		Domain:        domain,
		Address:       addr,
		Files:         append([]string{}, files...),
	}
	sort.Strings(report.Files)
	for _, file := range report.Files {
		entry := publishedSurfaceEntry{File: file}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		if resp, err := conn.Impact(ctx, file, domain); err != nil {
			entry.ImpactRefused = err.Error()
		} else {
			entry.ImpactNodes = len(resp.GetDirectInvariants()) + len(resp.GetDirectFailureModes()) +
				len(resp.GetDirectIntents()) + len(resp.GetForbiddenFixes()) + len(resp.GetRequiredTests()) +
				len(resp.GetDirectArchitecture())
		}
		if resp, err := conn.Briefing(ctx, file, "", "standard", domain); err != nil {
			entry.BriefingRefused = err.Error()
		} else {
			entry.BriefingStatus = resp.GetStatus().String()
		}
		cancel()
		report.Results = append(report.Results, entry)
	}
	elapsed[armBriefingImpactSurfaces] = time.Since(start).Milliseconds()

	art = writeReport(out, armBriefingImpactSurfaces, report)
	art.Subject = subjectPublishedDomain
	answered := 0
	for _, r := range report.Results {
		if r.ImpactRefused == "" {
			answered++
		}
	}
	art.SiteCoverage = fmt.Sprintf("%d/%d", answered, len(report.Results))
	return art
}

// requiredWorlds are the evaluation worlds #131 defines. Each one is always
// present in the index, whether it ran or not.
//
// World 1 is this repository, and it is required for the same reason as the
// other two: #131 asks for all worlds to run from exact pinned inputs, and a
// self-measurement that only ever happened by hand is not pinned. It measures
// the same extraction lane as worlds 2 and 3, which is what makes the three
// comparable — the investigation-composition lane is measured separately by
// the phase10_composition_* arms over the mutant suite.
var requiredWorlds = []string{"world1_sensei_self", "world2_globular", "world3_independent_calibration"}

// reservedArmNames are the arm names this command writes itself; a world may
// not claim one and overwrite its report.
var reservedArmNames = map[string]bool{
	"deterministic_extraction_without_composition": true,
	"phase10_composition_model_disabled":           true,
	"phase10_composition_model_bound":              true,
	"briefing_and_impact_surfaces":                 true,
	"evaluation_world":                             true,
}

// worldReport is one evaluation world run over an external checkout.
//
// It deliberately records the repository DOMAIN and the binding, never the
// local path: a report naming /home/somebody/checkout is not comparable with
// the same world run elsewhere, and the path is not what was measured.
type worldReport struct {
	SchemaVersion  string         `json:"schema_version"`
	World          string         `json:"world"`
	CapturedAt     string         `json:"captured_at"`
	Domain         string         `json:"repository_domain"`
	Revision       string         `json:"revision,omitempty"`
	RevisionStatus string         `json:"revision_status"`
	TreeDigest     string         `json:"tree_digest_sha256,omitempty"`
	Observations   int            `json:"observations"`
	EvidenceCount  int            `json:"evidence_receipts"`
	FilesCited     int            `json:"files_cited"`
	ByProvider     map[string]int `json:"observations_by_provider"`
	ByPredicate    map[string]int `json:"observations_by_predicate"`
	CoverageStatus map[string]int `json:"coverage_status_counts"`
	// AbsenceIntegrity answers #131's "unknown/unavailable truthfulness" in the
	// half that needs no adjudicator: every coverage entry claiming an absence
	// must SAY WHY, and a claim to have searched and found nothing must show it
	// searched. It does not judge whether the stated reason is honest — only
	// whether one exists and whether the entry carries what its status implies.
	AbsenceIntegrity absenceIntegrity `json:"absence_integrity"`
	// EvidenceResolution answers #131's "evidence coverage by provider and
	// target": for each provider, how many of its observations carry an anchor
	// that still resolves in the bound tree.
	//
	// Mechanical, not adjudicated. It measures whether an observation POINTS at
	// something real — file present, line range inside the file — never whether
	// what it says is true. Precision and recall need a reference set and are
	// deliberately absent rather than approximated by this.
	EvidenceResolution map[string]resolutionCounts `json:"evidence_resolution_by_provider"`
	EvidenceTotals     resolutionCounts            `json:"evidence_resolution_totals"`
	TargetResolution   resolutionCounts            `json:"evidence_resolution_by_target"`
	// UnresolvedExamples names a bounded, deterministic sample of the anchors
	// that did not resolve.
	//
	// A count says a number is wrong; an example says where to look. Bounded so
	// a systematically broken extractor cannot turn the report into its own log,
	// and sorted so the sample is part of the reproducible identity rather than
	// whatever the map yielded first.
	UnresolvedExamples []string `json:"unresolved_anchor_examples,omitempty"`
	Limitations        []string `json:"limitations"`
	BlockingCount      int      `json:"blocking_limitations"`
	SurfaceExcluded    bool     `json:"outside_observation_surface"`
}

// absenceIntegrity is whether the report's claims of absence are backed.
//
// The four unknown-ish statuses do not mean the same thing — unavailable,
// not_configured, skipped_with_reason and searched_no_result are different
// worlds — and each carries an obligation. Recorded separately from the
// coverage tally, because "how many said nothing" and "how many said nothing
// WITHOUT saying why" are different facts, and only the second is a defect.
type absenceIntegrity struct {
	// AbsenceClaims is how many entries reported an absence of any kind.
	AbsenceClaims int `json:"absence_claims"`
	// Unexplained is how many of those carry no reason at all.
	Unexplained int `json:"unexplained"`
	// SearchedWithoutProof is how many claimed searched_no_result while
	// carrying no source snapshot digest — asserting a search happened without
	// showing the tree it searched.
	SearchedWithoutProof int `json:"searched_no_result_without_snapshot"`
	// Examples names the offending providers, bounded and sorted.
	Examples []string `json:"examples,omitempty"`
}

// checkAbsenceIntegrity verifies each claimed absence against its own status.
func checkAbsenceIntegrity(coverage []investigation.CoverageEntry) absenceIntegrity {
	var out absenceIntegrity
	offenders := map[string]bool{}
	for _, c := range coverage {
		switch c.Status {
		case investigation.CoverageUnavailable, investigation.CoverageNotConfigured, investigation.CoverageSkipped:
			out.AbsenceClaims++
			if strings.TrimSpace(c.Reason) == "" {
				out.Unexplained++
				offenders[fmt.Sprintf("%s: status %s with no reason", c.ProviderID, c.Status)] = true
			}
		case investigation.CoverageNoResult:
			out.AbsenceClaims++
			if strings.TrimSpace(c.SourceSnapshotDigestSHA256) == "" {
				out.SearchedWithoutProof++
				offenders[fmt.Sprintf("%s: searched_no_result with no source snapshot digest", c.ProviderID)] = true
			}
		}
	}
	for o := range offenders {
		out.Examples = append(out.Examples, o)
	}
	sort.Strings(out.Examples)
	if len(out.Examples) > unresolvedExampleLimit {
		out.Examples = append(out.Examples[:unresolvedExampleLimit:unresolvedExampleLimit],
			fmt.Sprintf("… %d further distinct unbacked absence claims not listed", len(offenders)-unresolvedExampleLimit))
	}
	return out
}

// resolutionCounts is how an anchor set resolved against the tree that was read.
type resolutionCounts struct {
	// Resolved: the cited file exists and the line range lies inside it.
	Resolved int `json:"resolved"`
	// MissingFile: the observation names a file the bound tree does not have.
	MissingFile int `json:"missing_file"`
	// NotARegularFile: the path exists but is a directory or other non-file.
	// Distinct from missing because the causes are different — a cited
	// directory is an extractor anchoring mistake, an absent path is a stale or
	// wrong reference — and collapsing them would report the first as the
	// second (#216 was exactly this shape).
	NotARegularFile int `json:"not_a_regular_file"`
	// LineOutOfRange: the file exists but the cited lines do not.
	LineOutOfRange int `json:"line_out_of_range"`
	// NoAnchor: the observation cites no source file at all. Not a failure on
	// its own — some observations are about the repository rather than a line —
	// but it cannot be verified either, so it is counted apart from both.
	NoAnchor int `json:"no_anchor"`
}

// resolveEvidence measures whether each observation's anchor still resolves.
//
// This is the one metric from #131's list that can be produced without a frozen
// reference set: it asks whether an observation points at something that exists,
// which the filesystem answers, rather than whether it is correct, which only an
// adjudicator can. Reported per provider because the extraction is heavily
// skewed — one provider produces about two thirds of all observations — so a
// single overall number would mostly describe that provider.
func resolveEvidence(root string, observations []architecture.Fact) (map[string]resolutionCounts, resolutionCounts, resolutionCounts, []string) {
	lines := map[string]int{}
	lineCount := func(rel string) int {
		if n, ok := lines[rel]; ok {
			return n
		}
		// -1 absent, -2 present but not a regular file, otherwise the line count.
		n := -1
		full := filepath.Join(root, filepath.FromSlash(rel))
		if info, err := os.Lstat(full); err == nil {
			if !info.Mode().IsRegular() {
				n = -2
			} else if data, err := os.ReadFile(full); err == nil {
				n = bytes.Count(data, []byte("\n"))
				if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
					n++
				}
			}
		}
		lines[rel] = n
		return n
	}
	byProvider := map[string]resolutionCounts{}
	var totals resolutionCounts
	targets := map[string]string{}
	unresolved := map[string]bool{}
	for _, o := range observations {
		counts := byProvider[o.Extractor]
		file := strings.TrimSpace(o.Evidence.SourceFile)
		switch {
		case file == "":
			counts.NoAnchor++
			totals.NoAnchor++
		default:
			n := lineCount(file)
			switch {
			case n == -2:
				counts.NotARegularFile++
				totals.NotARegularFile++
				targets[file] = "not_a_regular_file"
				unresolved[fmt.Sprintf("%s: %s is not a regular file", o.Extractor, file)] = true
			case n < 0:
				counts.MissingFile++
				totals.MissingFile++
				targets[file] = "missing_file"
				unresolved[fmt.Sprintf("%s: %s is absent from the bound tree", o.Extractor, file)] = true
			case o.Evidence.LineStart > 0 && (o.Evidence.LineStart > n || o.Evidence.LineEnd > n):
				counts.LineOutOfRange++
				totals.LineOutOfRange++
				unresolved[fmt.Sprintf("%s: %s cites line %d-%d of a %d-line file", o.Extractor, file, o.Evidence.LineStart, o.Evidence.LineEnd, n)] = true
				if targets[file] == "" || targets[file] == "resolved" {
					targets[file] = "line_out_of_range"
				}
			default:
				counts.Resolved++
				totals.Resolved++
				if targets[file] == "" {
					targets[file] = "resolved"
				}
			}
		}
		byProvider[o.Extractor] = counts
	}
	var byTarget resolutionCounts
	for _, state := range targets {
		switch state {
		case "missing_file":
			byTarget.MissingFile++
		case "not_a_regular_file":
			byTarget.NotARegularFile++
		case "line_out_of_range":
			byTarget.LineOutOfRange++
		default:
			byTarget.Resolved++
		}
	}
	examples := make([]string, 0, len(unresolved))
	for e := range unresolved {
		examples = append(examples, e)
	}
	sort.Strings(examples)
	if len(examples) > unresolvedExampleLimit {
		examples = append(examples[:unresolvedExampleLimit:unresolvedExampleLimit],
			fmt.Sprintf("… %d further distinct unresolved anchors not listed", len(unresolved)-unresolvedExampleLimit))
	}
	return byProvider, totals, byTarget, examples
}

// unresolvedExampleLimit bounds the sample. The truncation is always reported,
// because a silently cut list reads as a complete one.
const unresolvedExampleLimit = 10

// runWorlds runs each requested world over its checkout and writes a pinned,
// content-addressed report.
//
// Worlds 2 and 3 were run by hand to produce their first numbers. A measurement
// that only exists as something somebody typed once is not the reproducible
// evidence this issue asks for, so they run here, bound the way any other
// result is bound, or they are recorded as not run.
func runWorlds(out string, specs []string, capturedAt string, elapsed map[string]int64) []armArtifact {
	var arts []armArtifact
	ran := map[string]bool{}
	names := map[string]bool{}
	for _, spec := range specs {
		name, domain, path, err := parseWorldSpec(spec)
		if err != nil {
			arts = append(arts, armArtifact{Arm: "evaluation_world", Subject: subjectPublishedDomain, Status: statusFailed,
				Reason: err.Error()})
			continue
		}
		// Two worlds under one name would overwrite each other's report after
		// the first digest was already recorded, leaving an index whose digest
		// does not describe the file it names.
		if names[name] || reservedArmNames[name] {
			arts = append(arts, armArtifact{Arm: name, Subject: subjectPublishedDomain, Status: statusFailed,
				Reason: "world name collides with another arm or world in this run; every arm writes its own report"})
			continue
		}
		names[name] = true
		start := time.Now()
		report, err := runWorld(name, domain, path, capturedAt)
		elapsed[name] = time.Since(start).Milliseconds()
		if err != nil {
			arts = append(arts, armArtifact{Arm: name, Subject: subjectPublishedDomain, Status: statusFailed, Reason: err.Error()})
			continue
		}
		art := writeReport(out, name, report)
		art.Subject = subjectPublishedDomain
		art.SiteCoverage = fmt.Sprintf("%d obs", report.Observations)
		if report.SurfaceExcluded {
			art.Reason = "the repository holds no files in the Go observation surface; the empty result describes the surface, not the repository"
		}
		arts = append(arts, art)
		ran[name] = true
	}
	// A world that was not supplied is recorded, not omitted. An index listing
	// only the worlds somebody happened to have a checkout for would read as a
	// complete protocol.
	for _, name := range requiredWorlds {
		if ran[name] || names[name] {
			continue
		}
		arts = append(arts, armArtifact{Arm: name, Subject: subjectPublishedDomain, Status: statusNotRun,
			Reason: "no --world " + name + "=<domain>=<path> given; this world needs an external checkout, which is not part of this repository"})
	}
	return arts
}

// parseWorldSpec reads name=domain=path.
func parseWorldSpec(spec string) (name, domain, path string, err error) {
	parts := strings.SplitN(spec, "=", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("--world must be name=domain=path, got %q", spec)
	}
	return parts[0], parts[1], parts[2], nil
}

func runWorld(name, domain, path, capturedAt string) (worldReport, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return worldReport{}, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return worldReport{}, fmt.Errorf("%s: not a directory", path)
	}
	binding, err := worldBinding(abs, domain)
	if err != nil {
		return worldReport{}, err
	}
	doc, err := howextract.Extract(abs, howextract.Options{CapturedAt: capturedAt, Repository: binding})
	if err != nil {
		return worldReport{}, err
	}
	report := worldReport{
		SchemaVersion: "sensei.eval_world.v1", World: name, CapturedAt: capturedAt, Domain: domain,
		Revision: binding.Revision, RevisionStatus: binding.RevisionStatus, TreeDigest: binding.TreeDigestSHA256,
		Observations: len(doc.Observations), EvidenceCount: len(doc.RawEvidence),
		ByProvider: map[string]int{}, ByPredicate: map[string]int{}, CoverageStatus: map[string]int{},
	}
	files := map[string]bool{}
	for _, o := range doc.Observations {
		report.ByProvider[o.Extractor]++
		report.ByPredicate[o.Predicate]++
		if o.Evidence.SourceFile != "" {
			files[o.Evidence.SourceFile] = true
		}
	}
	report.FilesCited = len(files)
	report.EvidenceResolution, report.EvidenceTotals, report.TargetResolution, report.UnresolvedExamples = resolveEvidence(abs, doc.Observations)
	for _, c := range doc.Coverage {
		report.CoverageStatus[string(c.Status)]++
	}
	report.AbsenceIntegrity = checkAbsenceIntegrity(doc.Coverage)
	for _, l := range doc.Limitations {
		// A limitation reason quotes whatever path the extractor read, so it
		// carries this machine's checkout location. Rewritten to a repository-
		// relative form: two machines that measured the same tree must produce
		// the same report, and where the checkout happens to live is not part
		// of what was measured.
		report.Limitations = append(report.Limitations, l.Source+": "+relocateRoot(abs, l.Reason))
		if l.Blocking {
			report.BlockingCount++
		}
		if strings.Contains(l.Reason, "observation surface") {
			report.SurfaceExcluded = true
		}
	}
	sort.Strings(report.Limitations)
	return report, nil
}

// worldBinding identifies the tree that was actually read.
//
// A clean checkout binds to its revision. A dirty one binds to the canonical
// tree digest and names NO revision, because the extraction did not read that
// commit (#216) — the same rule the composed document enforces, applied before
// it has to.
// relocateRoot rewrites absolute references to the checkout as "<repo>/...".
func relocateRoot(root, text string) string {
	text = strings.ReplaceAll(text, root+string(os.PathSeparator), "<repo>/")
	return strings.ReplaceAll(text, root, "<repo>")
}

func worldBinding(root, domain string) (architecture.ClaimDocumentBinding, error) {
	binding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  domain,
		GraphDigestStatus: architecture.GraphDigestNotRequested,
	}
	rev, status, _ := architecture.ResolveRevision(root, true)
	if status != architecture.RevisionResolved {
		return binding, fmt.Errorf("%s: repository revision is %s; an evaluation world must identify the tree it read", domain, status)
	}
	// An ignored .go file is still compiled into the semantic input, so it can
	// change the report while git reports nothing: `git status` does not list it
	// and `git add -A` skips it, so neither the revision nor the tree digest
	// below covers it. Neither identity would describe what was measured, so the
	// world is refused rather than labelled with an identity that excludes part
	// of its own input.
	inputs, err := gosemantics.SemanticInputFiles(root)
	if err != nil {
		return binding, fmt.Errorf("%s: %w", domain, err)
	}
	// SemanticInputFiles returns absolute paths and UncommittedSourceFiles skips
	// those, so the check silently passes on everything unless they are made
	// repository-relative first.
	rel := make([]string, 0, len(inputs))
	for _, in := range inputs {
		r, relErr := filepath.Rel(root, in)
		if relErr != nil {
			return binding, fmt.Errorf("%s: %w", domain, relErr)
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	unbound, err := architecture.UncommittedSourceFiles(root, rev, rel)
	if err != nil {
		return binding, fmt.Errorf("%s: %w", domain, err)
	}
	if ignored, err := ignoredPaths(root, unbound); err != nil {
		return binding, fmt.Errorf("%s: %w", domain, err)
	} else if len(ignored) > 0 {
		return binding, fmt.Errorf("%s: %d semantic input file(s) are git-ignored, so no revision or tree digest can identify what was read (%s)",
			domain, len(ignored), strings.Join(truncate(ignored, 5), ", "))
	}
	// Cleanliness is judged over the whole worktree, not just the Go inputs: the
	// composition reads non-Go files too (manifests, generated docs), and a tree
	// whose Go files happen to be committed is still not the commit it names.
	dirty, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return binding, fmt.Errorf("%s: %w", domain, err)
	}
	if strings.TrimSpace(string(dirty)) == "" && len(unbound) == 0 {
		binding.Revision = rev
		binding.RevisionStatus = architecture.RevisionResolved
		return binding, nil
	}
	digest, err := worktreeTreeDigest(root)
	if err != nil {
		return binding, err
	}
	binding.RevisionStatus = architecture.RevisionUnavailable
	binding.TreeDigestSHA256 = digest
	return binding, nil
}

// ignoredPaths returns the subset git refuses to track, which is therefore in
// neither the commit nor any tree digest.
func ignoredPaths(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", root, "check-ignore", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		// check-ignore exits 1 when nothing matched, which is not a failure.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("git check-ignore: %w", err)
	}
	var ignored []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ignored = append(ignored, line)
		}
	}
	sort.Strings(ignored)
	return ignored, nil
}

func truncate(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return append(append([]string{}, in[:n]...), "…")
}

// worktreeTreeDigest is the canonical Sensei tree identity of a dirty working
// tree: an isolated index is seeded from HEAD, every difference staged into it,
// and the resulting tree digested. The repository's own index and working tree
// are never touched.
func worktreeTreeDigest(root string) (string, error) {
	tmp, err := os.MkdirTemp("", "sensei-eval-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmp, "index"))
	for _, args := range [][]string{{"read-tree", "HEAD"}, {"add", "-A"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	writeTree := exec.Command("git", "-C", root, "write-tree")
	writeTree.Env = env
	treeOut, err := writeTree.Output()
	if err != nil {
		return "", fmt.Errorf("git write-tree: %w", err)
	}
	tree := strings.TrimSpace(string(treeOut))
	lsTree, err := exec.Command("git", "-C", root, "ls-tree", "-r", "-z", "--full-tree", tree).Output()
	if err != nil {
		return "", fmt.Errorf("git ls-tree: %w", err)
	}
	return sha256Hex(lsTree), nil
}

func writeReport(dir, arm string, report any) armArtifact {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return armArtifact{Arm: arm, Status: statusFailed, Reason: err.Error()}
	}
	data = append(data, '\n')
	name := arm + ".json"
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return armArtifact{Arm: arm, Status: statusFailed, Reason: err.Error()}
	}
	return armArtifact{Arm: arm, Status: statusRan, ReportFile: name, ReportDigest: sha256Hex(data)}
}

// newIndex builds the run index, binding BOTH halves of a run's identity: the
// evaluator's checkout (#216) and the Sensei authority that answered (#254).
//
// The authority is captured here, before any arm writes into the working
// directory, so the observation describes the tree the run started from rather
// than the harness's own output.
func newIndex(capturedAt, domain string) index {
	authority := benchmark.CaptureAuthorityState(".")
	return index{
		SchemaVersion: "sensei.eval_arms_index.v1",
		GeneratedBy:   "sensei eval-arms",
		Revision:      gitRevision(),
		RevisionState: revisionState(),
		CapturedAt:    capturedAt,
		Domain:        domain,
		Authority:     &authority,
	}
}

// gitRevision and revisionState bind the run to the tree it measured. A report
// that names no revision cannot be compared with the next one, and a dirty tree
// must not claim a commit it was not read from (#216).
func gitRevision() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func revisionState() string {
	if gitRevision() == "" {
		return "unavailable"
	}
	out, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return "unavailable"
	}
	if strings.TrimSpace(string(out)) != "" {
		return "dirty"
	}
	return "resolved"
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
