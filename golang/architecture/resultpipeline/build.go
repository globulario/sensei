// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/factextract"
	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/inference"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/proofrequirements"
	"github.com/globulario/sensei/golang/architecture/questiongen"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// BuildResult is the complete result architecture: the exact repository result,
// the complete frozen ResultBinding, the ten mandatory stage artifacts (each with
// its receipt and derivation), and the explicit limitations and open states.
type BuildResult struct {
	BoundRepositoryResult     resulttransition.BoundRepositoryResult
	ResultBinding             closureprotocol.ResultBinding
	ResultBindingDigestSHA256 string

	StageArtifacts []PipelineArtifact

	ClosureReport     closure.Report
	Dialogue          architecture.DialogueDocument
	ProofRequirements ProofRequirementDocument

	PipelinePolicyID string
	EvaluatedAt      string
	Limitations      []string
}

// Build composes the complete ten-stage result architecture for one admitted,
// scope-verified task and fails closed unless the result is semantically valid.
// It is pure and offline; it records nothing, certifies nothing, and completes
// nothing. A worktree-mode result has no verified revision and is therefore
// legitimately uncertifiable — such a result is refused, not returned.
func Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
	result, err := assembleBuildResult(ctx, req)
	if err != nil {
		return BuildResult{}, err
	}
	// Fail closed: a semantically incomplete result is never returned alongside a
	// nil error. The validator is pure and offline; it does not modify result.
	if err := ValidateBuildResult(result); err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: validate build result: %w", err)
	}
	return result, nil
}

