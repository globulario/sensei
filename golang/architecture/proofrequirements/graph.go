// SPDX-License-Identifier: AGPL-3.0-only

package proofrequirements

import (
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closure"
)

// Requirement origins — the authoritative source that contributed a requirement.
const (
	OriginAuthorityResolution        = "authority_resolution"
	OriginAdmission                  = "admission"
	OriginRepositoryAuthoritySurface = "repository_authority_surface"
	OriginResultGraph                = "result_graph"
	OriginClosure                    = "closure_assessment"
	OriginArchitectQuestions         = "architect_questions"
)

// Graph node classes the scoped projection recognizes. The strings match the
// governed graph vocabulary; classification is case-insensitive.
const (
	ClassProofObligation = "ProofObligation"
	ClassProofSlot       = "ProofSlot"
	ClassTest            = "Test"
	ClassRuntimeEvidence = "RuntimeEvidence"
	ClassEvidence        = "Evidence"
	ClassForbiddenFix    = "ForbiddenFix"
)

// HasAnyClass reports whether a node carries any of the given classes
// (case-insensitive). It is the single shared classification predicate used by
// both the admission guidance projection and the result-pipeline proof
// composition, so the two can never disagree on what a node is.
func HasAnyClass(n closure.Node, classes ...string) bool {
	want := map[string]bool{}
	for _, c := range classes {
		want[strings.ToLower(c)] = true
	}
	for _, c := range n.Classes {
		if want[strings.ToLower(c)] {
			return true
		}
	}
	return false
}

// NodeByReceipt resolves one closure node receipt against a graph index.
func NodeByReceipt(graph closure.GraphIndex, nr closure.NodeReceipt) (closure.Node, bool) {
	if nr.IRI != "" {
		n, ok := graph.Nodes[nr.IRI]
		return n, ok
	}
	if iri := graph.NodesByID[nr.ID]; iri != "" {
		n, ok := graph.Nodes[iri]
		return n, ok
	}
	return closure.Node{}, false
}

// ResolveScopedNodes resolves the closure report's relevant-node receipts against
// the graph, in receipt order, skipping any that do not resolve. It is the single
// shared definition of "the graph nodes represented by the exact result closure
// scope", so admission and the proof composer project from the identical node
// set. It never scans the whole graph.
func ResolveScopedNodes(report closure.Report, graph closure.GraphIndex) []closure.Node {
	out := make([]closure.Node, 0, len(report.RelevantNodes))
	for _, nr := range report.RelevantNodes {
		if n, ok := NodeByReceipt(graph, nr); ok {
			out = append(out, n)
		}
	}
	return out
}

