// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// Triple fixtures. Subjects determine ownership: meta.* is authored by the
// awareness-graph corpus (agOnly), scylla.*/infra.* by the services YAML.
const (
	agLabel  = `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <http://www.w3.org/2000/01/rdf-schema#label> "storage is not authority" .`
	agSev    = `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "high" .`
	svcLabel = `<https://globular.io/awareness#invariant/scylla.loopback_forbidden> <http://www.w3.org/2000/01/rdf-schema#label> "no loopback" .`
	svcSev   = `<https://globular.io/awareness#invariant/scylla.loopback_forbidden> <https://globular.io/awareness#severity> "critical" .`
)

func nt(lines ...string) []byte {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return []byte(out)
}

// Case 1: an awareness-graph seed PR generated from a services PR's YAML must
// NOT fail only because services master does not yet contain those definitions.
// committed = seed ahead (has services-PR triples); generated = regen from
// services master (lacks them). The diff is services-authored → tolerated.
func TestClassifySeedDiff_AgSeedPRAheadOfServicesMaster(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agSev, svcLabel, svcSev) // seed PR includes services-PR triples
	generated := nt(agLabel, agSev)                   // regen from services master (no services-PR triples)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("owned drift must be empty (services-authored diff), got %v", owned)
	}
	if len(external) != 2 {
		t.Fatalf("expected 2 external/context diffs, got %v", external)
	}
}

// Case 2: a services PR must NOT fail only because awareness-graph master does
// not yet contain the paired generated seed. committed = ag master seed (lacks
// services-PR triples); generated = regen from the services PR (has them). The
// diff is services-authored → tolerated.
func TestClassifySeedDiff_ServicesPRAheadOfAgMasterSeed(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agSev)                   // ag master seed
	generated := nt(agLabel, agSev, svcLabel, svcSev) // regen from services PR

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 0 {
		t.Fatalf("owned drift must be empty (services-authored diff), got %v", owned)
	}
	if len(external) != 2 {
		t.Fatalf("expected 2 external/context diffs, got %v", external)
	}
}

// Case 3: real stale seed INSIDE the repo under test must still fail. An
// awareness-graph-owned triple drifts (committed has the old value, the corpus
// now generates a new value) → owned drift → gate fails.
func TestClassifySeedDiff_RealOwnedDriftStillFails(t *testing.T) {
	agOld := `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "medium" .`
	agNew := agSev                  // "high"
	agOnly := nt(agLabel, agNew)    // corpus regenerates the new value
	committed := nt(agLabel, agOld) // committed seed is stale
	generated := nt(agLabel, agNew)

	owned, _ := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		t.Fatal("owned drift in the awareness-graph corpus must fail the gate")
	}
}

// A stale seed MISSING an owned triple (e.g. a newly added meta invariant not
// yet in the committed seed) must also fail — this is the in-repo "stale seed"
// regression the gate must keep catching.
func TestClassifySeedDiff_MissingOwnedTripleStillFails(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel)        // missing agSev
	generated := nt(agLabel, agSev) // corpus has it
	owned, _ := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		t.Fatal("a missing owned triple (stale seed) must fail the gate")
	}
}

// Identical seeds → no diffs at all.
func TestClassifySeedDiff_IdenticalIsClean(t *testing.T) {
	agOnly := nt(agLabel, agSev)
	seed := nt(agLabel, agSev, svcLabel)
	owned, external := classifySeedDiff(seed, seed, agOnly)
	if len(owned) != 0 || len(external) != 0 {
		t.Fatalf("identical seeds must produce no diffs, got owned=%v external=%v", owned, external)
	}
}

// Mixed drift: an owned change AND a services-context change in the same diff —
// the gate must fail (owned drift present) while still classifying the services
// line as external.
func TestClassifySeedDiff_MixedDriftFailsOnOwned(t *testing.T) {
	agOld := `<https://globular.io/awareness#invariant/meta.storage_is_not_semantic_authority> <https://globular.io/awareness#severity> "medium" .`
	agOnly := nt(agLabel, agSev)
	committed := nt(agLabel, agOld)           // owned drift (old severity)
	generated := nt(agLabel, agSev, svcLabel) // owned fixed + a services-context triple
	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) == 0 {
		t.Fatal("owned drift must fail even when external diffs are also present")
	}
	foundSvc := false
	for _, l := range external {
		if l == svcLabel {
			foundSvc = true
		}
	}
	if !foundSvc {
		t.Fatalf("services-authored line must be classified external, got %v", external)
	}
}

// Shared subjects must not imply shared ownership. The awareness-graph corpus
// can reference the same source file subject while services contributes a
// different predicate on that file; only the ag-authored predicate family is
// owned by this repo.
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

// Shared subject+predicate must still tolerate external triples when the
// awareness-graph corpus owns a different target family under that predicate.
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
		t.Fatalf("different object family on shared subject+predicate must be external, got %v", external)
	}
}

// A different object on an awareness-graph-owned subject+predicate pair is
// still owned drift and must fail the gate.
func TestClassifySeedDiff_SharedSubjectOwnedPredicateStillFails(t *testing.T) {
	oldLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "old engine.go" .`
	newLabel := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <http://www.w3.org/2000/01/rdf-schema#label> "engine.go" .`
	agOnly := nt(newLabel)
	committed := nt(oldLabel)
	generated := nt(newLabel)

	owned, external := classifySeedDiff(committed, generated, agOnly)
	if len(owned) != 2 {
		t.Fatalf("owned label drift must stay owned, got owned=%v external=%v", owned, external)
	}
	if len(external) != 0 {
		t.Fatalf("owned label drift must not leak into external, got %v", external)
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

func TestNtOwnershipKey(t *testing.T) {
	// B (#141): the object ownership-term is now the FULL minted id, not the
	// collapsed class family ("repairPlan"), so edges to different objects of the
	// same family on a shared subject+predicate classify by their specific owner.
	line := `<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> <https://globular.io/awareness#repairPlan/repair.workflow.foreach_guard_before_collection> .`
	if got := ntOwnershipKey(line); got != "<https://globular.io/awareness#sourceFile/golang%2Fworkflow%2Fengine%2Fengine.go> <https://globular.io/awareness#implements> repairPlan/repair.workflow.foreach_guard_before_collection" {
		t.Fatalf("ntOwnershipKey=%q", got)
	}
}
