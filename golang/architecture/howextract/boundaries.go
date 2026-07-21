// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/gosemantics"
	"github.com/globulario/sensei/golang/extractor/importgraph"
)

func extractBoundaries(semanticObs []gosemantics.Observation) []architecture.Fact {
	var facts []architecture.Fact
	for _, obs := range semanticObs {
		if obs.Predicate == gosemantics.PredicateCallsSymbol {
			callerFile := obs.File
			calleeFile := obs.Meta["target_file"]
			if callerFile == "" || calleeFile == "" {
				continue
			}
			callerComp, ok1 := importgraph.ComponentForFile(callerFile)
			calleeComp, ok2 := importgraph.ComponentForFile(calleeFile)
			if ok1 && ok2 && callerComp != calleeComp && callerComp != "" && calleeComp != "" {
				facts = append(facts, architecture.Fact{
					Kind:       "boundary",
					Subject:    obs.Subject,
					Predicate:  "crosses_component_boundary_to",
					Object:     obs.Object,
					Confidence: obs.Confidence,
					Extractor:  "boundary_extractor",
					Scope: architecture.Scope{
						Files:   []string{callerFile, calleeFile},
						Symbols: []string{obs.Subject, obs.Object},
					},
					Evidence: architecture.Evidence{
						SourceFile: callerFile,
						LineStart:  obs.Line,
						LineEnd:    obs.Line,
					},
					Meta: map[string]string{
						"caller_component": callerComp,
						"callee_component": calleeComp,
					},
				})
			}

			// Also check for cross-package/module boundaries
			callerPkg := getPackageFromSymbol(obs.Subject)
			calleePkg := getPackageFromSymbol(obs.Object)
			if callerPkg != "" && calleePkg != "" && callerPkg != calleePkg {
				facts = append(facts, architecture.Fact{
					Kind:       "boundary",
					Subject:    obs.Subject,
					Predicate:  "crosses_package_boundary_to",
					Object:     obs.Object,
					Confidence: obs.Confidence,
					Extractor:  "boundary_extractor",
					Scope: architecture.Scope{
						Files:   []string{callerFile, calleeFile},
						Symbols: []string{obs.Subject, obs.Object},
					},
					Evidence: architecture.Evidence{
						SourceFile: callerFile,
						LineStart:  obs.Line,
						LineEnd:    obs.Line,
					},
					Meta: map[string]string{
						"caller_package": callerPkg,
						"callee_package": calleePkg,
					},
				})
			}
		}
	}
	return facts
}

func getPackageFromSymbol(symbol string) string {
	parts := strings.Split(symbol, ".")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
