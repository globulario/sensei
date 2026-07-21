// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import "fmt"

// Verdict is the CLOSED runtime-boundary verdict vocabulary. Zero value fails closed. A positive
// verdict (satisfied) is only ever reached through the full conjunction in AssessRuntimeBoundary.
type Verdict string

const (
	// VerdictSatisfied: a fresh, admissible, in-scope observation showed an authorized crossing
	// against the required contract, under a current integrity-verified runtime-assessable identity
	// with explicit policy — and nothing contradicted it.
	VerdictSatisfied Verdict = "satisfied"
	// VerdictViolated: an admissible observation showed a policy-forbidden crossing.
	VerdictViolated Verdict = "violated"
	// VerdictDegraded: usable partial signal that cannot establish full compliance (stale authority,
	// truncated evidence, or contradictory observations).
	VerdictDegraded Verdict = "degraded"
	// VerdictUnknown: the boundary is visible but no admissible evidence determines its state.
	VerdictUnknown Verdict = "unknown"
	// VerdictNotApplicable: the boundary is explicitly not runtime-assessable (policy) or revoked.
	VerdictNotApplicable Verdict = "not_applicable"
	// VerdictUnavailable: a required evidence source (e.g. the collector) was not observed.
	VerdictUnavailable Verdict = "unavailable"
	// VerdictInvalid: the identity, policy, observation, or binding is contradictory or malformed.
	VerdictInvalid Verdict = "invalid"
)

func validVerdict(v Verdict) bool {
	switch v {
	case VerdictSatisfied, VerdictViolated, VerdictDegraded, VerdictUnknown,
		VerdictNotApplicable, VerdictUnavailable, VerdictInvalid:
		return true
	}
	return false
}

// ResultKind is the CLOSED finer classification distinguishing the structural reason for a verdict.
// It is a strictly more specific typing of Verdict, never a second independent decision.
type ResultKind string

const (
	KindObservedAuthorizedCrossing ResultKind = "observed_authorized_crossing"
	KindObservedForbiddenCrossing  ResultKind = "observed_forbidden_crossing"
	KindCrossingStaleAuthority     ResultKind = "crossing_stale_authority"
	KindRequiredEvidenceAbsent     ResultKind = "required_evidence_absent"
	KindCollectorUnavailable       ResultKind = "collector_unavailable"
	KindEvidenceTruncated          ResultKind = "evidence_truncated"
	KindIdentityUnresolved         ResultKind = "identity_unresolved"
	KindContradictoryObservations  ResultKind = "contradictory_observations"
	KindBoundaryNotAssessable      ResultKind = "boundary_not_runtime_assessable"
	KindBoundaryRevoked            ResultKind = "boundary_revoked"
	KindPolicyAbsent               ResultKind = "policy_absent"
	KindEvidenceOutOfScope         ResultKind = "evidence_out_of_scope"
	// KindNoRuntimeEvidence: proof is optional and nothing relevant was observed — the boundary was
	// simply not exercised in the window. Distinct from required_evidence_absent (unavailable).
	KindNoRuntimeEvidence ResultKind = "no_runtime_evidence"
)

func validResultKind(k ResultKind) bool {
	switch k {
	case KindObservedAuthorizedCrossing, KindObservedForbiddenCrossing, KindCrossingStaleAuthority,
		KindRequiredEvidenceAbsent, KindCollectorUnavailable, KindEvidenceTruncated,
		KindIdentityUnresolved, KindContradictoryObservations, KindBoundaryNotAssessable,
		KindBoundaryRevoked, KindPolicyAbsent, KindEvidenceOutOfScope, KindNoRuntimeEvidence:
		return true
	}
	return false
}

