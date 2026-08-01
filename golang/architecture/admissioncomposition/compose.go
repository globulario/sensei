// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func ComposeRequest(in ComposeInput) (Request, *admission.Request, error) {
	if err := validateChain(in); err != nil {
		return Request{}, nil, err
	}
	derived, unsupported, err := deriveScope(in.BaseManifest, in.CandidateArtifact.Manifest)
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
		DerivedScope:                  derived,
		UnsupportedOperations:         unsupported,
	})

	var concrete *admission.Request
	if len(unsupported) == 0 && len(derived.Files) > 0 {
		candidate := in.AdmissionTemplate
		candidate.SchemaVersion = admission.SchemaVersion
		candidate.Mode = admission.ModeModify
		candidate.Scope = derived
		normalized, err := admission.NormalizeRequest(candidate)
		if err != nil {
			return Request{}, nil, fmt.Errorf("admissioncomposition: normalize admission request: %w", err)
		}
		if normalized.Binding.RepositoryDomain != in.CandidateArtifact.RepositoryDomain || normalized.Binding.Revision != in.CandidateArtifact.BaseRevision {
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
	digest, err := ReceiptDigest(r)
	if err != nil {
		return Receipt{}, err
	}
	r.ReceiptDigestSHA256 = digest
	return r, ValidateReceipt(r)
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
	r := Receipt{
		SchemaVersion:                    ReceiptSchemaVersion,
		ReceiptID:                        "o5a-receipt." + req.RequestDigestSHA256[:16],
		GeneratedBy:                      GeneratedBy,
		RequestDigestSHA256:              req.RequestDigestSHA256,
		SynthesisReceiptDigestSHA256:     req.SynthesisReceiptDigestSHA256,
		CandidateArtifactDigestSHA256:    req.CandidateArtifactDigestSHA256,
		AdmissionDecision:                &decisionValue,
		AdmissionDecisionDigestSHA256:    &decisionDigest,
		Disposition:                      DispositionAdmissionDecided,
		CompletedAt:                      completedAt,
	}
	digest, err := ReceiptDigest(r)
	if err != nil {
		return Receipt{}, err
	}
	r.ReceiptDigestSHA256 = digest
	return r, ValidateReceipt(r)
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
	digest, err := ReceiptDigest(receipt)
	if err != nil {
		return Receipt{}, err
	}
	receipt.ReceiptDigestSHA256 = digest
	return receipt, ValidateReceipt(receipt)
}

func validateChain(in ComposeInput) error {
	receiptBytes, err := json.Marshal(in.SynthesisReceipt)
	if err != nil {
		return err
	}
	if err := synthesis.ValidateReceiptSchema(receiptBytes); err != nil {
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
	if in.EvaluationReceipt.RequestDigestSHA256 != in.RunnerReceipt.RequestDigestSHA256 || in.RunnerReceipt.ResultDigestSHA256 == nil || in.RunnerReceipt.O2ReceiptDigestSHA256 == nil || in.EvaluationReceipt.ResultDigestSHA256 != *in.RunnerReceipt.ResultDigestSHA256 || in.EvaluationReceipt.O2ReceiptDigestSHA256 != *in.RunnerReceipt.O2ReceiptDigestSHA256 {
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

func deriveScope(base, final []runnercomposition.CandidateManifestEntry) (admission.ChangeScope, []UnsupportedOperation, error) {
	baseCanonical, err := runnercomposition.CanonicalizeManifest(base)
	if err != nil {
		return admission.ChangeScope{}, nil, err
	}
	finalCanonical, err := runnercomposition.CanonicalizeManifest(final)
	if err != nil {
		return admission.ChangeScope{}, nil, err
	}
	baseByPath := map[string]runnercomposition.CandidateManifestEntry{}
	finalByPath := map[string]runnercomposition.CandidateManifestEntry{}
	paths := map[string]bool{}
	for _, entry := range baseCanonical {
		baseByPath[entry.Path] = entry
		paths[entry.Path] = true
	}
	for _, entry := range finalCanonical {
		finalByPath[entry.Path] = entry
		paths[entry.Path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	scope := admission.ChangeScope{
		Files:           []admission.FileOperation{},
		Symbols:         []string{},
		Components:      []string{},
		ClaimIDs:        []string{},
		PropositionKeys: []string{},
	}
	unsupported := []UnsupportedOperation{}
	for _, path := range ordered {
		before, hadBefore := baseByPath[path]
		after, hasAfter := finalByPath[path]
		switch {
		case hadBefore && !hasAfter:
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: admission.ChangeDeleted, Detail: "existing admission supports read/modify only"})
		case !hadBefore && hasAfter:
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: admission.ChangeAdded, Detail: "existing admission supports read/modify only"})
		case before.Mode != after.Mode:
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: admission.ChangeTypeChanged, Detail: "candidate changed the governed file mode/type"})
		case before.Mode == runnercomposition.ModeSymlink && (before.ContentDigestSHA256 != after.ContentDigestSHA256 || before.SymlinkTarget != after.SymlinkTarget):
			unsupported = append(unsupported, UnsupportedOperation{Path: path, Operation: "symlink_changed", Detail: "symlink mutation is outside the current admission operation vocabulary"})
		case before.ContentDigestSHA256 != after.ContentDigestSHA256 || !bytes.Equal(before.Content, after.Content):
			scope.Files = append(scope.Files, admission.FileOperation{Path: path, Operation: admission.OperationModify})
		}
	}
	return scope, unsupported, nil
}