// assembleBuildResult composes the ten-stage result without the final semantic
// gate. Callers other than Build must not use it to return a result to a
// governance consumer; it exists so assembly-structural properties can be tested
// on results the gate would legitimately refuse (e.g. an uncertifiable worktree
// result).
func assembleBuildResult(ctx context.Context, req BuildRequest) (BuildResult, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if root == "" || taskDir == "" {
		return BuildResult{}, fmt.Errorf("resultpipeline: repository root and task directory are required")
	}
	policyID := strings.TrimSpace(req.PipelinePolicyID)
	if policyID == "" {
		policyID = DefaultPipelinePolicyID
	}

	// 1. Bind the exact repository result and load the upstream truth.
	bound, err := resulttransition.BindRepositoryResult(ctx, resulttransition.BindResultRequest{
		RepositoryRoot: root,
		TaskDirectory:  taskDir,
		Mode:           req.ResultMode,
		ResultRevision: req.ResultRevision,
	})
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: bind result: %w", err)
	}
	base, err := admission.LoadTaskBaseBinding(taskDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: load base binding: %w", err)
	}
	// The stable evaluation clock is the ledger's scope_verified time, never
	// time.Now (spec §5).
	evaluatedAt, err := admission.LoadEventProducedAt(taskDir, closureprotocol.LedgerEventScopeVerified)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: load stable evaluation time: %w", err)
	}
	domain := strings.TrimSpace(req.RepositoryDomain)
	if domain == "" {
		domain = strings.TrimSpace(base.Repository.Domain)
	}

	var limitations []string

	// 2. Materialize the exact base and result roots into isolated directories.
	resultRoot, cleanupResult, err := materializeTree(ctx, root, bound.RepositoryResult.GitTreeObjectID)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: materialize result root: %w", err)
	}
	defer cleanupResult()
	baseRoot, cleanupBase, err := materializeTree(ctx, root, base.Repository.Revision)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: materialize base root: %w", err)
	}
	defer cleanupBase()

	// 3. Load the immutable graph-input snapshot admission recorded on
	// task_prepared, plus its content-addressed supplemental graph bytes. This is
	// the exact world admission observed — never a hardcoded root set and never a
	// current active governance-pack pointer.
	recorded, err := loadRecordedGraphInputs(taskDir, domain)
	if err != nil {
		return BuildResult{}, err
	}

	// 5 + 6. Reproduce the admitted base graph from the snapshot resolved against
	// the base root, and prove it equals the base binding's graph digest. Until
	// this equality holds, the snapshot is an immutable declaration, not a proven
	// derivation.
	baseInputs, err := resolveGraphInputs(baseRoot, recorded.Snapshot, recorded.SupplementalBytes)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: base_graph_inputs_unresolved: %w", err)
	}
	baseGraph, err := compileGovernedGraph(ctx, baseRoot, baseInputs)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: base_graph_inputs_unresolved: %w", err)
	}
	if want := strings.TrimSpace(base.Graph.DigestSHA256); want != "" && baseGraph.artifact.GraphSemanticDigestSHA256 != want {
		return BuildResult{}, fmt.Errorf("resultpipeline: base_graph_binding_stale: recomputed base graph digest %s does not match admitted %s", baseGraph.artifact.GraphSemanticDigestSHA256, want)
	}

	// 7 + 8. Build the result graph from the EXACT same snapshot and policy against
	// the result root — only repository-tree contents may differ from the base.
	resultInputs, err := resolveGraphInputs(resultRoot, recorded.Snapshot, recorded.SupplementalBytes)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: graph_input_snapshot_unavailable: %w", err)
	}
	cg, err := compileGovernedGraph(ctx, resultRoot, resultInputs)
	if err != nil {
		return BuildResult{}, err
	}

	// Stage 2: generated repository artifacts. Regenerate every governed artifact
	// in memory from the exact result architecture and compare byte-for-byte
	// against the materialized result tree; never write. Only verified artifacts
	// bind into the result binding.
	profile, err := generatedartifact.ProfileForDomain(domain)
	if err != nil {
		return BuildResult{}, err
	}
	sourceManifestDigest, err := closureprotocol.SemanticDigest(cg.compilation.SourceManifest)
	if err != nil {
		return BuildResult{}, err
	}
	genResult, err := generatedartifact.RegenerateAndVerify(ctx, generatedartifact.Context{
		RepositoryRoot:                 resultRoot,
		RepositoryDomain:               domain,
		GraphInputPolicyID:             recorded.Snapshot.PolicyID,
		GraphInputSnapshotDigestSHA256: recorded.SnapshotDigest,
		SourceManifestDigestSHA256:     sourceManifestDigest,
		SupplementalGraphs:             recorded.Snapshot.SupplementalGraphs,
		GraphArtifact:                  cg.artifact,
	}, profile)
	if err != nil {
		return BuildResult{}, err
	}
	generated := genResult.VerifiedArtifacts
	genArtifacts := genResult.Manifest

	// Complete the frozen ResultBinding now that the graph digest and generated
	// artifacts are known. It is never modified after any receipt binds it.
	rb, rbDigest, err := completeResultBinding(bound.RepositoryResult, cg.artifact.GraphSemanticDigestSHA256, generated)
	if err != nil {
		return BuildResult{}, err
	}

	// The exact result snapshot binding every claim stage is bound to.
	resultBinding := architecture.ClaimDocumentBinding{
		RepositoryDomain:  domain,
		Revision:          rb.ResultRevision,
		RevisionStatus:    revisionStatus(rb.ResultRevision),
		TreeDigestSHA256:  rb.ResultTreeDigestSHA256,
		GraphDigestSHA256: rb.GraphDigestSHA256,
		GraphDigestStatus: architecture.GraphDigestResolved,
	}

	// Stage 4: inferred claims over the exact result.
	inferred, err := inferredClaims(resultRoot, resultBinding, cg.triples)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: inferred claims: %w", err)
	}

	// Base-claim reference (spec §11): rebuild claims over the admitted base and
	// prove the base graph binding is not stale. Never published as a result
	// artifact; used only as maintenance's Previous input.
	baseClaims, err := baseClaimReference(baseRoot, domain, base, baseGraph)
	if err != nil {
		return BuildResult{}, err
	}

	// Stage 5: maintained claims.
	maintResult, err := maintenance.Evaluate(maintenance.Context{
		RepositoryRoot:  resultRoot,
		Current:         inferred.Document,
		Previous:        &baseClaims,
		Dialogue:        emptyResultDialogue(resultBinding),
		Evidence:        emptyResultEvidenceState(resultBinding),
		ObservedBinding: resultBinding,
		EvaluatedAt:     evaluatedAt,
	})
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: maintained claims: %w", err)
	}

	// Stage 6: plane assessment.
	planeReport, err := plane.Assess(plane.Context{
		Claims:              maintResult.Document,
		Maintenance:         &maintResult.Report,
		Graph:               cg.planeIndex,
		Evidence:            emptyResultEvidenceState(resultBinding),
		Dialogue:            emptyResultDialogue(resultBinding),
		GraphDigest:         rb.GraphDigestSHA256,
		GraphDigestStatus:   architecture.GraphDigestResolved,
		GraphDigestVerified: true,
	})
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: plane assessment: %w", err)
	}

	// Stage 7: closure assessment. Load the task's authoritative closure request
	// from the verified ledger (never the mutable projection), preserving its
	// scope — task class, risk, access mode, direction requirement, domains,
	// files, symbols, components, and additional dimensions — and rebinding only
	// the repository snapshot and graph to the exact result. The result scope is
	// NOT reduced to the observed change: read targets and architectural
	// consequences matter too.
	closureReq, err := loadRecordedClosureRequest(taskDir, bound.Task)
	if err != nil {
		return BuildResult{}, err
	}
	closureReq.Binding = resultBinding
	if strings.TrimSpace(closureReq.Scope.Domain) == "" {
		closureReq.Scope.Domain = domain
	}
	graphReceipt := graphsnapshot.Receipt{DigestSHA256: rb.GraphDigestSHA256, Status: architecture.GraphDigestResolved, Verified: true}
	closureReport, err := closure.Evaluate(closure.Context{
		Request:          closureReq,
		Claims:           maintResult.Document,
		Maintenance:      &maintResult.Report,
		Plane:            &planeReport,
		Dialogue:         emptyResultDialogue(resultBinding),
		Evidence:         emptyResultEvidenceState(resultBinding),
		Graph:            cg.closIndex,
		GraphReceipt:     graphReceipt,
		RepositoryRoot:   resultRoot,
		RepositoryRev:    rb.ResultRevision,
		RepositoryStatus: revisionStatus(rb.ResultRevision),
	})
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: closure assessment: %w", err)
	}

	// Stage 8: architect questions. Always runs.
	qReg, err := questiongen.DefaultRegistry()
	if err != nil {
		return BuildResult{}, err
	}
	closureSemantic, err := closureprotocol.SemanticDigest(closureReport)
	if err != nil {
		return BuildResult{}, err
	}
	qResult, err := questiongen.Generate(questiongen.Context{
		Closure:                       closureReport,
		Claims:                        maintResult.Document,
		Graph:                         cg.closIndex,
		Existing:                      emptyResultDialogue(resultBinding),
		CreatedAt:                     evaluatedAt,
		ClosureAssessmentDigestSHA256: closureSemantic,
	}, qReg)
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: architect questions: %w", err)
	}
	questions := architectQuestionsBundle(qResult, closureReport)

	// Stage 9: proof requirements. Compose the complete result-bound requirements
	// from the seven authoritative sources — carried authority/admission truth,
	// the verified Stage 2 outputs, the scoped result graph, closure, and the
	// architect questions — via the single composer. No source is re-extracted.
	proofDoc, err := proofrequirements.Compose(ctx, composeProofInput(rbDigest, rb.GraphDigestSHA256, bound, genResult, cg.closIndex, closureReport, questions))
	if err != nil {
		return BuildResult{}, fmt.Errorf("resultpipeline: proof requirements: %w", err)
	}

	// Assemble the nine stage artifacts, their receipts and derivations.
	artifacts, err := assembleStages(rbDigest, cg, inferred, maintResult, planeReport, closureReport, questions, proofDoc, genArtifacts)
	if err != nil {
		return BuildResult{}, err
	}

	// Stage 10: artifact manifest of the first nine stages (no self-reference).
	manifest, err := artifactManifest(rbDigest, artifacts)
	if err != nil {
		return BuildResult{}, err
	}
	artifacts = append(artifacts, manifest)

	return BuildResult{
		BoundRepositoryResult:     bound,
		ResultBinding:             rb,
		ResultBindingDigestSHA256: rbDigest,
		StageArtifacts:            artifacts,
		ClosureReport:             closureReport,
		Dialogue:                  qResult.Dialogue,
		ProofRequirements:         proofDoc,
		PipelinePolicyID:          policyID,
		EvaluatedAt:               evaluatedAt,
		Limitations:               limitations,
	}, nil
}

