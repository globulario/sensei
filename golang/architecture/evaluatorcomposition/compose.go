// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// These policy-owned classes describe deterministic O4 observations that do
// not have a universal severity floor. A caller may map them to any non-accept
// Recommendation in EvaluationPolicy. O4 never supplies a hidden default.
const (
	FailureClassOptionalEvaluatorUnavailable = "optional-evaluator-unavailable"
	FailureClassRequiredCheckUnsatisfied     = "required-check-unsatisfied"
	FailureClassIncompleteObservation        = "incomplete-evaluator-observation"
	FailureClassBlockingLimitation           = "blocking-limitation"
)

const (
	compositionEvaluatorKind    = "sensei-o4-composer"
	compositionEvaluatorVersion = "v1"
)

// EvidenceResolver returns the exact bytes named by one evaluator evidence
// reference. Implementations must verify the reference/digest binding again;
// the composer never trusts a provider-authored digest without loading bytes.
type EvidenceResolver interface {
	Resolve(ctx context.Context, reference EvidenceReference) ([]byte, error)
}

// Composition is the pure checkpoint-5 result. It performs no O1 transition.
// Disposition is evaluated, required-evaluator-unavailable, or
// composition-failure. The latter two carry bounded detail for the already
// contracted EvaluatorUnavailableCommand path.
type Composition struct {
	Disposition          Disposition
	Evaluation           *synthesis.Evaluation
	EvaluatorBindings    []EvaluatorResultBinding
	FailureDetail        string
	CleanupSucceeded     bool
	CleanupFailureDetail string
}

