// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/questiondisposition"
	"github.com/globulario/sensei/golang/propose"
)

// Dedicated governed-promotion authority (Slice 8.1b-0), distinct from disposition.
const (
	DomainGovernedPromotion  = "authority.sensei_governed_promotion"
	GrantGovernedPromotion   = "grant.sensei.governed_promotion"
	MechanismPathPromotion   = "mutation_path.governed_promotion"
	TargetKindGovernedRecord = "governed_record"
	promotionOperationID     = "op.promote.governed_record"
	promotionRiskClass       = "architecture_sensitive"
)

// Outcome is the closed set of promotion transaction outcomes.
type Outcome string

const (
	OutcomeCommitted                Outcome = "committed"
	OutcomeExactReplay              Outcome = "exact_replay"
	OutcomeIncompleteAtSource       Outcome = "promotion_incomplete_at_source"
	OutcomeIncompleteAtGraph        Outcome = "promotion_incomplete_at_graph"
	OutcomeIncompleteAtCommit       Outcome = "promotion_incomplete_at_commit"
	OutcomeIneligibleDisposition    Outcome = "ineligible_disposition"
	OutcomeStaleInput               Outcome = "stale_or_superseded_input"
	OutcomeAuthorityRefusal         Outcome = "authority_refusal"
	OutcomeScopeRefusal             Outcome = "scope_refusal"
	OutcomeContradiction            Outcome = "contradiction_or_collision"
	OutcomeManifestCASFailure       Outcome = "manifest_cas_failure"
	OutcomeGraphVerificationFailure Outcome = "graph_verification_failure"
	OutcomeTamperedJournal          Outcome = "tampered_journal_or_artifact"
)

// PromoteRequest promotes an accepted reusable-candidate disposition into a
// governed record and a verified repository-graph node. All trusted values are
// reloaded/recomputed; no governed status, graph digest, node IRI, or verdict is
// caller-supplied.
type PromoteRequest struct {
	RepositoryRoot                         string
	TaskDirectory                          string
	RepositoryDomain                       string
	IdentityRoot                           string // promotion actor identity store
	QuestionDispositionReceiptDigestSHA256 string // which disposition to promote
	Proposal                               propose.Request
	EffectiveScopeDomain                   string
	EffectiveScopeFiles                    []string
}

// PromoteResult is the typed transaction outcome.
type PromoteResult struct {
	Outcome                       Outcome
	PromotionLineageID            string
	ReceiptDigestSHA256           string
	CommittedCausalIdentitySHA256 string
	Receipt                       *QuestionPromotionReceipt
	Detail                        string
}

// Refusal is a typed pre-mutation refusal (ineligible / authority / scope / stale).
type Refusal struct {
	Outcome Outcome
	Detail  string
}

func (e *Refusal) Error() string { return string(e.Outcome) + ": " + e.Detail }

func refusal(o Outcome, format string, args ...any) *Refusal {
	return &Refusal{Outcome: o, Detail: fmt.Sprintf(format, args...)}
}

// promoteDeps is immutable DI for crash-window tests: stopAfter, when set to a
// journal event, makes the transaction return immediately after appending that
// event (simulating a crash before the next step). afterReceiptPersist simulates
// a crash between durable receipt and the promotion_committed append.
type promoteDeps struct {
	now                 func() time.Time
	stopAfter           JournalEventType
	afterSourceApply    func() // crash after source mutation, before source_committed append
	afterGraphBuild     func() // crash after graph build, before graph_verified append
	afterReceiptPersist func() // crash after durable receipt, before promotion_committed append
}

func productionDeps() promoteDeps {
	return promoteDeps{now: func() time.Time { return time.Now().UTC() }}
}

// Promote runs the full transaction (fresh or resuming). It is idempotent: an
// exact retry from identical inputs resolves to the same PromotionLineageID and
// reconciles to exactly one authoritative commit.
func Promote(ctx context.Context, req PromoteRequest) (PromoteResult, error) {
	return promoteWith(ctx, req, productionDeps())
}

func promoteWith(ctx context.Context, req PromoteRequest, deps promoteDeps) (PromoteResult, error) {
	prepared, lineageID, err := prepare(req)
	if err != nil {
		var r *Refusal
		if errors.As(err, &r) {
			return PromoteResult{Outcome: r.Outcome, Detail: r.Detail}, nil
		}
		return PromoteResult{}, err
	}
	promotionDir := filepath.Join(req.RepositoryRoot, ".sensei", "project", "promotions", lineageID)
	return drive(ctx, req, prepared, lineageID, promotionDir, deps)
}

