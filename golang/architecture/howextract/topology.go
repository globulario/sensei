// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
)

func extractTopology(semanticObs []gosemantics.Observation) []architecture.Fact {
	var facts []architecture.Fact
	for _, obs := range semanticObs {
		if obs.Predicate == gosemantics.PredicateDefinesSymbol || obs.Predicate == gosemantics.PredicateComponentDependsOn || obs.Predicate == gosemantics.PredicateExportsSymbol {
			facts = append(facts, architecture.Fact{
				Kind:       "topology",
				Subject:    obs.Subject,
				Predicate:  obs.Predicate,
				Object:     obs.Object,
				Confidence: obs.Confidence,
				Extractor:  "topology_extractor",
				Scope: architecture.Scope{
					Files:   []string{obs.File},
					Symbols: []string{obs.Symbol},
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
