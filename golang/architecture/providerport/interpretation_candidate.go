// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// MapInterpretationCandidate validates and detaches a completed O2
// interpretation result without granting it O1 authority. It deliberately
// returns data, not a synthesis.Command: provider output is candidate
// knowledge until a separate interpretation-closure owner certifies it.
//
// The validation is intentionally repeated at this trust boundary rather
// than relying on Run's earlier checks: Request and Result contain mutable
// slices/pointers and may have changed after Run returned. The returned
// Interpretation is a deep copy and shares no backing memory with O2.
func MapInterpretationCandidate(state synthesis.SessionState, request Request, result Result) (synthesis.Interpretation, error) {
	if request.Operation != OperationInterpretation || result.Operation != OperationInterpretation {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: interpretation candidate mapper requires operation %q", OperationInterpretation)
	}
	if result.TerminalOutcome != OutcomeCompleted {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: cannot map a non-completed interpretation result (terminal_outcome=%s)", result.TerminalOutcome)
	}
	if result.RequestDigestSHA256 != request.RequestDigestSHA256 || result.Operation != request.Operation {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: result does not reference the given request")
	}
	if err := validateDocument(ValidateRequestSchema, request); err != nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: request failed schema validation: %w", err)
	}
	reqDigest, err := RequestDigest(request)
	if err != nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: compute request digest: %w", err)
	}
	if reqDigest != request.RequestDigestSHA256 {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: request declares digest %q but its actual computed digest is %q -- mutated since validation", request.RequestDigestSHA256, reqDigest)
	}
	if err := validateDocument(ValidateResultSchema, result); err != nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: result failed schema validation: %w", err)
	}
	resDigest, err := ResultDigest(result)
	if err != nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: compute result digest: %w", err)
	}
	if resDigest != result.ResultDigestSHA256 {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: result declares digest %q but its actual computed digest is %q -- mutated since validation", result.ResultDigestSHA256, resDigest)
	}
	payloadDeclared, payloadComputed, err := payloadDigests(result)
	if err != nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: compute payload digest: %w", err)
	}
	if payloadDeclared != payloadComputed {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: embedded interpretation declares digest %q but its actual computed digest is %q -- mutated since validation", payloadDeclared, payloadComputed)
	}
	if result.PayloadDigestSHA256 == nil || *result.PayloadDigestSHA256 != payloadComputed {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: result payload_digest_sha256 does not match the embedded interpretation's actual computed digest -- mutated since validation")
	}
	if request.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: request references session digest %q, current session is %q -- stale identity", request.SessionDigestSHA256, state.Session.SessionDigestSHA256)
	}
	if request.RepositoryDomain != state.Session.RepositoryDomain || request.BaseRevision != state.Session.BaseRevision {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: request references %s@%s, current session is %s@%s -- stale identity", request.RepositoryDomain, request.BaseRevision, state.Session.RepositoryDomain, state.Session.BaseRevision)
	}
	if state.Phase != synthesis.PhaseCreated {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: interpretation is only legal from phase %s, session is in phase %s -- wrong phase", synthesis.PhaseCreated, state.Phase)
	}
	if request.InterpretationPayload == nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: interpretation request carries no interpretation_payload")
	}
	computed, digestErr := synthesis.SessionDigest(*request.InterpretationPayload)
	if err := verifyParentChain("session", request.InterpretationPayload.SessionDigestSHA256, computed, digestErr, request.ParentArtifactDigestSHA256, state.Session.SessionDigestSHA256); err != nil {
		return synthesis.Interpretation{}, err
	}
	if result.InterpretationPayload == nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: completed interpretation result carries no interpretation_payload")
	}
	if result.InterpretationPayload.SessionDigestSHA256 != state.Session.SessionDigestSHA256 {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: candidate interpretation references session digest %q, current session is %q -- stale candidate", result.InterpretationPayload.SessionDigestSHA256, state.Session.SessionDigestSHA256)
	}
	detached, err := deepCopy(*result.InterpretationPayload)
	if err != nil {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: deep-copy candidate interpretation: %w", err)
	}
	return detached, nil
}
