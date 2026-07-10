// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"fmt"
	"os"
	"strings"

	"github.com/globulario/awareness-graph/golang/rdf"
	yaml "gopkg.in/yaml.v3"
)

// Contract-spine Phase 2: link an IMPLEMENTATION contract (a generated gRPC /
// REST / HTTP surface) UP to the ARCHITECTURAL contract whose semantic promise
// it realizes. Authoritative links (`realizations:`) are hand-authored or
// promoted from a reviewed candidate; candidate links (`candidates:`) are
// produced from conservative evidence and MUST be promoted before they count.
//
// The two are kept as distinct predicates on purpose:
//   - realizations  -> aw:realizesContract (+ reverse aw:realizedByContract)
//   - candidates    -> aw:candidateRealizesContract ONLY
//
// so path/name overlap can never masquerade as an authoritative guarantee
// ("no YAML lipstick, no invented architecture"). Authoritative edges are the
// only ones a traversal treats as a realized promise.

type contractRealization struct {
	Implementation string   `yaml:"implementation"` // implementation contract id
	Realizes       string   `yaml:"realizes"`       // architectural contract id
	Source         string   `yaml:"source"`         // manual | promoted_candidate | deterministic_rule | path_overlap | ...
	Confidence     string   `yaml:"confidence"`     // high | medium | low
	Evidence       []string `yaml:"evidence"`       // review provenance (kept in YAML)
}

type contractRealizationsFile struct {
	ContractRealizations struct {
		Realizations []contractRealization `yaml:"realizations"`
		Candidates   []contractRealization `yaml:"candidates"`
	} `yaml:"contract_realizations"`
}

func importContractRealizations(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f contractRealizationsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Authoritative: forward + reverse, so the spine traverses impl→arch AND
	// arch→impl.
	for _, r := range f.ContractRealizations.Realizations {
		if r.Implementation == "" || r.Realizes == "" {
			continue
		}
		impl := rdf.MintIRI(rdf.ClassContract, r.Implementation)
		arch := rdf.MintIRI(rdf.ClassContract, r.Realizes)
		e.Triple(impl, rdf.IRI(rdf.PropRealizesContract), arch)
		e.Triple(arch, rdf.IRI(rdf.PropRealizedByContract), impl)
		emitContractRealizationEvidence(e, path, r, true)
	}

	// Candidate: NON-authoritative — a single forward candidate edge, no
	// realizesContract and no reverse. Promotion (moving the entry into
	// realizations:) is what makes it count.
	for _, c := range f.ContractRealizations.Candidates {
		if c.Implementation == "" || c.Realizes == "" {
			continue
		}
		impl := rdf.MintIRI(rdf.ClassContract, c.Implementation)
		arch := rdf.MintIRI(rdf.ClassContract, c.Realizes)
		e.Triple(impl, rdf.IRI(rdf.PropCandidateRealizesContract), arch)
		emitContractRealizationEvidence(e, path, c, false)
	}
	return nil
}

func emitContractRealizationEvidence(e *rdf.Emitter, path string, r contractRealization, authoritative bool) {
	impl := strings.TrimSpace(r.Implementation)
	arch := strings.TrimSpace(r.Realizes)
	if impl == "" || arch == "" {
		return
	}
	status := "candidate"
	if authoritative {
		status = "accepted"
	}
	evidenceID := "contract_realization." + status + "." + impl + "__" + arch
	subj := rdf.MintIRI(rdf.ClassEvidence, evidenceID)
	implIRI := rdf.MintIRI(rdf.ClassContract, impl)
	archIRI := rdf.MintIRI(rdf.ClassContract, arch)

	e.Typed(subj, rdf.ClassEvidence)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit("Contract realization evidence: "+impl+" -> "+arch))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	emitOptLit(e, subj, rdf.PropSourceKind, r.Source)
	emitOptLit(e, subj, rdf.PropConfidence, r.Confidence)
	emitOptLit(e, subj, rdf.PropPromotionStatus, status)
	for _, line := range r.Evidence {
		emitOptLit(e, subj, rdf.PropComment, line)
	}
	e.Triple(subj, rdf.IRI(rdf.PropSupports), implIRI)
	e.Triple(subj, rdf.IRI(rdf.PropSupports), archIRI)
	e.Triple(implIRI, rdf.IRI(rdf.PropSupportedByEvidence), subj)
	e.Triple(archIRI, rdf.IRI(rdf.PropSupportedByEvidence), subj)
}