// loadRecordedClosureRequest loads the task's authoritative closure request from
// the verified ledger-backed task_prepared artifact. It never trusts the mutable
// closure-request.yaml projection: the bytes come from the content-addressed
// ledger artifact, read only after the ledger chain verifies. It fails closed
// when no authoritative request exists.
func loadRecordedClosureRequest(taskDir string, task closureprotocol.TaskBinding) (closure.Request, error) {
	data, found, err := admission.LoadLatestArtifactBytes(taskDir, closureprotocol.LedgerEventTaskPrepared, "closure_request")
	if err != nil {
		return closure.Request{}, fmt.Errorf("resultpipeline: closure_request_unavailable: %w", err)
	}
	if !found {
		return closure.Request{}, fmt.Errorf("resultpipeline: closure_request_unavailable: task_prepared event has no closure_request artifact")
	}
	req, err := closure.UnmarshalRequestYAML(data)
	if err != nil {
		return closure.Request{}, fmt.Errorf("resultpipeline: closure_request_unavailable: %w", err)
	}
	if id := strings.TrimSpace(req.TaskID); id != "" && strings.TrimSpace(task.ID) != "" && id != strings.TrimSpace(task.ID) {
		return closure.Request{}, fmt.Errorf("resultpipeline: closure_request_unavailable: recorded closure request task %q does not match bound task %q", id, task.ID)
	}
	return req, nil
}

