// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.impact
// @awareness implements=globular.awareness_graph:intent.awareness.impact_distinguishes_direct_and_inferred
// @awareness enforces=globular.awareness_graph:invariant.awareness.store_unavailable_explicit
// @awareness tested_by=golang/server/package_inference_test.go:TestPackageInference_SiblingAnchorsAreInferredNotDirect
package main

import (
	"context"
	"path"
	"sort"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// packageInference is the outcome of one package walk.
//
// Unavailable is carried explicitly rather than collapsed into an empty result:
// "this package has no other governed files" and "the walk could not run" are
// different worlds, and a reader who cannot tell them apart will treat silence
// as safety.
type packageInference struct {
	nodes map[string]*awarenesspb.KnowledgeNode // node IRI -> node
	class map[string]string                     // node IRI -> awareness class
	from  map[string]string                     // node IRI -> repo-relative sibling file it came from

	Unavailable bool
	Reason      string
}

// inferPackageAnchors finds anchors carried by OTHER files in the same package
// directory, excluding anything already anchored directly to this file.
//
// Why this exists: anchoring is per-file, so a package whose contract is
// recorded on one file tells an agent nothing when it opens a sibling. On this
// repository that was four files in five — the guardrail fired, the briefing
// came back generic, and the agent learned the ritual was empty.
//
// What it deliberately does NOT do is promote these to direct anchors. A
// neighbour's invariant is evidence that this file sits in governed territory,
// not proof that the invariant binds this file. The caller places them in the
// inferred_* fields, which the proto has reserved for exactly this walk.
func (s *server) inferPackageAnchors(ctx context.Context, file string, direct map[string]bool, scope string) packageInference {
	out := packageInference{
		nodes: map[string]*awarenesspb.KnowledgeNode{},
		class: map[string]string{},
		from:  map[string]string{},
	}

	pkgStore, ok := s.store.(store.PackageAnchorStore)
	if !ok {
		out.Unavailable = true
		out.Reason = "backend does not support package-level anchor queries"
		return out
	}

	// file arrives already slash-normalized from the RPC boundary.
	dir := path.Dir(file)
	if dir == "." || dir == "/" || dir == "" {
		// A file at the repository root has no package to inherit from. This is
		// a real answer, not a failure.
		return out
	}

	facts, err := pkgStore.ImpactForPackage(ctx, mintedIRI(rdf.ClassSourceFile, dir+"/"))
	if err != nil {
		// Additive enrichment must not sink the direct architectural answer, so
		// the error does not propagate — but it is reported, so an empty
		// inferred section is never mistaken for "the package is ungoverned".
		out.Unavailable = true
		out.Reason = "package anchor query failed: " + err.Error()
		return out
	}

	domain := map[string]string{} // node IRI -> resolved domain
	for _, f := range facts {
		sibling, ok := repoPathFromSourceFileIRI(f.SourceFileIRI)
		if !ok {
			continue
		}
		// The prefix filter is coarse because path separators are encoded;
		// narrow to the EXACT package here so a nested directory's rules do not
		// climb into this file's context.
		if path.Dir(sibling) != dir || sibling == file {
			continue
		}
		class, ok := classFromTypeIRI(f.TypeIRI)
		if !ok {
			continue
		}
		if direct[f.NodeIRI] {
			continue // already stated as a direct anchor; inferring it again is noise
		}
		n, exists := out.nodes[f.NodeIRI]
		if !exists {
			id, ok := awarenessIDFromIRI(f.NodeIRI)
			if !ok {
				continue
			}
			n = &awarenesspb.KnowledgeNode{Iri: f.NodeIRI, Id: id, Class: class}
			out.nodes[f.NodeIRI] = n
			out.class[f.NodeIRI] = class
			out.from[f.NodeIRI] = sibling
			domain[f.NodeIRI] = s.homeDomain
		} else if sibling < out.from[f.NodeIRI] {
			// Deterministic attribution: when several siblings carry the same
			// node, always name the same one.
			out.from[f.NodeIRI] = sibling
		}
		switch f.Predicate {
		case rdf.PropRepo:
			if f.Object != "" {
				domain[f.NodeIRI] = f.Object
			}
		case rdf.PropDomain:
			if f.Object == rdf.DomainShared {
				domain[f.NodeIRI] = rdf.DomainShared
			}
		}
		applyNodeFact(n, store.Triple{Predicate: f.Predicate, Object: f.Object, ObjectIsIRI: f.ObjectIsIRI})
	}

	// Same domain scope as every other briefing section. A neighbour's rule
	// from another repo is exactly the leak the scoping invariant forbids, and
	// arriving by inference does not make it admissible.
	for iri := range out.nodes {
		if !InScope(domain[iri], scope) {
			delete(out.nodes, iri)
			delete(out.class, iri)
			delete(out.from, iri)
		}
	}
	return out
}

// apply writes the inferred nodes into the reserved inferred_* response fields,
// sorted for determinism.
func (p packageInference) apply(resp *awarenesspb.ImpactResponse, cap int) {
	iris := make([]string, 0, len(p.nodes))
	for iri := range p.nodes {
		iris = append(iris, iri)
	}
	sort.Strings(iris)

	for _, iri := range iris {
		n := p.nodes[iri]
		switch p.class[iri] {
		case "invariant":
			resp.InferredInvariants = append(resp.InferredInvariants, n)
		case "failure_mode":
			resp.InferredFailureModes = append(resp.InferredFailureModes, n)
		case "incident_pattern":
			resp.InferredIncidentPatterns = append(resp.InferredIncidentPatterns, n)
		case "intent":
			resp.InferredIntents = append(resp.InferredIntents, n)
		}
	}
	sortKnowledgeNodes(resp.InferredInvariants)
	sortKnowledgeNodes(resp.InferredFailureModes)
	sortKnowledgeNodes(resp.InferredIncidentPatterns)
	sortKnowledgeNodes(resp.InferredIntents)

	if cap > 0 {
		resp.InferredInvariants = resp.InferredInvariants[:min(len(resp.InferredInvariants), cap)]
		resp.InferredFailureModes = resp.InferredFailureModes[:min(len(resp.InferredFailureModes), cap)]
		resp.InferredIncidentPatterns = resp.InferredIncidentPatterns[:min(len(resp.InferredIncidentPatterns), cap)]
		resp.InferredIntents = resp.InferredIntents[:min(len(resp.InferredIntents), cap)]
	}
}

// empty reports whether the walk produced nothing to say.
func (p packageInference) empty() bool { return len(p.nodes) == 0 }

// repoPathFromSourceFileIRI recovers the repo-relative path from a SourceFile
// IRI of either identity generation -- the repository-scoped one and the
// unscoped one it replaced (issue #197).
func repoPathFromSourceFileIRI(iri string) (string, bool) {
	return rdf.SourceFilePathFromIRI(iri)
}
