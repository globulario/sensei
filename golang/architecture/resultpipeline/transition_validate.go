// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"bytes"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ValidateTransitionCandidate re-derives everything in a candidate and rejects any
// inconsistency. It is pure: it never reads the ledger (PrepareTransition performs
// the live-head checks). It enforces the stronger candidate-completeness boundary
// that the generic historical receipt validator deliberately does not.
func ValidateTransitionCandidate(c TransitionCandidate) error {
	// Build.
	if err := ValidateBuildResult(c.BuildResult); err != nil {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "build: " + err.Error()}
	}
	digest, err := BuildResultDigest(c.BuildResult)
	if err != nil {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: err.Error()}
	}
	if digest != c.BuildResultDigestSHA256 {
		return &ValidationError{Code: CodeBuildResultDigestMismatch, Detail: "candidate build digest does not recompute"}
	}
	if c.BuildResult.ResultBinding.ResultRevision == "" {
		return &ValidationError{Code: CodeTransitionRequiresCommittedResult, Detail: "candidate build is not revision-bound"}
	}

	// Ledger anchor (value only; no ledger read here).
	if !isHex64(c.ExpectedLedgerHeadDigestSHA256) {
		return &ValidationError{Code: CodeExpectedLedgerHeadInvalid, Detail: "candidate expected ledger head is not a 64-hex sha256"}
	}

	// Receipt.
	r := c.Receipt
	if err := closureprotocol.ValidateResultTransitionReceipt(r); err != nil {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "receipt: " + err.Error()}
	}
	wantDigest, err := closureprotocol.ResultTransitionReceiptDigest(r)
	if err != nil {
		return err
	}
	if r.ReceiptDigestSHA256 == "" || r.ReceiptDigestSHA256 != wantDigest {
		return &ValidationError{Code: CodeTransitionReceiptDigestMismatch, Detail: "receipt self-digest does not recompute"}
	}
	wantBytes, err := closureprotocol.MarshalCanonicalResultTransitionReceipt(r)
	if err != nil {
		return err
	}
	if !bytes.Equal(wantBytes, c.ReceiptBytes) {
		return &ValidationError{Code: CodeTransitionReceiptBytesMismatch, Detail: "canonical receipt bytes do not re-render"}
	}
	if c.ReceiptByteDigestSHA256 != sha256hex(c.ReceiptBytes) {
		return &ValidationError{Code: CodeTransitionReceiptBytesMismatch, Detail: "receipt byte digest does not recompute"}
	}
	if c.ReceiptMediaType != closureprotocol.ResultTransitionReceiptMediaType {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "receipt media type is not canonical"}
	}

	// Exact correspondence to the build result.
	if err := receiptMatchesBuild(r, c.BuildResult); err != nil {
		return err
	}

	// Stronger candidate completeness (not weakening the generic validator).
	if r.TransitionID == "" {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "candidate requires a transition id"}
	}
	if len(r.OperationalArtifactReceipts) != len(closureprotocol.ResultPipelineStages) ||
		len(r.Derivations) != len(closureprotocol.ResultPipelineStages) {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "candidate requires all ten operational receipts and derivations"}
	}
	if len(r.GovernedKnowledgeImpacts) != len(closureprotocol.GovernedKnowledgeCategories()) {
		return &ValidationError{Code: CodeTransitionImpactMismatch, Detail: "candidate requires all ten governed-knowledge impacts"}
	}
	if r.Status != closureprotocol.ReceiptValid {
		return &ValidationError{Code: CodeTransitionCandidateInvalid, Detail: "candidate status must be valid"}
	}
	return nil
}

func receiptMatchesBuild(r closureprotocol.ResultTransitionReceipt, result BuildResult) error {
	b := result.BoundRepositoryResult
	if r.Task != b.Task {
		return &ValidationError{Code: CodeTransitionReceiptMismatch, Detail: "receipt task differs from the build"}
	}
	for name, pair := range map[string][2]string{
		"base_binding":           {r.BaseBindingDigestSHA256, b.BaseBindingDigestSHA256},
		"actor_binding":          {r.ActorBindingDigestSHA256, b.ActorBindingDigestSHA256},
		"authority_resolution":   {r.AuthorityResolutionDigestSHA256, b.AuthorityResolutionDigestSHA256},
		"admission_decision":     {r.AdmissionDecisionDigestSHA256, b.AdmissionDecisionDigestSHA256},
		"capability_consumption": {r.CapabilityConsumptionDigestSHA256, b.CapabilityConsumptionDigestSHA256},
		"observed_change":        {r.ObservedChangeSetDigestSHA256, b.ObservedChangeSetDigestSHA256},
		"scope_verification":     {r.ScopeVerificationDigestSHA256, b.ScopeVerificationDigestSHA256},
		"result_binding_digest":  {r.ResultBindingDigestSHA256, result.ResultBindingDigestSHA256},
		"pipeline_policy":        {r.PipelinePolicyID, result.PipelinePolicyID},
	} {
		if pair[0] != pair[1] {
			return &ValidationError{Code: CodeTransitionReceiptMismatch, Detail: "receipt " + name + " differs from the build"}
		}
	}
	if closureprotocol.MustSemanticDigest(r.ResultBinding) != closureprotocol.MustSemanticDigest(result.ResultBinding) {
		return &ValidationError{Code: CodeTransitionReceiptMismatch, Detail: "receipt result binding differs from the build"}
	}
	// Artifact receipts and derivations, exactly one per stage in canonical order.
	byStage := map[closureprotocol.ResultPipelineStage]PipelineArtifact{}
	for _, a := range result.StageArtifacts {
		byStage[a.Stage] = a
	}
	for i, stage := range closureprotocol.ResultPipelineStages {
		a := byStage[stage]
		if closureprotocol.MustSemanticDigest(r.OperationalArtifactReceipts[i]) != closureprotocol.MustSemanticDigest(a.Receipt) {
			return &ValidationError{Code: CodeTransitionReceiptMismatch, Detail: "receipt artifact for " + string(stage) + " differs from the build"}
		}
		if closureprotocol.MustSemanticDigest(r.Derivations[i]) != closureprotocol.MustSemanticDigest(a.Derivation) {
			return &ValidationError{Code: CodeTransitionReceiptMismatch, Detail: "receipt derivation for " + string(stage) + " differs from the build"}
		}
	}
	// Governed impacts, exactly the validated report.
	if closureprotocol.MustSemanticDigest(r.GovernedKnowledgeImpacts) != closureprotocol.MustSemanticDigest(result.GovernedKnowledgeImpactReport.Impacts) {
		return &ValidationError{Code: CodeTransitionImpactMismatch, Detail: "receipt impacts differ from the validated impact report"}
	}
	// Producer summary, exact set equality with the stage receipts.
	if closureprotocol.MustSemanticDigest(r.PipelineProducerVersions) != closureprotocol.MustSemanticDigest(producerVersions(result)) {
		return &ValidationError{Code: CodeTransitionProducerSummaryMismatch, Detail: "receipt producer versions differ from the stage receipts"}
	}
	return nil
}