func revisionStatus(rev string) string {
	if strings.TrimSpace(rev) == "" {
		return architecture.RevisionUnavailable
	}
	return architecture.RevisionResolved
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// inferredClaims runs the result-local fact extraction and inference (spec §10).
func inferredClaims(resultRoot string, resultBinding architecture.ClaimDocumentBinding, triples []graphsnapshot.Triple) (InferredClaimsBundle, error) {
	report, err := factextract.Extract(resultRoot, resultInferenceProfile(resultRoot))
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	facts := rebindFactsToResult(report.Facts, resultBinding)
	limitations := append([]architecture.Limitation{}, report.Limitations...)

	governed, govLimitations, err := inference.GovernedDirectionFactsFromTriples(triples, resultRoot, resultBinding)
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	facts = append(facts, governed...)
	limitations = append(limitations, govLimitations...)

	reg, err := inference.DefaultRegistry()
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	rules, err := reg.Select(nil)
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	infCtx := inference.Context{Binding: resultBinding, Facts: facts, Limitations: limitations}
	apps, err := inference.NewEngine(rules).Apply(infCtx)
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	doc, err := inference.BuildClaimDocument(infCtx, apps)
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	doc.Claims = inference.MarkGovernedDirectionConflicts(doc.Claims)
	doc.Claims, err = architecture.CompactClaims(doc.Claims)
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	doc, err = architecture.NormalizeClaimDocument(doc)
	if err != nil {
		return InferredClaimsBundle{}, err
	}
	return InferredClaimsBundle{Document: doc, Limitations: limitations}, nil
}

// rebindFactsToResult binds extracted facts to the exact result domain. It never
// invents a revision: worktree facts stay bound by their per-file source digest.
func rebindFactsToResult(facts []architecture.Fact, b architecture.ClaimDocumentBinding) []architecture.Fact {
	out := make([]architecture.Fact, 0, len(facts))
	for _, f := range facts {
		if f.Scope.Repository == "" {
			f.Scope.Repository = b.RepositoryDomain
		}
		if f.Provenance != nil {
			p := *f.Provenance
			if p.RepositoryDomain == "" {
				p.RepositoryDomain = b.RepositoryDomain
				p.RepositoryDomainStatus = architecture.RepositoryDomainResolved
			}
			p.Revision = b.Revision
			p.RevisionStatus = b.RevisionStatus
			f.Provenance = &p
		}
		out = append(out, f)
	}
	return out
}

// baseClaimReference rebuilds claims over the admitted base tree using the SAME
// already-verified base graph — no second, independent graph build that could
// quietly observe different inputs (spec §9/§11). Never published as a result
// artifact; used only as maintenance's Previous input.
func baseClaimReference(baseRoot, domain string, base closureprotocol.BaseBinding, baseGraph compiledGraph) (architecture.ClaimDocument, error) {
	baseBinding := binding.ToClaimDocumentBinding(base)
	baseBinding.RepositoryDomain = domain
	baseBinding.GraphDigestSHA256 = baseGraph.artifact.GraphSemanticDigestSHA256
	baseBinding.GraphDigestStatus = architecture.GraphDigestResolved
	inferred, err := inferredClaims(baseRoot, baseBinding, baseGraph.triples)
	if err != nil {
		return architecture.ClaimDocument{}, fmt.Errorf("resultpipeline: base claims: %w", err)
	}
	return inferred.Document, nil
}

func architectQuestionsBundle(res questiongen.Result, rep closure.Report) ArchitectQuestionsBundle {
	architectRequired := 0
	for _, q := range res.Dialogue.OpenQuestions {
		if q.ArchitectRequired {
			architectRequired++
		}
	}

	// The exact set of current closure blockers.
	currentIDs := map[string]bool{}
	for _, b := range rep.Blockers {
		if id := strings.TrimSpace(b.ID); id != "" {
			currentIDs[id] = true
		}
	}

	// Count each current blocker's dispositions across the current (non-historical)
	// buckets. no_longer_backed is historical dialogue cleanup and never counts
	// toward a current blocker.
	dispositions := map[string][]string{}
	unsupported := map[string]bool{}
	current := func(items []questiongen.Item) {
		for _, it := range items {
			id := strings.TrimSpace(it.BlockerID)
			if id == "" || !currentIDs[id] {
				continue
			}
			dispositions[id] = append(dispositions[id], it.Disposition)
			if it.Disposition == questiongen.DispositionUnsupportedTemplate || it.Disposition == questiongen.DispositionInsufficientGrounding {
				unsupported[id] = true
			}
		}
	}
	current(res.Report.Generated)
	current(res.Report.ExistingCoverage)
	current(res.Report.Skipped)

	var accounted, unaccounted, duplicate, unsupportedCritical []string
	for _, id := range sortedKeys(currentIDs) {
		switch n := len(dispositions[id]); {
		case n == 1:
			accounted = append(accounted, id)
		case n == 0:
			unaccounted = append(unaccounted, id)
		default:
			duplicate = append(duplicate, id)
		}
		if unsupported[id] {
			unsupportedCritical = append(unsupportedCritical, id)
		}
	}
	var historical []string
	for _, it := range res.Report.NoLongerBacked {
		historical = append(historical, strings.TrimSpace(it.BlockerID))
	}

	all := len(unaccounted) == 0 && len(duplicate) == 0
	// A load-bearing blocker with an unsupported template or insufficient grounding
	// is accounted for (we explained why no question was generated) but the
	// architect decision is NOT resolved, so questions are not actionable and proof
	// stays blocked.
	actionable := all && len(unsupportedCritical) == 0

	return ArchitectQuestionsBundle{
		Dialogue:                     res.Dialogue,
		Report:                       res.Report,
		GeneratedCount:               len(res.Report.Generated),
		ArchitectRequiredCount:       architectRequired,
		CurrentBlockerIDs:            dedupeStrings(sortedKeys(currentIDs)),
		AccountedBlockerIDs:          dedupeStrings(accounted),
		UnaccountedBlockerIDs:        dedupeStrings(unaccounted),
		DuplicateAccountingIDs:       dedupeStrings(duplicate),
		HistoricalNoLongerBacked:     dedupeStrings(historical),
		UnsupportedCritical:          dedupeStrings(unsupportedCritical),
		AllBlockersAccountedFor:      all,
		ArchitectQuestionsActionable: actionable,
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// composeProofInput adapts the pipeline's in-memory stage state into the neutral
// proofrequirements.ComposeInput. It carries the exact verified upstream truth
// (authority resolution + admission decision and their digests, straight from the
// binding — never a ledger re-read), reuses the verified Stage 2 generated
// proof-obligations output rather than re-extracting authority surfaces, and
// passes the scoped result graph, closure report, and neutral architect-question
// accounting.
func composeProofInput(rbDigest, graphSemanticDigest string, bound resulttransition.BoundRepositoryResult, gen generatedartifact.VerificationResult, graph closure.GraphIndex, rep closure.Report, questions ArchitectQuestionsBundle) proofrequirements.ComposeInput {
	var verifiedPaths []string
	for _, out := range gen.ExpectedOutputs {
		if p := strings.TrimSpace(out.Path); p != "" {
			verifiedPaths = append(verifiedPaths, p)
		}
	}
	var repoProof proofrequirements.RepositoryProofOutput
	if out, ok := gen.ExpectedOutputByPath(generatedartifact.ProofObligationsPath); ok {
		repoProof = proofrequirements.RepositoryProofOutput{
			Path: out.Path, Bytes: out.Bytes,
			SemanticDigestSHA256: out.SemanticDigestSHA256, ByteDigestSHA256: out.ByteDigestSHA256,
		}
	}

	var unresolved []string
	for _, q := range questions.Dialogue.OpenQuestions {
		if q.ArchitectRequired {
			unresolved = append(unresolved, q.ID)
		}
	}

	return proofrequirements.ComposeInput{
		ResultBindingDigestSHA256:         rbDigest,
		AuthorityResolution:               bound.AuthorityResolution,
		ExpectedAuthorityResolutionDigest: bound.AuthorityResolutionDigestSHA256,
		AdmissionDecision:                 bound.AdmissionDecision,
		ExpectedAdmissionDecisionDigest:   bound.AdmissionDecisionDigestSHA256,
		GeneratedArtifacts: proofrequirements.GeneratedArtifactSummary{
			ManifestDigestSHA256: closureprotocol.MustSemanticDigest(gen.Manifest),
			VerifiedPaths:        verifiedPaths,
			AllRequiredMatched:   gen.Manifest.AllRequiredMatched,
		},
		RepositoryProofOutput:         repoProof,
		Graph:                         graph,
		GraphSemanticDigestSHA256:     graphSemanticDigest,
		ClosureReport:                 rep,
		QuestionsSemanticDigestSHA256: closureprotocol.MustSemanticDigest(questions),
		Questions: proofrequirements.QuestionInput{
			CurrentBlockerIDs:              questions.CurrentBlockerIDs,
			AccountedBlockerIDs:            questions.AccountedBlockerIDs,
			UnaccountedBlockerIDs:          questions.UnaccountedBlockerIDs,
			DuplicateAccountingIDs:         questions.DuplicateAccountingIDs,
			UnsupportedCriticalIDs:         questions.UnsupportedCritical,
			UnresolvedArchitectQuestionIDs: unresolved,
			Actionable:                     questions.ArchitectQuestionsActionable,
		},
	}
}
