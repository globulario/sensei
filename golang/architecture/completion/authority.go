// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// resolveCompletionAuthority requires a RESOLVED, valid operation result for the
// exact terminal-completion operation and returns the grant and role that
// authorized it. It resolves against the isolated completion triple, so it can
// never be cross-authorized by a certification, disposition, promotion, or
// question-resolution certification grant.
func resolveCompletionAuthority(index authority.PolicyIndex, binding closureprotocol.ActorBinding, verified authority.VerifiedActor, evaluatedAt time.Time, taskDir string) (string, string, error) {
	ra, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return "", "", fmt.Errorf("load recorded authority: %w", err)
	}
	plan := closureprotocol.ChangePlan{
		PlanID: "plan.complete." + ra.Base.Task.ID,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       completionOperationID,
			Kind:              closureprotocol.OperationComplete,
			TargetKind:        TargetKindTaskCompletion,
			SelectedMechanism: closureprotocol.MechanismGovernedWorkflow,
			RiskClass:         completionRiskClass,
		}},
	}
	app := []authority.AuthorityApplicability{{
		OperationID:                 completionOperationID,
		AuthorityDomainIDs:          []string{DomainTerminalCompletion},
		RequiredRuntimeMechanismIDs: []string{MechanismPathCompletion},
	}}
	resolution, err := admission.ResolveAuthority(index, admission.ResolveAuthorityInput{
		Actor:                            binding,
		VerifiedActor:                    verified,
		Base:                             ra.Base,
		ChangePlan:                       plan,
		Applicability:                    app,
		PolicyID:                         ra.Base.Policies.Admission,
		ClosureAssessmentDigestSHA256:    ra.Resolution.ClosureAssessmentDigestSHA256,
		AuthorityPolicyGraphDigestSHA256: closureprotocol.MustSemanticDigest(index),
		EvaluatedAt:                      evaluatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", "", fmt.Errorf("resolve completion authority: %w", err)
	}
	var op *closureprotocol.AuthorityResolutionOperation
	for i := range resolution.OperationResults {
		if resolution.OperationResults[i].OperationID == completionOperationID {
			op = &resolution.OperationResults[i]
			break
		}
	}
	if op == nil {
		return "", "", fmt.Errorf("no resolved operation result for the completion operation")
	}
	if op.Status != closureprotocol.ReceiptValid {
		return "", "", fmt.Errorf("completion operation not authorized (%s): %s", op.Status, strings.Join(op.Limitations, ","))
	}
	if !containsString(op.GrantIDs, GrantTerminalCompletion) {
		return "", "", fmt.Errorf("completion not authorized by %s", GrantTerminalCompletion)
	}
	role := grantRole(index, GrantTerminalCompletion, verified.VerifiedRoleIDs)
	if role == "" {
		return "", "", fmt.Errorf("no verified role authorizes %s", GrantTerminalCompletion)
	}
	return GrantTerminalCompletion, role, nil
}

func grantRole(index authority.PolicyIndex, grantID string, verifiedRoles []string) string {
	grant, ok := index.AuthorityGrants[grantID]
	if !ok {
		return ""
	}
	for _, r := range grant.ActorRoleIDs {
		if containsString(verifiedRoles, r) {
			return r
		}
	}
	return ""
}

func containsString(in []string, want string) bool {
	for _, s := range in {
		if s == want {
			return true
		}
	}
	return false
}
