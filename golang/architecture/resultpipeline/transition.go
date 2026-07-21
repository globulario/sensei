// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// Stable transition-candidate error codes.
const (
	CodeTransitionCandidateInvalid        = "resultpipeline.transition_candidate_invalid"
	CodeTransitionReceiptMismatch         = "resultpipeline.transition_receipt_mismatch"
	CodeTransitionReceiptDigestMismatch   = "resultpipeline.transition_receipt_digest_mismatch"
	CodeTransitionReceiptBytesMismatch    = "resultpipeline.transition_receipt_bytes_mismatch"
	CodeTransitionProducerSummaryMismatch = "resultpipeline.transition_producer_summary_mismatch"
	CodeTransitionImpactMismatch          = "resultpipeline.transition_impact_mismatch"
)

// PrepareTransitionRequest asks for a validated, deterministic transition
// candidate. RecordedAt is the explicit recording-time assertion — there is no
// internal clock.
type PrepareTransitionRequest struct {
	Build BuildRequest

	ExpectedLedgerHeadDigestSHA256 string
	RecordedAt                     string
}

// TransitionCandidate is a prepared, independently validated ResultTransitionReceipt
// and its deterministic bytes. It is never stored by this package.
type TransitionCandidate struct {
	BuildResult             BuildResult
	BuildResultDigestSHA256 string

	ExpectedLedgerHeadDigestSHA256 string

	Receipt closureprotocol.ResultTransitionReceipt

	ReceiptBytes            []byte
	ReceiptMediaType        string
	ReceiptByteDigestSHA256 string
}

// PrepareTransition builds the result twice deterministically, constructs the
// candidate receipt from that exact result, renders its canonical bytes, and
// validates it independently. It stores nothing and appends nothing.
func PrepareTransition(ctx context.Context, req PrepareTransitionRequest) (TransitionCandidate, error) {
	return prepareTransition(ctx, req, productionDeps())
}

func prepareTransition(ctx context.Context, req PrepareTransitionRequest, deps preparationDeps) (TransitionCandidate, error) {
	if req.Build.ResultMode != resulttransition.ResultModeRevision {
		return TransitionCandidate{}, &ValidationError{Code: CodeTransitionRequiresCommittedResult, Detail: "transition preparation requires a committed revision result"}
	}
	if !isCanonicalUTCRFC3339(req.RecordedAt) {
		return TransitionCandidate{}, &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "recorded_at must be canonical UTC RFC3339 (e.g. 2026-07-17T14:30:00Z)"}
	}

	db, err := buildDeterministically(ctx, DeterministicBuildRequest{
		BuildRequest: req.Build, ExpectedLedgerHeadDigestSHA256: req.ExpectedLedgerHeadDigestSHA256,
	}, deps)
	if err != nil {
		return TransitionCandidate{}, err
	}

	receipt := buildCandidateReceipt(db.Result, req.RecordedAt)
	digest, err := closureprotocol.ResultTransitionReceiptDigest(receipt)
	if err != nil {
		return TransitionCandidate{}, err
	}
	receipt.ReceiptDigestSHA256 = digest

	bytes, err := closureprotocol.MarshalCanonicalResultTransitionReceipt(receipt)
	if err != nil {
		return TransitionCandidate{}, err
	}

	candidate := TransitionCandidate{
		BuildResult:                    db.Result,
		BuildResultDigestSHA256:        db.BuildResultDigestSHA256,
		ExpectedLedgerHeadDigestSHA256: db.LedgerHeadDigestSHA256,
		Receipt:                        receipt,
		ReceiptBytes:                   bytes,
		ReceiptMediaType:               closureprotocol.ResultTransitionReceiptMediaType,
		ReceiptByteDigestSHA256:        sha256hex(bytes),
	}
	if err := closureprotocol.ValidateResultTransitionReceipt(receipt); err != nil {
		return TransitionCandidate{}, &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: err.Error()}
	}
	if err := ValidateTransitionCandidate(candidate); err != nil {
		return TransitionCandidate{}, err
	}
	// Final live-head check before returning the candidate.
	if err := requireHead(deps, strings.TrimSpace(req.Build.TaskDirectory), db.LedgerHeadDigestSHA256, CodeLedgerChangedDuringPreparation); err != nil {
		return TransitionCandidate{}, err
	}
	return candidate, nil
}

