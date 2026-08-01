// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func validateChain(in ComposeInput) error {
	data, err := json.Marshal(in.SynthesisReceipt)
	if err != nil {
		return err
	}
	if err := synthesis.ValidateReceiptSchema(data); err != nil {
		return fmt.Errorf("admissioncomposition: O1 receipt schema: %w", err)
	}
	wantO1, err := synthesis.ReceiptDigest(in.SynthesisReceipt)
	if err != nil || wantO1 != in.SynthesisReceipt.ReceiptDigestSHA256 {
		return errors.New("admissioncomposition: O1 receipt digest invalid")
	}
	if in.SynthesisReceipt.TerminalReason != synthesis.ReasonCandidateReadyForAdmission || in.SynthesisReceipt.FinalAttemptDigestSHA256 == nil || in.SynthesisReceipt.FinalEvaluationDigestSHA256 == nil {
		return errors.New("admissioncomposition: O1 receipt is not candidate-ready")
	}
	if in.SynthesisReceipt.AdmissionDecisionDigestSHA256 != nil || in.SynthesisReceipt.AdmissionVerificationDigestSHA256 != nil {
		return errors.New("admissioncomposition: O1 receipt must remain frozen before O5 evidence")
	}
	if err := runnercomposition.ValidateRunnerReceipt(in.RunnerReceipt); err != nil {
		return fmt.Errorf("admissioncomposition: O3 receipt: %w", err)
	}
	if in.RunnerReceipt.Disposition != runnercomposition.DispositionVerified || in.RunnerReceipt.CandidateArtifactDigestSHA256 == nil {
		return errors.New("admissioncomposition: O3 receipt is not verified")
	}
	if err := runnercomposition.ValidateCandidateArtifact(in.CandidateArtifact); err != nil {
		return fmt.Errorf("admissioncomposition: candidate artifact: %w", err)
	}
	if err := evaluatorcomposition.ValidateEvaluationReceipt(in.EvaluationReceipt); err != nil {
		return fmt.Errorf("admissioncomposition: O4 receipt: %w", err)
	}
	if in.EvaluationReceipt.Disposition != evaluatorcomposition.DispositionEvaluated || in.EvaluationReceipt.EvaluationDigestSHA256 == nil || in.EvaluationReceipt.O1TerminalReceiptDigestSHA256 == nil {
		return errors.New("admissioncomposition: O4 did not produce a terminal evaluated candidate")
	}
	if *in.EvaluationReceipt.O1TerminalReceiptDigestSHA256 != in.SynthesisReceipt.ReceiptDigestSHA256 || *in.EvaluationReceipt.EvaluationDigestSHA256 != *in.SynthesisReceipt.FinalEvaluationDigestSHA256 || in.EvaluationReceipt.AttemptDigestSHA256 != *in.SynthesisReceipt.FinalAttemptDigestSHA256 {
		return errors.New("admissioncomposition: O1/O4 terminal lineage mismatch")
	}
	if in.EvaluationReceipt.RunnerReceiptDigestSHA256 != in.RunnerReceipt.RunnerReceiptDigestSHA256 || in.EvaluationReceipt.CandidateArtifactDigestSHA256 != in.CandidateArtifact.CandidateArtifactDigestSHA256 || *in.RunnerReceipt.CandidateArtifactDigestSHA256 != in.CandidateArtifact.CandidateArtifactDigestSHA256 {
		return errors.New("admissioncomposition: O3/O4 candidate lineage mismatch")
	}
	if in.RunnerReceipt.ResultDigestSHA256 == nil || in.RunnerReceipt.O2ReceiptDigestSHA256 == nil || in.EvaluationReceipt.RequestDigestSHA256 != in.RunnerReceipt.RequestDigestSHA256 || in.EvaluationReceipt.ResultDigestSHA256 != *in.RunnerReceipt.ResultDigestSHA256 || in.EvaluationReceipt.O2ReceiptDigestSHA256 != *in.RunnerReceipt.O2ReceiptDigestSHA256 {
		return errors.New("admissioncomposition: O2/O3/O4 lineage mismatch")
	}
	if in.CandidateArtifact.SessionDigestSHA256 != in.EvaluationReceipt.SessionDigestSHA256 || in.CandidateArtifact.SessionDigestSHA256 != in.SynthesisReceipt.SessionDigestSHA256 {
		return errors.New("admissioncomposition: session lineage mismatch")
	}
	if in.CandidateArtifact.RepositoryDomain != in.AdmissionTemplate.Binding.RepositoryDomain || in.CandidateArtifact.BaseRevision != in.AdmissionTemplate.Binding.Revision {
		return errors.New("admissioncomposition: candidate/admission repository binding mismatch")
	}
	baseDigest, err := runnercomposition.ManifestDigest(in.BaseManifest)
	if err != nil {
		return fmt.Errorf("admissioncomposition: base manifest: %w", err)
	}
	if baseDigest != in.CandidateArtifact.InputCandidateDigestSHA256 || in.RunnerReceipt.InputCandidateDigestSHA256 == nil || *in.RunnerReceipt.InputCandidateDigestSHA256 != baseDigest {
		return errors.New("admissioncomposition: base manifest is not the candidate input")
	}
	if in.RunnerReceipt.ProposedChangeDigestSHA256 == nil || *in.RunnerReceipt.ProposedChangeDigestSHA256 != in.CandidateArtifact.ProposedChangeDigestSHA256 || in.RunnerReceipt.FinalCandidateContentDigestSHA256 == nil || *in.RunnerReceipt.FinalCandidateContentDigestSHA256 != in.CandidateArtifact.FinalCandidateContentDigestSHA256 {
		return errors.New("admissioncomposition: O3 structural evidence mismatch")
	}
	return nil
}
