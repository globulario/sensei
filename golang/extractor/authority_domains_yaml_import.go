// SPDX-License-Identifier: AGPL-3.0-only

// Importer for AuthorityDomain YAML files (Phase 3).
//
// An AuthorityDomain makes state ownership queryable: who owns this state,
// who may write/read it, which path mutations must flow through, which
// shortcuts are forbidden, and how fresh evidence about it must be. The four
// incident families this models are authority bugs — wrong writer, wrong
// reader, object presence mistaken for metadata truth, runtime observation
// mistaken for desired state.
//
// v1 flattening: each domain is ONE AuthorityDomain node carrying literals
// (same shape as ImplementationPattern). Preflight matches a touched file
// against aw:coversPath prefixes and surfaces the domain's fields — no
// graph traversal needed.
//
// Schema (top-level key `authority_domains`, list of domains):
//
//	authority_domains:
//	  - id:            authority.<slug>
//	    label:         Human-readable title
//	    status:        active | draft | deprecated
//	    truth_layer:   repository | desired | installed | runtime
//	    owner_service: service that owns the state
//	    covers_paths:        []repo-relative path prefixes (file → domain matching)
//	    owns_state:          []owned state object names
//	    may_write:           []allowed writers
//	    may_read:            []allowed readers
//	    must_mutate_via:     []legal mutation paths
//	    must_read_via:       []legal read paths
//	    observes_via:        []legal observation paths
//	    forbids_bypass:      []named illegal shortcuts
//	    evidence_freshness:  freshness requirement (string)
//	    notes:               free text → rdfs:comment
//
// Empty id is a soft skip — the entry produces no triples but the file
// does not error.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/rdf"
)

type yamlAuthorityDomain struct {
	ID                string   `yaml:"id"`
	Label             string   `yaml:"label"`
	Status            string   `yaml:"status"`
	TruthLayer        string   `yaml:"truth_layer"`
	OwnerService      string   `yaml:"owner_service"`
	CoversPaths       []string `yaml:"covers_paths"`
	OwnsState         []string `yaml:"owns_state"`
	MayWrite          []string `yaml:"may_write"`
	MayRead           []string `yaml:"may_read"`
	MustMutateVia     []string `yaml:"must_mutate_via"`
	MustReadVia       []string `yaml:"must_read_via"`
	ObservesVia       []string `yaml:"observes_via"`
	ForbidsBypass     []string `yaml:"forbids_bypass"`
	EvidenceFreshness string   `yaml:"evidence_freshness"`
	Notes             string   `yaml:"notes"`
}

type yamlAuthorityDomainsDoc struct {
	AuthorityDomains []yamlAuthorityDomain `yaml:"authority_domains"`
}

// importAuthorityDomains imports a YAML file carrying the authority_domains
// top-level list. One AuthorityDomain node per entry.
func importAuthorityDomains(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var doc yamlAuthorityDomainsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}

	for _, d := range doc.AuthorityDomains {
		if d.ID == "" {
			continue // soft skip — nameless entries cannot be queried
		}
		subj := rdf.MintIRI(rdf.ClassAuthorityDomain, d.ID)
		e.Typed(subj, rdf.ClassAuthorityDomain)

		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(d.Label, d.ID)))
		emitOptLit(e, subj, rdf.PropStatus, d.Status)
		emitOptLit(e, subj, rdf.PropHasTruthLayer, d.TruthLayer)
		emitOptLit(e, subj, rdf.PropOwnerService, d.OwnerService)
		emitOptLit(e, subj, rdf.PropHasEvidenceFreshnessWindow, d.EvidenceFreshness)
		if notes := strings.TrimSpace(d.Notes); notes != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(notes))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitOptLits(e, subj, rdf.PropCoversPath, d.CoversPaths)
		emitOptLits(e, subj, rdf.PropOwnsState, d.OwnsState)
		emitOptLits(e, subj, rdf.PropMayWrite, d.MayWrite)
		emitOptLits(e, subj, rdf.PropMayRead, d.MayRead)
		emitOptLits(e, subj, rdf.PropMustMutateVia, d.MustMutateVia)
		emitOptLits(e, subj, rdf.PropMustReadVia, d.MustReadVia)
		emitOptLits(e, subj, rdf.PropObservesVia, d.ObservesVia)
		emitOptLits(e, subj, rdf.PropForbidsBypass, d.ForbidsBypass)
	}

	return nil
}

// emitOptLits emits one literal triple per non-empty trimmed entry.
func emitOptLits(e *rdf.Emitter, subj, prop string, vals []string) {
	for _, v := range vals {
		emitOptLit(e, subj, prop, v)
	}
}