// ComposeEvaluation deterministically composes already-finalized evaluator
// executions. It validates every document and cross-binding again, sorts all
// composition-owned collections, verifies exact ProofDischarge bytes through
// closureprotocol, applies the caller-owned policy and fixed precedence, and
// constructs one O1 Evaluation. It never calls synthesis.Transition.
func ComposeEvaluation(
	ctx context.Context,
	state synthesis.SessionState,
	candidate runnercomposition.CandidateArtifact,
	policy EvaluationPolicy,
	executions []EvaluatorExecution,
	resolver EvidenceResolver,
) Composition {
	base := Composition{Disposition: DispositionCompositionFailure}
	if state.Phase != synthesis.PhaseEvaluating {
		base.FailureDetail = fmt.Sprintf("session phase %q is not %q", state.Phase, synthesis.PhaseEvaluating)
		return base
	}
	if err := runnercomposition.ValidateCandidateArtifact(candidate); err != nil {
		base.FailureDetail = "candidate artifact validation: " + err.Error()
		return base
	}
	if err := ValidateEvaluationPolicy(policy); err != nil {
		base.FailureDetail = "evaluation policy validation: " + err.Error()
		return base
	}
	if policy.SessionDigestSHA256 != state.Session.SessionDigestSHA256 || candidate.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		base.FailureDetail = "session identity mismatch among state, policy, and candidate"
		return base
	}
	if policy.AttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
		base.FailureDetail = fmt.Sprintf("policy attempt digest %q does not match state %q", policy.AttemptDigestSHA256, state.LatestAttemptDigestSHA256)
		return base
	}
	if policy.CandidateArtifactDigestSHA256 != candidate.CandidateArtifactDigestSHA256 {
		base.FailureDetail = fmt.Sprintf("policy candidate digest %q does not match artifact %q", policy.CandidateArtifactDigestSHA256, candidate.CandidateArtifactDigestSHA256)
		return base
	}

	ordered := append([]EvaluatorExecution(nil), executions...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Descriptor.EvaluatorID < ordered[j].Descriptor.EvaluatorID
	})

	policySpecs := make(map[string]EvaluatorSpec, len(policy.Evaluators))
	for _, spec := range policy.Evaluators {
		policySpecs[spec.EvaluatorID] = spec
	}

	executionByID := make(map[string]EvaluatorExecution, len(ordered))
	checkOwner := make(map[string]string)
	bindings := make([]EvaluatorResultBinding, 0, len(ordered))
	checks := make([]synthesis.CheckObservation, 0)
	failureClasses := make([]string, 0)
	limitations := make([]synthesis.Limitation, 0)
	evidence := make([]EvidenceReference, 0)
	referencedEvidence := make(map[string]bool)

	for _, execution := range ordered {
		id := execution.Descriptor.EvaluatorID
		if id == "" {
			base.FailureDetail = "execution carries an empty evaluator ID"
			return withCompositionEvidence(base, bindings, ordered)
		}
		if _, duplicate := executionByID[id]; duplicate {
			base.FailureDetail = fmt.Sprintf("evaluator %q appears more than once", id)
			return withCompositionEvidence(base, bindings, ordered)
		}
		spec, known := policySpecs[id]
		if !known {
			base.FailureDetail = fmt.Sprintf("evaluator %q is not present in the accepted policy", id)
			return withCompositionEvidence(base, bindings, ordered)
		}
		if err := validateExecutionForComposition(execution, state, candidate, policy); err != nil {
			base.FailureDetail = fmt.Sprintf("evaluator %q: %v", id, err)
			return withCompositionEvidence(base, bindings, ordered)
		}
		executionByID[id] = execution
		if execution.Result.CleanupSucceeded == nil {
			base.FailureDetail = fmt.Sprintf("evaluator %q result has no O4-owned cleanup truth", id)
			return withCompositionEvidence(base, bindings, ordered)
		}

		if execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted && spec.Required {
			unavailable := Composition{
				Disposition:       DispositionRequiredEvaluatorUnavailable,
				EvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),
				FailureDetail:     fmt.Sprintf("required evaluator %q ended with terminal outcome %q", id, execution.Result.TerminalOutcome),
			}
			return finalizeCompositionCleanup(unavailable, ordered)
		}

		bindings = append(bindings, EvaluatorResultBinding{
			EvaluatorID:            id,
			DescriptorDigestSHA256: execution.Descriptor.DescriptorDigestSHA256,
			ResultDigestSHA256:     execution.Result.ResultDigestSHA256,
		})
		if execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted {
			failureClasses = append(failureClasses, FailureClassOptionalEvaluatorUnavailable)
			limitations = append(limitations, synthesis.Limitation{
				Source: "evaluatorcomposition", Scope: id,
				Reason:   fmt.Sprintf("optional evaluator ended with terminal outcome %q", execution.Result.TerminalOutcome),
				Blocking: false,
			})
		}

		for _, check := range execution.Result.Checks {
			if owner, duplicate := checkOwner[check.CheckID]; duplicate {
				base.FailureDetail = fmt.Sprintf("duplicate check_id %q is reported by evaluators %q and %q", check.CheckID, owner, id)
				return withCompositionEvidence(base, bindings, ordered)
			}
			checkOwner[check.CheckID] = id
		}
		resultChecks := append([]synthesis.CheckObservation(nil), execution.Result.Checks...)
		sort.Slice(resultChecks, func(i, j int) bool { return resultChecks[i].CheckID < resultChecks[j].CheckID })
		checks = append(checks, resultChecks...)
		failureClasses = append(failureClasses, execution.Result.ClassifiedFailureReasons...)
		limitations = append(limitations, execution.Descriptor.Limitations...)
		limitations = append(limitations, execution.Result.Limitations...)
		evidence = append(evidence, execution.Result.EvidenceReferences...)
		for _, check := range execution.Result.Checks {
			for _, reference := range check.EvidenceReferences {
				referencedEvidence[reference] = true
			}
		}
	}

	for _, spec := range policy.Evaluators {
		if _, ok := executionByID[spec.EvaluatorID]; ok {
			continue
		}
		if spec.Required {
			unavailable := Composition{
				Disposition:       DispositionRequiredEvaluatorUnavailable,
				EvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),
				FailureDetail:     fmt.Sprintf("required evaluator %q produced no finalized result", spec.EvaluatorID),
			}
			return finalizeCompositionCleanup(unavailable, ordered)
		}
		failureClasses = append(failureClasses, FailureClassOptionalEvaluatorUnavailable)
		limitations = append(limitations, synthesis.Limitation{
			Source: "evaluatorcomposition", Scope: spec.EvaluatorID,
			Reason: "optional evaluator produced no finalized result", Blocking: false,
		})
	}

	checks = canonicalChecks(checks)
	limitations = canonicalLimitations(limitations)
	failureClasses = canonicalStrings(failureClasses)
	bindings = canonicalBindings(bindings)

	if reason := requiredCheckFailure(policy.RequiredCheckIDs, checks); reason != "" {
		failureClasses = canonicalStrings(append(failureClasses, FailureClassRequiredCheckUnsatisfied))
		limitations = canonicalLimitations(append(limitations, synthesis.Limitation{
			Source: "evaluatorcomposition", Scope: "required-checks", Reason: reason, Blocking: false,
		}))
	}
	if hasIncompleteObservation(checks) {
		failureClasses = canonicalStrings(append(failureClasses, FailureClassIncompleteObservation))
	}
	if hasBlockingLimitation(limitations) {
		failureClasses = canonicalStrings(append(failureClasses, FailureClassBlockingLimitation))
	}
	if hasFailedCheck(checks) && len(failureClasses) == 0 {
		base.EvaluatorBindings = bindings
		base.FailureDetail = "one or more checks failed without a classified failure reason"
		return finalizeCompositionCleanup(base, ordered)
	}

	if err := verifyRequiredProofDischarges(ctx, state.Session.ProofObligationDigests, evidence, referencedEvidence, resolver); err != nil {
		base.EvaluatorBindings = bindings
		base.FailureDetail = "required proof discharge validation: " + err.Error()
		return finalizeCompositionCleanup(base, ordered)
	}

	recommendation, err := recommendationForClasses(policy, failureClasses)
	if err != nil {
		base.EvaluatorBindings = bindings
		base.FailureDetail = err.Error()
		return finalizeCompositionCleanup(base, ordered)
	}
	if recommendation == "" {
		recommendation = synthesis.RecommendAcceptCandidate
	}

	evaluation := synthesis.Evaluation{
		SchemaVersion:            synthesis.EvaluationSchemaVersion,
		AttemptDigestSHA256:      state.LatestAttemptDigestSHA256,
		EvaluatorKind:            compositionEvaluatorKind,
		EvaluatorVersion:         compositionEvaluatorVersion,
		Checks:                   checks,
		ClassifiedFailureReasons: failureClasses,
		Recommendation:           recommendation,
		Limitations:              limitations,
	}
	evaluation.EvaluationID, err = compositionEvaluationID(state, candidate, policy, bindings, evaluation)
	if err != nil {
		base.EvaluatorBindings = bindings
		base.FailureDetail = "evaluation identity: " + err.Error()
		return finalizeCompositionCleanup(base, ordered)
	}
	evaluation = synthesis.NormalizeEvaluation(evaluation)
	digest, err := synthesis.EvaluationDigest(evaluation)
	if err != nil {
		base.EvaluatorBindings = bindings
		base.FailureDetail = "evaluation digest: " + err.Error()
		return finalizeCompositionCleanup(base, ordered)
	}
	evaluation.EvaluationDigestSHA256 = digest
	data, err := json.Marshal(evaluation)
	if err != nil {
		base.EvaluatorBindings = bindings
		base.FailureDetail = "marshal evaluation: " + err.Error()
		return finalizeCompositionCleanup(base, ordered)
	}
	if err := synthesis.ValidateEvaluationSchema(data); err != nil {
		base.EvaluatorBindings = bindings
		base.FailureDetail = "constructed evaluation failed schema validation: " + err.Error()
		return finalizeCompositionCleanup(base, ordered)
	}

	composed := Composition{
		Disposition:       DispositionEvaluated,
		Evaluation:        &evaluation,
		EvaluatorBindings: bindings,
	}
	return finalizeCompositionCleanup(composed, ordered)
}

