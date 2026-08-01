// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"encoding/json"
	"fmt"
)

// ValidateEvaluationPolicy is the one canonical acceptance path for an
// EvaluationPolicy, running, in order:
//
//  1. marshal p and validate it against the embedded closed schema
//     (ValidateEvaluationPolicySchema);
//  2. every EvaluatorSpec.EvaluatorID is unique -- a policy naming the same
//     evaluator twice, possibly with contradictory Required values, would
//     make selection non-deterministic;
//  3. every FailureClassRecommendation.FailureClass is unique -- a policy
//     mapping the same failure class to two different recommendations
//     would make composition non-deterministic (hard law 12);
//  4. the declared PolicyDigestSHA256 equals a fresh recomputation.
func ValidateEvaluationPolicy(p EvaluationPolicy) error {
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateEvaluationPolicySchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	seenEvaluators := make(map[string]bool, len(p.Evaluators))
	for _, spec := range p.Evaluators {
		if seenEvaluators[spec.EvaluatorID] {
			return fmt.Errorf("evaluator_id %q appears more than once in evaluators", spec.EvaluatorID)
		}
		seenEvaluators[spec.EvaluatorID] = true
	}

	seenClasses := make(map[string]bool, len(p.FailureClassRecommendations))
	for _, rule := range p.FailureClassRecommendations {
		if seenClasses[rule.FailureClass] {
			return fmt.Errorf("failure_class %q appears more than once in failure_class_recommendations", rule.FailureClass)
		}
		seenClasses[rule.FailureClass] = true
	}

	wantDigest, err := EvaluationPolicyDigest(p)
	if err != nil {
		return fmt.Errorf("policy_digest_sha256: %w", err)
	}
	if p.PolicyDigestSHA256 != wantDigest {
		return fmt.Errorf("policy_digest_sha256 %q does not match recomputed %q", p.PolicyDigestSHA256, wantDigest)
	}
	return nil
}

// ValidateEvaluatorDescriptor is the one canonical acceptance path for an
// EvaluatorDescriptor: schema validation, then declared-versus-recomputed
// digest.
func ValidateEvaluatorDescriptor(d EvaluatorDescriptor) error {
	data, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateEvaluatorDescriptorSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	wantDigest, err := EvaluatorDescriptorDigest(d)
	if err != nil {
		return fmt.Errorf("descriptor_digest_sha256: %w", err)
	}
	if d.DescriptorDigestSHA256 != wantDigest {
		return fmt.Errorf("descriptor_digest_sha256 %q does not match recomputed %q", d.DescriptorDigestSHA256, wantDigest)
	}
	return nil
}

// ValidateEvaluationInput is the one canonical acceptance path for an
// EvaluationInput: schema validation, then declared-versus-recomputed
// digest.
func ValidateEvaluationInput(i EvaluationInput) error {
	data, err := json.Marshal(i)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateEvaluationInputSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	wantDigest, err := EvaluationInputDigest(i)
	if err != nil {
		return fmt.Errorf("evaluation_input_digest_sha256: %w", err)
	}
	if i.EvaluationInputDigestSHA256 != wantDigest {
		return fmt.Errorf("evaluation_input_digest_sha256 %q does not match recomputed %q", i.EvaluationInputDigestSHA256, wantDigest)
	}
	return nil
}

// ValidateEvaluatorResult is the one canonical acceptance path for an
// EvaluatorResult: schema validation, then declared-versus-recomputed
// digest. TerminalOutcome-conditional presence rules (e.g. Checks empty
// when not completed) are not enforced here -- checkpoint 2 fixes only the
// document's shape; real conditional semantics arrive at checkpoint 4.
func ValidateEvaluatorResult(r EvaluatorResult) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateEvaluatorResultSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	wantDigest, err := EvaluatorResultDigest(r)
	if err != nil {
		return fmt.Errorf("result_digest_sha256: %w", err)
	}
	if r.ResultDigestSHA256 != wantDigest {
		return fmt.Errorf("result_digest_sha256 %q does not match recomputed %q", r.ResultDigestSHA256, wantDigest)
	}
	return nil
}

