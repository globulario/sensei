// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

// InvalidOutputError marks an agent response that cannot be accepted as one
// closed mutation plan. Provider execution converts it into O2's typed
// invalid-output outcome rather than treating it as infrastructure failure.
type InvalidOutputError struct {
	Detail string
}

func (e *InvalidOutputError) Error() string { return "agentcommand: invalid output: " + e.Detail }

func invalidOutput(format string, args ...any) error {
	return &InvalidOutputError{Detail: fmt.Sprintf(format, args...)}
}

func NormalizeMutationPlan(plan MutationPlan) MutationPlan {
	if plan.Operations == nil {
		plan.Operations = []MutationOperation{}
	}
	for i := range plan.Operations {
		if plan.Operations[i].Content == nil {
			plan.Operations[i].Content = []byte{}
		}
	}
	return plan
}

func MutationPlanDigest(plan MutationPlan) (string, error) {
	plan = NormalizeMutationPlan(plan)
	plan.MutationPlanDigestSHA256 = ""
	return closureprotocol.SemanticDigest(plan)
}

func ValidateMutationPlan(plan MutationPlan) error {
	plan = NormalizeMutationPlan(plan)
	if plan.SchemaVersion != MutationPlanSchemaVersion {
		return invalidOutput("schema_version %q is not %q", plan.SchemaVersion, MutationPlanSchemaVersion)
	}
	computed, err := MutationPlanDigest(plan)
	if err != nil {
		return invalidOutput("compute mutation plan digest: %v", err)
	}
	if plan.MutationPlanDigestSHA256 != computed {
		return invalidOutput("mutation plan declares digest %q but computed %q", plan.MutationPlanDigestSHA256, computed)
	}

	seen := make(map[string]struct{}, len(plan.Operations))
	for i, operation := range plan.Operations {
		if strings.TrimSpace(operation.OperationID) == "" {
			return invalidOutput("operations[%d].operation_id is empty", i)
		}
		if _, exists := seen[operation.OperationID]; exists {
			return invalidOutput("duplicate operation_id %q", operation.OperationID)
		}
		seen[operation.OperationID] = struct{}{}
		if err := validateOperation(operation); err != nil {
			return invalidOutput("operations[%d] %q: %v", i, operation.OperationID, err)
		}
	}
	return nil
}

func validateOperation(operation MutationOperation) error {
	if err := runnercomposition.ValidateCandidatePath(operation.Path); err != nil {
		return fmt.Errorf("path: %w", err)
	}
	emptyContent := len(operation.Content) == 0
	switch operation.Kind {
	case MutationWrite:
		if operation.NewPath != "" || operation.Mode != "" || operation.SymlinkTarget != "" {
			return errors.New("write permits only path and content")
		}
	case MutationDelete:
		if operation.NewPath != "" || !emptyContent || operation.Mode != "" || operation.SymlinkTarget != "" {
			return errors.New("delete permits only path")
		}
	case MutationRename:
		if err := runnercomposition.ValidateCandidatePath(operation.NewPath); err != nil {
			return fmt.Errorf("new_path: %w", err)
		}
		if !emptyContent || operation.Mode != "" || operation.SymlinkTarget != "" {
			return errors.New("rename permits only path and new_path")
		}
	case MutationSetMode:
		if operation.NewPath != "" || !emptyContent || operation.SymlinkTarget != "" {
			return errors.New("set-mode permits only path and mode")
		}
		if operation.Mode != runnercomposition.ModeRegular && operation.Mode != runnercomposition.ModeExecutable {
			return fmt.Errorf("mode %q is not regular or executable", operation.Mode)
		}
	case MutationSymlink:
		if operation.NewPath != "" || !emptyContent || operation.Mode != "" {
			return errors.New("symlink permits only path and symlink_target")
		}
		if operation.SymlinkTarget == "" {
			return errors.New("symlink_target is empty")
		}
	default:
		return fmt.Errorf("unknown kind %q", operation.Kind)
	}
	return nil
}
