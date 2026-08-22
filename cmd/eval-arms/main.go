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
	"github.com/globulario/sensei/golang/architecture/evalmodel"
	"github.com/globulario/sensei/golang/architecture/evalsample"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/modelexec"
	"github.com/globulario/sensei/golang/architecture/whyinvestigation"
	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
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
	// SampleManifestDigest is the sample's SELF-EXCLUDING identity, which a
	// reference-set release names as its sample_manifest_digest_sha256. It is
	// deliberately not ReportDigest: that one hashes the bytes on disk, and the
	// bytes contain this value, so the two can never be equal.
	SampleManifestDigest string `json:"sample_manifest_digest_sha256,omitempty"`
	// AcquisitionFile/Digest name the frozen model measurements this arm
	// produced, so a later scoring run can be pointed at exact material.
	AcquisitionFile   string `json:"acquisition_file,omitempty"`
	AcquisitionDigest string `json:"acquisition_digest_sha256,omitempty"`
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
	// armCompositionModelBound is #131's optional-model arm. It is named here
	// rather than repeated as a literal so the index, the report file and the
	// required-arm list cannot drift apart.
	armCompositionModelBound = "phase10_composition_model_bound"
	statusFailed             = "failed"
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
	var modelProviderArgs repeatable
	var labelFiles repeatable
	modelProviderID := flag.String("model-provider-id", "", "arm 3: provider id to bind (empty leaves the model arm not_run)")
	modelProviderVersion := flag.String("model-provider-version", "", "arm 3: provider version; a name without a version cannot distinguish two behaviours")
	modelName := flag.String("model-name", "", "arm 3: model to request")
	modelProviderPath := flag.String("model-provider-path", "", "arm 3: executable implementing the command bridge")
	modelPromptContract := flag.String("model-prompt-contract", "", "arm 3: identity of the exact prompt/schema contract the bridge uses")
	// The seed is COMMITTED before labels exist (protocol section 6.2). It is a
	// required input rather than a defaulted one: a seed the runner chose
	// silently is a seed nobody can be shown to have fixed in advance, and the
	// freeze order is the only thing separating a sample from a re-draw taken
	// after somebody saw a score.
	selectionSeed := flag.String("selection-seed", "", "committed seed ordering the frozen sample manifest; required to draw a sample, and changing it creates a new sample version")
	protocolFile := flag.String("protocol-file", protocolPath, "the frozen reference protocol the sample serves; its digest is recorded in the manifest")
	// The ID travels WITH the file. Forwarding a different protocol document
	// while the identity stayed hard-coded would produce a manifest naming v1
	// and carrying another protocol's digest — a ruler whose identity is a lie
	// about itself, which is worse than one that refuses to be built.
	protocolIDFlag := flag.String("protocol-id", protocolID, "identity of the protocol named by --protocol-file; change both together or neither")
	referenceSetPath := flag.String("reference-set", "", "arm 3: frozen reference-set release manifest to score against; without one the scorer reports reference_set_absent")
	flag.Var(&labelFiles, "label-file", "arm 3: a label file the release names; repeatable, and every named file must be supplied")
	flag.Var(&modelProviderArgs, "model-provider-arg", "arm 3: argument for the bridge executable; repeatable, passed without a shell")
	flag.Var(&worlds, "world", "an evaluation world as name=domain=path, e.g. world2_globular=github.com/globulario/Globular=/path/to/checkout; repeatable")
	flag.Parse()

	// The protocol's identity and its document move together or not at all.
	//
	// Checked by VALUE, not by whether the flags were mentioned. An earlier
	// version compared only presence, so `--protocol-file <the v1 document>
	// --protocol-id v2` passed: it recorded the v1 digest under a v2 identity
	// AND made the completeness check below think a custom protocol was in use,
	// disabling the very guard that protects v1. Naming both flags is not the
	// same as keeping them consistent.
	//
	// This pins the pair this binary knows. It cannot validate that some other
	// id belongs to some other document — nothing here could — but it can
	// refuse to let either known default move without its counterpart.
	// Read ONCE, here, and carry the bytes' digest forward. Validating the pair
	// now and re-reading the file in writeSample leaves a window: the arms run
	// for minutes, and a file edited in between would be validated as one
	// document and recorded as another — reproducing the identity split this
	// check exists to prevent.
	protocolDigest, protocolErr := fileDigest(*protocolFile)
	defaultFile := isDefaultProtocolDocument(*protocolFile, protocolDigest, protocolErr)
	defaultID := *protocolIDFlag == protocolID
	if defaultFile != defaultID {
		fmt.Fprintln(os.Stderr, "sensei eval-arms: --protocol-file and --protocol-id disagree about which protocol this is.")
		fmt.Fprintf(os.Stderr, "  file=%s\n  id=%s\n", *protocolFile, *protocolIDFlag)
		fmt.Fprintln(os.Stderr, "  One names the known default and the other does not, so the manifest would")
		fmt.Fprintln(os.Stderr, "  record one protocol's digest under another protocol's identity.")
		os.Exit(2)
	}

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

	var sampledWorlds []evalsample.World
	idx.Arms = append(idx.Arms, runWorlds(*out, worlds, *capturedAt, elapsed, &sampledWorlds)...)
	// Step 9 of the #131 handoff: freeze the SELECTION from the worlds this run
	// pinned, before any label exists. It is written from the same documents
	// the reports above describe, so the sample and the measurement cannot
	// describe two different extraction runs.
	// A sample stamped with a protocol whose worlds did not all run claims
	// compliance with a definition it did not follow. The default protocol
	// consumes every world in requiredWorlds, so drawing from a subset under it
	// is exactly the substitution this harness refuses elsewhere — and it is
	// the case that looked safe, because nothing was swapped, only missing.
	missing := missingRequiredWorlds(sampledWorlds)
	// The protocol consumes FOUR worlds, and the fourth is the mutant suite,
	// which is not a checkout and so never appeared in requiredWorlds. A run
	// whose mutant arms failed would otherwise have written a v1 sample over an
	// incomplete v1 evaluation — the same omission defect one level out, in the
	// world that does not look like a world.
	missing = append(missing, incompleteMutantSuite(idx.Arms)...)
	// Gating on the mutant arms' STATUS is not the same as sampling them. The
	// protocol consumes the mutant suite as its fourth world, and nothing here
	// puts that world's observations into evalsample.Build — sampledWorlds is
	// populated only from checkouts. So a v1 manifest would carry three worlds
	// while claiming a protocol that defines four.
	//
	// Typed as a blocker rather than quietly omitted. Wiring the suite's
	// material into the sampler is real work and belongs in its own change;
	// until then a v1 sample cannot honestly be drawn, and an operator who
	// wants a reduced set can bind a protocol that defines one.
	missing = append(missing, "mutant suite (its material is not represented in the sample manifest; the sampler draws only from checkout worlds)")
	sort.Strings(missing)
	idx.Arms = append(idx.Arms, writeSample(*out, *protocolFile, *protocolIDFlag, protocolDigest, protocolErr, defaultID, missing, sampledWorlds, *selectionSeed, *capturedAt))

	idx.Arms = append(idx.Arms, runModelBoundArm(*out, *capturedAt, modelArmConfig{
		ProviderID:      *modelProviderID,
		ProviderVersion: *modelProviderVersion,
		ModelName:       *modelName,
		ProviderPath:    *modelProviderPath,
		ProviderArgs:    modelProviderArgs,
		PromptContract:  *modelPromptContract,
		Domain:          *domain,
		ReferenceSet:    *referenceSetPath,
		LabelFiles:      labelFiles,
	}, elapsed))

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

	// RemoteAuthority is the authority of the SERVER that answered, which is a
	// different subject from the index's local evaluator authority. This arm's
	// measurements come from --addr, and that server may have been built from
	// another revision and may serve another graph. Recording only the local
	// checkout would let two runs against distinct remote authorities carry
	// identical authority blocks and read as comparable.
	RemoteAuthority *remoteAuthority `json:"remote_authority,omitempty"`
}

