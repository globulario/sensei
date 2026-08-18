// SPDX-License-Identifier: AGPL-3.0-only

package evalharness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/evalmutant"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigationsurface"
	"github.com/globulario/sensei/golang/architecture/investigator"
	"github.com/globulario/sensei/golang/architecture/whyinvestigation"
)

// ArmCompositionModelDisabled is the second baseline issue #131 section 5
// requires: "Phase 10 investigation with model disabled".
//
// # What separates it from the deterministic arm
//
// The deterministic arm stops at observations. This one runs the full Phase 10
// path -- HOW extraction, WHY investigation over real git history, then
// investigator composition -- with NO model bound. Composition is what turns
// observations into candidates and challenges, so this arm can be graded on
// whether it named a defect, and the deterministic arm structurally cannot.
//
// That difference is the measurement. If composition names no more defects
// than extraction did, the composition layer is not earning its cost on this
// suite, and reporting the two side by side is the only way to see it.
//
// # Why it needs mutants with history
//
// WHY investigation is bounded by an explicit commit range, so this arm cannot
// run over a bare directory. It uses evalmutant.MaterializeRepo, which commits
// the clean baseline and then the defect carrying the mutant's own message --
// so the range this arm binds to contains exactly the mutation, and a
// history-reading provider sees the commit message a misleading-commit-message
// defect is actually about.
const ArmCompositionModelDisabled = "phase10_composition_model_disabled"

// CompositionSiteResult is one mutant's outcome under the composition arm.
// It extends the deterministic arm's shape with what composition adds, and
// deliberately reuses SiteResult's fields for everything else so the two arms
// are comparable field by field rather than by narrative.
type CompositionSiteResult struct {
	SiteResult `json:",inline" yaml:",inline"`

	// Candidates and Challenges are what composition produced. A candidate is
	// advisory, never a verdict: this arm proposes, it does not admit.
	Candidates int `json:"candidates" yaml:"candidates"`
	Challenges int `json:"challenges" yaml:"challenges"`
	// EvidenceRequests are the gaps composition asked to have filled. A high
	// count with few candidates is a real and reportable result: the arm knew
	// what it was missing.
	EvidenceRequests int `json:"evidence_requests" yaml:"evidence_requests"`

	// CandidateRefs identify each candidate as "kind:claim_id", kept so a
	// grader can follow one back to the document and judge whether it actually
	// matched the defect or merely landed near it. Identities rather than prose,
	// because a CandidateEnvelope carries no proposition text and inventing one
	// here would report a finding the arm never made.
	CandidateRefs []string `json:"candidate_refs" yaml:"candidate_refs"`

	// WhyUnavailable records that WHY investigation could not run for this
	// mutant, TYPED rather than left as a zero candidate count. A missing WHY
	// and a WHY that found nothing are different facts, and collapsing them
	// would let an infrastructure failure read as a negative result.
	WhyUnavailable string `json:"why_unavailable,omitempty" yaml:"why_unavailable,omitempty"`
}

// CompositionReport is the composition arm's complete run.
type CompositionReport struct {
	SchemaVersion string                  `json:"schema_version" yaml:"schema_version"`
	Arm           string                  `json:"arm" yaml:"arm"`
	CapturedAt    string                  `json:"captured_at" yaml:"captured_at"`
	Baseline      CompositionSiteResult   `json:"baseline_control" yaml:"baseline_control"`
	Results       []CompositionSiteResult `json:"results" yaml:"results"`
	Limitations   []string                `json:"limitations" yaml:"limitations"`
}

// CandidateRate is the fraction of mutants for which composition produced any
// candidate. Deliberately NOT called a detection rate: a candidate is a
// proposal about the tree, and whether it matches the defect is a grading
// judgement this package does not make.
func (r CompositionReport) CandidateRate() (produced, total int) {
	for _, res := range r.Results {
		total++
		if res.Candidates > 0 {
			produced++
		}
	}
	return produced, total
}

