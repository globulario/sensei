// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
)

func extractContracts(semanticObs []gosemantics.Observation) []architecture.Fact {
	var facts []architecture.Fact
	for _, obs := range semanticObs {
		if obs.Predicate == gosemantics.PredicateImplementsInterface {
			facts = append(facts, architecture.Fact{
				Kind:       "contract_seam",
				Subject:    obs.Subject,
				Predicate:  obs.Predicate,
				Object:     obs.Object,
				Confidence: obs.Confidence,
				Extractor:  "contract_extractor",
				Scope: architecture.Scope{
					Files:   []string{obs.File},
					Symbols: []string{obs.Subject, obs.Object},
				},
				Evidence: architecture.Evidence{
					SourceFile: obs.File,
					LineStart:  obs.Line,
					LineEnd:    obs.Line,
				},
				Meta: obs.Meta,
			})
		}
		if obs.Predicate == "exports_interface" || (obs.Predicate == gosemantics.PredicateExportsSymbol && obs.Object == "interface") {
			facts = append(facts, architecture.Fact{
				Kind:       "contract_seam",
				Subject:    obs.Subject,
				Predicate:  "exports_interface",
				Object:     obs.Object,
				Confidence: obs.Confidence,
				Extractor:  "contract_extractor",
				Scope: architecture.Scope{
					Files:   []string{obs.File},
					Symbols: []string{obs.Subject},
				},
				Evidence: architecture.Evidence{
					SourceFile: obs.File,
					LineStart:  obs.Line,
					LineEnd:    obs.Line,
				},
				Meta: obs.Meta,
			})
		}
	}
	return facts
}
