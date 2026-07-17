// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/factextract"
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
// scope-verified task. It is pure and offline; it records nothing, certifies
// nothing, and completes nothing.
func Build(ctx context.Context, req BuildRequest) (BuildResult, error) {
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

	// Stage 1 + Stage 3: governed source manifest and architecture graph.
	cg, err := compileGovernedGraph(ctx, resultRoot, domain, nil)
	if err != nil {
		return BuildResult{}, err
	}

	// Stage 2: generated repository artifacts (verify presence/digest against the
	// materialized result tree; never write).
	generated, genArtifacts, genLimitations := verifyGeneratedArtifacts(resultRoot)
	limitations = append(limitations, genLimitations...)

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
	baseClaims, err := baseClaimReference(ctx, baseRoot, domain, base)
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

	// Stage 7: closure assessment.
	closureReq := closure.Request{
		SchemaVersion: "1",
		TaskID:        bound.Task.ID,
		Binding:       resultBinding,
		Scope: closureScopeFromRequest(closure.Scope{
			Domain:               domain,
			TaskClass:            "architecture_change",
			RiskClass:            "architecture_sensitive",
			AccessMode:           "modify",
			DirectionRequirement: "not_required",
			Files:                observedFiles(bound),
		}, domain),
	}
	limitations = append(limitations, "closure scope synthesized from admitted change set; task_class/risk_class/direction default to conservative values")
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

	// Stage 9: proof requirements.
	proofDoc := proofRequirements(resultRoot, rbDigest, rb.GraphDigestSHA256, closureSemantic, qResult, closureReport)

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

func revisionStatus(rev string) string {
	if strings.TrimSpace(rev) == "" {
		return architecture.RevisionUnavailable
	}
	return architecture.RevisionResolved
}

func observedFiles(bound resulttransition.BoundRepositoryResult) []string {
	var files []string
	for _, f := range bound.ObservedChange.Files {
		if p := strings.TrimSpace(f.Path); p != "" {
			files = append(files, p)
		}
	}
	return dedupeStrings(files)
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

// baseClaimReference rebuilds claims over the admitted base tree and proves the
// base graph binding is not stale (spec §11).
func baseClaimReference(ctx context.Context, baseRoot, domain string, base closureprotocol.BaseBinding) (architecture.ClaimDocument, error) {
	cg, err := compileGovernedGraph(ctx, baseRoot, domain, nil)
	if err != nil {
		return architecture.ClaimDocument{}, fmt.Errorf("resultpipeline: compile base graph: %w", err)
	}
	if strings.TrimSpace(base.Graph.DigestSHA256) != "" && cg.artifact.GraphSemanticDigestSHA256 != strings.TrimSpace(base.Graph.DigestSHA256) {
		return architecture.ClaimDocument{}, fmt.Errorf("resultpipeline: base_graph_binding_stale: recomputed base graph digest differs from the admitted base binding")
	}
	baseBinding := binding.ToClaimDocumentBinding(base)
	baseBinding.RepositoryDomain = domain
	baseBinding.GraphDigestSHA256 = cg.artifact.GraphSemanticDigestSHA256
	baseBinding.GraphDigestStatus = architecture.GraphDigestResolved
	inferred, err := inferredClaims(baseRoot, baseBinding, cg.triples)
	if err != nil {
		return architecture.ClaimDocument{}, fmt.Errorf("resultpipeline: base claims: %w", err)
	}
	return inferred.Document, nil
}

func architectQuestionsBundle(res questiongen.Result, rep closure.Report) ArchitectQuestionsBundle {
	generated := len(res.Report.Generated)
	architectRequired := 0
	for _, q := range res.Dialogue.OpenQuestions {
		if q.ArchitectRequired {
			architectRequired++
		}
	}
	var unsupported []string
	for _, item := range res.Report.Skipped {
		if item.ReasonCode == "unsupported_template" || item.Disposition == questiongen.DispositionUnsupportedTemplate || item.Disposition == questiongen.DispositionInsufficientGrounding {
			unsupported = append(unsupported, item.BlockerID)
		}
	}
	accounted := len(rep.Blockers) == len(res.Report.Generated)+len(res.Report.ExistingCoverage)+len(res.Report.Skipped)+len(res.Report.NoLongerBacked)
	return ArchitectQuestionsBundle{
		Dialogue:                res.Dialogue,
		Report:                  res.Report,
		GeneratedCount:          generated,
		ArchitectRequiredCount:  architectRequired,
		AllBlockersAccountedFor: accounted,
		UnsupportedCritical:     unsupported,
	}
}

func proofRequirements(resultRoot, rbDigest, graphDigest, closureDigest string, q questiongen.Result, rep closure.Report) ProofRequirementDocument {
	doc := ProofRequirementDocument{
		SchemaVersion:             "1",
		GeneratedBy:               "sensei.proofrequirements",
		ResultBindingDigestSHA256: rbDigest,
		SourceGraphDigestSHA256:   graphDigest,
		SourceClosureDigestSHA256: closureDigest,
	}
	if qd, err := closureprotocol.SemanticDigest(q.Report); err == nil {
		doc.SourceQuestionsDigestSHA256 = qd
	}
	surfaces, err := factextract.ExtractAuthorityCandidates(resultRoot)
	if err != nil {
		doc.Limitations = append(doc.Limitations, "authority-surface extraction failed: "+err.Error())
	} else {
		doc.Obligations = proofrequirements.BuildObligations(surfaces).ProofObligations
	}
	for _, q := range q.Dialogue.OpenQuestions {
		if q.ArchitectRequired {
			doc.UnresolvedArchitectQuestion = append(doc.UnresolvedArchitectQuestion, q.ID)
		}
	}
	if len(doc.Obligations) == 0 {
		doc.Limitations = append(doc.Limitations, "no proof obligations derived for this result (explicit empty requirement set)")
	}
	doc.Limitations = append(doc.Limitations, "result-side required tests and forbidden moves not yet composed from the result graph")
	return doc
}
