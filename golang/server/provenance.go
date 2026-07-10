// SPDX-License-Identifier: AGPL-3.0-only

// Provenance rendering — the chain of custody for a promoted repo-scoped rule.
//
// A rule that entered the graph through the cold-source pilot path carries
// aw:provenance* / aw:repo / aw:origin / aw:reviewLabel facts. This file reifies
// those facts (per node) and renders them compactly so a briefing can EXPLAIN
// why a foreign rule should be trusted — repo, origin, review label, the bundle
// and commit range it was drawn from, and the citations that support it.
//
// Provenance explains trust; it grants no authority and is read by no filter.
// Rendering is deliberately bounded (citations capped) so it never floods a
// briefing.
package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// maxCitationsShown bounds how many citations a provenance line prints; the
// remainder are summarized as "(+N more)".
const maxCitationsShown = 3

// nodeProvenance is the unpacked provenance receipt for one node. A node with no
// provenance facts yields the zero value, for which has() is false.
type nodeProvenance struct {
	Repo        string
	Domain      string
	Origin      string
	ReviewLabel string
	BundleID    string
	CommitRange string
	Citations   []string
}

// has reports whether this node carries any provenance worth rendering. A node
// merely tagged with a domain but lacking origin/bundle/review is not treated as
// "promoted provenance" — there is nothing to explain.
func (p nodeProvenance) has() bool {
	return p.Origin != "" || p.ReviewLabel != "" || p.BundleID != "" ||
		p.CommitRange != "" || len(p.Citations) > 0
}

// applyProvenanceFact folds one fact into the provenance receipt. Unrelated
// predicates are ignored.
func applyProvenanceFact(p *nodeProvenance, predicate, object string) {
	switch predicate {
	case rdf.PropRepo:
		p.Repo = object
	case rdf.PropDomain:
		p.Domain = object
	case rdf.PropOrigin:
		p.Origin = object
	case rdf.PropReviewLabel:
		p.ReviewLabel = object
	case rdf.PropProvenanceBundleID:
		p.BundleID = object
	case rdf.PropProvenanceCommitRange:
		p.CommitRange = object
	case rdf.PropProvenanceCitation:
		if s := strings.TrimSpace(object); s != "" {
			p.Citations = append(p.Citations, s)
		}
	}
}

// provenanceFromTriples builds a receipt from a node's Describe triples (used by
// Resolve and by EditCheck's detect-rule reification).
func provenanceFromTriples(triples []store.Triple) nodeProvenance {
	var p nodeProvenance
	for _, t := range triples {
		applyProvenanceFact(&p, t.Predicate, t.Object)
	}
	sort.Strings(p.Citations)
	return p
}

// oneLine renders a compact single-line provenance summary, e.g.
//
//	repo github.com/caddyserver/caddy · origin coldsource · review load-bearing · bundle X · range Y · cites: a; b
//
// Empty when there is nothing to explain.
func (p nodeProvenance) oneLine() string {
	if !p.has() {
		return ""
	}
	var parts []string
	if p.Repo != "" {
		parts = append(parts, "repo "+p.Repo)
	}
	if p.Origin != "" {
		parts = append(parts, "origin "+p.Origin)
	}
	if p.ReviewLabel != "" {
		parts = append(parts, "review "+p.ReviewLabel)
	}
	if p.BundleID != "" {
		parts = append(parts, "bundle "+p.BundleID)
	}
	if p.CommitRange != "" {
		parts = append(parts, "range "+p.CommitRange)
	}
	if c := p.renderCitations(); c != "" {
		parts = append(parts, "cites: "+c)
	}
	return strings.Join(parts, " · ")
}

// renderCitations renders up to maxCitationsShown citations, summarizing the
// rest, so provenance never floods the briefing.
func (p nodeProvenance) renderCitations() string {
	if len(p.Citations) == 0 {
		return ""
	}
	shown := p.Citations
	extra := 0
	if len(shown) > maxCitationsShown {
		extra = len(shown) - maxCitationsShown
		shown = shown[:maxCitationsShown]
	}
	out := strings.Join(shown, "; ")
	if extra > 0 {
		out += fmt.Sprintf(" (+%d more)", extra)
	}
	return out
}

// provenanceBriefingSection renders a compact multi-line provenance block for a
// briefing, one entry per in-scope node that carries provenance. Returns "" when
// no in-scope node is provenanced (i.e. for every untagged Globular briefing).
func provenanceBriefingSection(idToProv map[string]nodeProvenance) string {
	var keys []string
	for id, p := range idToProv {
		if p.has() {
			keys = append(keys, id)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("\nProvenance (promoted repo-scoped rules):\n")
	for _, id := range keys {
		b.WriteString("- " + id + ": " + idToProv[id].oneLine() + "\n")
	}
	return b.String()
}
