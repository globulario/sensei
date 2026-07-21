// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
)

func extractDataShapes(semanticObs []gosemantics.Observation) []architecture.Fact {
	var facts []architecture.Fact
	for _, obs := range semanticObs {
		if obs.Kind == "data_shape" {
			facts = append(facts, architecture.Fact{
				Kind:       "data_shape",
				Subject:    obs.Subject,
				Predicate:  obs.Predicate,
				Object:     obs.Object,
				Confidence: obs.Confidence,
				Extractor:  "data_shape_extractor",
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
