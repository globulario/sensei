// SPDX-License-Identifier: AGPL-3.0-only

package authority

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

func TestResolveAuthorizesExactRepositoryEditGrant(t *testing.T) {
	actor, index, binding := verifiedActorFixture(t)
	res, err := Resolve(index, ResolveRequest{
		ActorBindingDigestSHA256:      closureprotocol.MustSemanticDigest(binding),
		BaseBindingDigestSHA256:       "base.digest",
		ClosureAssessmentDigestSHA256: "closure.digest",
		Actor:                         actor,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       "operation.modify.closure",
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "source_file",
			Target:            "golang/architecture/closure/model.go",
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
			RiskClass:         "architecture_sensitive",
		}},
		Applicability: []AuthorityApplicability{{
			OperationID:                 "operation.modify.closure",
			TargetFile:                  "golang/architecture/closure/model.go",
			AuthorityDomainIDs:          []string{"authority.sensei_closure"},
			RequiredRuntimeMechanismIDs: []string{"mutation_path.owner_rpc"},
			RelationPaths:               [][]string{{"task_modifies_file", "file_realizes_component", "component_governed_by_authority_domain"}},
		}},
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T13:00:00Z",
		AuthorityPolicyGraphDigestSHA256: "graph.digest",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %s, want %s", res.Status, closureprotocol.ReceiptValid)
	}
	if len(res.OperationResults) != 1 {
		t.Fatalf("operation_results = %d, want 1", len(res.OperationResults))
	}
	got := res.OperationResults[0]
	if got.Status != closureprotocol.ReceiptValid {
		t.Fatalf("operation status = %s, want %s", got.Status, closureprotocol.ReceiptValid)
	}
	if len(got.GrantIDs) != 1 || got.GrantIDs[0] != "grant.sensei.closure_repository_edit" {
		t.Fatalf("grant_ids = %v", got.GrantIDs)
	}
	if got.SelectedMechanism != closureprotocol.MechanismRepositoryEdit {
		t.Fatalf("selected_mechanism = %s", got.SelectedMechanism)
	}
	if len(got.RequiredRuntimeMechanismIDs) != 1 || got.RequiredRuntimeMechanismIDs[0] != "mutation_path.owner_rpc" {
		t.Fatalf("required_runtime_mechanism_ids = %v", got.RequiredRuntimeMechanismIDs)
	}
}

func TestResolveRejectsWrongMechanism(t *testing.T) {
	actor, index, binding := verifiedActorFixture(t)
	res, err := Resolve(index, ResolveRequest{
		ActorBindingDigestSHA256:      closureprotocol.MustSemanticDigest(binding),
		BaseBindingDigestSHA256:       "base.digest",
		ClosureAssessmentDigestSHA256: "closure.digest",
		Actor:                         actor,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       "operation.modify.closure",
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "source_file",
			Target:            "golang/architecture/closure/model.go",
			SelectedMechanism: closureprotocol.MechanismOwnerRPC,
			RiskClass:         "architecture_sensitive",
		}},
		Applicability: []AuthorityApplicability{{
			OperationID:        "operation.modify.closure",
			TargetFile:         "golang/architecture/closure/model.go",
			AuthorityDomainIDs: []string{"authority.sensei_closure"},
		}},
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T13:00:00Z",
		AuthorityPolicyGraphDigestSHA256: "graph.digest",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %s, want %s", res.Status, closureprotocol.ReceiptInvalid)
	}
	got := res.OperationResults[0]
	if got.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("operation status = %s, want %s", got.Status, closureprotocol.ReceiptInvalid)
	}
	if len(got.Limitations) == 0 || got.Limitations[0] != "grant_cover_missing:authority.sensei_closure" {
		t.Fatalf("limitations = %v", got.Limitations)
	}
}

func TestResolveAllowsNarrowDelegation(t *testing.T) {
	bundle := newTestBundle(t)
	index := testPolicyIndex()
	artifact := bundle.storeBytes(t, []byte("signed-local-authn"), "bin", "application/octet-stream")
	authnDigest := bundle.storeAuthenticationReceipt(t, closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn.local.actor-2",
		PrincipalID:            "actor.codex.session-2",
		Issuer:                 "sensei.local",
		AuthenticationArtifact: artifact,
		AuthenticatedAt:        "2026-07-15T12:00:00Z",
		Status:                 closureprotocol.ReceiptValid,
	})
	roleDigest := bundle.storeRoleAttestationReceipt(t, closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role.local.actor-2",
		PrincipalID:                       "actor.codex.session-2",
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            "sensei.local",
		RoleIDs:                           []string{"role.repository_maintainer"},
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          "2026-07-15T12:00:00Z",
		ValidUntil:                        "2026-07-16T12:00:00Z",
		Status:                            closureprotocol.ReceiptValid,
	})
	delegationDigest := bundle.storeDelegationReceipt(t, closureprotocol.DelegationReceipt{
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
	})
	binding := closureprotocol.ActorBinding{
		PrincipalID:                       "actor.codex.session-2",
		ActorKind:                         closureprotocol.ActorAgent,
		Roles:                             []string{"role.repository_maintainer"},
		Issuer:                            "sensei.local",
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
		DelegationReceiptDigests:          []string{delegationDigest},
	}
	actor, err := VerifyActorBinding(binding, bundle.resolver(), index, mustTime("2026-07-15T13:00:00Z"))
	if err != nil {
		t.Fatalf("VerifyActorBinding: %v", err)
	}
	res, err := Resolve(index, ResolveRequest{
		ActorBindingDigestSHA256:      closureprotocol.MustSemanticDigest(binding),
		BaseBindingDigestSHA256:       "base.digest",
		ClosureAssessmentDigestSHA256: "closure.digest",
		Actor:                         actor,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       "operation.modify.closure",
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "source_file",
			Target:            "golang/architecture/closure/model.go",
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
			RiskClass:         "architecture_sensitive",
		}},
		Applicability: []AuthorityApplicability{{
			OperationID:        "operation.modify.closure",
			TargetFile:         "golang/architecture/closure/model.go",
			AuthorityDomainIDs: []string{"authority.sensei_closure"},
		}},
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T13:00:00Z",
		AuthorityPolicyGraphDigestSHA256: "graph.digest",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != closureprotocol.ReceiptValid {
		t.Fatalf("status = %s, want %s", res.Status, closureprotocol.ReceiptValid)
	}
	got := res.OperationResults[0]
	if len(got.DelegationChain) != 1 || got.DelegationChain[0] != "delegation.repository_repair.actor-2" {
		t.Fatalf("delegation_chain = %v", got.DelegationChain)
	}
}