func admissionRequestIdentityDigest(req admission.Request) (string, error) {
	req.RequestedBy = ""
	req.Note = ""
	data, err := admission.MarshalCanonicalRequestYAML(req)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func validateDecision(in admission.Decision) (admission.Decision, error) {
	data, err := admission.MarshalCanonicalDecisionJSON(in)
	if err != nil {
		return admission.Decision{}, err
	}
	var env struct {
		ArchitectureAdmissionDecision admission.Decision `json:"architecture_admission_decision"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return admission.Decision{}, err
	}
	if env.ArchitectureAdmissionDecision.DecisionDigestSHA256 != in.DecisionDigestSHA256 {
		return admission.Decision{}, errors.New("admissioncomposition: admission decision digest invalid")
	}
	if env.ArchitectureAdmissionDecision.CorrectnessCertified || !env.ArchitectureAdmissionDecision.ScopeOnly {
		return admission.Decision{}, errors.New("admissioncomposition: admission decision exceeds scope-only authority")
	}
	return env.ArchitectureAdmissionDecision, nil
}

func validateVerification(in admission.Verification) (admission.Verification, error) {
	data, err := admission.MarshalCanonicalVerificationJSON(in)
	if err != nil {
		return admission.Verification{}, err
	}
	var env struct {
		ArchitectureAdmissionVerification admission.Verification `json:"architecture_admission_verification"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return admission.Verification{}, err
	}
	if env.ArchitectureAdmissionVerification.VerificationDigestSHA256 != in.VerificationDigestSHA256 {
		return admission.Verification{}, errors.New("admissioncomposition: admission verification digest invalid")
	}
	if env.ArchitectureAdmissionVerification.CorrectnessCertified || !env.ArchitectureAdmissionVerification.ScopeOnly {
		return admission.Verification{}, errors.New("admissioncomposition: admission verification exceeds scope-only authority")
	}
	return env.ArchitectureAdmissionVerification, nil
}

func unsupportedDetail(ops []UnsupportedOperation) string {
	if len(ops) == 0 {
		return "candidate produced no supported modify operation"
	}
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		parts = append(parts, op.Operation+":"+op.Path)
	}
	return "unsupported candidate operations: " + strings.Join(parts, ",")
}