// prepare runs the accepted-input gate and freezes the pre-graph promotion world,
// returning the prepared receipt (pre-graph fields) and the PromotionLineageID.
func prepare(req PromoteRequest) (QuestionPromotionReceipt, string, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if root == "" || taskDir == "" || strings.TrimSpace(req.IdentityRoot) == "" {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "repository root, task dir, and identity root are required")
	}

	// Reload the exact recorded disposition and gate eligibility.
	rd, err := questiondisposition.LoadRecordedDisposition(taskDir, req.QuestionDispositionReceiptDigestSHA256)
	if err != nil {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "load disposition: %v", err)
	}
	d := rd.Receipt
	if d.Disposition != questiondisposition.DispositionAnswered {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "disposition is %q, not answered", d.Disposition)
	}
	if d.Reusability != questiondisposition.ReusabilityReusableCandidate {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "reusability is %q, not reusable_candidate", d.Reusability)
	}
	proj, perr := questiondisposition.ProjectQuestion(taskDir, d.QuestionID)
	if perr != nil {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "project question: %v", perr)
	}
	if proj.Contested {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "question %q is contested", d.QuestionID)
	}
	if proj.Latest.ReceiptDigestSHA256 != d.ReceiptDigestSHA256 {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeStaleInput, "disposition is superseded by a later one")
	}

	// Verify the SEPARATE promotion actor + the exact governed promote operation.
	index, err := authority.LoadPolicyIndex(root)
	if err != nil {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeAuthorityRefusal, "load policy index: %v", err)
	}
	id, enrolled, err := identity.LoadManifest(req.IdentityRoot)
	if err != nil || !enrolled {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeAuthorityRefusal, "promotion actor is not enrolled")
	}
	binding := id.ActorBinding()
	evaluatedAt := time.Now().UTC()
	verified, err := authority.VerifyActorBinding(binding, identity.Resolver(req.IdentityRoot), index, evaluatedAt)
	if err != nil || verified.Status != closureprotocol.ReceiptValid {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeAuthorityRefusal, "promotion actor not verified")
	}
	actorDigest, _ := closureprotocol.SemanticDigest(binding)
	grantID, roleID, aerr := resolvePromotionAuthority(index, binding, verified, evaluatedAt, taskDir)
	if aerr != nil {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeAuthorityRefusal, "%v", aerr)
	}

	// Validate the proposed governed record and derive canonical identity.
	plan, err := governedmutation.Plan(governedmutation.Request{RepositoryRoot: root, Proposal: req.Proposal})
	if err != nil {
		var ce *governedmutation.ContradictionError
		if errors.As(err, &ce) {
			return QuestionPromotionReceipt{}, "", refusal(OutcomeContradiction, "%v", ce)
		}
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "invalid governed record: %v", err)
	}
	if plan.IsCandidate {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "promotion target must be a governed kind, not a candidate")
	}
	nodeIRI := GovernedNodeIRIFor(req.Proposal.Kind, plan.CanonicalID)
	if nodeIRI == "" {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeIneligibleDisposition, "kind %q has no governed node class", req.Proposal.Kind)
	}

	// Effective scope must be a subset of the accepted disposition scope.
	if err := boundScope(req, d); err != nil {
		return QuestionPromotionReceipt{}, "", err
	}

	preManifest, err := governedmutation.GovernedManifestDigest(root)
	if err != nil {
		return QuestionPromotionReceipt{}, "", refusal(OutcomeManifestCASFailure, "pre-manifest: %v", err)
	}

	rc := QuestionPromotionReceipt{
		SchemaVersion:                          SchemaVersion,
		Task:                                   d.Task,
		ResultBindingDigestSHA256:              d.ResultBindingDigestSHA256,
		ResultTransitionReceiptDigestSHA256:    d.ResultTransitionReceiptDigestSHA256,
		DispositionEntryDigestSHA256:           rd.EntryDigestSHA256,
		QuestionDispositionReceiptDigestSHA256: d.ReceiptDigestSHA256,
		QuestionID:                             d.QuestionID,
		AnswerID:                               d.AnswerID,
		AnswerBytesDigestSHA256:                d.AnswerBytesDigestSHA256,
		DispositionActorBindingDigestSHA256:    d.AnsweringActorBindingDigestSHA256,
		DispositionAuthorityGrantID:            d.AuthorityGrantID,
		DispositionAuthorityRoleID:             d.AuthorityRoleID,
		PromotionActorBindingDigestSHA256:      actorDigest,
		PromotionAuthorityGrantID:              grantID,
		PromotionAuthorityRoleID:               roleID,
		GovernedTargetKind:                     req.Proposal.Kind,
		CanonicalRecordID:                      plan.CanonicalID,
		SourceDocument:                         plan.TargetRelPath,
		TopLevelKey:                            plan.TopKey,
		EffectiveScopeDomain:                   strings.TrimSpace(req.EffectiveScopeDomain),
		EffectiveScopeFiles:                    cleanSorted(req.EffectiveScopeFiles),
		GovernedNodeIRI:                        nodeIRI,
		CanonicalMutationDigestSHA256:          plan.MutationDigestSHA256,
		PreMutationManifestDigestSHA256:        preManifest,
		Producer:                               GeneratedBy,
		CombinedSeedObligationOutstanding:      true,
	}
	lineageID, err := ComputeLineageID(rc)
	if err != nil {
		return QuestionPromotionReceipt{}, "", err
	}
	rc.PromotionLineageID = lineageID
	return rc, lineageID, nil
}