// resultKindVerdict maps each ResultKind to its only permitted Verdict. This is the single place
// the two closed vocabularies are related, so a verdict can never disagree with its result kind.
func resultKindVerdict(k ResultKind) Verdict {
	switch k {
	case KindObservedAuthorizedCrossing:
		return VerdictSatisfied
	case KindObservedForbiddenCrossing:
		return VerdictViolated
	case KindCrossingStaleAuthority, KindEvidenceTruncated, KindContradictoryObservations:
		return VerdictDegraded
	case KindRequiredEvidenceAbsent, KindCollectorUnavailable:
		return VerdictUnavailable
	case KindPolicyAbsent, KindEvidenceOutOfScope, KindNoRuntimeEvidence:
		return VerdictUnknown
	case KindBoundaryNotAssessable, KindBoundaryRevoked:
		return VerdictNotApplicable
	case KindIdentityUnresolved:
		return VerdictInvalid
	}
	return VerdictInvalid
}

// InteractionKind is the CLOSED runtime interaction vocabulary. Zero value fails closed.
type InteractionKind string

const (
	InteractionRead       InteractionKind = "read"
	InteractionWrite      InteractionKind = "write"
	InteractionInvoke     InteractionKind = "invoke"
	InteractionSubscribe  InteractionKind = "subscribe"
	InteractionAdminister InteractionKind = "administer"
	InteractionUnknown    InteractionKind = "unknown"
)

func validInteractionKind(k InteractionKind) bool {
	switch k {
	case InteractionRead, InteractionWrite, InteractionInvoke, InteractionSubscribe,
		InteractionAdminister, InteractionUnknown:
		return true
	}
	return false
}

// CrossingDirection is the CLOSED crossing-direction vocabulary. Zero value fails closed.
type CrossingDirection string

const (
	DirectionInbound       CrossingDirection = "inbound"
	DirectionOutbound      CrossingDirection = "outbound"
	DirectionBidirectional CrossingDirection = "bidirectional"
	DirectionUnknown       CrossingDirection = "unknown"
)

func validDirection(d CrossingDirection) bool {
	switch d {
	case DirectionInbound, DirectionOutbound, DirectionBidirectional, DirectionUnknown:
		return true
	}
	return false
}

// RuntimeProof is the CLOSED "is runtime proof required for this boundary" vocabulary.
type RuntimeProof string

const (
	ProofRequired    RuntimeProof = "required"
	ProofOptional    RuntimeProof = "optional"
	ProofUnsupported RuntimeProof = "unsupported"
)

func validRuntimeProof(p RuntimeProof) bool {
	switch p {
	case ProofRequired, ProofOptional, ProofUnsupported:
		return true
	}
	return false
}

// LifecycleState is the CLOSED boundary-lifecycle vocabulary used by the identity.
type LifecycleState string

const (
	LifecycleActive     LifecycleState = "active"
	LifecycleDeprecated LifecycleState = "deprecated"
	LifecycleRevoked    LifecycleState = "revoked"
	LifecycleUnknown    LifecycleState = "unknown"
)

func validLifecycle(l LifecycleState) bool {
	switch l {
	case LifecycleActive, LifecycleDeprecated, LifecycleRevoked, LifecycleUnknown:
		return true
	}
	return false
}

// RuntimeBoundaryAssessment is the deterministic, non-authoritative assessment of one boundary. It
// certifies nothing: it reports whether the running system was observed to respect a declared
// boundary, with an explicit result kind and honest availability.
type RuntimeBoundaryAssessment struct {
	Meta ProjectionMeta `json:"meta" yaml:"meta"`

	BoundaryIRI string     `json:"boundary_iri" yaml:"boundary_iri"`
	Verdict     Verdict    `json:"verdict" yaml:"verdict"`
	ResultKind  ResultKind `json:"result_kind" yaml:"result_kind"`
	ReasonCode  string     `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`

	// Traceability: the exact digests of the inputs this assessment was computed against. They bind
	// the verdict to a specific identity/policy/binding without recomputing them here.
	IdentityDigest string `json:"identity_digest,omitempty" yaml:"identity_digest,omitempty"`
	PolicyDigest   string `json:"policy_digest,omitempty" yaml:"policy_digest,omitempty"`
	BindingDigest  string `json:"binding_digest,omitempty" yaml:"binding_digest,omitempty"`

	// AdmissibleObservations / RefusedObservations preserve unknown-vs-zero: a zero admissible count
	// is real data only when the collector source was Available.
	AdmissibleObservations int      `json:"admissible_observations" yaml:"admissible_observations"`
	RefusedObservations    int      `json:"refused_observations" yaml:"refused_observations"`
	RefusalReasons         []string `json:"refusal_reasons,omitempty" yaml:"refusal_reasons,omitempty"`
	// Conflicts are the observation identities that contradicted one another (preserved, never
	// first-row-wins).
	Conflicts []string `json:"conflicts,omitempty" yaml:"conflicts,omitempty"`

	NextActionOwner string `json:"next_action_owner,omitempty" yaml:"next_action_owner,omitempty"`
}

