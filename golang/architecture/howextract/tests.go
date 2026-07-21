// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
)

func extractTests(semanticObs []gosemantics.Observation) []architecture.Fact {
	var facts []architecture.Fact
	for _, obs := range semanticObs {
		if obs.Predicate == gosemantics.PredicateTestCallsSymbol {
			facts = append(facts, architecture.Fact{
				Kind:       "test_protection",
				Subject:    obs.Subject,
				Predicate:  obs.Predicate,
				Object:     obs.Object,
				Confidence: obs.Confidence,
				Extractor:  "test_extractor",
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
		if obs.Predicate == gosemantics.PredicateDefinesSymbol && strings.HasSuffix(obs.File, "_test.go") {
			facts = append(facts, architecture.Fact{
				Kind:       "test_protection",
				Subject:    obs.Subject,
				Predicate:  "defines_test",
				Object:     obs.Object,
				Confidence: obs.Confidence,
				Extractor:  "test_extractor",
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