func boundScope(req PromoteRequest, d questiondisposition.QuestionDispositionReceipt) error {
	if strings.TrimSpace(req.EffectiveScopeDomain) != "" && strings.TrimSpace(d.EffectiveScopeDomain) != "" &&
		req.EffectiveScopeDomain != d.EffectiveScopeDomain {
		return refusal(OutcomeScopeRefusal, "effective scope domain broadens the disposition")
	}
	allowed := map[string]bool{}
	for _, f := range d.EffectiveScopeFiles {
		allowed[strings.TrimSpace(f)] = true
	}
	if len(allowed) > 0 {
		for _, f := range req.EffectiveScopeFiles {
			if !allowed[strings.TrimSpace(f)] {
				return refusal(OutcomeScopeRefusal, "effective scope file %q broadens the disposition", f)
			}
		}
	}
	return nil
}

func cleanSorted(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func grantRole(index authority.PolicyIndex, grantID string, verifiedRoles []string) string {
	grant, ok := index.AuthorityGrants[grantID]
	if !ok {
		return ""
	}
	for _, r := range grant.ActorRoleIDs {
		for _, vr := range verifiedRoles {
			if r == vr {
				return r
			}
		}
	}
	return ""
}

// resolvePromotionAuthority requires a RESOLVED, valid operation result for the
// exact governed promote operation and returns its grant + role.
func resolvePromotionAuthority(index authority.PolicyIndex, binding closureprotocol.ActorBinding, verified authority.VerifiedActor, evaluatedAt time.Time, taskDir string) (string, string, error) {
	ra, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return "", "", fmt.Errorf("load recorded authority: %w", err)
	}
	plan := closureprotocol.ChangePlan{
		PlanID: "plan.promote." + ra.Base.Task.ID,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       promotionOperationID,
			Kind:              closureprotocol.OperationPromote,
			TargetKind:        TargetKindGovernedRecord,
			SelectedMechanism: closureprotocol.MechanismGovernedWorkflow,
			RiskClass:         promotionRiskClass,
		}},
	}
	app := []authority.AuthorityApplicability{{
		OperationID:                 promotionOperationID,
		AuthorityDomainIDs:          []string{DomainGovernedPromotion},
		RequiredRuntimeMechanismIDs: []string{MechanismPathPromotion},
	}}
	resolution, err := admission.ResolveAuthority(index, admission.ResolveAuthorityInput{
		Actor:                            binding,
		VerifiedActor:                    verified,
		Base:                             ra.Base,
		ChangePlan:                       plan,
		Applicability:                    app,
		PolicyID:                         ra.Base.Policies.Admission,
		ClosureAssessmentDigestSHA256:    ra.Resolution.ClosureAssessmentDigestSHA256,
		AuthorityPolicyGraphDigestSHA256: closureprotocol.MustSemanticDigest(index),
		EvaluatedAt:                      evaluatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve promotion authority: %w", err)
	}
	var op *closureprotocol.AuthorityResolutionOperation
	for i := range resolution.OperationResults {
		if resolution.OperationResults[i].OperationID == promotionOperationID {
			op = &resolution.OperationResults[i]
			break
		}
	}
	if op == nil || op.Status != closureprotocol.ReceiptValid {
		return "", "", fmt.Errorf("promote operation not authorized")
	}
	granted := false
	for _, g := range op.GrantIDs {
		if g == GrantGovernedPromotion {
			granted = true
		}
	}
	if !granted {
		return "", "", fmt.Errorf("promotion not authorized by %s", GrantGovernedPromotion)
	}
	role := grantRole(index, GrantGovernedPromotion, verified.VerifiedRoleIDs)
	if role == "" {
		return "", "", fmt.Errorf("no verified role authorizes %s", GrantGovernedPromotion)
	}
	return GrantGovernedPromotion, role, nil
}
