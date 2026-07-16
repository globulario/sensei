// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// authorizingIndex is a minimal policy index that authorizes an agent holding
// role.repository_repair_agent to modify a source_file via repository_edit.
// It mirrors the authority package's own known-good fixture.
func authorizingIndex() authority.PolicyIndex {
	return authority.PolicyIndex{
		ActorRoles: map[string]authority.ActorRole{
			"role.repository_repair_agent": {
				ID:                "role.repository_repair_agent",
				Status:            "active",
				AllowedActorKinds: []closureprotocol.ActorKind{closureprotocol.ActorAgent, closureprotocol.ActorHuman},
				TrustedIssuers:    []string{"sensei.local"},
			},
		},
		MutationPaths: map[string]authority.MutationPath{
			"mutation_path.repository_edit": {
				ID:            "mutation_path.repository_edit",
				Status:        "active",
				MechanismKind: closureprotocol.MechanismRepositoryEdit,
				TargetKinds:   []string{"source_file", "governed_source"},
			},
		},
		AuthorityDomains: map[string]authority.AuthorityDomain{
			"authority.sensei_closure": {
				ID:               "authority.sensei_closure",
				Status:           "active",
				MayWriteRoleIDs:  []string{"role.repository_repair_agent"},
				MustMutateViaIDs: []string{"mutation_path.repository_edit"},
			},
		},
		AuthorityGrants: map[string]authority.AuthorityGrant{
			"grant.sensei.closure_repository_edit": {
				ID:                   "grant.sensei.closure_repository_edit",
				Status:               "active",
				ActorRoleIDs:         []string{"role.repository_repair_agent"},
				AuthorityDomainIDs:   []string{"authority.sensei_closure"},
				Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationRead},
				TargetKinds:          []string{"source_file"},
				RequiredMechanismIDs: []string{"mutation_path.repository_edit"},
				MaximumRiskClass:     "architecture_sensitive",
				ValidFrom:            "2026-07-15T00:00:00Z",
			},
		},
	}
}

func writerVerifiedActor() authority.VerifiedActor {
	return authority.VerifiedActor{
		PrincipalID:     "actor.codex.session-1",
		ActorKind:       closureprotocol.ActorAgent,
		Issuer:          "sensei.local",
		Status:          closureprotocol.ReceiptValid,
		VerifiedRoleIDs: []string{"role.repository_repair_agent"},
	}
}

func writerActorBinding() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{
		PrincipalID: "actor.codex.session-1",
		ActorKind:   closureprotocol.ActorAgent,
		Roles:       []string{"role.repository_repair_agent"},
		Issuer:      "sensei.local",
	}
}

func writerChangePlan() closureprotocol.ChangePlan {
	return closureprotocol.ChangePlan{
		PlanID: "plan.writer",
		Operations: []closureprotocol.ChangeOperation{{
			OperationID:       "operation.modify.closure",
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "source_file",
			Target:            "golang/architecture/closure/model.go",
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
			RiskClass:         "architecture_sensitive",
		}},
	}
}

func writerApplicability() []authority.AuthorityApplicability {
	return []authority.AuthorityApplicability{{
		OperationID:        "operation.modify.closure",
		TargetFile:         "golang/architecture/closure/model.go",
		AuthorityDomainIDs: []string{"authority.sensei_closure"},
	}}
}

func writerInput(mechanism closureprotocol.MechanismKind) ResolveAuthorityInput {
	plan := writerChangePlan()
	plan.Operations[0].SelectedMechanism = mechanism
	return ResolveAuthorityInput{
		Actor:                            writerActorBinding(),
		VerifiedActor:                    writerVerifiedActor(),
		Base:                             v2BaseBinding(),
		ChangePlan:                       plan,
		Applicability:                    writerApplicability(),
		PolicyID:                         "authority.strict.v1",
		ClosureAssessmentDigestSHA256:    "closure.digest",
		AuthorityPolicyGraphDigestSHA256: "graph.digest",
		EvaluatedAt:                      "2026-07-16T12:00:00Z",
	}
}

func TestResolveAuthorityAuthorizesGrant(t *testing.T) {
	res, err := ResolveAuthority(authorizingIndex(), writerInput(closureprotocol.MechanismRepositoryEdit))
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	if res.Status != closureprotocol.ReceiptValid {
		t.Fatalf("expected valid resolution, got %s (limitations %v)", res.Status, res.Limitations)
	}
	if len(res.OperationResults) != 1 || res.OperationResults[0].Status != closureprotocol.ReceiptValid {
		t.Fatalf("expected the operation authorized, got %+v", res.OperationResults)
	}
	if res.AuthorityResolutionDigestSHA256 == "" {
		t.Fatal("resolution self-digest was not stamped")
	}
}

func TestResolveAuthorityRefusesWrongMechanism(t *testing.T) {
	res, err := ResolveAuthority(authorizingIndex(), writerInput(closureprotocol.MechanismOwnerRPC))
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}
	if res.Status == closureprotocol.ReceiptValid {
		t.Fatal("expected refusal for an unauthorized mechanism")
	}
}

// TestWriterSubstrateEndToEnd proves the writer connects to the reader: resolve
// authority -> record authority_resolved -> build an admission request bound to
// the recorded resolution -> DecideAdmission admits -> record admission_decided,
// all on one hash-chained ledger.
func TestWriterSubstrateEndToEnd(t *testing.T) {
	task := closureprotocol.TaskBinding{ID: "task.writer", SessionID: "session.writer"}
	store, head := admissionLedgerStore(t, task)

	in := writerInput(closureprotocol.MechanismRepositoryEdit)
	resolution, err := ResolveAuthority(authorizingIndex(), in)
	if err != nil {
		t.Fatalf("ResolveAuthority: %v", err)
	}

	resolved, err := RecordAuthorityResolved(store, head, task, resolution, in.Actor, in.ChangePlan, in.Base, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordAuthorityResolved: %v", err)
	}
	if resolved.Entry.EventType != closureprotocol.LedgerEventAuthorityResolved {
		t.Fatalf("unexpected event type %q", resolved.Entry.EventType)
	}

	// Build the admission request from the same records the resolution bound.
	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    in.Actor,
		BaseBinding:                     in.Base,
		ChangePlan:                      in.ChangePlan,
		AuthorityResolutionDigestSHA256: resolution.AuthorityResolutionDigestSHA256,
		PolicyID:                        "admission.strict.v2",
	}
	policy := AdmissionV2Policy{PolicyID: "admission.strict.v2", CompletionPolicyID: "completion.architectural_closure.v1"}
	decision, err := DecideAdmission(req, resolution, policy, v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission against the recorded resolution: %v", err)
	}
	if !AllAdmitted(decision) {
		t.Fatalf("expected admission of the authorized operation, got %+v", decision.OperationVerdicts)
	}

	decided, err := RecordAdmissionDecided(store, resolved.Entry.EntryDigestSHA256, decision, task, ledgerProducedAt())
	if err != nil {
		t.Fatalf("RecordAdmissionDecided: %v", err)
	}

	report, err := store.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Valid || report.EntryCount != 3 { // genesis + authority_resolved + admission_decided
		t.Fatalf("expected 3 valid chained entries, got count=%d valid=%v", report.EntryCount, report.Valid)
	}
	if report.HeadDigestSHA256 != decided.Entry.EntryDigestSHA256 {
		t.Fatal("head does not point at the admission_decided event")
	}
}
