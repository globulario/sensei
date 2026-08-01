// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func ComposeRequest(in ComposeInput) (Request, *admission.Request, error) {
	if err := validateChain(in); err != nil {
		return Request{}, nil, err
	}
	scope, unsupported, err := deriveScope(in.BaseManifest, in.CandidateArtifact.Manifest)
	if err != nil {
		return Request{}, nil, err
	}
	r := NormalizeRequest(Request{
		SchemaVersion:                 RequestSchemaVersion,
		RequestID:                     "o5a." + in.CandidateArtifact.CandidateArtifactDigestSHA256[:12] + "." + in.SynthesisReceipt.ReceiptDigestSHA256[:12],
		GeneratedBy:                   GeneratedBy,
		SynthesisReceiptDigestSHA256:  in.SynthesisReceipt.ReceiptDigestSHA256,
		RunnerReceiptDigestSHA256:     in.RunnerReceipt.RunnerReceiptDigestSHA256,
		EvaluationReceiptDigestSHA256: in.EvaluationReceipt.ReceiptDigestSHA256,
		CandidateArtifactDigestSHA256: in.CandidateArtifact.CandidateArtifactDigestSHA256,
		RepositoryDomain:              in.CandidateArtifact.RepositoryDomain,
		BaseRevision:                  in.CandidateArtifact.BaseRevision,
		DerivedScope:                  scope,
		UnsupportedOperations:         unsupported,
	})

	var concrete *admission.Request
	if len(unsupported) == 0 && len(scope.Files) > 0 {
		candidate := in.AdmissionTemplate
		candidate.SchemaVersion = admission.SchemaVersion
		candidate.Mode = admission.ModeModify
		candidate.Scope = scope
		normalized, err := admission.NormalizeRequest(candidate)
		if err != nil {
			return Request{}, nil, fmt.Errorf("admissioncomposition: normalize admission request: %w", err)
		}
		if normalized.Binding.RepositoryDomain != r.RepositoryDomain || normalized.Binding.Revision != r.BaseRevision {
			return Request{}, nil, errors.New("admissioncomposition: admission binding does not match candidate repository/base")
		}
		documentDigest, err := closureprotocol.SemanticDigest(normalized)
		if err != nil {
			return Request{}, nil, err
		}
		identityDigest, err := admissionRequestIdentityDigest(normalized)
		if err != nil {
			return Request{}, nil, err
		}
		r.AdmissionEligible = true
		r.AdmissionRequestDigestSHA256 = &documentDigest
		r.AdmissionRequestIdentityDigestSHA256 = &identityDigest
		concrete = &normalized
	}

	digest, err := RequestDigest(r)
	if err != nil {
		return Request{}, nil, err
	}
	r.RequestDigestSHA256 = digest
	if err := ValidateRequest(r); err != nil {
		return Request{}, nil, err
	}
	return r, concrete, nil
}

func ComposeUnsupportedReceipt(req Request, completedAt string) (Receipt, error) {
	if err := ValidateRequest(req); err != nil {
		return Receipt{}, err
	}
	if req.AdmissionEligible {
		return Receipt{}, errors.New("admissioncomposition: eligible request cannot produce unsupported-operation receipt")
	}
	r := Receipt{
		SchemaVersion:                 ReceiptSchemaVersion,
		ReceiptID:                     "o5a-receipt." + req.RequestDigestSHA256[:16],
		GeneratedBy:                   GeneratedBy,
		RequestDigestSHA256:           req.RequestDigestSHA256,
		SynthesisReceiptDigestSHA256:  req.SynthesisReceiptDigestSHA256,
		CandidateArtifactDigestSHA256: req.CandidateArtifactDigestSHA256,
		Disposition:                   DispositionUnsupportedOperationRefused,
		Detail:                        unsupportedDetail(req.UnsupportedOperations),
		CompletedAt:                   completedAt,
	}
	return finalizeReceipt(r)
}

