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
		Task: TaskBinding{ID: "task.x", SessionID: "session.x"},
		TerminalStatus: TerminalCompleted,
		BaseBinding: BaseBinding{
			Repository: RepositorySnapshot{Domain: "github.com/globulario/sensei", RevisionStatus: "resolved"},
			Graph: GraphSnapshot{DigestStatus: "resolved"},
			Task: TaskBinding{ID: "task.x", SessionID: "session.x"},
			Policies: PolicyBinding{Completion: "completion.architectural_closure.v1", Canonicalization: "canonicalization.architectural_closure.v1"},
		},
		ResultBinding: ResultBinding{BaseRevision: "abc", PatchDigestSHA256: "p", ResultTreeDigestSHA256: "t", GraphDigestSHA256: "g"},
		ClosureAssessmentDigestSHA256: "c",
		AuthorityResolutionDigestSHA256: "a",
		AdmissionDecisionDigestSHA256: "d",
		AdmissionVerificationDigestSHA256: "v",
		CertificationDigestSHA256: "z",
		CompletionPolicy: "completion.architectural_closure.v1",
		CompletedAt: "2026-07-15T20:00:00Z",
		CompletingActor: "actor.a",
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