// remoteAuthority is what the answering server states about itself. It is the
// server's own claim, carried verbatim: this harness cannot verify a remote
// build, and must not present an unverified claim as if it were established.
type remoteAuthority struct {
	Observed         bool   `json:"observed"`
	Reason           string `json:"reason,omitempty"`
	Authoritative    bool   `json:"authoritative,omitempty"`
	SourceRepoCommit string `json:"source_repo_commit,omitempty"`
	GraphBuildCommit string `json:"graph_build_commit,omitempty"`
	FreshnessState   string `json:"graph_freshness_state,omitempty"`
	SeedState        string `json:"seed_state,omitempty"`
	BuildProvenance  string `json:"build_provenance_state,omitempty"`
	// The remote analogue of the local authority's seed identity: which
	// compiled graph the answering server actually served.
	EmbeddedSeedDigestSHA256   string `json:"embedded_seed_digest_sha256,omitempty"`
	LiveStoreGraphDigestSHA256 string `json:"live_store_graph_digest_sha256,omitempty"`
	LiveStoreGraphTripleCount  int64  `json:"live_store_graph_triple_count,omitempty"`
}

type publishedSurfaceEntry struct {
	File            string `json:"file"`
	ImpactNodes     int    `json:"impact_nodes"`
	BriefingStatus  string `json:"briefing_status"`
	BriefingRefused string `json:"briefing_refused,omitempty"`
	ImpactRefused   string `json:"impact_refused,omitempty"`
}

