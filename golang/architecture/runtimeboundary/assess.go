// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

// AssessmentInput is the complete, typed input to a runtime-boundary assessment. Every field is
// caller-supplied and validated; nothing is read from disk, network, clock, or environment.
type AssessmentInput struct {
	Identity           RuntimeBoundaryIdentity
	IdentityResolution BoundaryClassResolution
	// Policy is the boundary's governed policy. A nil policy is absence — never "allowed".
	Policy *BoundaryPolicy
	// Binding is the non-self-authorizing runtime→architecture mapping. A nil binding means no
	// observation can be admitted (traffic cannot authorize its own binding).
	Binding *RuntimeArchitectureBinding
	// Observations are the typed runtime observations offered for this boundary.
	Observations []RuntimeObservation
	// CollectorAvailable reports whether the runtime collector source itself was reachable. It lets
	// the owner distinguish "collector outage" from "no violation observed" when no evidence arrives.
	CollectorAvailable bool
}

// AssessRuntimeBoundary is the pure, deterministic runtime-boundary assessment. It returns exactly
// one honest verdict. It never mutates anything, never emits RDF, never certifies correctness, and
// never lets an observation create, authorize, or redefine a boundary. It returns a Go error only on
// an internal digest failure; every semantic outcome (including invalid input) is a well-formed
// assessment.
func AssessRuntimeBoundary(in AssessmentInput) (RuntimeBoundaryAssessment, error) {
	id := in.Identity
	fin := func(kind ResultKind, reason string, admissible, refused int, refusals, conflicts []string, nextOwner string) (RuntimeBoundaryAssessment, error) {
		return finalizeAssessment(id, kind, reason, admissible, refused, refusals, conflicts, nextOwner)
	}

	// 1. Identity must be well-formed and actually a boundary.
	if err := ValidateRuntimeBoundaryIdentity(id, in.IdentityResolution); err != nil {
		return fin(KindIdentityUnresolved, "identity_invalid", 0, 0, nil, nil, "")
	}
	if id.CanonicalClass != rdf.ClassBoundary {
		reason := in.IdentityResolution.ReasonCode
		if reason == "" {
			reason = "not_a_boundary"
		}
		return fin(KindIdentityUnresolved, reason, 0, 0, nil, nil, "")
	}
	// 2. A supplied policy/binding must itself be valid and in scope for this boundary.
	if in.Policy != nil {
		if err := ValidateBoundaryPolicy(*in.Policy); err != nil {
			return fin(KindIdentityUnresolved, "policy_invalid", 0, 0, nil, nil, "")
		}
		if in.Policy.BoundaryIRI != id.BoundaryIRI {
			return fin(KindIdentityUnresolved, "policy_boundary_mismatch", 0, 0, nil, nil, "")
		}
	}
	if in.Binding != nil {
		if err := ValidateRuntimeArchitectureBinding(*in.Binding); err != nil {
			return fin(KindIdentityUnresolved, "binding_invalid", 0, 0, nil, nil, "")
		}
	}

	// 3. Structural non-assessability (fail-closed, before any evidence is consulted).
	if id.Lifecycle == LifecycleRevoked || id.Lifecycle == LifecycleDeprecated {
		return fin(KindBoundaryRevoked, string(id.Lifecycle), 0, 0, nil, nil, "")
	}
	// Stale/unverified graph authority can NEVER yield satisfied — we cannot trust what the boundary
	// even is, so no observation is admitted as authoritative.
	if !id.AuthorityCurrent || !id.IntegrityVerified {
		return fin(KindCrossingStaleAuthority, "stale_or_unverified_graph_authority", 0, len(in.Observations), refusalList("stale_authority", len(in.Observations)), nil, "")
	}
	if !id.RuntimeAssessable {
		return fin(KindBoundaryNotAssessable, "not_runtime_assessable", 0, 0, nil, nil, "")
	}
	// 4. Absence of policy is never "allowed" or "not applicable" — it is unknown.
	if in.Policy == nil {
		return fin(KindPolicyAbsent, "policy_absent", 0, 0, nil, nil, "")
	}
	pol := *in.Policy
	if pol.RuntimeProof == ProofUnsupported {
		return fin(KindBoundaryNotAssessable, "runtime_proof_unsupported", 0, 0, nil, nil, "")
	}
	nextOwner := pol.NextActionOwner

	// 5. Admit observations through the non-self-authorizing binding.
	admitted, refused, refusals := admitObservations(in, id)

	// 6. No admissible evidence: distinguish collector outage / required-absent / optional-none.
	if len(admitted) == 0 {
		if !in.CollectorAvailable {
			return fin(KindCollectorUnavailable, "collector_unavailable", 0, refused, refusals, nil, nextOwner)
		}
		if pol.RuntimeProof == ProofRequired {
			return fin(KindRequiredEvidenceAbsent, "required_evidence_absent", 0, refused, refusals, nil, nextOwner)
		}
		return fin(KindNoRuntimeEvidence, "no_runtime_evidence", 0, refused, refusals, nil, nextOwner)
	}

	// 7. Classify admitted crossings against the policy and aggregate deterministically.
	kind, conflicts := classifyAdmitted(admitted, pol)
	return fin(kind, string(kind), len(admitted), refused, refusals, conflicts, nextOwner)
}

