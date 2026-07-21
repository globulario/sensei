// SPDX-License-Identifier: AGPL-3.0-only

package authority

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// delegationCase returns a baseline one-level delegation that CheckDelegationForOperation
// accepts, together with the governed index, so each test can mutate a single
// field and assert the resulting verdict. The baseline mirrors the accepted
// scenario in TestResolveAllowsNarrowDelegation.
func delegationCase() (PolicyIndex, AuthorityGrant, closureprotocol.DelegationReceipt, closureprotocol.ChangeOperation, string) {
	index := testPolicyIndex()
	grant := index.AuthorityGrants["grant.sensei.closure_repository_edit"]
	receipt := closureprotocol.DelegationReceipt{
		DelegationID:         "delegation.repository_repair.actor-2",
		ParentGrantID:        "grant.sensei.closure_repository_edit",
		DelegatorPrincipalID: "actor.dave",
		DelegatedPrincipalID: "actor.codex.session-2",
		RoleIDs:              []string{"role.repository_repair_agent"},
		AuthorityDomainIDs:   []string{"authority.sensei_closure"},
		Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify},
		MechanismKinds:       []closureprotocol.MechanismKind{closureprotocol.MechanismRepositoryEdit},
		TargetKinds:          []string{"source_file"},
		TargetSelectors:      []string{"golang/architecture/closure/model.go"},
		MaximumRiskClass:     "architecture_sensitive",
		PolicyID:             "delegation_policy.repository_repair",
		Issuer:               "sensei.local",
		IssuedAt:             "2026-07-15T12:00:00Z",
		ValidFrom:            "2026-07-15T12:00:00Z",
		ValidUntil:           "2026-07-15T18:00:00Z",
		Status:               closureprotocol.ReceiptValid,
	}
	op := closureprotocol.ChangeOperation{
		OperationID:       "operation.modify.closure",
		Kind:              closureprotocol.OperationModify,
		TargetKind:        "source_file",
		Target:            "golang/architecture/closure/model.go",
		SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
		RiskClass:         "architecture_sensitive",
	}
	return index, grant, receipt, op, "authority.sensei_closure"
}

func TestCheckDelegationAcceptsMonotonicOneLevel(t *testing.T) {
	index, grant, receipt, op, domain := delegationCase()
	if v := CheckDelegationForOperation(index, grant, receipt, op, domain, mustTime("2026-07-15T13:00:00Z")); v != DelegationOK {
		t.Fatalf("verdict = %q, want DelegationOK", v)
	}
}

func TestCheckDelegationVerdicts(t *testing.T) {
	at := mustTime("2026-07-15T13:00:00Z")
	cases := []struct {
		name   string
		mutate func(index *PolicyIndex, grant *AuthorityGrant, receipt *closureprotocol.DelegationReceipt, op *closureprotocol.ChangeOperation, domain *string)
		want   DelegationVerdict
	}{
		{
			name: "parent grant mismatch",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.ParentGrantID = "grant.some.other"
			},
			want: DelegationParentMismatch,
		},
		{
			name: "revoked receipt",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.Status = closureprotocol.ReceiptRevoked
			},
			want: DelegationRevoked,
		},
		{
			name: "unresolved parent delegation",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.ParentDelegationID = "delegation.upstream"
			},
			want: DelegationParentUnresolved,
		},
		{
			name: "domain broadening",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.AuthorityDomainIDs = []string{"authority.sensei_closure", "authority.unbounded"}
			},
			want: DelegationDomainBroadening,
		},
		{
			name: "action broadening",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.Actions = []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationDelete}
			},
			want: DelegationActionBroadening,
		},
		{
			name: "mechanism broadening",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, op *closureprotocol.ChangeOperation, _ *string) {
				r.MechanismKinds = []closureprotocol.MechanismKind{closureprotocol.MechanismOwnerRPC}
				op.SelectedMechanism = closureprotocol.MechanismOwnerRPC
			},
			want: DelegationMechanismBroadening,
		},
		{
			name: "target selector broadening",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, _ *closureprotocol.DelegationReceipt, op *closureprotocol.ChangeOperation, _ *string) {
				op.Target = "golang/architecture/closure/other.go"
			},
			want: DelegationTargetBroadening,
		},
		{
			name: "risk broadening",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, _ *closureprotocol.DelegationReceipt, op *closureprotocol.ChangeOperation, _ *string) {
				op.RiskClass = "data_loss_risk"
			},
			want: DelegationRiskBroadening,
		},
		{
			name: "grant not delegable",
			mutate: func(_ *PolicyIndex, g *AuthorityGrant, _ *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				g.Delegable = false
			},
			want: DelegationPolicyInvalid,
		},
		{
			name: "subdelegation forbidden by policy",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.AllowSubdelegation = true
			},
			want: DelegationSubdelegationForbidden,
		},
		{
			name: "not yet active",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.ValidFrom = "2026-07-15T14:00:00Z"
			},
			want: DelegationNotActive,
		},
		{
			name: "expired",
			mutate: func(_ *PolicyIndex, _ *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				r.ValidUntil = "2026-07-15T12:30:00Z"
			},
			want: DelegationExpired,
		},
		{
			name: "time broadening beyond parent",
			mutate: func(_ *PolicyIndex, g *AuthorityGrant, r *closureprotocol.DelegationReceipt, _ *closureprotocol.ChangeOperation, _ *string) {
				g.ValidUntil = "2026-07-15T17:00:00Z"
				r.ValidUntil = "2026-07-15T18:00:00Z"
			},
			want: DelegationTimeBroadening,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index, grant, receipt, op, domain := delegationCase()
			tc.mutate(&index, &grant, &receipt, &op, &domain)
			if v := CheckDelegationForOperation(index, grant, receipt, op, domain, at); v != tc.want {
				t.Fatalf("verdict = %q, want %q", v, tc.want)
			}
		})
	}
}