// observedRemoteAuthority records what the answering server said about its own
// authority. A response that carries none is recorded as unobserved with a
// typed reason rather than omitted: a missing block must not read as a server
// whose authority happened to match the local checkout.
func observedRemoteAuthority(a *awarenesspb.GraphAuthority) *remoteAuthority {
	if a == nil {
		return &remoteAuthority{Observed: false, Reason: "the server returned no authority stamp"}
	}
	return &remoteAuthority{
		Observed:                   true,
		Authoritative:              a.GetAuthoritative(),
		SourceRepoCommit:           a.GetSourceRepoCommit(),
		GraphBuildCommit:           a.GetGraphBuildCommit(),
		FreshnessState:             a.GetGraphFreshnessState().String(),
		SeedState:                  a.GetSeedState().String(),
		BuildProvenance:            a.GetBuildProvenanceState().String(),
		EmbeddedSeedDigestSHA256:   a.GetEmbeddedSeedDigestSha256(),
		LiveStoreGraphDigestSHA256: a.GetLiveStoreGraphDigestSha256(),
		LiveStoreGraphTripleCount:  a.GetLiveStoreGraphTripleCount(),
	}
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
			if report.RemoteAuthority == nil {
				report.RemoteAuthority = observedRemoteAuthority(resp.GetAuthority())
			}
		}
		if resp, err := conn.Briefing(ctx, file, "", "standard", domain); err != nil {
			entry.BriefingRefused = err.Error()
		} else {
			entry.BriefingStatus = resp.GetStatus().String()
		}
		cancel()
		report.Results = append(report.Results, entry)
	}
	if report.RemoteAuthority == nil {
		report.RemoteAuthority = &remoteAuthority{Observed: false, Reason: "no impact response was obtained, so the answering server's authority was never seen"}
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

// isDefaultProtocolDocument reports whether a path names the frozen default
// protocol, by CONTENT rather than by how the path was spelled.
//
// An earlier version compared path strings, so `./docs/...v1.md` read as a
// custom protocol while being the v1 document — which let the v1 digest be
// recorded under another identity AND disabled the world-completeness guard
// that protects v1. A path is a name for a file; two names for one file are
// still one file, and the manifest records what the bytes were.
//
// Falls back to comparing resolved paths when the default cannot be read, so a
// run from outside the repository root degrades to the older, weaker check
// rather than silently deciding every document is custom.
func isDefaultProtocolDocument(path, got string, gotErr error) bool {
	if gotErr == nil {
		return got == defaultProtocolDigest
	}
	a, errA := filepath.Abs(path)
	b, errB := filepath.Abs(protocolPath)
	if errA != nil || errB != nil {
		return path == protocolPath
	}
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return a == b
}

// verifyRequiredWorldCheckout checks that a checkout claiming a protocol-named
// world really is that repository, by its git remote.
//
// The honest ceiling, stated rather than implied: this defeats mislabelling,
// not a determined forger. A local path has no unforgeable link to an upstream
// repository — a remote can be set to anything — so what this buys is that a
// world cannot be misidentified by accident or convenience, which is the
// failure mode an evaluation harness actually suffers. A world whose remote
// cannot be read at all is refused rather than assumed correct.
func verifyRequiredWorldCheckout(name, path string) error {
	isProtocolWorld := false
	for _, n := range requiredWorlds {
		if n == name {
			isProtocolWorld = true
			break
		}
	}
	if !isProtocolWorld {
		return nil
	}
	want, known := requiredWorldRemotes[name]
	if !known {
		// Fail CLOSED. This world is one the protocol names, so a checkout
		// claiming it must be shown to be it — and no upstream identity is
		// registered here, so it cannot be. Returning success would let an
		// arbitrary tree be reported as that world, which is the finding this
		// check exists to answer; inventing a URL for a repository whose
		// identity is exactly what is undecided would be worse.
		return fmt.Errorf("%s: the protocol names this world but no upstream identity is registered for it, so a checkout cannot be shown to be it; bind it as an operator world instead", name)
	}
	got, err := resolveUpstream(path)
	if err != nil {
		return fmt.Errorf("%s: cannot read the checkout's origin remote, so it cannot be shown to be %s: %w", name, want, err)
	}
	if got != want {
		return fmt.Errorf("%s: the protocol names %s but this checkout's origin resolves to %s; a world's name is not evidence about the tree", name, want, got)
	}
	return nil
}

// resolveUpstream follows origin until it reaches something that is not a
// local directory.
//
// The worlds are measured from clones, so a clone's origin is the local path it
// was made from, not the upstream repository — the first version of this check
// compared that local path against the expected repository and refused every
// legitimate run. Following the chain is what makes the check usable on the
// trees this harness actually measures.
//
// Bounded, because a pair of checkouts pointing at each other would otherwise
// loop forever.
func resolveUpstream(path string) (string, error) {
	const maxHops = 8
	for hop := 0; hop < maxHops; hop++ {
		out, err := exec.Command("git", "-C", path, "remote", "get-url", "origin").Output()
		if err != nil {
			return "", err
		}
		url := strings.TrimSpace(string(out))
		// A file:// origin is a local checkout wearing a URL. Converted to a
		// path BEFORE the directory test, because os.Stat on the literal URI
		// says "not a directory" and the walk would stop there — returning the
		// intermediate clone's path as the repository identity and rejecting a
		// legitimate world. Stripping the scheme afterwards, as the normalizer
		// does, is too late: the recursion decision has already been made.
		local := strings.TrimPrefix(url, "file://")
		if info, statErr := os.Stat(local); statErr == nil && info.IsDir() {
			path = local
			continue
		}
		return normalizeRemote(url), nil
	}
	return "", fmt.Errorf("origin chain did not reach an upstream repository within %d hops", maxHops)
}

// requiredWorldRemotes is what each protocol-named world's checkout must be.
//
// world3_independent_calibration is deliberately ABSENT. Which repository it
// should be is the open question this whole file keeps running into, and
// registering a guess here would let an arbitrary tree pass as the SQLite
// calibration. A protocol-named world with no entry fails closed.
var requiredWorldRemotes = map[string]string{
	"world1_sensei_self": "github.com/globulario/sensei",
	"world2_globular":    "github.com/globulario/Globular",
}

// normalizeRemote reduces the forms git accepts to a comparable host/path.
//
// git:// is included: it is a documented transport, and omitting it left a
// legitimate checkout normalized to "git///github.com/..." and rejected. The
// scp-like form (git@host:path) is handled by the "@" split below.
func normalizeRemote(url string) string {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	for _, scheme := range []string{"https://", "http://", "ssh://", "git://", "git+ssh://", "file://"} {
		url = strings.TrimPrefix(url, scheme)
	}
	if i := strings.Index(url, "@"); i >= 0 {
		url = url[i+1:]
	}
	return strings.Replace(url, ":", "/", 1)
}

// requiredMutantArms are the mutant-suite arms the default protocol's fourth
// world depends on. The optional model arm is excluded: the protocol treats a
// bound model as available-when-available, so its absence is not incompleteness.
// The operational-surface arm is excluded too — it is refused by the authority
// model on purpose, and that refusal is a measured result rather than a gap.
var requiredMutantArms = []string{
	evalharness.ArmDeterministicExtraction,
	evalharness.ArmCompositionModelDisabled,
}

// incompleteMutantSuite names the mutant-suite arms that did not run.
func incompleteMutantSuite(arms []armArtifact) []string {
	status := map[string]string{}
	for _, a := range arms {
		if a.Subject == subjectMutantSuite {
			status[a.Arm] = a.Status
		}
	}
	var missing []string
	for _, name := range requiredMutantArms {
		if status[name] != statusRan {
			missing = append(missing, fmt.Sprintf("mutant suite arm %s (%s)", name, orNotRun(status[name])))
		}
	}
	return missing
}

func orNotRun(s string) string {
	if strings.TrimSpace(s) == "" {
		return statusNotRun
	}
	return s
}

// missingRequiredWorlds names the worlds the default protocol consumes that
// this run did not measure. Sorted so a refusal reads the same way twice.
func missingRequiredWorlds(ran []evalsample.World) []string {
	seen := map[string]string{}
	for _, w := range ran {
		seen[w.Name] = w.Binding.RepositoryDomain
	}
	var missing []string
	for _, name := range requiredWorlds {
		domain, ok := seen[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		// Present under the right name but bound to something else. Reported
		// as a distinct problem, because "you did not run it" and "you ran
		// something else and called it that" need different corrections.
		if want := requiredWorldDomains[name]; want != "" && domain != want {
			missing = append(missing, fmt.Sprintf("%s (bound to %s, protocol names %s)", name, domain, want))
		}
	}
	sort.Strings(missing)
	return missing
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

// requiredWorldDomains is what each required world must actually BE.
//
// A name is a label the caller chose; it is not proof of identity. A direct
// caller could point three arbitrary Go checkouts at these names and the
// completeness check would have seen a full v1 world set. The protocol binds
// specific repositories, so completeness compares the domain the world reports
// against the domain the protocol names.
var requiredWorldDomains = map[string]string{
	"world1_sensei_self":             "github.com/globulario/sensei",
	"world2_globular":                "github.com/globulario/Globular",
	"world3_independent_calibration": "sqlite.org/sqlite",
}

// reservedArmNames are the arm names this command writes itself; a world may
// not claim one and overwrite its report.
var reservedArmNames = map[string]bool{
	"deterministic_extraction_without_composition": true,
	"phase10_composition_model_disabled":           true,
	armCompositionModelBound:                       true,
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
func runWorlds(out string, specs []string, capturedAt string, elapsed map[string]int64, sampled *[]evalsample.World) []armArtifact {
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
		report, doc, binding, err := runWorld(name, domain, path, capturedAt)
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
		if sampled != nil {
			inventory, invErr := recallUnitInventory(path)
			if invErr != nil {
				// A world sampled with a silently empty recall inventory would
				// report a recall lane that is honestly absent for a dishonest
				// reason. The world still runs; only its sampling is refused.
				arts = append(arts, armArtifact{Arm: name + "_sample", Subject: subjectPublishedDomain, Status: statusFailed,
					Reason: "recall unit inventory: " + invErr.Error()})
			} else {
				*sampled = append(*sampled, evalsample.World{
					Name: name, Binding: binding,
					Observations:       doc.Observations,
					Counterexamples:    doc.Counterexamples,
					CandidateQuestions: doc.CandidateQuestions,
					RecallInventory:    inventory,
				})
			}
		}
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

func runWorld(name, domain, path, capturedAt string) (worldReport, investigation.Document, architecture.ClaimDocumentBinding, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return worldReport{}, investigation.Document{}, architecture.ClaimDocumentBinding{}, err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return worldReport{}, investigation.Document{}, architecture.ClaimDocumentBinding{}, fmt.Errorf("%s: not a directory", path)
	}
	// For a world the protocol NAMES, the checkout must actually be that
	// repository. Both the world's name and its domain come from the same
	// caller-supplied --world string, so neither is evidence about the tree;
	// the remote is at least a property of the checkout rather than of the
	// argument. Checked before extraction so an impostor never produces a
	// report at all.
	if err := verifyRequiredWorldCheckout(name, abs); err != nil {
		return worldReport{}, investigation.Document{}, architecture.ClaimDocumentBinding{}, err
	}
	binding, err := worldBinding(abs, domain)
	if err != nil {
		return worldReport{}, investigation.Document{}, architecture.ClaimDocumentBinding{}, err
	}
	doc, err := howextract.Extract(abs, howextract.Options{CapturedAt: capturedAt, Repository: binding})
	if err != nil {
		return worldReport{}, investigation.Document{}, architecture.ClaimDocumentBinding{}, err
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
	return report, doc, binding, nil
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

// modelArmConfig is what a CALLER may ask arm 3 for. It carries provider and
// model selection and nothing else: there is no field here for a status, an
// artifact digest, or a request digest, because those are execution evidence
// and the evaluator does not get to supply them.
type modelArmConfig struct {
	ProviderID      string
	ProviderVersion string
	ModelName       string
	ProviderPath    string
	ProviderArgs    []string
	PromptContract  string
	Domain          string
	// ReferenceSet is the path to a frozen human answer key. It is a path
	// rather than a value because the loader verifies the bytes against the
	// release's own label-file digests: a caller must not be able to hand the
	// scorer labels that no published release carries.
	ReferenceSet string
	// LabelFiles are the adjudicator outputs the release names. They are
	// supplied separately because the release identifies them by digest and
	// the loader checks their bytes.
	LabelFiles []string
}

func (c modelArmConfig) configured() bool {
	return strings.TrimSpace(c.ProviderID) != "" && strings.TrimSpace(c.ProviderPath) != "" && strings.TrimSpace(c.ModelName) != ""
}

// runModelBoundArm is #131's optional-model arm.
//
// It calls the PRODUCTION model path. The evaluator does not implement model
// execution, does not interpret a provider's answer, and cannot mint a terminal
// status — it configures the capability, runs it, and copies the outcome.
//
// The status it reports when nothing is bound is `not_run`, NOT
// `not_implemented_in_evaluated_path`. Those say different things: one is "this
// run did not ask", the other is "no such behaviour exists to measure". The
// second stopped being true when the execution path and a real adapter landed,
// and continuing to report it would understate the system.
func runModelBoundArm(out, capturedAt string, cfg modelArmConfig, elapsed map[string]int64) armArtifact {
	art := armArtifact{Arm: armCompositionModelBound, Subject: subjectMutantSuite}
	if !cfg.configured() {
		art.Status = statusNotRun
		art.Reason = "optional model capability is available; this run did not bind a provider (supply --model-provider-id, --model-provider-path and --model-name to measure it)"
		return art
	}

	start := time.Now()
	provider := &modelexec.CommandProvider{
		ProviderID:      cfg.ProviderID,
		ProviderVersion: cfg.ProviderVersion,
		Path:            cfg.ProviderPath,
		Argv:            cfg.ProviderArgs,
	}
	lane := whyinvestigation.ModelLane{
		Config: modelexec.Config{
			Requested:  true,
			ProviderID: cfg.ProviderID,
			ModelName:  cfg.ModelName,
		},
		Registry: modelexec.Registry{cfg.ProviderID: provider},
		Request: modelexec.Request{
			SchemaVersion:        modelexec.ArtifactSchemaVersion,
			PromptContractDigest: cfg.PromptContract,
			OutputSchemaVersion:  modelexec.ArtifactSchemaVersion,
			ToolPolicy:           "none",
			Model:                modelexec.ModelIdentity{Name: cfg.ModelName, DigestAbsent: true},
		},
	}

	report, err := evalharness.RunCompositionArmWithModel(evalharness.Options{
		RepositoryDomain: cfg.Domain,
		CapturedAt:       capturedAt,
		MaterializeInto: func(name string) (string, error) {
			path := filepath.Join(out, "mutants", name)
			return path, os.MkdirAll(path, 0o755)
		},
	}, lane)
	elapsed[armCompositionModelBound] = time.Since(start).Milliseconds()
	if err != nil {
		art.Status = statusFailed
		art.Reason = err.Error()
		return art
	}

	art = writeReport(out, armCompositionModelBound, report)
	art.Subject = subjectMutantSuite
	produced, total := report.CandidateRate()
	art.CandidateRate = fmt.Sprintf("%d/%d", produced, total)

	// The model outcome is copied VERBATIM. An evaluator that re-derived a
	// status from what it saw would be grading the provider on its own
	// authority instead of reporting what the production path concluded.
	// The control is counted too. Summarising only the mutants would hide a
	// control-only refusal or error — the case where the model answered every
	// planted defect but declined the clean repository, which is a finding
	// about the model rather than about the mutants.
	statuses := map[string]int{}
	for _, r := range append([]evalharness.CompositionSiteResult{report.Baseline}, report.Results...) {
		if r.ModelStatus != "" {
			statuses[r.ModelStatus]++
		}
	}
	art.Reason = "model outcome per site: " + renderCounts(statuses)

	// Persist the frozen acquisition bundles as their own artifact, and score
	// them. The scorer runs even with no reference set: it reports
	// reference_set_absent, which is the honest state before human adjudication
	// and is what proves the deterministic half of the pipeline is wired.
	// Load the frozen ruler if one was supplied. A failure to LOAD it is not a
	// reason to fall back to scoring against nothing: that would silently turn
	// a requested measurement into reference_set_absent and look like a run
	// that never had a ruler.
	var reference evalmodel.ReferenceSet
	if strings.TrimSpace(cfg.ReferenceSet) != "" {
		loaded, err := evalmodel.LoadReferenceSet(cfg.ReferenceSet, cfg.LabelFiles)
		if err != nil {
			art.Status = statusFailed
			art.Reason = "frozen reference set could not be loaded: " + err.Error()
			return art
		}
		reference = loaded
	}

	bundle := modelAcquisitionBundle{
		SchemaVersion:         "sensei.eval_model_acquisition_bundle.v1",
		CapturedAt:            capturedAt,
		Arm:                   armCompositionModelBound,
		ReferenceDigestSHA256: reference.DigestSHA256,
	}
	// The clean control FIRST. The model runs over it as well as over every
	// mutant, and the control's answer is the measurement that exposes model
	// false positives — a claimed defect where none was planted — or a
	// control-only refusal. Freezing only the mutants would discard exactly the
	// half that says whether the model is finding things or inventing them.
	for _, r := range append([]evalharness.CompositionSiteResult{report.Baseline}, report.Results...) {
		if r.ModelAcquisition.SchemaVersion == "" {
			continue
		}
		bundle.Acquisitions = append(bundle.Acquisitions, r.ModelAcquisition)
		bundle.Scores = append(bundle.Scores, evalmodel.ScoreAcquisition(r.ModelAcquisition, reference))
	}
	if len(bundle.Acquisitions) > 0 {
		if bundleArt := writeReport(out, armCompositionModelBound+"_acquisitions", bundle); bundleArt.ReportFile != "" {
			art.AcquisitionFile = bundleArt.ReportFile
			art.AcquisitionDigest = bundleArt.ReportDigest
		}
	}
	return art
}

// modelAcquisitionBundle is the frozen scoring input: what the model actually
// answered, per site, with the deterministic baseline it answered against.
//
// It is a SEPARATE artifact from the composition report on purpose. The report
// is a summary; this is the immutable material a human adjudicates and the
// scorer replays over. Keeping counts without the claims they count would leave
// the promised scoring workflow with nothing to consume.
type modelAcquisitionBundle struct {
	SchemaVersion string `json:"schema_version"`
	CapturedAt    string `json:"captured_at"`
	Arm           string `json:"arm"`
	// ReferenceDigestSHA256 names the exact ruler these scores were measured
	// against, and is empty when none was supplied. A score that does not name
	// its ruler cannot be compared with another score.
	ReferenceDigestSHA256 string                  `json:"reference_digest_sha256,omitempty"`
	Acquisitions          []evalmodel.Acquisition `json:"acquisitions"`
	Scores                []evalmodel.Score       `json:"scores"`
}

func renderCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	if len(parts) == 0 {
		return "(none)"
	}
	return strings.Join(parts, " ")
}
