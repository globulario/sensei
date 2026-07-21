// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// A legal path with the wrong operation type must not pass scope verification.
// The admitted operation kind must match the observed Git change type.

func TestVerifyScopeRejectsKindMismatch(t *testing.T) {
	cases := []struct {
		name       string
		admitted   closureprotocol.OperationKind
		observedCT string
	}{
		{"modify_vs_delete", closureprotocol.OperationModify, "delete"},
		{"modify_vs_create", closureprotocol.OperationModify, "create"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp, observed := scopeFixture(t)
			exp.Operations[0].Kind = tc.admitted
			observed.Files = []ObservedFile{{Path: v2Target, ChangeType: tc.observedCT}}
			v, err := VerifyScope(exp, observed, v2VerifiedAt)
			if err != nil {
				t.Fatal(err)
			}
			if ScopeVerified(v) {
				t.Fatalf("kind mismatch (%s vs %s) must not verify", tc.admitted, tc.observedCT)
			}
			if !hasViolation(v, "scope.operation.kind_mismatch") {
				t.Fatalf("expected scope.operation.kind_mismatch, got %+v", v.Violations)
			}
		})
	}
}

func TestOperationKindMatches(t *testing.T) {
	ok := []struct {
		k  closureprotocol.OperationKind
		ct string
	}{
		{closureprotocol.OperationModify, "modify"},
		{closureprotocol.OperationCreate, "create"},
		{closureprotocol.OperationDelete, "delete"},
		{closureprotocol.OperationRebuild, "create"},
		{closureprotocol.OperationRebuild, "modify"},
	}
	for _, c := range ok {
		if !operationKindMatches(c.k, c.ct) {
			t.Fatalf("%s should match observed %s", c.k, c.ct)
		}
	}
	bad := []struct {
		k  closureprotocol.OperationKind
		ct string
	}{
		{closureprotocol.OperationCreate, "delete"},
		{closureprotocol.OperationDelete, "modify"},
		{closureprotocol.OperationRebuild, "delete"},
	}
	for _, c := range bad {
		if operationKindMatches(c.k, c.ct) {
			t.Fatalf("%s should NOT match observed %s", c.k, c.ct)
		}
	}
}
