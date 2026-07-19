// SPDX-License-Identifier: Apache-2.0

package closureprotocol

import (
	"strings"
	"testing"
)

// rename is reserved in the operation vocabulary but unsupported in v1:
// ChangeOperation has a single Target and cannot encode distinct source and
// destination endpoints. Every change-plan validation must fail it closed,
// while explicit delete and create remain legal when independently declared.

func renameOp() ChangeOperation {
	return ChangeOperation{
		OperationID:       "op.rename",
		Kind:              OperationRename,
		TargetKind:        "source_file",
		Target:            "internal/owner.go",
		SelectedMechanism: MechanismRepositoryEdit,
	}
}

func TestValidateChangePlanRejectsRename(t *testing.T) {
	err := ValidateChangePlan(ChangePlan{PlanID: "plan.rename", Operations: []ChangeOperation{renameOp()}})
	if err == nil || !strings.Contains(err.Error(), "protocol.rename_requires_explicit_source_and_destination") {
		t.Fatalf("expected rename to fail closed, got %v", err)
	}
}

func TestValidateAdmissionRequestRejectsRename(t *testing.T) {
	// rename must be refused even inside an otherwise well-formed request; the
	// change-plan chokepoint (shared by DecideAdmission) is what enforces it.
	err := ValidateChangePlan(ChangePlan{
		PlanID: "plan.mixed",
		Operations: []ChangeOperation{
			{OperationID: "op.modify", Kind: OperationModify, TargetKind: "source_file", Target: "a.go", SelectedMechanism: MechanismRepositoryEdit},
			renameOp(),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "protocol.rename_requires_explicit_source_and_destination") {
		t.Fatalf("expected a request carrying a rename to fail, got %v", err)
	}
}

func TestValidateChangePlanAllowsExplicitDeleteAndCreate(t *testing.T) {
	// A genuine delete-then-create architecture is legal; it just may not be
	// used as a compatibility encoding for rename.
	err := ValidateChangePlan(ChangePlan{
		PlanID: "plan.delete_create",
		Operations: []ChangeOperation{
			{OperationID: "op.delete", Kind: OperationDelete, TargetKind: "source_file", Target: "owner.go", SelectedMechanism: MechanismRepositoryEdit},
			{OperationID: "op.create", Kind: OperationCreate, TargetKind: "source_file", Target: "internal/owner.go", SelectedMechanism: MechanismRepositoryEdit},
		},
	})
	if err != nil {
		t.Fatalf("explicit delete+create must remain valid, got %v", err)
	}
}
