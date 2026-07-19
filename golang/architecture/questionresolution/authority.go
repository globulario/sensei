// SPDX-License-Identifier: AGPL-3.0-only

package questionresolution

import (
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// resolveCertificationAuthority requires a RESOLVED, valid operation result for the
// exact bounded question-resolution certification operation and returns the concrete
// grant and role that authorized it. It never accepts "the actor holds some grant".
// The operation is an owner-RPC observation over durable task evidence, resolved
// against the isolated certification triple so it cannot cross-authorize with the
// disposition or promotion grants.
func resolveCertificationAuthority(index authority.PolicyIndex, binding closureprotocol.ActorBinding, verified authority.VerifiedActor, evaluatedAt time.Time, taskDir string) (string, string, error) {
	ra, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return "", "", fmt.Errorf("load recorded authority: %w", err)
	}
	plan := closureprotocol.ChangePlan{
		PlanID: "plan.certify_question_resolution." + ra.Base.Task.ID,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       certOperationID,
			Kind:              closureprotocol.OperationObserve,
			TargetKind:        TargetKindCertificate,
			SelectedMechanism: closureprotocol.MechanismOwnerRPC,
			RiskClass:         certRiskClass,
		}},
	}
	app := []authority.AuthorityApplicability{{
		OperationID:                 certOperationID,
		AuthorityDomainIDs:          []string{DomainCertification},
		RequiredRuntimeMechanismIDs: []string{MechanismPathCert},
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
		return "", "", fmt.Errorf("resolve certification authority: %w", err)
	}
	var op *closureprotocol.AuthorityResolutionOperation
	for i := range resolution.OperationResults {
		if resolution.OperationResults[i].OperationID == certOperationID {
			op = &resolution.OperationResults[i]
			break
		}
	}
	if op == nil {
		return "", "", fmt.Errorf("no resolved operation result for the certification operation")
	}
	if op.Status != closureprotocol.ReceiptValid {
		return "", "", fmt.Errorf("certification operation not authorized (%s): %s", op.Status, strings.Join(op.Limitations, ","))
	}
	if !containsString(op.GrantIDs, GrantCertification) {
		return "", "", fmt.Errorf("certification not authorized by %s", GrantCertification)
	}
	role := grantRole(index, GrantCertification, verified.VerifiedRoleIDs)
	if role == "" {
		return "", "", fmt.Errorf("no verified role authorizes %s", GrantCertification)
	}
	return GrantCertification, role, nil
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
