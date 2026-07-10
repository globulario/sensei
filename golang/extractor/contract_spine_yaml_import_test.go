// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

// TestFailureMode_ViolatesContract_EmitsSpineEdges verifies the Phase-1 spine
// ligament: a failure mode's violates_contracts produces BOTH the forward
// FailureMode --violatesContract--> Contract edge and the reverse
// Contract --violatedBy--> FailureMode edge, so the contract-first chain
// (failure -> contract -> invariant/test) is traversable from either side.
func TestFailureMode_ViolatesContract_EmitsSpineEdges(t *testing.T) {
	root := makeDir(t, map[string]string{
		"failure_modes.yaml": `
failure_modes:
  - id: test.fm.spine
    title: A failure that breaks a contract
    severity: high
    violates_contracts:
      - contract.test_guard
    related_invariants:
      - test.inv.guard
`,
	})

	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	fm := rdf.MintIRI(rdf.ClassFailureMode, "test.fm.spine")
	contract := rdf.MintIRI(rdf.ClassContract, "contract.test_guard")

	forward := fm + " " + rdf.IRI(rdf.PropViolatesContract) + " " + contract + " ."
	if !strings.Contains(out, forward) {
		t.Errorf("missing forward FM->violatesContract->Contract edge:\n  want: %s", forward)
	}

	reverse := contract + " " + rdf.IRI(rdf.PropViolatedBy) + " " + fm + " ."
	if !strings.Contains(out, reverse) {
		t.Errorf("missing reverse Contract->violatedBy->FM edge:\n  want: %s", reverse)
	}

	// Referencing a contract must not silently author a new Contract node.
	if strings.Contains(out, contract+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassContract)+" .") {
		t.Error("referenced contract must not be auto-typed; undefined contracts should stay validation-visible")
	}
}
