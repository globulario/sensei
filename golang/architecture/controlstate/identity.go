// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"fmt"
	"strings"
)

// ArtifactIdentity is the exact, non-inferrable identity of an architectural artifact. No label,
// graph position, edge count, or client-selected class establishes it.
type ArtifactIdentity struct {
	NodeIRI                string   `json:"node_iri" yaml:"node_iri"`
	CanonicalClass         string   `json:"canonical_class" yaml:"canonical_class"`
	ObservedClasses        []string `json:"observed_classes" yaml:"observed_classes"`
	RepositoryIdentity     string   `json:"repository_identity" yaml:"repository_identity"`
	DomainIdentity         string   `json:"domain_identity,omitempty" yaml:"domain_identity,omitempty"`
	GraphAuthorityIdentity string   `json:"graph_authority_identity" yaml:"graph_authority_identity"`
	ProvenanceIdentities   []string `json:"provenance_identities,omitempty" yaml:"provenance_identities,omitempty"`
}

// ClassResolution is the outcome of canonical class resolution for a possibly multi-typed node.
type ClassResolution struct {
	CanonicalClass string `json:"canonical_class" yaml:"canonical_class"`
	Ambiguous      bool   `json:"ambiguous" yaml:"ambiguous"`
	Unknown        bool   `json:"unknown" yaml:"unknown"`
	ReasonCode     string `json:"reason_code,omitempty" yaml:"reason_code,omitempty"`
}

// ResolveCanonicalClass resolves a canonical class from a node's OBSERVED class IRIs, using
// explicit registry policy only — never first-seen graph order, label, or query class. If no
// observed class is known → unclassified (unknown). If the known observed classes are mutually
// compatible → the lowest-precedence (most specific) wins. If any incompatible pair remains →
// unclassified (ambiguous).
func (r Registry) ResolveCanonicalClass(observed []string) ClassResolution {
	unclassified := r.unclassifiedPolicy().ClassIRI
	var known []ClassPolicy
	for _, iri := range observed {
		if p, ok := r.classByIRI(iri); ok && !p.Unclassified {
			known = append(known, p)
		}
	}
	if len(known) == 0 {
		return ClassResolution{CanonicalClass: unclassified, Unknown: true, ReasonCode: "unknown_class"}
	}
	// Pairwise compatibility among the known observed classes.
	for i := 0; i < len(known); i++ {
		for j := i + 1; j < len(known); j++ {
			if !classesCompatible(known[i], known[j]) {
				return ClassResolution{CanonicalClass: unclassified, Ambiguous: true, ReasonCode: "artifact_class_ambiguous"}
			}
		}
	}
	// Lowest precedence wins; exact IRI is the deterministic final tie-breaker.
	best := known[0]
	for _, p := range known[1:] {
		if p.Precedence < best.Precedence || (p.Precedence == best.Precedence && p.ClassIRI < best.ClassIRI) {
			best = p
		}
	}
	return ClassResolution{CanonicalClass: best.ClassIRI}
}

// classesCompatible reports whether two known classes may co-occur on one node without ambiguity
// (identical, or one lists the other as a compatible facet/alias).
func classesCompatible(a, b ClassPolicy) bool {
	if a.ClassIRI == b.ClassIRI {
		return true
	}
	for _, al := range a.Aliases {
		if al == b.ClassIRI {
			return true
		}
	}
	for _, al := range b.Aliases {
		if al == a.ClassIRI {
			return true
		}
	}
	return false
}

// BuildArtifactIdentity constructs and validates an artifact identity, resolving the canonical
// class through registry policy. It never errors on an unknown/ambiguous class — the artifact
// stays visible under the unclassified class with a reason recorded on the returned resolution.
func BuildArtifactIdentity(reg Registry, nodeIRI string, observed []string, repo, domain, authorityID string, provenance []string) (ArtifactIdentity, ClassResolution, error) {
	if nodeIRI == "" {
		return ArtifactIdentity{}, ClassResolution{}, fmt.Errorf("artifact node IRI is empty")
	}
	if repo == "" {
		return ArtifactIdentity{}, ClassResolution{}, fmt.Errorf("artifact repository identity is empty")
	}
	if authorityID == "" {
		return ArtifactIdentity{}, ClassResolution{}, fmt.Errorf("artifact graph-authority identity is empty")
	}
	res := reg.ResolveCanonicalClass(observed)
	id := ArtifactIdentity{
		NodeIRI: nodeIRI, CanonicalClass: res.CanonicalClass, ObservedClasses: sortedUnique(observed),
		RepositoryIdentity: repo, DomainIdentity: domain, GraphAuthorityIdentity: authorityID,
		ProvenanceIdentities: sortedUnique(provenance),
	}
	return id, res, nil
}

// ValidateArtifactIdentity recomputes canonical class resolution from the identity's OBSERVED
// classes and requires exact agreement with the supplied identity + resolution. A caller-supplied
// (identity, resolution) pair is never trusted: a fabricated canonical class cannot select an
// assessment policy.
func ValidateArtifactIdentity(reg Registry, id ArtifactIdentity, res ClassResolution) error {
	for name, v := range map[string]string{"node_iri": id.NodeIRI, "repository_identity": id.RepositoryIdentity, "graph_authority_identity": id.GraphAuthorityIdentity} {
		if v == "" {
			return fmt.Errorf("artifact identity missing %s", name)
		}
		if v != strings.TrimSpace(v) {
			return fmt.Errorf("artifact identity %s is padded", name)
		}
	}
	if id.DomainIdentity != strings.TrimSpace(id.DomainIdentity) {
		return fmt.Errorf("artifact domain identity is padded")
	}
	if !equalStrings(id.ObservedClasses, sortedUnique(id.ObservedClasses)) {
		return fmt.Errorf("artifact observed classes are not canonical (sorted+unique)")
	}
	if !equalStrings(id.ProvenanceIdentities, sortedUnique(id.ProvenanceIdentities)) {
		return fmt.Errorf("artifact provenance identities are not canonical (sorted+unique)")
	}
	recomputed := reg.ResolveCanonicalClass(id.ObservedClasses)
	if recomputed.CanonicalClass != id.CanonicalClass {
		return fmt.Errorf("artifact canonical class %q disagrees with registry resolution %q", id.CanonicalClass, recomputed.CanonicalClass)
	}
	if recomputed.CanonicalClass != res.CanonicalClass || recomputed.Ambiguous != res.Ambiguous || recomputed.Unknown != res.Unknown || recomputed.ReasonCode != res.ReasonCode {
		return fmt.Errorf("supplied class resolution disagrees with registry resolution")
	}
	// A known canonical class must be present in the observed classes or justified by a compatible
	// registry facet.
	if p, ok := reg.classByIRI(id.CanonicalClass); ok && !p.Unclassified {
		if !observedContains(id.ObservedClasses, id.CanonicalClass) && !observedHasCompatibleFacet(reg, id.ObservedClasses, p) {
			return fmt.Errorf("canonical class %q is not present or justified in observed classes", id.CanonicalClass)
		}
	}
	return nil
}

func observedContains(observed []string, iri string) bool {
	for _, o := range observed {
		if o == iri {
			return true
		}
	}
	return false
}

func observedHasCompatibleFacet(reg Registry, observed []string, canonical ClassPolicy) bool {
	for _, o := range observed {
		if p, ok := reg.classByIRI(o); ok && classesCompatible(canonical, p) {
			return true
		}
	}
	return false
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
