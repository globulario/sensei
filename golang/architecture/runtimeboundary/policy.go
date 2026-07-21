// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import "fmt"

// PolicySchema is the canonical schema id of a boundary policy.
const PolicySchema = "runtime.boundary_policy/v1"

// BoundaryPolicy declares, for one runtime-assessable boundary, the crossings it permits and the
// runtime proof it requires. Absence of a policy is never "allowed" or "not applicable" — a boundary
// with no policy cannot be satisfied (it resolves to unknown). The policy is authored/governed
// architecture, never derived from observed traffic.
type BoundaryPolicy struct {
	PolicyID    string `json:"policy_id" yaml:"policy_id"`
	BoundaryIRI string `json:"boundary_iri" yaml:"boundary_iri"`

	PermittedDirections     []CrossingDirection `json:"permitted_directions,omitempty" yaml:"permitted_directions,omitempty"`
	AllowedInteractionKinds []InteractionKind   `json:"allowed_interaction_kinds,omitempty" yaml:"allowed_interaction_kinds,omitempty"`
	AllowedCallers          []string            `json:"allowed_callers,omitempty" yaml:"allowed_callers,omitempty"`
	AllowedCallees          []string            `json:"allowed_callees,omitempty" yaml:"allowed_callees,omitempty"`
	AllowedAuthorityClasses []string            `json:"allowed_authority_classes,omitempty" yaml:"allowed_authority_classes,omitempty"`

	// RequiredContract is the exact contract identity a satisfying crossing must be against. A
	// crossing against a different contract does not satisfy this boundary.
	RequiredContract  string `json:"required_contract,omitempty" yaml:"required_contract,omitempty"`
	RequiredInvariant string `json:"required_invariant,omitempty" yaml:"required_invariant,omitempty"`
	// RequireAuthContext demands an authenticated context on every permitted crossing.
	RequireAuthContext bool `json:"require_auth_context" yaml:"require_auth_context"`

	RuntimeProof RuntimeProof `json:"runtime_proof" yaml:"runtime_proof"`

	EvidenceOwner   string `json:"evidence_owner,omitempty" yaml:"evidence_owner,omitempty"`
	NextActionOwner string `json:"next_action_owner,omitempty" yaml:"next_action_owner,omitempty"`

	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// BuildBoundaryPolicy canonicalizes and stamps the self-excluding digest.
func BuildBoundaryPolicy(p BoundaryPolicy) (BoundaryPolicy, error) {
	p.AllowedCallers = sortedUnique(p.AllowedCallers)
	p.AllowedCallees = sortedUnique(p.AllowedCallees)
	p.AllowedAuthorityClasses = sortedUnique(p.AllowedAuthorityClasses)
	p.PermittedDirections = sortedUniqueDirections(p.PermittedDirections)
	p.AllowedInteractionKinds = sortedUniqueInteractions(p.AllowedInteractionKinds)
	if p.RuntimeProof == "" {
		p.RuntimeProof = ProofUnsupported
	}
	if err := validatePolicyFields(p); err != nil {
		return BoundaryPolicy{}, err
	}
	p.DigestSHA256 = ""
	dig, err := digestOf(p)
	if err != nil {
		return BoundaryPolicy{}, err
	}
	p.DigestSHA256 = dig
	return p, nil
}

func validatePolicyFields(p BoundaryPolicy) error {
	if !trimmedNonEmpty(p.PolicyID) {
		return fmt.Errorf("boundary policy id is empty or padded")
	}
	if !trimmedNonEmpty(p.BoundaryIRI) {
		return fmt.Errorf("boundary policy %q has empty boundary IRI", p.PolicyID)
	}
	if !validRuntimeProof(p.RuntimeProof) {
		return fmt.Errorf("boundary policy %q runtime proof %q is off-vocabulary", p.PolicyID, p.RuntimeProof)
	}
	for _, d := range p.PermittedDirections {
		if !validDirection(d) {
			return fmt.Errorf("boundary policy %q permitted direction %q is off-vocabulary", p.PolicyID, d)
		}
	}
	for _, k := range p.AllowedInteractionKinds {
		if !validInteractionKind(k) {
			return fmt.Errorf("boundary policy %q allowed interaction %q is off-vocabulary", p.PolicyID, k)
		}
	}
	return nil
}

// ValidateBoundaryPolicy verifies vocabularies, canonical lists, and the self-excluding digest.
func ValidateBoundaryPolicy(p BoundaryPolicy) error {
	if err := validatePolicyFields(p); err != nil {
		return err
	}
	if !equalStrings(p.AllowedCallers, sortedUnique(p.AllowedCallers)) ||
		!equalStrings(p.AllowedCallees, sortedUnique(p.AllowedCallees)) ||
		!equalStrings(p.AllowedAuthorityClasses, sortedUnique(p.AllowedAuthorityClasses)) {
		return fmt.Errorf("boundary policy %q lists are not canonical (sorted+unique)", p.PolicyID)
	}
	c := p
	c.DigestSHA256 = ""
	want, err := digestOf(c)
	if err != nil {
		return err
	}
	if p.DigestSHA256 != want {
		return fmt.Errorf("boundary policy %q digest mismatch", p.PolicyID)
	}
	return nil
}

func sortedUniqueDirections(in []CrossingDirection) []CrossingDirection {
	s := make([]string, 0, len(in))
	for _, d := range in {
		s = append(s, string(d))
	}
	s = sortedUnique(s)
	out := make([]CrossingDirection, 0, len(s))
	for _, v := range s {
		out = append(out, CrossingDirection(v))
	}
	return out
}

func sortedUniqueInteractions(in []InteractionKind) []InteractionKind {
	s := make([]string, 0, len(in))
	for _, k := range in {
		s = append(s, string(k))
	}
	s = sortedUnique(s)
	out := make([]InteractionKind, 0, len(s))
	for _, v := range s {
		out = append(out, InteractionKind(v))
	}
	return out
}
