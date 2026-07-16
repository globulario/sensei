// SPDX-License-Identifier: Apache-2.0

package admission

import "testing"

// Every admitted mutation operation must appear in the observed change. An empty
// or missing observation fails closed, and an operation is verified only once a
// matching observation exists — never merely because it was admitted.

func TestVerifyScopeRequiresAdmittedMutationObserved(t *testing.T) {
	exp, observed := scopeFixture(t)
	observed.Files = nil // the admitted modify never happened
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeVerified(v) {
		t.Fatal("an unobserved admitted mutation must not scope-verify")
	}
	if !hasViolation(v, "scope.operation.not_observed") {
		t.Fatalf("expected scope.operation.not_observed, got %+v", v.Violations)
	}
	if len(v.VerifiedOperationIDs) != 0 {
		t.Fatalf("no operation should be verified without an observation, got %+v", v.VerifiedOperationIDs)
	}
}

func TestVerifyScopeFlagsDuplicateObservation(t *testing.T) {
	exp, observed := scopeFixture(t)
	observed.Files = []ObservedFile{
		{Path: v2Target, ChangeType: "modify"},
		{Path: v2Target, ChangeType: "modify"},
	}
	v, err := VerifyScope(exp, observed, v2VerifiedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !hasViolation(v, "scope.operation.duplicate_observation") {
		t.Fatalf("expected scope.operation.duplicate_observation, got %+v", v.Violations)
	}
}