// ValidateEvaluationReceipt is the one canonical acceptance path for an
// EvaluationReceipt, running, in order:
//
//  1. marshal r and validate it against the embedded closed schema
//     (ValidateEvaluationReceiptSchema);
//  2. Disposition is one of the six closed values, and
//     CandidateArtifactVerified/EvaluatorResultDigestsSHA256's emptiness/
//     EvaluationDigestSHA256's presence match FieldPresenceFor(Disposition)
//     exactly (the design doc's disposition/evidence-presence matrix);
//  3. O1TerminalReceiptDigestSHA256's presence matches
//     O1TerminalReceiptRequirementFor(Disposition) -- required for every
//     disposition except DispositionEvaluated, which is ambiguous;
//  4. FailureDetail's presence rule (non-empty unless evaluated);
//  5. CleanupSucceeded's presence rule (nil for the two dispositions that
//     never construct an evaluator surface, non-nil otherwise);
//     CleanupFailureDetail's presence rule (non-empty iff CleanupSucceeded
//     is false);
//  6. the declared ReceiptDigestSHA256 equals a fresh recomputation.
func ValidateEvaluationReceipt(r EvaluationReceipt) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateEvaluationReceiptSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	want, err := FieldPresenceFor(r.Disposition)
	if err != nil {
		return err
	}
	if r.CandidateArtifactVerified != want.CandidateArtifactVerified {
		return fmt.Errorf("candidate_artifact_verified must be %v for disposition %q", want.CandidateArtifactVerified, r.Disposition)
	}
	if want.EvaluatorResultDigestsMustBeEmpty && len(r.EvaluatorResultDigestsSHA256) != 0 {
		return fmt.Errorf("evaluator_result_digests_sha256 must be empty for disposition %q", r.Disposition)
	}
	if want.EvaluationDigest && r.EvaluationDigestSHA256 == nil {
		return fmt.Errorf("evaluation_digest_sha256 must be non-nil for disposition %q", r.Disposition)
	}
	if !want.EvaluationDigest && r.EvaluationDigestSHA256 != nil {
		return fmt.Errorf("evaluation_digest_sha256 must be nil for disposition %q", r.Disposition)
	}

	o1Req, err := O1TerminalReceiptRequirementFor(r.Disposition)
	if err != nil {
		return err
	}
	if o1Req == O1TerminalReceiptRequired && r.O1TerminalReceiptDigestSHA256 == nil {
		return fmt.Errorf("o1_terminal_receipt_digest_sha256 must be non-nil for disposition %q", r.Disposition)
	}

	if r.Disposition == DispositionEvaluated && r.FailureDetail != "" {
		return fmt.Errorf("failure_detail must be empty when disposition is evaluated")
	}
	if r.Disposition != DispositionEvaluated && r.FailureDetail == "" {
		return fmt.Errorf("failure_detail must be non-empty when disposition is %q", r.Disposition)
	}

	if want.CleanupSucceeded {
		if r.CleanupSucceeded == nil {
			return fmt.Errorf("cleanup_succeeded must be non-nil for disposition %q", r.Disposition)
		}
	} else if r.CleanupSucceeded != nil {
		return fmt.Errorf("cleanup_succeeded must be nil for disposition %q -- no evaluator surface was ever constructed", r.Disposition)
	}
	if r.CleanupSucceeded != nil && !*r.CleanupSucceeded && r.CleanupFailureDetail == "" {
		return fmt.Errorf("cleanup_failure_detail must be non-empty when cleanup_succeeded is false")
	}
	if (r.CleanupSucceeded == nil || *r.CleanupSucceeded) && r.CleanupFailureDetail != "" {
		return fmt.Errorf("cleanup_failure_detail must be empty when cleanup_succeeded is nil or true")
	}

	wantDigest, err := EvaluationReceiptDigest(r)
	if err != nil {
		return fmt.Errorf("receipt_digest_sha256: %w", err)
	}
	if r.ReceiptDigestSHA256 != wantDigest {
		return fmt.Errorf("receipt_digest_sha256 %q does not match recomputed %q", r.ReceiptDigestSHA256, wantDigest)
	}
	return nil
}
