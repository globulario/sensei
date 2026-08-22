// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import (
	"github.com/globulario/sensei/golang/architecture"
)

// Closed model-execution outcome vocabulary.
//
// The distinctions are machine-visible on purpose. Collapsing them would erase
// the difference between "we chose not to ask", "we could not reach anyone",
// "it was asked and said no", and "it was asked and broke" — four different
// facts about a run that an operator and an evaluator must be able to tell
// apart without parsing prose.
const (
	// ModelStatusDisabled: the capability is intentionally off. Zero calls.
	ModelStatusDisabled = "disabled"
	// ModelStatusNotRequested: no model execution was asked for. Zero calls.
	ModelStatusNotRequested = "not_requested"
	// ModelStatusUnavailable: execution WAS requested, but the provider or
	// model could not be resolved or reached BEFORE any invocation began.
	ModelStatusUnavailable = "unavailable"
	// ModelStatusRefused: the provider was invoked and explicitly declined.
	// A refusal is an answer, not an outage, and must not read as one.
	ModelStatusRefused = "refused"
	// ModelStatusErrored: invocation began and execution or transport failed.
	ModelStatusErrored = "errored"
	// ModelStatusInvalid: an artifact came back and failed the model-artifact
	// contract or its grounding rules.
	ModelStatusInvalid = "invalid"
	// ModelStatusResolved: a provider genuinely ran and produced exactly one
	// accepted, content-addressed artifact. This status is EARNED by observed
	// execution; it can never be configured. See ValidateModelBinding.
	ModelStatusResolved = "resolved"
)

// Typed reasons for a non-resolved outcome (closed; no prose matching).
const (
	ModelReasonCapabilityDisabled  = "model_capability_disabled"
	ModelReasonNoModelRequested    = "no_model_requested"
	ModelReasonProviderUnknown     = "provider_not_registered"
	ModelReasonProviderUnreachable = "provider_unreachable"
	ModelReasonModelUnknown        = "model_not_offered_by_provider"
	ModelReasonProviderRefused     = "provider_refused_request"
	ModelReasonExecutionFailed     = "provider_execution_failed"
	ModelReasonArtifactMalformed   = "artifact_malformed"
	ModelReasonArtifactUngrounded  = "artifact_cites_material_not_supplied"
	ModelReasonArtifactOutOfScope  = "artifact_outside_bound_scope"
	ModelReasonArtifactAuthority   = "artifact_attempted_authority_assignment"
	ModelReasonArtifactUnhashable  = "artifact_empty_or_unhashable"
)

// ModelDigestAbsent is the typed statement that a provider genuinely exposes no
// model identity digest. It is NOT interchangeable with an empty digest field:
// one says "this provider cannot supply that identity", the other says nothing
// at all, and a resolved binding must not be allowed to say nothing.
const ModelDigestAbsent = "provider_exposes_no_model_digest"

type ProviderBinding struct {
	ID      string `json:"id" yaml:"id"`
	Version string `json:"version" yaml:"version"`
}

// ModelBinding is the canonical serialized identity of one optional model
// execution. It is the single model contract: an executor living in another
// package still terminates here, so two packages cannot tell two stories about
// the same run.
//
// A model label is not evidence of execution. Everything below the status
// exists so that "a model ran" is a claim backed by the exact request that was
// sent, the exact artifact that came back, and who produced it.
type ModelBinding struct {
	Status string `json:"status" yaml:"status"`
	// Reason is the typed ModelReason* cause. Required for every status that
	// is not resolved, so an absence always says why.
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
	// Provider identifies WHO ran, including version. A provider name without
	// a version cannot distinguish two behaviours behind one label.
	Provider ProviderBinding `json:"provider,omitempty" yaml:"provider,omitempty"`
	// ModelName and ModelDigestSHA256 identify WHAT ran. When a provider
	// genuinely exposes no model digest, ModelDigestAbsence carries that as a
	// typed statement rather than leaving the digest silently empty.
	ModelName          string `json:"model_name,omitempty" yaml:"model_name,omitempty"`
	ModelDigestSHA256  string `json:"model_digest_sha256,omitempty" yaml:"model_digest_sha256,omitempty"`
	ModelDigestAbsence string `json:"model_digest_absence,omitempty" yaml:"model_digest_absence,omitempty"`
	// RequestDigestSHA256 is the exact request that was sent, hashed BEFORE
	// invocation. A digest computed afterwards from a reconstruction would
	// describe what we believe we asked, not what we asked.
	RequestDigestSHA256 string `json:"request_digest_sha256,omitempty" yaml:"request_digest_sha256,omitempty"`
	// ArtifactDigestSHA256 identifies the accepted, normalized artifact. It is
	// set ONLY on resolved: an artifact that failed validation was returned but
	// not accepted, and recording its digest here would make a rejection look
	// like a result.
	ArtifactDigestSHA256 string `json:"artifact_digest_sha256,omitempty" yaml:"artifact_digest_sha256,omitempty"`
	// NondeterminismDeclaration states what about this execution may differ on
	// replay. A model lane that claimed determinism it cannot deliver would
	// make an unreproducible run look reproducible.
	NondeterminismDeclaration string `json:"nondeterminism_declaration,omitempty" yaml:"nondeterminism_declaration,omitempty"`
}

