// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// CounterexampleRecord extends investigation.Counterexample with Phase 10.4 semantics.
type CounterexampleRecord struct {
	Counterexample investigation.Counterexample `json:"counterexample" yaml:"counterexample"`

	StrategyVersion string `json:"strategy_version" yaml:"strategy_version"`
	MinimalityBasis string `json:"minimality_basis" yaml:"minimality_basis"`
}
