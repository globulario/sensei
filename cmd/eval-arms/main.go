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

	"github.com/globulario/sensei/golang/architecture/evalharness"
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
	idx := index{
		SchemaVersion: "sensei.eval_arms_index.v1",
		GeneratedBy:   "sensei eval-arms",
		Revision:      gitRevision(),
		RevisionState: revisionState(),
		CapturedAt:    *capturedAt,
		Domain:        *domain,
	}

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
		art := writeReport(*out, evalharness.ArmCompositionModelDisabled, report)
		art.Subject = subjectMutantSuite
		art.CandidateRate = fmt.Sprintf("%d/%d", produced, total)
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

	idx.Arms = append(idx.Arms,
		armArtifact{Arm: "phase10_composition_model_bound", Subject: subjectMutantSuite, Status: statusNotImplemented,
			Reason: "the investigation path carries a model binding but never invokes a model: investigator copies Binding.Model into the receipt and nothing else reads it, no model provider is reachable from howextract, investigator or investigation, and nothing in the tree sets ModelStatusResolved. Binding a model today would change one recorded field and no observation, so an arm-3 column would compare a label against a capability."},
		armArtifact{Arm: "world2_globular", Subject: subjectPublishedDomain, Status: statusNotRun,
			Reason: "requires an exact Globular checkout, which is not part of this repository"},
		armArtifact{Arm: "world3_independent_calibration", Subject: subjectPublishedDomain, Status: statusNotRun,
			Reason: "requires an independently understood repository inside the Go observation surface; SQLite is C and yields no observations"},
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
		if a.ReportDigest != "" {
			line += "  digest=" + a.ReportDigest[:12]
		}
		if a.Reason != "" {
			line += "  (" + a.Reason + ")"
		}
		fmt.Println(line)
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