// compileMutantGraph builds the mutant's own awareness corpus into N-Triples
// and returns the graph plus its digest.
//
// This is what makes the composition arm runnable. Compose requires a real
// graph digest that AGREES with the extraction's binding, and a bare tree has
// neither -- so the arm previously stopped with "receipt graph digest does not
// match binding". Compiling the mutant's corpus produces a graph it genuinely
// has, and sha256 over those bytes is a digest it genuinely earned, so the
// binding and the receipt can agree without anyone declaring a resolution that
// did not happen.
func compileMutantGraph(ctx context.Context, root, domain string) ([]byte, string, error) {
	corpus := filepath.Join(root, "docs", "awareness")
	if _, err := os.Stat(corpus); err != nil {
		return nil, "", fmt.Errorf("mutant has no awareness corpus at docs/awareness: %w", err)
	}
	comp, err := graphbuild.Compile(ctx, graphbuild.CompileRequest{
		Sources: []graphbuild.SourceRoot{{
			FilesystemPath: corpus,
			IdentityRoot:   root,
			// The mutant lives in a fresh temp directory every run, so without
			// stripping, the absolute path leaks into authoredIn literals and
			// the graph digest changes between two runs of the SAME mutant --
			// which then changes the HOW binding and breaks the reproducibility
			// this evaluation requires. StripPathPrefixes exists for exactly
			// this: identity is the repository-relative path, never where the
			// tree happened to be staged.
			StripPathPrefixes: []string{root},
			RepositoryDomain:  domain,
		}},
	})
	if err != nil {
		return nil, "", fmt.Errorf("compile mutant corpus: %w", err)
	}
	sum := sha256.Sum256(comp.CanonicalNTriples)
	return comp.CanonicalNTriples, hex.EncodeToString(sum[:]), nil
}

// emptyInputDigest is the digest of an explicitly EMPTY input.
//
// Compose requires five digests -- graph, current claims, closure state,
// existing questions, review history -- and offers no "unavailable" status for
// any of them. A synthetic mutant has none of those things.
//
// Each is therefore bound to the digest of emptiness, a true statement (the
// input WAS empty) rather than an invented one, because #131's authority
// boundary forbids manufacturing truth to make an evaluation run.
//
// THIS IS NOT SUFFICIENT, and the arm reports so rather than working around it.
// A composed document's receipt graph digest is validated against its binding's
// repository graph digest, so supplying a graph digest here while the HOW
// binding carries none is refused. Making it agree would mean declaring a
// resolved graph digest for a repository that has no graph -- claiming a
// resolution that never happened, which is the fabrication this arm exists to
// measure, not to commit.
//
// Composition over synthetic mutants therefore requires each mutant to be
// PUBLISHED into a real graph first, so the binding carries a digest it earned.
// Until then this arm reports HOW and WHY and types the composition step as
// unavailable with the exact refusal, which is a measurement of a structural
// limit rather than a negative result about composition's capability.
func emptyInputDigest() string { return investigator.SHA256String("") }

