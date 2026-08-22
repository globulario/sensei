// SPDX-License-Identifier: AGPL-3.0-only

package evalharness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/evalmodel"
	"github.com/globulario/sensei/golang/architecture/evalmutant"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/howextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigationsurface"
	"github.com/globulario/sensei/golang/architecture/investigator"
	"github.com/globulario/sensei/golang/architecture/modelexec"
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

// ArmCompositionModelBound is the SAME composition measured with a model bound.
// It is a different arm because it is a different measurement, and a report
// that named itself model_disabled while a model ran would misidentify its own
// result — the identity failure this arm exists to measure elsewhere.
const ArmCompositionModelBound = "phase10_composition_model_bound"

// armIdentityFor derives the arm's identity from whether the lane is actually
// engaged, rather than from a constant chosen when the function was written.
func armIdentityFor(lane whyinvestigation.ModelLane) string {
	if lane.Config.Requested && !lane.Config.Disabled {
		return ArmCompositionModelBound
	}
	return ArmCompositionModelDisabled
}

// CompositionSiteResult is one mutant's outcome under the composition arm.
// It extends the deterministic arm's shape with what composition adds, and
// deliberately reuses SiteResult's fields for everything else so the two arms
// are comparable field by field rather than by narrative.
type CompositionSiteResult struct {
	SiteResult `json:",inline" yaml:",inline"`

	// Model* record the optional lane's outcome for this site, kept SEPARATE
	// from the deterministic counts below. Merging them would attribute the
	// deterministic lane's work to the model.
	ModelStatus               string         `json:"model_status,omitempty" yaml:"model_status,omitempty"`
	ModelReason               string         `json:"model_reason,omitempty" yaml:"model_reason,omitempty"`
	ModelRequestDigestSHA256  string         `json:"model_request_digest_sha256,omitempty" yaml:"model_request_digest_sha256,omitempty"`
	ModelArtifactDigestSHA256 string         `json:"model_artifact_digest_sha256,omitempty" yaml:"model_artifact_digest_sha256,omitempty"`
	ModelProviderCalls        int            `json:"model_provider_calls,omitempty" yaml:"model_provider_calls,omitempty"`
	ModelItemsByKind          map[string]int `json:"model_items_by_kind,omitempty" yaml:"model_items_by_kind,omitempty"`
	// ModelAcquisition is the frozen, content-addressed measurement this site
	// produced. It is what deterministic scoring consumes later; without it the
	// report would retain counts nobody can adjudicate.
	ModelAcquisition evalmodel.Acquisition `json:"model_acquisition,omitempty" yaml:"model_acquisition,omitempty"`

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

	// GroundedCandidates is how many candidates cite only observation and
	// evidence identities that exist in the documents they were composed from.
	//
	// #131 asks for candidate grounding precision and an unsupported-claim rate.
	// The half of that which needs no adjudicator is referential: a candidate
	// pointing at an observation or receipt that is not in the document is
	// citing something that does not exist, and no reference set is needed to
	// say so. Whether a grounded candidate is CORRECT is a separate question
	// this does not answer.
	//
	// MEASURED AFTER VALIDATION, and that bounds what it can prove. Compose
	// ends in Validate, which rejects a candidate carrying a dangling
	// reference and fails the whole site — so a site that reaches here has
	// already had exactly this defect filtered out of it, and 60/60 grounded
	// means "of the candidates that survived validation", not "no dangling
	// reference was ever produced". A dangling reference appears in this arm as
	// a COMPOSITION FAILURE, not as an ungrounded candidate, which is why
	// CompositionFailures is reported beside the rate rather than left implicit
	// in a missing denominator. Reaching the pre-validation drafts would mean
	// widening investigator's surface for the benchmark's convenience, which
	// #131 forbids.
	GroundedCandidates int `json:"grounded_candidates" yaml:"grounded_candidates"`

	// DanglingCandidateRefs names the identities that did not resolve, bounded
	// and sorted. Empty is the expected result; a non-empty list is the finding.
	DanglingCandidateRefs []string `json:"dangling_candidate_refs,omitempty" yaml:"dangling_candidate_refs,omitempty"`

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
// CandidateGrounding totals referential grounding across every site.
//
// grounded and candidates are counts of candidates; dangling counts the
// distinct unresolved references behind them. failures is how many sites did
// not compose at all — reported with the rate because a site that failed
// contributes nothing to either side of it, and a grounding rate quoted without
// its population would read as coverage the arm never had.
func (r CompositionReport) CandidateGrounding() (grounded, candidates, dangling, failures int) {
	for _, res := range r.Results {
		grounded += res.GroundedCandidates
		candidates += res.Candidates
		dangling += len(res.DanglingCandidateRefs)
		if strings.TrimSpace(res.WhyUnavailable) != "" {
			failures++
		}
	}
	return grounded, candidates, dangling, failures
}

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
			// A synthetic mutant is staged in a temp directory with no
			// checkout to resolve a repository identity from, so the
			// evaluation names it: this corpus belongs to the repository
			// the arm declared it is evaluating. Naming it here is not the
			// publication-domain-as-identity hazard #197 forbids -- there
			// is no separate repository behind this tree to disagree with.
			RepositoryIdentity: domain,
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
// RunCompositionArm runs the composition arm with NO model bound. It is the
// deterministic floor a model-assisted configuration must beat to have earned
// its cost.
func RunCompositionArm(opts Options) (CompositionReport, error) {
	return RunCompositionArmWithModel(opts, whyinvestigation.ModelLane{
		Config: modelexec.Config{Disabled: true},
	})
}

// RunCompositionArmWithModel runs the same arm with an optional model lane
// threaded into the PRODUCTION investigation path. The evaluator never
// implements model execution of its own.
func RunCompositionArmWithModel(opts Options, lane whyinvestigation.ModelLane) (CompositionReport, error) {
	if strings.TrimSpace(opts.RepositoryDomain) == "" {
		return CompositionReport{}, fmt.Errorf("evalharness: RepositoryDomain is required; the arm must not resolve identity from its own checkout")
	}
	if strings.TrimSpace(opts.CapturedAt) == "" {
		return CompositionReport{}, fmt.Errorf("evalharness: CapturedAt is required; a self-stamped report is not reproducible")
	}
	if opts.MaterializeInto == nil {
		return CompositionReport{}, fmt.Errorf("evalharness: MaterializeInto is required")
	}

	arm := armIdentityFor(lane)
	modelBound := arm == ArmCompositionModelBound
	floorLimitation := "no model is bound: this arm measures what deterministic composition alone recovers, which is the floor a model-assisted configuration must beat to have earned its cost"
	if modelBound {
		floorLimitation = "a model is bound: deterministic composition and model-derived additions are reported separately, and the deterministic lane remains the floor this configuration must beat to have earned its cost"
	}
	report := CompositionReport{
		SchemaVersion: "sensei.evalharness.composition.v1",
		Arm:           arm,
		CapturedAt:    opts.CapturedAt,
		Limitations: []string{
			floorLimitation,
			"a candidate is advisory and is not a verdict; whether one matched the intended defect is a grading judgement this harness does not make",
			"candidate counts are reported, never a detection rate, because this arm cannot grade its own output",
			"a synthetic mutant has no graph to bind, and composition validates a composed document's receipt graph digest against its binding's; supplying one without the other is refused, and making them agree would declare a resolved graph digest for a repository that has none. Composition over this suite therefore requires publishing each mutant into a real graph first: until then the composition step is reported as unavailable with its exact refusal, and candidate counts of zero mean COMPOSITION DID NOT RUN, not that composition found nothing",
		},
	}

	control, err := runComposition(opts, "baseline-composition", evalmutant.Baseline(), lane)
	if err != nil {
		return CompositionReport{}, fmt.Errorf("evalharness: composition control: %w", err)
	}
	report.Baseline = control

	for _, d := range evalmutant.Defects() {
		m, err := evalmutant.Build(d)
		if err != nil {
			return CompositionReport{}, fmt.Errorf("evalharness: build %s: %w", d, err)
		}
		res, err := runComposition(opts, string(d)+"-composition", m, lane)
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

func runComposition(opts Options, name string, m evalmutant.Mutant, lane whyinvestigation.ModelLane) (res CompositionSiteResult, err error) {
	// The acquisition is frozen on the way OUT, not where the model outcome
	// happens to arrive. Freezing it mid-function captured res.Candidates
	// before RunArchitecture had assigned them, so a bundle could record a
	// deterministic baseline of zero candidates for a run that produced
	// several — a frozen identity describing a state that never existed.
	//
	// A deferred freeze also means every early return path carries the same
	// baseline the report does, including the paths where WHY or composition
	// failed: the model lane still ran, and its outcome is still a result.
	var modelOutcome modelexec.Outcome
	var composedDigest string
	defer func() {
		if modelOutcome.Binding.Status == "" {
			return
		}
		res.ModelAcquisition = evalmodel.NewAcquisition(opts.CapturedAt, evalmodel.DeterministicBaseline{
			DocumentDigestSHA256:       res.DocumentDigest,
			ComposedResultDigestSHA256: composedDigest,
			ObservationCount:           res.Observations,
			CandidateCount:             res.Candidates,
		}, modelOutcome)
	}()

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
	// Bound the model lane to THIS site's material. Without it the lane sends a
	// request naming no targets and supplying no evidence, so a bridge whose
	// contract requires every claim to cite supplied evidence cannot produce a
	// valid grounded claim — the arm would report a model that "found nothing"
	// when it was never shown anything.
	lane.Request.TargetObservationIDs = observationIDs
	lane.Request.SuppliedEvidence = suppliedEvidenceFor(how)

	var why investigation.Document
	why, modelOutcome, err = investigationsurface.RunWhyWithModel(context.Background(), investigationsurface.WhyRequest{
		Root:           root,
		CapturedAt:     opts.CapturedAt,
		How:            how,
		QueryID:        "query.eval.composition",
		ObservationIDs: observationIDs,
		HistoryStart:   baseRev,
		HistoryEnd:     headRev,
		ProviderIDs:    []string{whyinvestigation.GitProviderID},
	}, lane)
	// The model outcome is recorded VERBATIM, including for a WHY that failed:
	// a refusal or an error is an evaluation result, not a reason to report
	// nothing about the model lane.
	res.ModelStatus = modelOutcome.Binding.Status
	res.ModelReason = modelOutcome.Binding.Reason
	res.ModelRequestDigestSHA256 = modelOutcome.Binding.RequestDigestSHA256
	res.ModelArtifactDigestSHA256 = modelOutcome.Binding.ArtifactDigestSHA256
	res.ModelProviderCalls = modelOutcome.ProviderCalls
	if modelOutcome.Artifact != nil {
		res.ModelItemsByKind = map[string]int{}
		for _, item := range modelOutcome.Artifact.Items {
			res.ModelItemsByKind[item.Kind]++
		}
	}
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
			GeneratorVersion: armIdentityFor(lane),
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
			ResourceLimits: map[string]string{"arm": armIdentityFor(lane), "model": modelResourceLabel(lane)},
		},
	})
	if err != nil {
		res.WhyUnavailable = "composition: " + err.Error()
		return res, nil
	}

	// The composed result's own content identity, so a baseline that changed
	// what it produced without changing how much is a different baseline.
	composedDigest = composedResultDigest(result)

	res.Candidates = len(result.Candidates)
	res.Challenges = len(result.Challenges)
	res.EvidenceRequests = len(result.EvidenceRequests)
	for _, c := range result.Candidates {
		res.CandidateRefs = append(res.CandidateRefs, candidateText(c))
	}
	res.GroundedCandidates, res.DanglingCandidateRefs = groundingOfCandidates(result.Candidates, how, why)
	return res, nil
}

