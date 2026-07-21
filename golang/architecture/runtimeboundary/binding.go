// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// BindingSchema is the canonical schema id of a runtime→architecture binding.
const BindingSchema = "runtime.architecture_binding/v1"

// RuntimeArchitectureBinding is the EXPLICIT, NON-SELF-AUTHORIZING mapping from runtime identities
// to a declared architectural boundary. A runtime observation may claim to belong to a boundary, but
// that claim alone is never authority: the observation is admissible only when a binding — authored
// on the architecture side and carrying a distinct authority grant — maps its runtime target and
// identities to this exact boundary, in scope. Traffic cannot mint or authorize its own binding.
type RuntimeArchitectureBinding struct {
	BindingID   string `json:"binding_id" yaml:"binding_id"`
	BoundaryIRI string `json:"boundary_iri" yaml:"boundary_iri"`

	RepositoryIdentity string `json:"repository_identity" yaml:"repository_identity"`
	DomainIdentity     string `json:"domain_identity,omitempty" yaml:"domain_identity,omitempty"`

	// RuntimeTarget is the exact runtime target this binding maps to the boundary. An observation
	// from a different target is not admitted (mirrors evidencereceipt runtime-target matching).
	RuntimeTarget closureprotocol.RuntimeTarget `json:"runtime_target" yaml:"runtime_target"`

	// Explicit runtime→architecture identity mappings. An observation whose caller/callee do not map
	// through these is not admitted for this boundary.
	MappedCallers []string `json:"mapped_callers,omitempty" yaml:"mapped_callers,omitempty"`
	MappedCallees []string `json:"mapped_callees,omitempty" yaml:"mapped_callees,omitempty"`

	// AuthorityGrantIdentity is the SEPARATE authority that established this binding. It must be
	// present and distinct from any runtime identity — the binding is authorized by architecture,
	// not by the observation it admits (the "claim ≠ authority" separation).
	AuthorityGrantIdentity string `json:"authority_grant_identity" yaml:"authority_grant_identity"`

	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// BuildRuntimeArchitectureBinding canonicalizes and stamps the self-excluding digest.
func BuildRuntimeArchitectureBinding(b RuntimeArchitectureBinding) (RuntimeArchitectureBinding, error) {
	b.MappedCallers = sortedUnique(b.MappedCallers)
	b.MappedCallees = sortedUnique(b.MappedCallees)
	if err := validateBindingFields(b); err != nil {
		return RuntimeArchitectureBinding{}, err
	}
	b.DigestSHA256 = ""
	dig, err := digestOf(b)
	if err != nil {
		return RuntimeArchitectureBinding{}, err
	}
	b.DigestSHA256 = dig
	return b, nil
}

func validateBindingFields(b RuntimeArchitectureBinding) error {
	for name, v := range map[string]string{
		"binding_id":          b.BindingID,
		"boundary_iri":        b.BoundaryIRI,
		"repository_identity": b.RepositoryIdentity,
		// The grant is mandatory: a binding with no separate authority is self-authorizing and refused.
		"authority_grant_identity": b.AuthorityGrantIdentity,
	} {
		if !trimmedNonEmpty(v) {
			return fmt.Errorf("binding missing or padded %s", name)
		}
	}
	return nil
}

// ValidateRuntimeArchitectureBinding verifies canonical lists and the self-excluding digest.
func ValidateRuntimeArchitectureBinding(b RuntimeArchitectureBinding) error {
	if err := validateBindingFields(b); err != nil {
		return err
	}
	if !equalStrings(b.MappedCallers, sortedUnique(b.MappedCallers)) ||
		!equalStrings(b.MappedCallees, sortedUnique(b.MappedCallees)) {
		return fmt.Errorf("binding %q mapped lists are not canonical (sorted+unique)", b.BindingID)
	}
	c := b
	c.DigestSHA256 = ""
	want, err := digestOf(c)
	if err != nil {
		return err
	}
	if b.DigestSHA256 != want {
		return fmt.Errorf("binding %q digest mismatch", b.BindingID)
	}
	return nil
}

// runtimeTargetMatches reports whether an observation's runtime target is admitted by the binding's
// target: platform must match, and any target field the binding pins must match exactly. An empty
// binding field is a wildcard; an observation cannot broaden a pinned field.
func runtimeTargetMatches(bind, obs closureprotocol.RuntimeTarget) bool {
	if bind.Platform != "" && bind.Platform != obs.Platform {
		return false
	}
	if bind.EnvironmentID != "" && bind.EnvironmentID != obs.EnvironmentID {
		return false
	}
	if bind.DeploymentID != "" && bind.DeploymentID != obs.DeploymentID {
		return false
	}
	if bind.ReleaseRevision != "" && bind.ReleaseRevision != obs.ReleaseRevision {
		return false
	}
	if bind.ConfigurationGeneration != "" && bind.ConfigurationGeneration != obs.ConfigurationGeneration {
		return false
	}
	return true
}

// bindingScopes reports whether the binding is in scope for the identity: same boundary IRI, same
// repository, and (when the binding pins a domain) same domain. A cross-repository or cross-domain
// binding does not admit evidence to this boundary.
func bindingScopes(b RuntimeArchitectureBinding, id RuntimeBoundaryIdentity) bool {
	if b.BoundaryIRI != id.BoundaryIRI {
		return false
	}
	if b.RepositoryIdentity != id.RepositoryIdentity {
		return false
	}
	if b.DomainIdentity != "" && id.DomainIdentity != "" && b.DomainIdentity != id.DomainIdentity {
		return false
	}
	return true
}