func validateExecutionForComposition(execution EvaluatorExecution, state synthesis.SessionState, candidate runnercomposition.CandidateArtifact, policy EvaluationPolicy) error {
	if err := ValidateEvaluatorDescriptor(execution.Descriptor); err != nil {
		return fmt.Errorf("descriptor: %w", err)
	}
	if err := ValidateEvaluationInput(execution.Input); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if err := ValidateEvaluatorResult(execution.Result); err != nil {
		return fmt.Errorf("result: %w", err)
	}
	if execution.Result.EvaluatorID != execution.Descriptor.EvaluatorID {
		return fmt.Errorf("result evaluator ID does not match descriptor")
	}
	if execution.Result.EvaluatorDescriptorDigestSHA256 != execution.Descriptor.DescriptorDigestSHA256 {
		return fmt.Errorf("result descriptor digest does not match descriptor")
	}
	if execution.Result.EvaluationInputDigestSHA256 != execution.Input.EvaluationInputDigestSHA256 {
		return fmt.Errorf("result input digest does not match input")
	}
	if execution.Input.SessionDigestSHA256 != state.Session.SessionDigestSHA256 || execution.Input.AttemptDigestSHA256 != state.LatestAttemptDigestSHA256 {
		return fmt.Errorf("input session/attempt identity does not match current state")
	}
	if execution.Input.CandidateArtifactDigestSHA256 != candidate.CandidateArtifactDigestSHA256 || execution.Input.RepositoryDomain != candidate.RepositoryDomain || execution.Input.BaseRevision != candidate.BaseRevision {
		return fmt.Errorf("input candidate identity does not match exact sealed artifact")
	}
	if execution.Input.PlanGeneration != state.PlanGeneration || execution.Input.AttemptNumber != state.AttemptNumber {
		return fmt.Errorf("input plan generation/attempt number does not match state")
	}
	if execution.Input.DeadlineAt != policy.DeadlineAt || execution.Input.MaxEvidenceCount != policy.MaxEvidenceCount || execution.Input.MaxEvidenceBytes != policy.MaxEvidenceBytes {
		return fmt.Errorf("input execution limits do not match accepted policy")
	}
	if !equalStrings(execution.Input.RequiredProofObligationDigests, state.Session.ProofObligationDigests) {
		return fmt.Errorf("input proof discharge digests do not match session")
	}
	return nil
}