// buildCandidateReceipt constructs the receipt fields exactly from the
// deterministic build result. Every field is copied, never re-derived from a
// second source.
func buildCandidateReceipt(result BuildResult, recordedAt string) closureprotocol.ResultTransitionReceipt {
	b := result.BoundRepositoryResult
	receipt := closureprotocol.ResultTransitionReceipt{
		TransitionID:                      transitionID(result),
		Task:                              b.Task,
		BaseBindingDigestSHA256:           b.BaseBindingDigestSHA256,
		ActorBindingDigestSHA256:          b.ActorBindingDigestSHA256,
		AuthorityResolutionDigestSHA256:   b.AuthorityResolutionDigestSHA256,
		AdmissionDecisionDigestSHA256:     b.AdmissionDecisionDigestSHA256,
		CapabilityConsumptionDigestSHA256: b.CapabilityConsumptionDigestSHA256,
		ObservedChangeSetDigestSHA256:     b.ObservedChangeSetDigestSHA256,
		ScopeVerificationDigestSHA256:     b.ScopeVerificationDigestSHA256,
		ResultBinding:                     result.ResultBinding,
		ResultBindingDigestSHA256:         result.ResultBindingDigestSHA256,
		PipelinePolicyID:                  result.PipelinePolicyID,
		Limitations:                       cleanStringsSorted(result.Limitations),
		RecordedAt:                        recordedAt,
		Status:                            closureprotocol.ReceiptValid,
	}
	// Exactly one receipt and one derivation per canonical stage, in order.
	byStage := map[closureprotocol.ResultPipelineStage]PipelineArtifact{}
	for _, a := range result.StageArtifacts {
		byStage[a.Stage] = a
	}
	for _, stage := range closureprotocol.ResultPipelineStages {
		a := byStage[stage]
		receipt.OperationalArtifactReceipts = append(receipt.OperationalArtifactReceipts, a.Receipt)
		receipt.Derivations = append(receipt.Derivations, a.Derivation)
	}
	receipt.GovernedKnowledgeImpacts = append([]closureprotocol.GovernedKnowledgeImpact(nil), result.GovernedKnowledgeImpactReport.Impacts...)
	receipt.PipelineProducerVersions = producerVersions(result)
	return receipt
}

// transitionID names the logical result transition: task + session + result
// binding + pipeline policy, domain-separated. It excludes the recording time, so
// the same logical transition recorded at different times keeps one transition id
// but produces different receipt digests.
func transitionID(result BuildResult) string {
	b := result.BoundRepositoryResult
	d := closureprotocol.MustSemanticDigest(struct {
		Domain           string `json:"domain"`
		TaskID           string `json:"task_id"`
		SessionID        string `json:"session_id"`
		ResultBinding    string `json:"result_binding_digest"`
		PipelinePolicyID string `json:"pipeline_policy_id"`
	}{
		Domain:           "sensei.result-transition/v1",
		TaskID:           b.Task.ID,
		SessionID:        b.Task.SessionID,
		ResultBinding:    result.ResultBindingDigestSHA256,
		PipelinePolicyID: result.PipelinePolicyID,
	})
	return "result-transition." + d
}

// producerVersions is the sorted, duplicate-free set of the ten stage receipts'
// producers. No producer is omitted; none absent from the stages is added.
func producerVersions(result BuildResult) []closureprotocol.ProducerVersion {
	seen := map[string]closureprotocol.ProducerVersion{}
	for _, a := range result.StageArtifacts {
		key := a.Receipt.Producer.ID + "\x00" + a.Receipt.Producer.Version
		seen[key] = closureprotocol.ProducerVersion{Producer: a.Receipt.Producer.ID, Version: a.Receipt.Producer.Version}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]closureprotocol.ProducerVersion, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

func cleanStringsSorted(in []string) []string {
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

func isCanonicalUTCRFC3339(s string) bool {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return false
	}
	// Canonical: UTC ("Z"), and the exact string round-trips.
	return t.Location() == time.UTC && t.UTC().Format(time.RFC3339) == s
}
