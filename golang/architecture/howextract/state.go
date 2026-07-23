// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"github.com/globulario/sensei/golang/architecture"
)

func extractState(astFacts []architecture.Fact) []architecture.Fact {
	var facts []architecture.Fact
	for _, f := range astFacts {
		if f.Kind == "read" || f.Kind == "write" || f.Kind == "generation_check" {
			f.Extractor = "state_extractor"
			facts = append(facts, f)
		}
	}
	return facts
}