// Requirement is a neutral, normalizable proof requirement.
type Requirement struct {
	Class            string   `json:"class" yaml:"class"`
	ID               string   `json:"id" yaml:"id"`
	Origins          []string `json:"origins" yaml:"origins"`
	SourceIDs        []string `json:"source_ids,omitempty" yaml:"source_ids,omitempty"`
	Status           string   `json:"status,omitempty" yaml:"status,omitempty"`
	DefinitionStatus string   `json:"definition_status,omitempty" yaml:"definition_status,omitempty"`
	EvidenceLane     string   `json:"evidence_lane,omitempty" yaml:"evidence_lane,omitempty"`
	RequiredSlotIDs  []string `json:"required_slot_ids,omitempty" yaml:"required_slot_ids,omitempty"`
	Detail           []string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// ObligationRequirement is a structured proof obligation (never flattened).
type ObligationRequirement struct {
	ID              string   `json:"id" yaml:"id"`
	Label           string   `json:"label,omitempty" yaml:"label,omitempty"`
	EvidenceLane    string   `json:"evidence_lane,omitempty" yaml:"evidence_lane,omitempty"`
	TemplateKind    string   `json:"template_kind,omitempty" yaml:"template_kind,omitempty"`
	RequiredSlotIDs []string `json:"required_slot_ids,omitempty" yaml:"required_slot_ids,omitempty"`
	Origins         []string `json:"origins" yaml:"origins"`
	SourceIDs       []string `json:"source_ids,omitempty" yaml:"source_ids,omitempty"`
	Notes           []string `json:"notes,omitempty" yaml:"notes,omitempty"`
}

// GraphProjection is the scoped-graph contribution to proof requirements.
type GraphProjection struct {
	Obligations             []ObligationRequirement
	RequiredSlots           []Requirement
	RequiredTests           []Requirement
	RuntimeEvidenceProfiles []Requirement
	ForbiddenMoves          []Requirement
}

// ProjectScopedGraph projects proof requirements from the graph nodes represented
// by the exact result closure scope. Closure blockers are NOT projected here — a
// forbidden move is a governed ForbiddenFix or an explicit forbid relation, never
// a closure blocker. The caller composes closure blockers separately.
func ProjectScopedGraph(report closure.Report, graph closure.GraphIndex) (GraphProjection, error) {
	nodes := ResolveScopedNodes(report, graph)
	var proj GraphProjection

	seenProof := map[string]bool{}
	seenTest := map[string]bool{}
	for _, n := range nodes {
		if HasAnyClass(n, ClassProofObligation, ClassProofSlot) && !seenProof[n.ID] {
			seenProof[n.ID] = true
			proj.Obligations = append(proj.Obligations, ObligationRequirement{
				ID: n.ID, Label: firstNonEmpty(n.Label, n.Comment), EvidenceLane: n.Kind,
				RequiredSlotIDs: cleanStrings(n.DependsOn), Origins: []string{OriginResultGraph}, SourceIDs: cleanStrings([]string{n.IRI}),
			})
			if HasAnyClass(n, ClassProofSlot) {
				proj.RequiredSlots = append(proj.RequiredSlots, Requirement{
					Class: "ProofSlot", ID: n.ID, Origins: []string{OriginResultGraph},
					Status: "pending", DefinitionStatus: "defined", EvidenceLane: n.Kind, SourceIDs: cleanStrings([]string{n.IRI}),
				})
			}
		}
		for _, id := range n.RequiresTests {
			if id = strings.TrimSpace(id); id != "" && !seenTest[id] {
				seenTest[id] = true
				proj.RequiredTests = append(proj.RequiredTests, Requirement{Class: "Test", ID: id, Origins: []string{OriginResultGraph}, SourceIDs: []string{n.ID}})
			}
		}
		if HasAnyClass(n, ClassTest) && !seenTest[n.ID] {
			seenTest[n.ID] = true
			proj.RequiredTests = append(proj.RequiredTests, Requirement{Class: "Test", ID: n.ID, Origins: []string{OriginResultGraph}, SourceIDs: cleanStrings([]string{n.IRI})})
		}
		if HasAnyClass(n, ClassRuntimeEvidence, ClassEvidence) {
			proj.RuntimeEvidenceProfiles = append(proj.RuntimeEvidenceProfiles, Requirement{
				Class: "RuntimeEvidence", ID: n.ID, Origins: []string{OriginResultGraph}, Status: "pending",
				EvidenceLane: n.Kind, SourceIDs: cleanStrings([]string{n.IRI}),
			})
		}
		if HasAnyClass(n, ClassForbiddenFix) || len(n.Forbids)+len(n.ForbidsBypass) > 0 {
			proj.ForbiddenMoves = append(proj.ForbiddenMoves, Requirement{
				Class: "ForbiddenFix", ID: n.ID, Origins: []string{OriginResultGraph},
				SourceIDs: cleanStrings([]string{n.IRI}), Detail: cleanStrings(append(append([]string{}, n.Forbids...), n.ForbidsBypass...)),
			})
		}
	}
	sortRequirements(proj.RequiredSlots)
	sortRequirements(proj.RequiredTests)
	sortRequirements(proj.RuntimeEvidenceProfiles)
	sortRequirements(proj.ForbiddenMoves)
	sort.SliceStable(proj.Obligations, func(i, j int) bool { return proj.Obligations[i].ID < proj.Obligations[j].ID })
	return proj, nil
}

func sortRequirements(in []Requirement) {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Class != in[j].Class {
			return in[i].Class < in[j].Class
		}
		return in[i].ID < in[j].ID
	})
}

func cleanStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