// RunCompositionArm runs the model-disabled Phase 10 composition over the whole
// mutant suite plus the clean control.
func RunCompositionArm(opts Options) (CompositionReport, error) {
	if strings.TrimSpace(opts.RepositoryDomain) == "" {
		return CompositionReport{}, fmt.Errorf("evalharness: RepositoryDomain is required; the arm must not resolve identity from its own checkout")
	}
	if strings.TrimSpace(opts.CapturedAt) == "" {
		return CompositionReport{}, fmt.Errorf("evalharness: CapturedAt is required; a self-stamped report is not reproducible")
	}
	if opts.MaterializeInto == nil {
		return CompositionReport{}, fmt.Errorf("evalharness: MaterializeInto is required")
	}

	report := CompositionReport{
		SchemaVersion: "sensei.evalharness.composition.v1",
		Arm:           ArmCompositionModelDisabled,
		CapturedAt:    opts.CapturedAt,
		Limitations: []string{
			"no model is bound: this arm measures what deterministic composition alone recovers, which is the floor a model-assisted configuration must beat to have earned its cost",
			"a candidate is advisory and is not a verdict; whether one matched the intended defect is a grading judgement this harness does not make",
			"candidate counts are reported, never a detection rate, because this arm cannot grade its own output",
			"a synthetic mutant has no graph to bind, and composition validates a composed document's receipt graph digest against its binding's; supplying one without the other is refused, and making them agree would declare a resolved graph digest for a repository that has none. Composition over this suite therefore requires publishing each mutant into a real graph first: until then the composition step is reported as unavailable with its exact refusal, and candidate counts of zero mean COMPOSITION DID NOT RUN, not that composition found nothing",
		},
	}

	control, err := runComposition(opts, "baseline-composition", evalmutant.Baseline())
	if err != nil {
		return CompositionReport{}, fmt.Errorf("evalharness: composition control: %w", err)
	}
	report.Baseline = control

	for _, d := range evalmutant.Defects() {
		m, err := evalmutant.Build(d)
		if err != nil {
			return CompositionReport{}, fmt.Errorf("evalharness: build %s: %w", d, err)
		}
		res, err := runComposition(opts, string(d)+"-composition", m)
		if err != nil {
			return CompositionReport{}, fmt.Errorf("evalharness: compose %s: %w", d, err)
		}
		if witness, werr := evalmutant.WitnessFor(d); werr == nil {
			present, detail, _ := witness(res.observedRoot)
			res.DefectPresent, res.WitnessDetail = present, detail
		}
		report.Results = append(report.Results, res)
	}
	return report, nil
}

// compositionResult carries the materialized root back for witness re-reading
// without putting a filesystem path into the reported record.
type compositionSiteInternal = CompositionSiteResult