func TestResolveRejectsBroadeningDelegationMechanism(t *testing.T) {
	bundle := newTestBundle(t)
	index := testPolicyIndex()
	artifact := bundle.storeBytes(t, []byte("signed-local-authn"), "bin", "application/octet-stream")
	authnDigest := bundle.storeAuthenticationReceipt(t, closureprotocol.AuthenticationReceipt{
		ReceiptID:              "authn.local.actor-2",
		PrincipalID:            "actor.codex.session-2",
		Issuer:                 "sensei.local",
		AuthenticationArtifact: artifact,
		AuthenticatedAt:        "2026-07-15T12:00:00Z",
		Status:                 closureprotocol.ReceiptValid,
	})
	roleDigest := bundle.storeRoleAttestationReceipt(t, closureprotocol.RoleAttestationReceipt{
		ReceiptID:                         "role.local.actor-2",
		PrincipalID:                       "actor.codex.session-2",
		ActorKind:                         closureprotocol.ActorAgent,
		Issuer:                            "sensei.local",
		RoleIDs:                           []string{"role.repository_maintainer"},
		AuthenticationReceiptDigestSHA256: authnDigest,
		IssuedAt:                          "2026-07-15T12:00:00Z",
		ValidUntil:                        "2026-07-16T12:00:00Z",
		Status:                            closureprotocol.ReceiptValid,
	})
	delegationDigest := bundle.storeDelegationReceipt(t, closureprotocol.DelegationReceipt{
		DelegationID:         "delegation.repository_repair.actor-2",
		ParentGrantID:        "grant.sensei.closure_repository_edit",
		DelegatorPrincipalID: "actor.dave",
		DelegatedPrincipalID: "actor.codex.session-2",
		RoleIDs:              []string{"role.repository_repair_agent"},
		AuthorityDomainIDs:   []string{"authority.sensei_closure"},
		Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify},
		MechanismKinds:       []closureprotocol.MechanismKind{closureprotocol.MechanismOwnerRPC},
		TargetKinds:          []string{"source_file"},
		TargetSelectors:      []string{"golang/architecture/closure/model.go"},
		MaximumRiskClass:     "architecture_sensitive",
		PolicyID:             "delegation_policy.repository_repair",
		Issuer:               "sensei.local",
		IssuedAt:             "2026-07-15T12:00:00Z",
		ValidFrom:            "2026-07-15T12:00:00Z",
		ValidUntil:           "2026-07-15T18:00:00Z",
		Status:               closureprotocol.ReceiptValid,
	})
	binding := closureprotocol.ActorBinding{
		PrincipalID:                       "actor.codex.session-2",
		ActorKind:                         closureprotocol.ActorAgent,
		Roles:                             []string{"role.repository_maintainer"},
		Issuer:                            "sensei.local",
		AuthenticationReceiptDigestSHA256: authnDigest,
		RoleAttestationReceiptDigests:     []string{roleDigest},
		DelegationReceiptDigests:          []string{delegationDigest},
	}
	actor, err := VerifyActorBinding(binding, bundle.resolver(), index, mustTime("2026-07-15T13:00:00Z"))
	if err != nil {
		t.Fatalf("VerifyActorBinding: %v", err)
	}
	res, err := Resolve(index, ResolveRequest{
		ActorBindingDigestSHA256:      closureprotocol.MustSemanticDigest(binding),
		BaseBindingDigestSHA256:       "base.digest",
		ClosureAssessmentDigestSHA256: "closure.digest",
		Actor:                         actor,
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       "operation.modify.closure",
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "source_file",
			Target:            "golang/architecture/closure/model.go",
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
			RiskClass:         "architecture_sensitive",
		}},
		Applicability: []AuthorityApplicability{{
			OperationID:        "operation.modify.closure",
			TargetFile:         "golang/architecture/closure/model.go",
			AuthorityDomainIDs: []string{"authority.sensei_closure"},
		}},
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T13:00:00Z",
		AuthorityPolicyGraphDigestSHA256: "graph.digest",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Status != closureprotocol.ReceiptInvalid {
		t.Fatalf("status = %s, want %s", res.Status, closureprotocol.ReceiptInvalid)
	}
}

func mustTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}