func ComposeDecisionReceipt(req Request, concrete admission.Request, decision admission.Decision, completedAt string) (Receipt, error) {
	if err := ValidateRequest(req); err != nil {
		return Receipt{}, err
	}
	if !req.AdmissionEligible || req.AdmissionRequestIdentityDigestSHA256 == nil {
		return Receipt{}, errors.New("admissioncomposition: request is not eligible for admission")
	}
	normalized, err := admission.NormalizeRequest(concrete)
	if err != nil {
		return Receipt{}, err
	}
	identity, err := admissionRequestIdentityDigest(normalized)
	if err != nil {
		return Receipt{}, err
	}
	if identity != *req.AdmissionRequestIdentityDigestSHA256 || !reflect.DeepEqual(normalized.Scope, req.DerivedScope) {
		return Receipt{}, errors.New("admissioncomposition: admission request does not match composed request")
	}
	canonicalDecision, err := validateDecision(decision)
	if err != nil {
		return Receipt{}, err
	}
	if canonicalDecision.RequestReceipt.DigestSHA256 != identity || !reflect.DeepEqual(canonicalDecision.RequestReceipt.Scope, normalized.Scope) || canonicalDecision.RequestedMode != admission.ModeModify {
		return Receipt{}, errors.New("admissioncomposition: decision is not bound to the composed admission request")
	}
	decisionValue := canonicalDecision.Decision
	decisionDigest := canonicalDecision.DecisionDigestSHA256
	return finalizeReceipt(Receipt{
		SchemaVersion:                 ReceiptSchemaVersion,
		ReceiptID:                     "o5a-receipt." + req.RequestDigestSHA256[:16],
		GeneratedBy:                   GeneratedBy,
		RequestDigestSHA256:           req.RequestDigestSHA256,
		SynthesisReceiptDigestSHA256:  req.SynthesisReceiptDigestSHA256,
		CandidateArtifactDigestSHA256: req.CandidateArtifactDigestSHA256,
		AdmissionDecision:             &decisionValue,
		AdmissionDecisionDigestSHA256: &decisionDigest,
		Disposition:                   DispositionAdmissionDecided,
		CompletedAt:                   completedAt,
	})
}

func AttachVerification(receipt Receipt, decision admission.Decision, verification admission.Verification, completedAt string) (Receipt, error) {
	if err := ValidateReceipt(receipt); err != nil {
		return Receipt{}, err
	}
	if receipt.Disposition != DispositionAdmissionDecided || receipt.AdmissionDecisionDigestSHA256 == nil {
		return Receipt{}, errors.New("admissioncomposition: verification requires an admission-decided receipt")
	}
	canonicalDecision, err := validateDecision(decision)
	if err != nil {
		return Receipt{}, err
	}
	if canonicalDecision.DecisionDigestSHA256 != *receipt.AdmissionDecisionDigestSHA256 {
		return Receipt{}, errors.New("admissioncomposition: decision digest changed before verification")
	}
	canonicalVerification, err := validateVerification(verification)
	if err != nil {
		return Receipt{}, err
	}
	if canonicalVerification.AdmissionID != canonicalDecision.AdmissionID || canonicalVerification.DecisionDigestSHA256 != canonicalDecision.DecisionDigestSHA256 || !reflect.DeepEqual(canonicalVerification.Binding, canonicalDecision.Binding) {
		return Receipt{}, errors.New("admissioncomposition: verification is not bound to the admitted decision")
	}
	status := canonicalVerification.Status
	verificationDigest := canonicalVerification.VerificationDigestSHA256
	receipt.AdmissionVerificationStatus = &status
	receipt.AdmissionVerificationDigestSHA256 = &verificationDigest
	receipt.Disposition = DispositionVerificationRecorded
	receipt.CompletedAt = completedAt
	return finalizeReceipt(receipt)
}

func finalizeReceipt(r Receipt) (Receipt, error) {
	digest, err := ReceiptDigest(r)
	if err != nil {
		return Receipt{}, err
	}
	r.ReceiptDigestSHA256 = digest
	return r, ValidateReceipt(r)
}