// ComputeDigest returns the self-excluding SHA-256 of the assessment (Meta.DigestSHA256 cleared).
func (a RuntimeBoundaryAssessment) ComputeDigest() (string, error) {
	c := a
	c.Meta.DigestSHA256 = ""
	c.RefusalReasons = sortedUnique(c.RefusalReasons)
	c.Conflicts = sortedUnique(c.Conflicts)
	return digestOf(c)
}

// ValidateAssessment enforces the closed vocabularies, verdict↔result-kind agreement, availability
// coherence, and the self-excluding digest. A caller-supplied assessment cannot claim satisfied
// with an off-vocabulary or mismatched result kind, nor carry a tampered digest.
func ValidateAssessment(a RuntimeBoundaryAssessment) error {
	if err := validateMeta(a.Meta, SchemaAssessment); err != nil {
		return err
	}
	if !trimmedNonEmpty(a.BoundaryIRI) {
		return fmt.Errorf("assessment boundary IRI is empty or padded")
	}
	if !validVerdict(a.Verdict) {
		return fmt.Errorf("assessment verdict %q is off-vocabulary", a.Verdict)
	}
	if !validResultKind(a.ResultKind) {
		return fmt.Errorf("assessment result kind %q is off-vocabulary", a.ResultKind)
	}
	if resultKindVerdict(a.ResultKind) != a.Verdict {
		return fmt.Errorf("verdict %q disagrees with result kind %q (must be %q)",
			a.Verdict, a.ResultKind, resultKindVerdict(a.ResultKind))
	}
	if a.AdmissibleObservations < 0 || a.RefusedObservations < 0 {
		return fmt.Errorf("assessment observation counts must be non-negative")
	}
	// satisfied requires admissible evidence and an available assessment — never a bare claim.
	if a.Verdict == VerdictSatisfied {
		if a.Meta.Availability != AvailabilityAvailable {
			return fmt.Errorf("satisfied assessment must be Available, got %q", a.Meta.Availability)
		}
		if a.AdmissibleObservations < 1 {
			return fmt.Errorf("satisfied assessment requires at least one admissible observation")
		}
	}
	// A satisfied/violated verdict cannot coexist with an unavailable/invalid availability.
	if (a.Verdict == VerdictSatisfied || a.Verdict == VerdictViolated) &&
		(a.Meta.Availability == AvailabilityUnavailable || a.Meta.Availability == AvailabilityInvalid) {
		return fmt.Errorf("verdict %q is inconsistent with availability %q", a.Verdict, a.Meta.Availability)
	}
	if !equalStrings(a.Conflicts, sortedUnique(a.Conflicts)) {
		return fmt.Errorf("assessment conflicts are not canonical (sorted+unique)")
	}
	if !equalStrings(a.RefusalReasons, sortedUnique(a.RefusalReasons)) {
		return fmt.Errorf("assessment refusal reasons are not canonical (sorted+unique)")
	}
	want, err := a.ComputeDigest()
	if err != nil {
		return err
	}
	if a.Meta.DigestSHA256 != want {
		return fmt.Errorf("assessment digest mismatch")
	}
	return nil
}
