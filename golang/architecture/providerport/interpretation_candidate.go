// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// MapInterpretationCandidate validates and detaches a completed O2
// interpretation result without granting it O1 authority. It deliberately
// returns data, not a synthesis.Command: provider output is candidate
// knowledge until a separate interpretation-closure owner certifies it.
//
// The current implementation reuses MapToCommand's mature envelope, parent,
// digest, stale-identity, and deep-copy checks, then discards the command
// carrier. RecordInterpretationCommand produced there has no closure marker
// and therefore must never be sent to Transition. MapToCommand's
// interpretation branch is retained temporarily for source compatibility;
// O7 uses only this candidate API before closure.
func MapInterpretationCandidate(state synthesis.SessionState, request Request, result Result) (synthesis.Interpretation, error) {
	if request.Operation != OperationInterpretation || result.Operation != OperationInterpretation {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: interpretation candidate mapper requires operation %q", OperationInterpretation)
	}
	command, err := MapToCommand(state, request, result, "")
	if err != nil {
		return synthesis.Interpretation{}, err
	}
	record, ok := command.(synthesis.RecordInterpretationCommand)
	if !ok {
		return synthesis.Interpretation{}, fmt.Errorf("providerport: interpretation mapping produced unexpected command %T", command)
	}
	return record.Interpretation, nil
}
