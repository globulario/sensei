// SPDX-License-Identifier: AGPL-3.0-only

package closureprotocol

import "testing"

func TestSemanticDigestIgnoresSetOrder(t *testing.T) {
	a := ActorBinding{PrincipalID: "actor.a", ActorKind: ActorHuman, Roles: []string{"reviewer", "owner"}}
	b := ActorBinding{PrincipalID: "actor.a", ActorKind: ActorHuman, Roles: []string{"owner", "reviewer", "owner"}}
	da := MustSemanticDigest(a)
	db := MustSemanticDigest(b)
	if da != db {
		t.Fatalf("expected same digest, got %s and %s", da, db)
	}
}

func TestSemanticDigestPreservesOrderedMeaning(t *testing.T) {
	a := MigrationExecutionReceipt{
		MigrationPlanID: "migration.a", StepID: "step.1", SourceState: "a", TargetState: "b",
		Mechanism: MechanismMigrationRunner, ActorID: "actor.a",
		MutationReceiptDigestSHA256: "x", ProofDischargeDigestSHA256: "y", RollbackAvailable: true, Status: ReceiptValid,
	}
	b := a
	a.StepID = "step.1"
	b.StepID = "step.2"
	da := MustSemanticDigest(a)
	db := MustSemanticDigest(b)
	if da == db {
		t.Fatal("expected different digest when ordered meaning changes")
	}
}

func TestCompletionReceiptDigestOmitsSelfDigest(t *testing.T) {
	base := CompletionReceipt{
		Task:           TaskBinding{ID: "task.x", SessionID: "session.x"},
		TerminalStatus: TerminalCompleted,
		BaseBinding: BaseBinding{
			Repository: RepositorySnapshot{Domain: "github.com/globulario/sensei", RevisionStatus: "resolved"},
			Graph:      GraphSnapshot{DigestStatus: "resolved"},
			Task:       TaskBinding{ID: "task.x", SessionID: "session.x"},
			Policies:   PolicyBinding{Completion: "completion.architectural_closure.v1", Canonicalization: "canonicalization.architectural_closure.v1"},
		},
		ResultBinding:                     ResultBinding{BaseRevision: "abc", PatchDigestSHA256: "p", ResultTreeDigestSHA256: "t", GraphDigestSHA256: "g"},
		ClosureAssessmentDigestSHA256:     "c",
		AuthorityResolutionDigestSHA256:   "a",
		AdmissionDecisionDigestSHA256:     "d",
		AdmissionVerificationDigestSHA256: "v",
		CertificationDigestSHA256:         "z",
		CompletionPolicy:                  "completion.architectural_closure.v1",
		CompletedAt:                       "2026-07-15T20:00:00Z",
		CompletingActor:                   "actor.a",
	}
	d1, err := CompletionReceiptDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	base.ReceiptDigestSHA256 = "different"
	d2, err := CompletionReceiptDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("self digest field changed semantic digest")
	}
}

func TestAuthorityResolutionDigestIgnoresSetOrder(t *testing.T) {
	a := AuthorityResolution{
		ActorBindingDigestSHA256:         "actor123",
		BaseBindingDigestSHA256:          "base123",
		ClosureAssessmentDigestSHA256:    "closure123",
		OperationSetDigestSHA256:         "ops123",
		AuthorityPolicyGraphDigestSHA256: "graph123",
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T20:00:00Z",
		Status:                           ReceiptValid,
		OperationResults: []AuthorityResolutionOperation{{
			OperationID:        "op.modify.closure",
			Status:             ReceiptValid,
			AuthorityDomainIDs: []string{"authority.b", "authority.a"},
			GrantIDs:           []string{"grant.b", "grant.a"},
			LegalMechanisms:    []string{"repository_edit", "generated_artifact_rebuild"},
			SelectedMechanism:  MechanismRepositoryEdit,
			Limitations:        []string{"b", "a"},
		}},
	}
	b := a
	b.OperationResults = append([]AuthorityResolutionOperation(nil), a.OperationResults...)
	b.OperationResults[0].AuthorityDomainIDs = append([]string(nil), a.OperationResults[0].AuthorityDomainIDs...)
	b.OperationResults[0].GrantIDs = append([]string(nil), a.OperationResults[0].GrantIDs...)
	b.OperationResults[0].LegalMechanisms = append([]string(nil), a.OperationResults[0].LegalMechanisms...)
	b.OperationResults[0].Limitations = append([]string(nil), a.OperationResults[0].Limitations...)
	b.OperationResults[0].AuthorityDomainIDs = []string{"authority.a", "authority.b", "authority.a"}
	b.OperationResults[0].GrantIDs = []string{"grant.a", "grant.b"}
	b.OperationResults[0].LegalMechanisms = []string{"generated_artifact_rebuild", "repository_edit"}
	b.OperationResults[0].Limitations = []string{"a", "b", "a"}
	da := MustSemanticDigest(a)
	db := MustSemanticDigest(b)
	if da != db {
		t.Fatalf("expected same digest, got %s and %s", da, db)
	}
}

func TestAuthorityResolutionDigestPreservesDelegationChainOrder(t *testing.T) {
	a := AuthorityResolution{
		ActorBindingDigestSHA256:         "actor123",
		BaseBindingDigestSHA256:          "base123",
		ClosureAssessmentDigestSHA256:    "closure123",
		OperationSetDigestSHA256:         "ops123",
		AuthorityPolicyGraphDigestSHA256: "graph123",
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T20:00:00Z",
		Status:                           ReceiptValid,
		OperationResults: []AuthorityResolutionOperation{{
			OperationID:       "op.modify.closure",
			Status:            ReceiptValid,
			DelegationChain:   []string{"delegation.root", "delegation.child"},
			SelectedMechanism: MechanismRepositoryEdit,
		}},
	}
	b := a
	b.OperationResults = append([]AuthorityResolutionOperation(nil), a.OperationResults...)
	b.OperationResults[0].DelegationChain = append([]string(nil), a.OperationResults[0].DelegationChain...)
	b.OperationResults[0].DelegationChain = []string{"delegation.child", "delegation.root"}
	da := MustSemanticDigest(a)
	db := MustSemanticDigest(b)
	if da == db {
		t.Fatal("expected delegation chain order to change semantic digest")
	}
}