// admitObservations applies the non-self-authorizing binding: an observation is admissible only when
// it is well-formed, has resolved identity, and the binding validly (in scope, target-matched,
// identity-mapped) attaches it to THIS boundary. Everything else is refused with a typed reason.
func admitObservations(in AssessmentInput, id RuntimeBoundaryIdentity) (admitted []RuntimeObservation, refused int, refusals []string) {
	bindingInScope := in.Binding != nil && bindingScopes(*in.Binding, id)
	for _, o := range in.Observations {
		reason := ""
		switch {
		case ValidateObservation(o) != nil:
			reason = "malformed"
		case !o.hasResolvedIdentity():
			// Ambiguous/unknown runtime identity is preserved, never guessed.
			reason = "ambiguous_identity"
		case in.Binding == nil:
			// Traffic cannot authorize its own binding.
			reason = "no_binding"
		case !bindingInScope:
			reason = "out_of_scope"
		case !runtimeTargetMatches(in.Binding.RuntimeTarget, o.RuntimeTarget):
			reason = "runtime_target_mismatch"
		case !mappedThroughBinding(*in.Binding, o):
			reason = "unmapped_identity"
		case o.Availability != SourceAvailable:
			// Degraded/unavailable/invalid evidence is not admitted as authoritative.
			reason = string(o.Availability)
		}
		if reason != "" {
			refused++
			refusals = append(refusals, reason)
			continue
		}
		admitted = append(admitted, o)
	}
	return admitted, refused, sortedUnique(refusals)
}

// mappedThroughBinding reports whether the observation's caller and callee map through the binding's
// explicit mappings. An empty mapping list is a wildcard for that side; a non-empty list requires
// membership (traffic from an unmapped identity is not admitted for this boundary).
func mappedThroughBinding(b RuntimeArchitectureBinding, o RuntimeObservation) bool {
	if len(b.MappedCallers) > 0 && !containsString(b.MappedCallers, o.CallerIdentity) {
		return false
	}
	if len(b.MappedCallees) > 0 && !containsString(b.MappedCallees, o.CalleeIdentity) {
		return false
	}
	return true
}

// crossingClass is the owner's per-observation classification of an admitted crossing under policy.
type crossingClass int

const (
	classForbidden  crossingClass = iota // violates a policy rule
	classSatisfying                      // authorized AND against the required contract
	classOutOfScope                      // authorized but not against the required contract
)

// classifyAdmitted classifies each admitted observation, detects contradictions (same crossing key
// classified differently — conflicting evidence, never first-row-wins), and aggregates one result
// kind by fixed precedence: violation > contradiction > truncation > satisfied > out-of-scope.
func classifyAdmitted(admitted []RuntimeObservation, pol BoundaryPolicy) (ResultKind, []string) {
	type keyed struct {
		cls crossingClass
		id  string
	}
	byKey := map[string][]keyed{}
	order := []string{}
	for _, o := range admitted {
		k := crossingKey(o)
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], keyed{cls: classifyCrossing(o, pol), id: o.ObservationID})
	}

	anyViolating, anySatisfying, anyOutOfScope := false, false, false
	conflicts := []string{}
	for _, k := range order {
		entries := byKey[k]
		distinct := map[crossingClass]bool{}
		for _, e := range entries {
			distinct[e.cls] = true
		}
		if len(distinct) > 1 {
			// Same crossing classified more than one way → conflicting evidence.
			for _, e := range entries {
				conflicts = append(conflicts, e.id)
			}
			continue
		}
		switch entries[0].cls {
		case classForbidden:
			anyViolating = true
		case classSatisfying:
			anySatisfying = true
		case classOutOfScope:
			anyOutOfScope = true
		}
	}

	switch {
	case anyViolating:
		return KindObservedForbiddenCrossing, nil
	case len(conflicts) > 0:
		return KindContradictoryObservations, sortedUnique(conflicts)
	case anySatisfying && anyTruncated(admitted):
		return KindEvidenceTruncated, nil
	case anySatisfying:
		return KindObservedAuthorizedCrossing, nil
	case anyOutOfScope:
		return KindEvidenceOutOfScope, nil
	default:
		// Admitted evidence existed but classified as nothing conclusive.
		return KindEvidenceOutOfScope, nil
	}
}

