// SPDX-License-Identifier: AGPL-3.0-only

// Domain-scope emission for repo-scoped knowledge.
//
// One AWG instance can host many domains: Globular's own self-knowledge (the
// untagged "home" domain) plus foreign repos brought in through the cold-source
// pilot (e.g. caddy). The scope filter in golang/server/scope.go keeps a
// briefing for one domain from ever surfacing another repo's rules — but that
// filter only works if the build actually TAGS each foreign node with its
// domain. This file is the producer side of that contract: it turns the
// domain-scope YAML fields into aw:domain / aw:repo / aw:sourceSet triples (read
// by the scope filter) plus aw:provenance* literals (the chain of custody back
// to the cold-source evidence — read by no filter, they grant no authority).
//
// An untagged entry emits NOTHING here, so every existing home-domain entry
// compiles byte-for-byte as before — the embedded seed is unchanged.
package extractor

import (
	"strings"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// provenance is the receipt of how a promoted rule was earned. All fields are
// optional, but the pilot promotion path requires them so a foreign rule is
// always traceable to its evidence.
type provenance struct {
	BundleID    string   `yaml:"bundle_id"`
	CommitRange string   `yaml:"commit_range"`
	Citations   []string `yaml:"citations"`
	ReviewLabel string   `yaml:"review_label"`
}

// domainScope carries the domain assignment + provenance for a knowledge entry.
// It is embedded (yaml ",inline") into the per-class YAML shapes that may be
// repo-scoped. Absent on an entry → untagged → home domain (default behaviour).
type domainScope struct {
	Repo       string     `yaml:"repo"`       // e.g. "github.com/caddyserver/caddy"
	Domain     string     `yaml:"domain"`     // "repo" | "shared"; inferred "repo" when repo set
	SourceSet  string     `yaml:"source_set"` // namespace within a domain, e.g. "pilot/caddy"
	Origin     string     `yaml:"origin"`     // "coldsource" | "authored" | ...
	Provenance provenance `yaml:"provenance"`
}

// normalizeDomainScope trims fields and infers domain=repo when a repo is named
// but no explicit domain is given. A node that names a repo is, by definition, a
// repo-domain node.
func normalizeDomainScope(ds domainScope) domainScope {
	ds.Repo = strings.TrimSpace(ds.Repo)
	ds.Domain = strings.ToLower(strings.TrimSpace(ds.Domain))
	ds.SourceSet = strings.TrimSpace(ds.SourceSet)
	ds.Origin = strings.TrimSpace(ds.Origin)
	if ds.Domain == "" && ds.Repo != "" {
		ds.Domain = rdf.DomainRepo
	}
	return ds
}

// emitDomainScope writes the domain + provenance triples for one node. It is the
// ONLY place that mints aw:repo / aw:domain on a knowledge node, so the
// scope-filter contract has a single producer to audit.
//
// Emission rules (deliberately fail-quiet, never fail-loud, to keep the build
// total — a malformed scope downgrades to untagged rather than aborting):
//   - domain=shared             → aw:domain "shared"            (portable meta)
//   - domain=repo (repo set)    → aw:domain "repo" + aw:repo R  (repo-scoped)
//   - domain=repo, repo empty   → emit nothing (meaningless; do not mislabel)
//   - untagged                  → emit nothing (home domain)
//
// Provenance / sourceSet / origin are emitted only for tagged nodes; a home
// node carries none of this, so existing seed output is unaffected.
func emitDomainScope(e *rdf.Emitter, subj string, ds domainScope) {
	ds = normalizeDomainScope(ds)
	// Untagged node + an import-wide default → adopt the default scope. This is
	// how a foreign-repo bootstrap scopes domain-agnostic structural extractor
	// output: the import names the repo once and every otherwise-untagged node
	// inherits it. A node with its own inline scope always wins.
	if ds.Domain == "" && ds.Repo == "" && (e.DefaultRepo != "" || e.DefaultDomain != "") {
		ds = normalizeDomainScope(domainScope{
			Repo:      e.DefaultRepo,
			Domain:    e.DefaultDomain,
			SourceSet: e.DefaultSourceSet,
		})
	}
	switch ds.Domain {
	case rdf.DomainShared:
		e.Triple(subj, rdf.IRI(rdf.PropDomain), rdf.Lit(rdf.DomainShared))
	case rdf.DomainRepo:
		if ds.Repo == "" {
			return
		}
		e.Triple(subj, rdf.IRI(rdf.PropDomain), rdf.Lit(rdf.DomainRepo))
		e.Triple(subj, rdf.IRI(rdf.PropRepo), rdf.Lit(ds.Repo))
	default:
		return // untagged → home domain; nothing to emit
	}

	if ds.SourceSet != "" {
		e.Triple(subj, rdf.IRI(rdf.PropSourceSet), rdf.Lit(ds.SourceSet))
	}
	if ds.Origin != "" {
		e.Triple(subj, rdf.IRI(rdf.PropOrigin), rdf.Lit(ds.Origin))
	}
	p := ds.Provenance
	if s := strings.TrimSpace(p.BundleID); s != "" {
		e.Triple(subj, rdf.IRI(rdf.PropProvenanceBundleID), rdf.Lit(s))
	}
	if s := strings.TrimSpace(p.CommitRange); s != "" {
		e.Triple(subj, rdf.IRI(rdf.PropProvenanceCommitRange), rdf.Lit(s))
	}
	for _, c := range p.Citations {
		if s := strings.TrimSpace(c); s != "" {
			e.Triple(subj, rdf.IRI(rdf.PropProvenanceCitation), rdf.Lit(s))
		}
	}
	if s := strings.TrimSpace(p.ReviewLabel); s != "" {
		e.Triple(subj, rdf.IRI(rdf.PropReviewLabel), rdf.Lit(s))
	}
}