func verifyRequiredProofDischarges(ctx context.Context, required []string, evidence []EvidenceReference, cited map[string]bool, resolver EvidenceResolver) error {
	if len(required) == 0 {
		return nil
	}
	if resolver == nil {
		return fmt.Errorf("no evidence resolver was supplied for %d required proof discharges", len(required))
	}
	byReference := make(map[string]EvidenceReference, len(evidence))
	for _, reference := range evidence {
		if existing, ok := byReference[reference.Reference]; ok && existing.DigestSHA256 != reference.DigestSHA256 {
			return fmt.Errorf("evidence reference %q has conflicting digests", reference.Reference)
		}
		byReference[reference.Reference] = reference
	}
	references := make([]EvidenceReference, 0, len(byReference))
	for _, reference := range byReference {
		if cited[reference.Reference] {
			references = append(references, reference)
		}
	}
	sort.Slice(references, func(i, j int) bool { return references[i].Reference < references[j].Reference })
	validated := make(map[string]bool, len(required))
	for _, reference := range references {
		content, err := resolver.Resolve(ctx, reference)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", reference.Reference, err)
		}
		sum := sha256.Sum256(content)
		actual := hex.EncodeToString(sum[:])
		if actual != reference.DigestSHA256 {
			return fmt.Errorf("evidence reference %q declares digest %q but bytes hash to %q", reference.Reference, reference.DigestSHA256, actual)
		}
		var marker struct {
			DischargeDigestSHA256 string `json:"discharge_digest_sha256"`
		}
		if err := json.Unmarshal(content, &marker); err != nil || marker.DischargeDigestSHA256 == "" {
			continue
		}
		var discharge closureprotocol.ProofDischarge
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&discharge); err != nil {
			return fmt.Errorf("decode ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
		}
		if err := ensureJSONEOF(decoder); err != nil {
			return fmt.Errorf("decode ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
		}
		if err := closureprotocol.ValidateProofDischarge(discharge); err != nil {
			return fmt.Errorf("validate ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
		}
		recomputed, err := closureprotocol.ProofDischargeDigest(discharge)
		if err != nil {
			return fmt.Errorf("digest ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
		}
		if discharge.DischargeDigestSHA256 != recomputed {
			return fmt.Errorf("ProofDischarge declared digest %q does not match recomputed %q", discharge.DischargeDigestSHA256, recomputed)
		}
		if discharge.Status != closureprotocol.ReceiptValid {
			return fmt.Errorf("ProofDischarge %q status is %q, not valid", recomputed, discharge.Status)
		}
		validated[recomputed] = true
	}
	for _, digest := range required {
		if !validated[digest] {
			return fmt.Errorf("required discharge digest %q is absent from check-cited, validated evaluator evidence", digest)
		}
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("trailing JSON value")
}

func recommendationForClasses(policy EvaluationPolicy, classes []string) (synthesis.Recommendation, error) {
	if len(classes) == 0 {
		return "", nil
	}
	policyRules := make(map[string]synthesis.Recommendation, len(policy.FailureClassRecommendations))
	for _, rule := range policy.FailureClassRecommendations {
		policyRules[rule.FailureClass] = rule.Recommendation
	}
	selected := synthesis.Recommendation("")
	selectedRank := 99
	for _, class := range classes {
		recommendation, ok := policyRules[class]
		if !ok {
			recommendation, ok = GovernedFailureClassMinimumRecommendationFor(class)
		}
		if !ok {
			return "", fmt.Errorf("failure class %q has no precommitted recommendation and no governed minimum", class)
		}
		rank := recommendationSeverityRank(recommendation)
		if rank < 0 {
			return "", fmt.Errorf("failure class %q resolved to invalid recommendation %q", class, recommendation)
		}
		if rank < selectedRank {
			selected = recommendation
			selectedRank = rank
		}
	}
	return selected, nil
}

func compositionEvaluationID(state synthesis.SessionState, candidate runnercomposition.CandidateArtifact, policy EvaluationPolicy, bindings []EvaluatorResultBinding, evaluation synthesis.Evaluation) (string, error) {
	identity := struct {
		SessionDigestSHA256   string                       `json:"session_digest_sha256"`
		AttemptDigestSHA256   string                       `json:"attempt_digest_sha256"`
		CandidateDigestSHA256 string                       `json:"candidate_digest_sha256"`
		PolicyDigestSHA256    string                       `json:"policy_digest_sha256"`
		Bindings              []EvaluatorResultBinding     `json:"bindings"`
		Checks                []synthesis.CheckObservation `json:"checks"`
		FailureClasses        []string                     `json:"failure_classes"`
		Recommendation        synthesis.Recommendation     `json:"recommendation"`
		Limitations           []synthesis.Limitation       `json:"limitations"`
	}{
		SessionDigestSHA256:   state.Session.SessionDigestSHA256,
		AttemptDigestSHA256:   state.LatestAttemptDigestSHA256,
		CandidateDigestSHA256: candidate.CandidateArtifactDigestSHA256,
		PolicyDigestSHA256:    policy.PolicyDigestSHA256,
		Bindings:              bindings,
		Checks:                evaluation.Checks,
		FailureClasses:        evaluation.ClassifiedFailureReasons,
		Recommendation:        evaluation.Recommendation,
		Limitations:           evaluation.Limitations,
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "evaluation.o4." + hex.EncodeToString(sum[:]), nil
}

func requiredCheckFailure(required []string, checks []synthesis.CheckObservation) string {
	byID := make(map[string][]synthesis.CheckObservation)
	for _, check := range checks {
		byID[check.CheckID] = append(byID[check.CheckID], check)
	}
	for _, checkID := range required {
		observations := byID[checkID]
		if len(observations) == 0 {
			return fmt.Sprintf("required check %q is missing", checkID)
		}
		for _, observation := range observations {
			if observation.Status != synthesis.CheckPassed {
				return fmt.Sprintf("required check %q has status %q", checkID, observation.Status)
			}
		}
	}
	return ""
}

func hasFailedCheck(checks []synthesis.CheckObservation) bool {
	for _, check := range checks {
		if check.Status == synthesis.CheckFailed {
			return true
		}
	}
	return false
}

func hasIncompleteObservation(checks []synthesis.CheckObservation) bool {
	for _, check := range checks {
		if check.Status == synthesis.CheckSkipped || check.Status == synthesis.CheckUnavailable {
			return true
		}
	}
	return false
}

func hasBlockingLimitation(limitations []synthesis.Limitation) bool {
	for _, limitation := range limitations {
		if limitation.Blocking {
			return true
		}
	}
	return false
}

func canonicalChecks(in []synthesis.CheckObservation) []synthesis.CheckObservation {
	out := append([]synthesis.CheckObservation(nil), in...)
	for i := range out {
		out[i].EvidenceReferences = canonicalStrings(out[i].EvidenceReferences)
	}
	return out
}

func canonicalBindings(in []EvaluatorResultBinding) []EvaluatorResultBinding {
	out := append([]EvaluatorResultBinding(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].EvaluatorID < out[j].EvaluatorID })
	return out
}

func canonicalStrings(in []string) []string {
	set := make(map[string]bool, len(in))
	for _, value := range in {
		if strings.TrimSpace(value) != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func canonicalLimitations(in []synthesis.Limitation) []synthesis.Limitation {
	keyed := make(map[string]synthesis.Limitation, len(in))
	for _, limitation := range in {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%t", limitation.Source, limitation.Scope, limitation.Reason, limitation.Blocking)
		keyed[key] = limitation
	}
	keys := make([]string, 0, len(keyed))
	for key := range keyed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]synthesis.Limitation, 0, len(keys))
	for _, key := range keys {
		out = append(out, keyed[key])
	}
	return out
}

func finalizeCompositionCleanup(composition Composition, executions []EvaluatorExecution) Composition {
	failures := make([]string, 0)
	for _, execution := range executions {
		if execution.Result.CleanupSucceeded == nil {
			continue
		}
		if !*execution.Result.CleanupSucceeded {
			failures = append(failures, execution.Descriptor.EvaluatorID)
		}
	}
	sort.Strings(failures)
	composition.CleanupSucceeded = len(failures) == 0
	if len(failures) > 0 {
		composition.CleanupFailureDetail = "cleanup failed for evaluators: " + strings.Join(failures, ", ")
	}
	return composition
}

func withCompositionEvidence(composition Composition, bindings []EvaluatorResultBinding, executions []EvaluatorExecution) Composition {
	composition.EvaluatorBindings = canonicalBindings(bindings)
	return finalizeCompositionCleanup(composition, executions)
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
