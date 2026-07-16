// SPDX-License-Identifier: Apache-2.0

package authority

import (
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// DelegationVerdict names why a delegation receipt does or does not legitimately
// authorize an operation under its parent grant. The empty verdict means the
// delegation is monotonic, active, and within its parent's envelope. It is the
// single source of truth for delegation legality: the authority resolver gates
// on it, and the certification engine re-runs it independently to verify a
// delegated authority chain from the recorded receipts.
type DelegationVerdict string

const (
	DelegationOK                     DelegationVerdict = ""
	DelegationParentMismatch         DelegationVerdict = "parent_mismatch"
	DelegationParentUnresolved       DelegationVerdict = "parent_delegation_unresolved"
	DelegationRevoked                DelegationVerdict = "revoked"
	DelegationExpired                DelegationVerdict = "expired"
	DelegationNotActive              DelegationVerdict = "not_active"
	DelegationRoleBroadening         DelegationVerdict = "role_broadening"
	DelegationDomainBroadening       DelegationVerdict = "domain_broadening"
	DelegationActionBroadening       DelegationVerdict = "action_broadening"
	DelegationTargetBroadening       DelegationVerdict = "target_broadening"
	DelegationMechanismBroadening    DelegationVerdict = "mechanism_broadening"
	DelegationRiskBroadening         DelegationVerdict = "risk_broadening"
	DelegationTimeBroadening         DelegationVerdict = "time_broadening"
	DelegationPolicyInvalid          DelegationVerdict = "policy_invalid"
	DelegationSubdelegationForbidden DelegationVerdict = "subdelegation_forbidden"
)

// CheckDelegationForOperation verifies that a single-level delegation receipt
// legitimately authorizes op under grant, monotonically (never broadening the
// parent grant's roles, domains, actions, targets, mechanisms, risk ceiling, or
// validity window), through an active delegation policy, and current at
// evaluatedAt. It returns DelegationOK or the first violating verdict.
func CheckDelegationForOperation(index PolicyIndex, grant AuthorityGrant, receipt closureprotocol.DelegationReceipt, op closureprotocol.ChangeOperation, domainID string, evaluatedAt time.Time) DelegationVerdict {
	if receipt.ParentGrantID != grant.ID {
		return DelegationParentMismatch
	}
	if receipt.Status != closureprotocol.ReceiptValid {
		return DelegationRevoked
	}
	if receipt.ParentDelegationID != "" {
		// Multi-level chains are not resolved against a direct grant here; the
		// parent delegation must be verified in its own step.
		return DelegationParentUnresolved
	}
	if !subsetStrings(receipt.RoleIDs, grant.ActorRoleIDs) ||
		(len(receipt.RoleIDs) > 0 && !intersectsString(receipt.RoleIDs, grant.ActorRoleIDs)) {
		return DelegationRoleBroadening
	}
	if !subsetStrings(receipt.AuthorityDomainIDs, grant.AuthorityDomainIDs) || !containsString(receipt.AuthorityDomainIDs, domainID) {
		return DelegationDomainBroadening
	}
	if !subsetOperations(receipt.Actions, grant.Actions) || !containsOperation(receipt.Actions, op.Kind) {
		return DelegationActionBroadening
	}
	if !subsetTargetKinds(receipt.TargetKinds, grant.TargetKinds) || !containsString(receipt.TargetKinds, op.TargetKind) {
		return DelegationTargetBroadening
	}
	if !subsetMechanismKinds(index, receipt.MechanismKinds, grant.RequiredMechanismIDs) || !delegationMechanismMatches(receipt.MechanismKinds, op.SelectedMechanism) {
		return DelegationMechanismBroadening
	}
	if len(receipt.TargetSelectors) > 0 && !containsString(receipt.TargetSelectors, op.Target) {
		return DelegationTargetBroadening
	}
	if riskRank(op.RiskClass) > riskRank(grant.MaximumRiskClass) || riskRank(op.RiskClass) > riskRank(receipt.MaximumRiskClass) {
		return DelegationRiskBroadening
	}
	if !grant.Delegable || strings.TrimSpace(grant.DelegationPolicyID) == "" || grant.DelegationPolicyID != receipt.PolicyID {
		return DelegationPolicyInvalid
	}
	policy, ok := index.DelegationPolicies[receipt.PolicyID]
	if !ok || policy.Status != "active" {
		return DelegationPolicyInvalid
	}
	if len(policy.AllowedActions) > 0 && !subsetOperations(receipt.Actions, policy.AllowedActions) {
		return DelegationPolicyInvalid
	}
	if len(policy.AllowedMechanismIDs) > 0 && !subsetMechanismIDs(index, receipt.MechanismKinds, policy.AllowedMechanismIDs) {
		return DelegationPolicyInvalid
	}
	if !policy.AllowSubdelegation && receipt.AllowSubdelegation {
		return DelegationSubdelegationForbidden
	}
	validFrom, err := time.Parse(time.RFC3339, receipt.ValidFrom)
	if err != nil || evaluatedAt.Before(validFrom) {
		return DelegationNotActive
	}
	if receipt.ValidUntil != "" {
		validUntil, err := time.Parse(time.RFC3339, receipt.ValidUntil)
		if err != nil || !evaluatedAt.Before(validUntil) {
			return DelegationExpired
		}
		if grant.ValidUntil != "" {
			parentValidUntil, err := time.Parse(time.RFC3339, grant.ValidUntil)
			if err != nil || validUntil.After(parentValidUntil) {
				return DelegationTimeBroadening
			}
		}
	}
	if grant.ValidFrom != "" {
		parentValidFrom, err := time.Parse(time.RFC3339, grant.ValidFrom)
		if err != nil || validFrom.Before(parentValidFrom) {
			return DelegationTimeBroadening
		}
	}
	return DelegationOK
}
