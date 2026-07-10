// SPDX-License-Identifier: AGPL-3.0-only

// Importer for RuntimeEvidence YAML files (Phase 2C).
//
// A RuntimeEvidence profile describes the LIVE proof required to verify a rule:
// which owner service it must come from, the legal observation path, how fresh
// it must be, its trust level, and — critically — that stale or non-owner-path
// evidence must NOT be promoted to PASS. It describes the evidence contract; it
// is never itself the authority. The importer emits one RuntimeEvidence node
// per entry with flat literals plus object-link edges to the invariant /
// repair plan / authority domain it gates. Linking never types the target.
//
// Schema (top-level key `runtime_evidence`, list of profiles):
//
//	runtime_evidence:
//	  - id:                        evidence.<slug>
//	    label:                     ...
//	    status:                    active | draft | deprecated
//	    observed_from_service:     owner service the evidence must come from
//	    observed_via_paths:        []string legal observation paths
//	    freshness_window:          string
//	    trust_level:               high | medium | low
//	    expires_after:             string
//	    must_come_from_owner_path: bool
//	    cannot_promote_to_pass_when_stale: bool
//	    supporting_evidence:       []string (presence that is evidence-only)
//	    conflicts_with_evidence:   []string
//	    evidence_for_authority_domains: []authority_domain refs
//	    evidence_for_invariants:   []invariant refs
//	    evidence_for_repair_plans: []repair_plan refs
//	    notes:                     free text → rdfs:comment
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type yamlRuntimeEvidence struct {
	ID                           string   `yaml:"id"`
	Label                        string   `yaml:"label"`
	Status                       string   `yaml:"status"`
	ObservedFromService          string   `yaml:"observed_from_service"`
	ObservedViaPaths             []string `yaml:"observed_via_paths"`
	FreshnessWindow              string   `yaml:"freshness_window"`
	TrustLevel                   string   `yaml:"trust_level"`
	ExpiresAfter                 string   `yaml:"expires_after"`
	MustComeFromOwnerPath        bool     `yaml:"must_come_from_owner_path"`
	CannotPromoteToPassWhenStale bool     `yaml:"cannot_promote_to_pass_when_stale"`
	SupportingEvidence           []string `yaml:"supporting_evidence"`
	ConflictsWithEvidence        []string `yaml:"conflicts_with_evidence"`
	EvidenceForAuthorityDomains  []string `yaml:"evidence_for_authority_domains"`
	EvidenceForInvariants        []string `yaml:"evidence_for_invariants"`
	EvidenceForRepairPlans       []string `yaml:"evidence_for_repair_plans"`
	Notes                        string   `yaml:"notes"`
}

type yamlRuntimeEvidenceDoc struct {
	RuntimeEvidence []yamlRuntimeEvidence `yaml:"runtime_evidence"`
}

func importRuntimeEvidence(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var doc yamlRuntimeEvidenceDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}

	for _, ev := range doc.RuntimeEvidence {
		if ev.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassRuntimeEvidence, ev.ID)
		e.Typed(subj, rdf.ClassRuntimeEvidence)

		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(ev.Label, ev.ID)))
		emitOptLit(e, subj, rdf.PropStatus, ev.Status)
		emitOptLit(e, subj, rdf.PropObservedFromService, ev.ObservedFromService)
		emitOptLit(e, subj, rdf.PropHasFreshnessWindow, ev.FreshnessWindow)
		emitOptLit(e, subj, rdf.PropHasTrustLevel, ev.TrustLevel)
		emitOptLit(e, subj, rdf.PropExpiresAfter, ev.ExpiresAfter)
		if ev.MustComeFromOwnerPath {
			e.Triple(subj, rdf.IRI(rdf.PropMustComeFromOwnerPath), rdf.Lit("true"))
		}
		if ev.CannotPromoteToPassWhenStale {
			e.Triple(subj, rdf.IRI(rdf.PropCannotPromoteToPassWhenStale), rdf.Lit("true"))
		}
		if notes := strings.TrimSpace(ev.Notes); notes != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(notes))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitOptLits(e, subj, rdf.PropObservedViaPath, ev.ObservedViaPaths)
		emitOptLits(e, subj, rdf.PropConflictsWithEvidence, ev.ConflictsWithEvidence)

		emitRefEdges(e, subj, rdf.PropEvidenceForAuthorityDomain, ev.EvidenceForAuthorityDomains)
		emitRefEdges(e, subj, rdf.PropEvidenceForInvariant, ev.EvidenceForInvariants)
		emitRefEdges(e, subj, rdf.PropEvidenceForRepairPlan, ev.EvidenceForRepairPlans)
	}
	return nil
}