// classifyCrossing applies the policy to one admitted observation. It is a pure function of the
// crossing facts + policy — the collector never pre-decides compliance.
func classifyCrossing(o RuntimeObservation, pol BoundaryPolicy) crossingClass {
	forbidden := false
	if len(pol.PermittedDirections) > 0 && !containsDirection(pol.PermittedDirections, o.Direction) {
		forbidden = true
	}
	if len(pol.AllowedInteractionKinds) > 0 && !containsInteraction(pol.AllowedInteractionKinds, o.InteractionKind) {
		forbidden = true
	}
	if len(pol.AllowedCallers) > 0 && !containsString(pol.AllowedCallers, o.CallerIdentity) {
		forbidden = true
	}
	if len(pol.AllowedCallees) > 0 && !containsString(pol.AllowedCallees, o.CalleeIdentity) {
		forbidden = true
	}
	if pol.RequireAuthContext && !o.AuthContextPresent {
		forbidden = true
	}
	if len(pol.AllowedAuthorityClasses) > 0 && !containsString(pol.AllowedAuthorityClasses, o.AuthorityClass) {
		forbidden = true
	}
	if forbidden {
		return classForbidden
	}
	// Authorized; does it exercise the required contract?
	if pol.RequiredContract != "" && o.EndpointOrContractIdentity != pol.RequiredContract {
		return classOutOfScope
	}
	return classSatisfying
}

func crossingKey(o RuntimeObservation) string {
	// Auth context is deliberately EXCLUDED from the key so that the same crossing observed with
	// conflicting auth evidence classifies differently → detected as a contradiction.
	return strings.Join([]string{
		string(o.Direction), o.CallerIdentity, o.CalleeIdentity,
		o.EndpointOrContractIdentity, string(o.InteractionKind),
	}, "\x00")
}

func anyTruncated(obs []RuntimeObservation) bool {
	for _, o := range obs {
		if o.Truncated {
			return true
		}
	}
	return false
}

func containsDirection(in []CrossingDirection, want CrossingDirection) bool {
	for _, d := range in {
		if d == want {
			return true
		}
	}
	return false
}

func containsInteraction(in []InteractionKind, want InteractionKind) bool {
	for _, k := range in {
		if k == want {
			return true
		}
	}
	return false
}

func refusalList(reason string, n int) []string {
	if n <= 0 {
		return nil
	}
	return []string{reason}
}

// finalizeAssessment builds the well-formed, digest-bound assessment for a chosen result kind. The
// verdict is derived solely from the result kind; availability is derived from the verdict through a
// single primary source, so the two can never disagree.
func finalizeAssessment(id RuntimeBoundaryIdentity, kind ResultKind, reason string, admissible, refused int, refusals, conflicts []string, nextOwner string) (RuntimeBoundaryAssessment, error) {
	verdict := resultKindVerdict(kind)
	avail, srcAvail := verdictAvailability(verdict)
	primary := srcStatus(ProducerName, SchemaAssessment, bareBoundaryID(id.BoundaryIRI), id.DigestSHA256, srcAvail, ImpactPrimary, string(kind))
	sources := []SourceStatus{primary}

	a := RuntimeBoundaryAssessment{
		Meta:                   newMeta(SchemaAssessment, id.RepositoryIdentity, id.DomainIdentity, avail, sources, nil),
		BoundaryIRI:            id.BoundaryIRI,
		Verdict:                verdict,
		ResultKind:             kind,
		ReasonCode:             reason,
		IdentityDigest:         id.DigestSHA256,
		AdmissibleObservations: admissible,
		RefusedObservations:    refused,
		RefusalReasons:         sortedUnique(refusals),
		Conflicts:              sortedUnique(conflicts),
		NextActionOwner:        nextOwner,
	}
	dig, err := a.ComputeDigest()
	if err != nil {
		return RuntimeBoundaryAssessment{}, err
	}
	a.Meta.DigestSHA256 = dig
	if err := ValidateAssessment(a); err != nil {
		return RuntimeBoundaryAssessment{}, fmt.Errorf("internal: assessment failed self-validation: %w", err)
	}
	return a, nil
}

// verdictAvailability maps a verdict to its assessment availability + the single primary source's
// availability. Coherent by construction with availabilityConsistent.
func verdictAvailability(v Verdict) (Availability, SourceAvailability) {
	switch v {
	case VerdictSatisfied, VerdictViolated, VerdictNotApplicable:
		return AvailabilityAvailable, SourceAvailable
	case VerdictDegraded, VerdictUnknown:
		return AvailabilityPartial, SourceDegraded
	case VerdictUnavailable:
		return AvailabilityUnavailable, SourceUnavailable
	default: // VerdictInvalid
		return AvailabilityInvalid, SourceInvalid
	}
}

// bareBoundaryID derives a short, non-absolute source identity from a boundary IRI (fragment tail).
func bareBoundaryID(iri string) string {
	if i := strings.LastIndexAny(iri, "#/"); i >= 0 && i+1 < len(iri) {
		return iri[i+1:]
	}
	return iri
}
