// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

const (
	agLabel  = `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <http://www.w3.org/2000/01/rdf-schema#label> "storage is not authority" .`
	agSev    = `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "high" .`
	svcLabel = `<https://globular.io/awareness#invariant/scylla.loopback_forbidden> <http://www.w3.org/2000/01/rdf-schema#label> "no loopback" .`
	svcSev   = `<https://globular.io/awareness#invariant/scylla.loopback_forbidden> <https://globular.io/awareness#severity> "critical" .`
)

func nt(lines ...string) []byte {
	out := ""
	for _, line := range lines {
		out += line + "\n"
	}
	return []byte(out)
}

func TestClassifySeedDiff_AgSeedPRAheadOfServicesMaster(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agSev, svcLabel, svcSev)
	generated := nt(agLabel, agSev)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("owned drift must be empty (services-authored diff), got %v", owned)
	}
	if len(external) != 2 {
		t.Fatalf("expected 2 external/context diffs, got %v", external)
	}
}

func TestClassifySeedDiff_ServicesPRAheadOfAgMasterSeed(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agSev)
	generated := nt(agLabel, agSev, svcLabel, svcSev)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("owned drift must be empty (services-authored diff), got %v", owned)
	}
	if len(external) != 2 {
		t.Fatalf("expected 2 external/context diffs, got %v", external)
	}
}

func TestClassifySeedDiff_RealOwnedDriftStillFails(t *testing.T) {
	agOld := `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "medium" .`
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agOld)
	generated := nt(agLabel, agSev)

	owned, _ := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		t.Fatal("owned drift in the Sensei corpus must fail the gate")
	}
	if !containsLine(owned, agSev) {
		t.Fatalf("the newly generated owned value must carry ownership, got %v", owned)
	}
}

func TestClassifySeedDiff_MissingOwnedTripleStillFails(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel)
	generated := nt(agLabel, agSev)
	owned, _ := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		t.Fatal("a missing owned triple must fail the gate")
	}
}

func TestClassifySeedDiff_IdenticalIsClean(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	seed := nt(agLabel, agSev, svcLabel)
	owned, external := classifySeedDiff(seed, seed, agOnly)
	if len(owned) != 0 || len(external) != 0 {
		t.Fatalf("identical seeds must produce no diffs, got owned=%v external=%v", owned, external)
	}
}

func TestClassifySeedDiff_MixedDriftFailsOnOwned(t *testing.T) {
	agOld := `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "medium" .`
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agOld)
	generated := nt(agLabel, agSev, svcLabel)
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		t.Fatal("owned drift must fail even when external diffs are also present")
	}
	if !containsLine(external, svcLabel) {
		t.Fatalf("services-authored line must be classified external, got %v", external)
	}
}

func TestClassifySeedDiff_SharedSubjectDifferentPredicateIsExternal(t *testing.T) {
	fileLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "engine.go" .`
	svcFailure := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> <https://globular.io/awareness#failureMode/workflow.foreach_when_guard_evaluated_after_collection_resolution> .`
	agOnly := nt(fileLabel)
	committed := nt(fileLabel, svcFailure)
	generated := nt(fileLabel)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("shared-subject services edge must stay external, got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcFailure {
		t.Fatalf("shared-subject services edge must be external, got %v", external)
	}
}

func TestClassifySeedDiff_SharedSubjectPredicateDifferentObjectFamilyIsExternal(t *testing.T) {
	agPlan := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> <https://globular.io/awareness#repairPlan/repair.workflow.foreach_guard_before_collection> .`
	svcFailure := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> <https://globular.io/awareness#failureMode/workflow.foreach_when_guard_evaluated_after_collection_resolution> .`
	agOnly := nt(agPlan)
	committed := nt(agPlan, svcFailure)
	generated := nt(agPlan)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("different object family on shared subject+predicate must stay external, got owned=%v", owned)
	}
	if len(external) != 1 || external[0] != svcFailure {
		t.Fatalf("different object family must be external, got %v", external)
	}
}

// Regression for the services #219 combined audit. Both repositories may emit
// literals under the same shared subject and predicate. Literal-kind collapsing
// incorrectly claimed the services literal as Sensei-owned.
func TestClassifySeedDiff_SharedSubjectPredicateDifferentLiteralIsExternal(t *testing.T) {
	agLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "engine.go" .`
	svcLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "workflow engine source" .`
	agOnly := nt(agLabel)
	committed := nt(agLabel, svcLabel)
	generated := nt(agLabel)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("different services literal on a shared subject+predicate must stay external, got %v", owned)
	}
	if len(external) != 1 || external[0] != svcLabel {
		t.Fatalf("services literal must be external, got %v", external)
	}
}

func TestClassifySeedDiff_SharedSubjectOwnedPredicateStillFails(t *testing.T) {
	oldLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "old engine.go" .`
	newLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "engine.go" .`
	agOnly := nt(newLabel)
	committed := nt(oldLabel)
	generated := nt(newLabel)

	owned, _ := classifySeedDiff(committed, generated, agOnly)
	if !containsLine(owned, newLabel) {
		t.Fatalf("the generated owned label must keep the gate failing, got %v", owned)
	}
}

func TestNtSubject(t *testing.T) {
	if got := ntSubject(agSev); got != "<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority>" {
		t.Fatalf("ntSubject=%q", got)
	}
}

func TestNtSubjectPredicate(t *testing.T) {
	if got := ntSubjectPredicate(agSev); got != "<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity>" {
		t.Fatalf("ntSubjectPredicate=%q", got)
	}
}

func TestNtObjectTermPreservesLiteralWithSpaces(t *testing.T) {
	line := `<https://globular.io/awareness#sourceFile/x> <http://www.w3.org/2000/01/rdf-schema#label> "literal with spaces"@en .`
	if got := ntObjectTerm(line); got != `"literal with spaces"@en` {
		t.Fatalf("ntObjectTerm=%q", got)
	}
}

func TestNtOwnershipKey(t *testing.T) {
	line := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> <https://globular.io/awareness#repairPlan/repair.workflow.foreach_guard_before_collection> .`
	if got := ntOwnershipKey(line); got != "<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> repairPlan/repair.workflow.foreach_guard_before_collection" {
		t.Fatalf("ntOwnershipKey=%q", got)
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