// groundingOfCandidates checks each candidate's citations against the documents
// it was composed from.
//
// Purely referential: it asks whether every observation and evidence identity a
// candidate cites is present, never whether the candidate is right. A candidate
// citing an identity that is not in the document is pointing at nothing, which
// is checkable without a frozen reference set — unlike precision, which is not.
func groundingOfCandidates(candidates []investigator.CandidateEnvelope, how, why investigation.Document) (int, []string) {
	known := map[string]bool{}
	for _, o := range how.Observations {
		known[o.ID] = true
	}
	for _, e := range how.RawEvidence {
		known[e.ID] = true
	}
	for _, e := range why.RawEvidence {
		known[e.ID] = true
	}
	grounded := 0
	dangling := map[string]bool{}
	for _, c := range candidates {
		ok := true
		for _, refs := range [][]string{c.ObservationRefIDs, c.SupportingEvidenceRefIDs, c.RefutingEvidenceRefIDs} {
			for _, id := range refs {
				if id == "" || known[id] {
					continue
				}
				ok = false
				dangling[c.CandidateID+" -> "+id] = true
			}
		}
		if ok {
			grounded++
		}
	}
	out := make([]string, 0, len(dangling))
	for d := range dangling {
		out = append(out, d)
	}
	sort.Strings(out)
	const limit = 10
	if len(out) > limit {
		out = append(out[:limit:limit], fmt.Sprintf("… %d further dangling references not listed", len(dangling)-limit))
	}
	return grounded, out
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

// suppliedEvidenceFor turns a HOW document into the bounded material the model
// is permitted to read and cite.
//
// The file path travels with each excerpt so artifact scope can be checked
// against what was actually shown, and the excerpt is the observation's own
// recorded statement rather than a re-read of the working tree: the model must
// see what the deterministic lane saw, not a file that may have moved since.
func suppliedEvidenceFor(how investigation.Document) []modelexec.SuppliedEvidence {
	out := make([]modelexec.SuppliedEvidence, 0, len(how.Observations))
	for _, obs := range how.Observations {
		// The observation's own subject/predicate/object IS the deterministic
		// statement. The model sees what the deterministic lane recorded, not a
		// re-read of a working tree that may have moved since.
		excerpt := strings.TrimSpace(obs.Subject + " " + obs.Predicate + " " + obs.Object)
		if strings.TrimSpace(obs.ID) == "" || excerpt == "" {
			continue
		}
		file := obs.Evidence.SourceFile
		if file == "" && len(obs.Scope.Files) > 0 {
			file = obs.Scope.Files[0]
		}
		sum := sha256.Sum256([]byte(excerpt))
		out = append(out, modelexec.SuppliedEvidence{
			ID:           obs.ID,
			DigestSHA256: hex.EncodeToString(sum[:]),
			FilePath:     file,
			Excerpt:      excerpt,
		})
	}
	return out
}

// modelResourceLabel records what the lane actually was, not a constant. A run
// that engaged a model must not describe its own resources as "disabled".
func modelResourceLabel(lane whyinvestigation.ModelLane) string {
	switch {
	case lane.Config.Disabled:
		return "disabled"
	case !lane.Config.Requested:
		return "not_requested"
	default:
		return "bound:" + lane.Config.ProviderID + ":" + lane.Config.ModelName
	}
}

// composedResultDigest content-addresses what deterministic composition
// produced. It hashes the result itself rather than a summary of it, because a
// summary is exactly what fails to notice a changed candidate that kept the
// count the same.
func composedResultDigest(result investigator.Result) string {
	data, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
