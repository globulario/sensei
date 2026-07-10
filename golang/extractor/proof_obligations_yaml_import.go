// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type yamlProofSlot struct {
	ID          string `yaml:"id"`
	Kind        string `yaml:"kind"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

type yamlProofObligation struct {
	ID                          string          `yaml:"id"`
	Label                       string          `yaml:"label"`
	Status                      string          `yaml:"status"`
	DerivedFromStatus           string          `yaml:"derived_from_status"`
	DerivedFromAuthoritySurface string          `yaml:"derived_from_authority_surface"`
	AppliesToAuthoritySurfaces  []string        `yaml:"applies_to_authority_surfaces"`
	EvidenceLane                string          `yaml:"evidence_lane"`
	RequiredSlots               []yamlProofSlot `yaml:"required_slots"`
	Notes                       string          `yaml:"notes"`
}

type yamlProofObligationsDoc struct {
	ProofObligations []yamlProofObligation `yaml:"proof_obligations"`
}

func importProofObligations(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	var doc yamlProofObligationsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	for _, ob := range doc.ProofObligations {
		if strings.TrimSpace(ob.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassProofObligation, ob.ID)
		e.Typed(subj, rdf.ClassProofObligation)
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(ob.Label, ob.ID)))
		emitOptLit(e, subj, rdf.PropStatus, ob.Status)
		emitOptLit(e, subj, rdf.PropDerivedFromStatus, ob.DerivedFromStatus)
		emitOptLit(e, subj, rdf.PropHasEvidenceLane, ob.EvidenceLane)
		if notes := strings.TrimSpace(ob.Notes); notes != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(notes))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		if ref := strings.TrimSpace(ob.DerivedFromAuthoritySurface); ref != "" {
			e.Triple(subj, rdf.IRI(rdf.PropDerivedFromAuthoritySurface), rdf.MintIRI(rdf.ClassAuthoritySurface, ref))
		}
		for _, ref := range ob.AppliesToAuthoritySurfaces {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			e.Triple(subj, rdf.IRI(rdf.PropAppliesToAuthoritySurface), rdf.MintIRI(rdf.ClassAuthoritySurface, ref))
		}
		for _, slot := range ob.RequiredSlots {
			if strings.TrimSpace(slot.ID) == "" {
				continue
			}
			slotIRI := rdf.MintIRI(rdf.ClassProofSlot, slot.ID)
			e.Typed(slotIRI, rdf.ClassProofSlot)
			e.Triple(slotIRI, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(slot.Description, slot.ID)))
			emitOptLit(e, slotIRI, rdf.PropSlotKind, slot.Kind)
			if slot.Required {
				e.Triple(slotIRI, rdf.IRI(rdf.PropRequired), rdf.Lit("true"))
			}
			e.Triple(slotIRI, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
			e.Triple(subj, rdf.IRI(rdf.PropRequiresProofSlot), slotIRI)
		}
	}
	return nil
}
