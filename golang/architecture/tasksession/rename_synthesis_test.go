// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// The legacy scope-to-operation synthesizer must never emit an OperationRename:
// v1 cannot represent a rename honestly. Even when a scope file carries a
// rename-shaped operation, changePlanFromScope produces no rename operation (it
// governs only modify operations), and the plan validates.
func TestChangePlanFromScopeNeverEmitsRename(t *testing.T) {
	req := TaskRequest{
		TaskID:    "task.rename",
		RiskClass: "architecture_sensitive",
		Scope: TaskScope{Files: []FileOperation{
			{Path: "owner.go", Operation: "rename"},
			{Path: "internal/owner.go", Operation: "modify"},
		}},
	}
	plan := changePlanFromScope(req)
	for _, op := range plan.Operations {
		if op.Kind == closureprotocol.OperationRename {
			t.Fatalf("synthesizer emitted a rename operation: %+v", op)
		}
	}
	if err := closureprotocol.ValidateChangePlan(plan); err != nil {
		t.Fatalf("synthesized plan must validate, got %v", err)
	}
}