// WhyBinding pins a WHY investigation to the exact HOW document and local
// history snapshot it was allowed to inspect.
type WhyBinding struct {
	HowDocumentDigestSHA256   string   `json:"how_document_digest_sha256,omitempty" yaml:"how_document_digest_sha256,omitempty"`
	QueryDigestSHA256         string   `json:"query_digest_sha256,omitempty" yaml:"query_digest_sha256,omitempty"`
	TargetObservationIDs      []string `json:"target_observation_ids,omitempty" yaml:"target_observation_ids,omitempty"`
	TargetEvidenceIDs         []string `json:"target_evidence_ids,omitempty" yaml:"target_evidence_ids,omitempty"`
	HistoryRangeStart         string   `json:"history_range_start,omitempty" yaml:"history_range_start,omitempty"`
	HistoryRangeEnd           string   `json:"history_range_end,omitempty" yaml:"history_range_end,omitempty"`
	ResolvedHistoryRangeStart string   `json:"resolved_history_range_start,omitempty" yaml:"resolved_history_range_start,omitempty"`
	ResolvedHistoryRangeEnd   string   `json:"resolved_history_range_end,omitempty" yaml:"resolved_history_range_end,omitempty"`
}

type Binding struct {
	Repository                    architecture.ClaimDocumentBinding `json:"repository" yaml:"repository"`
	EvidenceSnapshotDigestSHA256  string                            `json:"evidence_snapshot_digest_sha256,omitempty" yaml:"evidence_snapshot_digest_sha256,omitempty"`
	InvestigationPlanDigestSHA256 string                            `json:"investigation_plan_digest_sha256" yaml:"investigation_plan_digest_sha256"`
	ExtractorProfileDigestSHA256  string                            `json:"extractor_profile_digest_sha256" yaml:"extractor_profile_digest_sha256"`
	Model                         ModelBinding                      `json:"model" yaml:"model"`
	Why                           WhyBinding                        `json:"why,omitempty" yaml:"why,omitempty"`
}

// DisabledModelBinding is the canonical binding for a deterministic lane that
// intentionally runs no model. It exists so every deterministic composer says
// the same thing the same way: five hand-written literals would drift, and a
// status without its typed reason is the drift that matters.
func DisabledModelBinding() ModelBinding {
	return ModelBinding{Status: ModelStatusDisabled, Reason: ModelReasonCapabilityDisabled}
}

func IsValidModelStatus(status string) bool {
	switch status {
	case ModelStatusDisabled, ModelStatusNotRequested, ModelStatusUnavailable,
		ModelStatusRefused, ModelStatusErrored, ModelStatusInvalid, ModelStatusResolved:
		return true
	default:
		return false
	}
}

// ModelStatusInvoked reports whether a status means the provider was actually
// called. It is the machine-readable form of the "provider call?" column in the
// #256 contract, so callers never infer invocation by matching status strings.
func ModelStatusInvoked(status string) bool {
	switch status {
	case ModelStatusRefused, ModelStatusErrored, ModelStatusInvalid, ModelStatusResolved:
		return true
	default:
		return false
	}
}
