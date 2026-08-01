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
//  4. no FailureClassRecommendation naming a GovernedFailureClass downgrades
//     below its canonical minimum recommendation
//     (GovernedFailureClassMinimumRecommendationFor) -- e.g. a policy
//     cannot map audit-forbidden-fix (minimum abort) to retry-generation;
//     equal-severity and escalating (more severe) assignments are legal;
//  5. the declared PolicyDigestSHA256 equals a fresh recomputation.
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

		if minimum, ok := GovernedFailureClassMinimumRecommendationFor(rule.FailureClass); ok {
			if recommendationSeverityRank(rule.Recommendation) > recommendationSeverityRank(minimum) {
				return fmt.Errorf("failure_class %q maps to %q, which downgrades below its governed minimum recommendation %q", rule.FailureClass, rule.Recommendation, minimum)
			}
		}
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
// EvaluatorResult, running, in order:
//
//  1. marshal r and validate it against the embedded closed schema
//     (ValidateEvaluatorResultSchema);
//  2. every Checks[i].CheckID is unique -- a result reporting the same
//     check twice, possibly with conflicting Status, is self-contradictory;
//  3. every check-level EvidenceReferences entry resolves to some top-level
//     EvidenceReferences[j].Reference -- a check may not cite evidence this
//     result never actually captured;
//  4. every top-level EvidenceReferences[i].Reference is unique -- a
//     repeated Reference is rejected whether or not its DigestSHA256 also
//     conflicts, since either shape corrupts the "one entry per reference"
//     invariant the rest of this validation (and check-level resolution)
//     depends on;
//  5. every ClassifiedFailureReasons entry is non-empty and unique;
//  6. the declared ResultDigestSHA256 equals a fresh recomputation.
//
// TerminalOutcome-conditional presence rules (e.g. Checks empty when not
// completed) are not enforced here -- checkpoint 2 fixes only the
// document's shape; real conditional semantics arrive at checkpoint 4.
func ValidateEvaluatorResult(r EvaluatorResult) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := ValidateEvaluatorResultSchema(data); err != nil {
		return fmt.Errorf("schema: %w", err)
	}

	seenChecks := make(map[string]bool, len(r.Checks))
	for _, c := range r.Checks {
		if seenChecks[c.CheckID] {
			return fmt.Errorf("check_id %q appears more than once in checks", c.CheckID)
		}
		seenChecks[c.CheckID] = true
	}

	topLevelDigestByReference := make(map[string]string, len(r.EvidenceReferences))
	for _, ref := range r.EvidenceReferences {
		if existing, ok := topLevelDigestByReference[ref.Reference]; ok {
			if existing != ref.DigestSHA256 {
				return fmt.Errorf("evidence_references: reference %q appears more than once with conflicting digests %q and %q", ref.Reference, existing, ref.DigestSHA256)
			}
			return fmt.Errorf("evidence_references: reference %q appears more than once", ref.Reference)
		}
		topLevelDigestByReference[ref.Reference] = ref.DigestSHA256
	}

	for _, c := range r.Checks {
		for _, ref := range c.EvidenceReferences {
			if _, ok := topLevelDigestByReference[ref]; !ok {
				return fmt.Errorf("check %q cites evidence reference %q, which does not resolve to any top-level evidence_references entry", c.CheckID, ref)
			}
		}
	}

	seenFailureReasons := make(map[string]bool, len(r.ClassifiedFailureReasons))
	for _, reason := range r.ClassifiedFailureReasons {
		if reason == "" {
			return fmt.Errorf("classified_failure_reasons must not contain an empty entry")
		}
		if seenFailureReasons[reason] {
			return fmt.Errorf("classified_failure_reasons entry %q appears more than once", reason)
		}
		seenFailureReasons[reason] = true
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
//     CandidateArtifactVerified/EvaluatorResultBindings's emptiness/
//     EvaluationDigestSHA256's presence match FieldPresenceFor(Disposition)
//     exactly (the design doc's disposition/evidence-presence matrix);
//  3. every EvaluatorResultBindings[i].EvaluatorID is unique, and the slice
//     is sorted in strictly ascending EvaluatorID order -- the design doc's
//     "evaluator results ordered by evaluator ID" canonical-ordering
//     requirement (hard law 18), enforced here rather than left to a
//     silent normalization reorder;
//  4. O1TerminalReceiptDigestSHA256's presence matches
//     O1TerminalReceiptRequirementFor(Disposition) -- required for every
//     disposition except DispositionEvaluated, which is ambiguous;
//  5. FailureDetail's presence rule (non-empty unless evaluated);
//  6. CleanupSucceeded's presence rule (nil for the two dispositions that
//     never construct an evaluator surface, non-nil otherwise);
//     CleanupFailureDetail's presence rule (non-empty iff CleanupSucceeded
//     is false);
//  7. the declared ReceiptDigestSHA256 equals a fresh recomputation.
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
	if want.EvaluatorResultBindingsMustBeEmpty && len(r.EvaluatorResultBindings) != 0 {
		return fmt.Errorf("evaluator_result_bindings must be empty for disposition %q", r.Disposition)
	}
	seenEvaluatorIDs := make(map[string]bool, len(r.EvaluatorResultBindings))
	for i, b := range r.EvaluatorResultBindings {
		if seenEvaluatorIDs[b.EvaluatorID] {
			return fmt.Errorf("evaluator_result_bindings: evaluator_id %q appears more than once", b.EvaluatorID)
		}
		seenEvaluatorIDs[b.EvaluatorID] = true
		if i > 0 && r.EvaluatorResultBindings[i-1].EvaluatorID >= b.EvaluatorID {
			return fmt.Errorf("evaluator_result_bindings must be sorted in strictly ascending evaluator_id order: %q does not follow %q", b.EvaluatorID, r.EvaluatorResultBindings[i-1].EvaluatorID)
		}
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
