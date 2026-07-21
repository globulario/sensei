// SPDX-License-Identifier: AGPL-3.0-only

package runtimeboundary

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

// spinePeerClasses are the architectural-spine classes that conflict with Boundary: a node typed as
// both a Boundary and one of these is ambiguous and cannot be assessed as a runtime boundary. The
// canonical class is resolved ONLY from observed rdf:type IRIs, never from a caller-selected class.
var spinePeerClasses = map[string]bool{
	rdf.ClassComponent:     true,
	rdf.ClassContract:      true,
	rdf.ClassDecision:      true,
	rdf.ClassInvariant:     true,
	rdf.ClassEvidence:      true,
	rdf.ClassMetaPrinciple: true,
}

// BoundaryClassResolution is the outcome of resolving whether an observed node is a runtime-
// assessable boundary. Unknown/ambiguous stay visible but never yield a compliant verdict.
type BoundaryClassResolution struct {
	CanonicalClass string `json:"canonical_class" yaml:"canonical_class"`
	Ambiguous      bool   `json:"ambiguous" yaml:"ambiguous"`
	Unknown        bool   `json:"unknown" yaml:"unknown"`
	ReasonCode     string `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
}

// resolveBoundaryClass resolves the canonical class from OBSERVED rdf:type IRIs. A boundary is
// canonical only when aw:Boundary is observed and no conflicting spine class co-occurs.
func resolveBoundaryClass(observed []string) BoundaryClassResolution {
	hasBoundary := false
	conflicting := false
	for _, iri := range observed {
		if iri == rdf.ClassBoundary {
			hasBoundary = true
		} else if spinePeerClasses[iri] {
			conflicting = true
		}
	}
	switch {
	case !hasBoundary:
		return BoundaryClassResolution{Unknown: true, ReasonCode: "not_a_boundary"}
	case conflicting:
		return BoundaryClassResolution{Ambiguous: true, ReasonCode: "boundary_class_ambiguous"}
	default:
		return BoundaryClassResolution{CanonicalClass: rdf.ClassBoundary}
	}
}

// RuntimeBoundaryIdentity is the exact, non-inferrable identity of a runtime-assessable boundary.
// No label, graph position, edge count, or caller-selected class establishes it — the canonical
// class is resolved only from observed rdf:type IRIs. A boundary yields a positive verdict only when
// its identity is current (AuthorityCurrent), integrity-verified (IntegrityVerified), and explicitly
// RuntimeAssessable; otherwise it stays visible but cannot be satisfied.
type RuntimeBoundaryIdentity struct {
	BoundaryIRI     string   `json:"boundary_iri" yaml:"boundary_iri"`
	CanonicalClass  string   `json:"canonical_class" yaml:"canonical_class"`
	ObservedClasses []string `json:"observed_classes" yaml:"observed_classes"`
	BoundaryKind    string   `json:"boundary_kind,omitempty" yaml:"boundary_kind,omitempty"`

	RepositoryIdentity     string `json:"repository_identity" yaml:"repository_identity"`
	DomainIdentity         string `json:"domain_identity,omitempty" yaml:"domain_identity,omitempty"`
	GraphAuthorityIdentity string `json:"graph_authority_identity" yaml:"graph_authority_identity"`
	RegistryIdentity       string `json:"registry_identity,omitempty" yaml:"registry_identity,omitempty"`

	// Protected architecture the crossing must respect (edge targets; never inferred from traffic).
	ProtectedContracts  []string `json:"protected_contracts,omitempty" yaml:"protected_contracts,omitempty"`
	ProtectedInvariants []string `json:"protected_invariants,omitempty" yaml:"protected_invariants,omitempty"`
	// Source/destination components or authority domains the boundary separates.
	SourceIdentity      string `json:"source_identity,omitempty" yaml:"source_identity,omitempty"`
	DestinationIdentity string `json:"destination_identity,omitempty" yaml:"destination_identity,omitempty"`

	OwningAuthority string         `json:"owning_authority,omitempty" yaml:"owning_authority,omitempty"`
	Lifecycle       LifecycleState `json:"lifecycle" yaml:"lifecycle"`

	// The three gates a positive verdict requires. They are asserted by the resolving consumer from
	// graph authority; the owner never self-authorizes them from an observation.
	RuntimeAssessable bool `json:"runtime_assessable" yaml:"runtime_assessable"`
	IntegrityVerified bool `json:"integrity_verified" yaml:"integrity_verified"`
	AuthorityCurrent  bool `json:"authority_current" yaml:"authority_current"`

	DigestSHA256 string `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

// BuildRuntimeBoundaryIdentity constructs and validates a boundary identity, resolving the canonical
// class from observed classes. It requires a non-empty boundary IRI, repository, and graph-authority
// identity. It never errors on unknown/ambiguous class — the boundary stays visible with the reason
// on the returned resolution, but such a boundary cannot be satisfied.
func BuildRuntimeBoundaryIdentity(
	boundaryIRI string, observed []string, kind, repo, domain, authorityID, registryID string,
	protectedContracts, protectedInvariants []string, source, destination, owningAuthority string,
	lifecycle LifecycleState, runtimeAssessable, integrityVerified, authorityCurrent bool,
) (RuntimeBoundaryIdentity, BoundaryClassResolution, error) {
	if !trimmedNonEmpty(boundaryIRI) {
		return RuntimeBoundaryIdentity{}, BoundaryClassResolution{}, fmt.Errorf("boundary IRI is empty or padded")
	}
	if !trimmedNonEmpty(repo) {
		return RuntimeBoundaryIdentity{}, BoundaryClassResolution{}, fmt.Errorf("boundary repository identity is empty or padded")
	}
	if !trimmedNonEmpty(authorityID) {
		return RuntimeBoundaryIdentity{}, BoundaryClassResolution{}, fmt.Errorf("boundary graph-authority identity is empty or padded")
	}
	if lifecycle == "" {
		lifecycle = LifecycleUnknown
	}
	if !validLifecycle(lifecycle) {
		return RuntimeBoundaryIdentity{}, BoundaryClassResolution{}, fmt.Errorf("boundary lifecycle %q is off-vocabulary", lifecycle)
	}
	res := resolveBoundaryClass(observed)
	id := RuntimeBoundaryIdentity{
		BoundaryIRI: boundaryIRI, CanonicalClass: res.CanonicalClass, ObservedClasses: sortedUnique(observed),
		BoundaryKind: kind, RepositoryIdentity: repo, DomainIdentity: domain,
		GraphAuthorityIdentity: authorityID, RegistryIdentity: registryID,
		ProtectedContracts: sortedUnique(protectedContracts), ProtectedInvariants: sortedUnique(protectedInvariants),
		SourceIdentity: source, DestinationIdentity: destination, OwningAuthority: owningAuthority,
		Lifecycle: lifecycle, RuntimeAssessable: runtimeAssessable,
		IntegrityVerified: integrityVerified, AuthorityCurrent: authorityCurrent,
	}
	dig, err := id.computeDigest()
	if err != nil {
		return RuntimeBoundaryIdentity{}, BoundaryClassResolution{}, err
	}
	id.DigestSHA256 = dig
	return id, res, nil
}

func (id RuntimeBoundaryIdentity) computeDigest() (string, error) {
	c := id
	c.DigestSHA256 = ""
	return digestOf(c)
}

// Assessable reports whether the identity's three gates permit a positive verdict at all: canonical
// boundary class, current authority, integrity-verified, and explicitly runtime-assessable.
func (id RuntimeBoundaryIdentity) Assessable() bool {
	return id.CanonicalClass == rdf.ClassBoundary && id.AuthorityCurrent &&
		id.IntegrityVerified && id.RuntimeAssessable
}

// ValidateRuntimeBoundaryIdentity recomputes the canonical class from OBSERVED classes and requires
// exact agreement with the supplied identity + resolution, and verifies the self-excluding digest. A
// caller-supplied canonical class is never trusted: a fabricated boundary class cannot make an
// arbitrary node assessable.
func ValidateRuntimeBoundaryIdentity(id RuntimeBoundaryIdentity, res BoundaryClassResolution) error {
	for name, v := range map[string]string{
		"boundary_iri":             id.BoundaryIRI,
		"repository_identity":      id.RepositoryIdentity,
		"graph_authority_identity": id.GraphAuthorityIdentity,
	} {
		if !trimmedNonEmpty(v) {
			return fmt.Errorf("boundary identity missing or padded %s", name)
		}
	}
	if id.DomainIdentity != strings.TrimSpace(id.DomainIdentity) {
		return fmt.Errorf("boundary domain identity is padded")
	}
	if !validLifecycle(id.Lifecycle) {
		return fmt.Errorf("boundary lifecycle %q is off-vocabulary", id.Lifecycle)
	}
	for _, list := range [][]string{id.ObservedClasses, id.ProtectedContracts, id.ProtectedInvariants} {
		if !equalStrings(list, sortedUnique(list)) {
			return fmt.Errorf("boundary identity list is not canonical (sorted+unique)")
		}
	}
	recomputed := resolveBoundaryClass(id.ObservedClasses)
	if recomputed.CanonicalClass != id.CanonicalClass {
		return fmt.Errorf("boundary canonical class %q disagrees with observed-class resolution %q", id.CanonicalClass, recomputed.CanonicalClass)
	}
	if recomputed != res {
		return fmt.Errorf("supplied boundary class resolution disagrees with observed-class resolution")
	}
	// A canonical Boundary class must actually be present in the observed classes.
	if id.CanonicalClass == rdf.ClassBoundary && !containsString(id.ObservedClasses, rdf.ClassBoundary) {
		return fmt.Errorf("canonical Boundary class is not present in observed classes")
	}
	want, err := id.computeDigest()
	if err != nil {
		return err
	}
	if id.DigestSHA256 != want {
		return fmt.Errorf("boundary identity digest mismatch")
	}
	return nil
}

func containsString(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