func runComposition(opts Options, name string, m evalmutant.Mutant) (CompositionSiteResult, error) {
	root, err := opts.MaterializeInto(name)
	if err != nil {
		return CompositionSiteResult{}, err
	}
	// A real two-commit history, so WHY has a range to bind to and the defect
	// commit's message is present as a commit message rather than as prose in
	// a struct nobody reads.
	baseRev, headRev, err := evalmutant.MaterializeRepo(root, m, evalmutant.RepoOptions{CommittedAt: opts.CapturedAt})
	if err != nil {
		return CompositionSiteResult{}, fmt.Errorf("materialize repo: %w", err)
	}

	res := CompositionSiteResult{}
	res.Defect = m.Defect
	res.Statement = m.Statement
	res.DefectPaths = normalizePaths(m.TouchedPaths)
	res.observedRoot = root
	res.NamedTheDefect = false

	// Compile the mutant's OWN corpus, so the binding below carries a graph
	// digest this repository actually earned rather than one asserted for it.
	_, graphDigest, gerr := compileMutantGraph(context.Background(), root, opts.RepositoryDomain)
	if gerr != nil {
		res.WhyUnavailable = "graph: " + gerr.Error()
		return res, nil
	}

	how, err := investigationsurface.RunHow(investigationsurface.HowRequest{
		Root:       root,
		CapturedAt: opts.CapturedAt,
		Repository: architecture.ClaimDocumentBinding{
			RepositoryDomain:  opts.RepositoryDomain,
			Revision:          headRev,
			RevisionStatus:    "resolved",
			GraphDigestSHA256: graphDigest,
			GraphDigestStatus: "resolved",
		},
	})
	if err != nil {
		return CompositionSiteResult{}, fmt.Errorf("HOW: %w", err)
	}

	observed := map[string]bool{}
	for _, obs := range how.Observations {
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
	res.Observations = len(how.Observations)
	res.EvidenceReceipts = len(how.RawEvidence)
	res.Limitations = len(how.Limitations)
	res.DocumentDigest = how.Receipt.OutputDocumentDigestSHA256

	// WHY is bounded by the exact range this mutant introduced.
	var observationIDs []string
	for _, obs := range how.Observations {
		observationIDs = append(observationIDs, obs.ID)
	}
	if len(observationIDs) == 0 {
		res.WhyUnavailable = "HOW produced no observations to investigate"
		return res, nil
	}
	why, err := investigationsurface.RunWhy(context.Background(), investigationsurface.WhyRequest{
		Root:           root,
		CapturedAt:     opts.CapturedAt,
		How:            how,
		QueryID:        "query.eval.composition",
		ObservationIDs: observationIDs,
		HistoryStart:   baseRev,
		HistoryEnd:     headRev,
		ProviderIDs:    []string{whyinvestigation.GitProviderID},
	})
	if err != nil {
		// TYPED, not silent. A WHY that could not run and a WHY that found
		// nothing are different facts about this arm.
		res.WhyUnavailable = err.Error()
		return res, nil
	}

	result, err := investigationsurface.RunArchitecture(investigationsurface.ArchitectureRequest{
		How:       how,
		Why:       why,
		Grounding: investigationsurface.GroundingFromDocuments(how, why),
		Digests: investigator.InputDigests{
			// The mutant's real graph, so the composed document's receipt and
			// its binding agree. The remaining four are genuinely empty for a
			// synthetic repository and say so.
			GraphDigestSHA256:             graphDigest,
			CurrentClaimsDigestSHA256:     emptyInputDigest(),
			ClosureStateDigestSHA256:      emptyInputDigest(),
			ExistingQuestionsDigestSHA256: emptyInputDigest(),
			ReviewHistoryDigestSHA256:     emptyInputDigest(),
		},
		Options: investigator.ComposeOptions{
			GeneratorVersion: ArmCompositionModelDisabled,
			RulesetVersion:   "eval.v1",
			// The caller's explicit capture time, not a label. Compose requires
			// RFC3339 here; passing the string "caller" made every composition
			// fail with "timestamp source must be RFC3339", which the typed
			// WhyUnavailable field surfaced as an infrastructure failure instead
			// of letting 0 candidates read as "composition adds nothing".
			TimestampSource: opts.CapturedAt,
			// Declared because Compose requires them. They record the arm's
			// configuration in the receipt, which keeps a report traceable to
			// the configuration that produced it.
			ResourceLimits: map[string]string{"arm": ArmCompositionModelDisabled, "model": "disabled"},
		},
	})
	if err != nil {
		res.WhyUnavailable = "composition: " + err.Error()
		return res, nil
	}

	res.Candidates = len(result.Candidates)
	res.Challenges = len(result.Challenges)
	res.EvidenceRequests = len(result.EvidenceRequests)
	for _, c := range result.Candidates {
		res.CandidateRefs = append(res.CandidateRefs, candidateText(c))
	}
	return res, nil
}

// candidateText records what the envelope actually carries.
//
// A CandidateEnvelope holds no proposition text of its own -- it references a
// claim by id and states its output kind. Synthesizing prose here would invent
// a finding the arm never produced, so this reports the identity a grader can
// follow back to the document instead.
func candidateText(c investigator.CandidateEnvelope) string {
	kind := strings.TrimSpace(string(c.OutputKind))
	claim := strings.TrimSpace(c.ClaimID)
	switch {
	case kind != "" && claim != "":
		return kind + ":" + claim
	case claim != "":
		return claim
	case kind != "":
		return kind
	default:
		return c.CandidateID
	}
}

// investigationDocumentIsHow guards the arm against silently investigating the
// wrong artifact if the surface's contract ever changes.
func investigationDocumentIsHow(d investigation.Document) bool {
	return d.Mode == investigation.ModeHow
}

var _ = investigationDocumentIsHow
var _ = howextract.Options{}
