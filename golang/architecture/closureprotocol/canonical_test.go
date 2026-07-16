// SPDX-License-Identifier: Apache-2.0

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

func TestResultTransitionReceiptDigestOmitsSelfDigest(t *testing.T) {
	base := ResultTransitionReceipt{
		Task:                              TaskBinding{ID: "task.x", SessionID: "session.x"},
		BaseBindingDigestSHA256:           "base",
		AdmissionDecisionDigestSHA256:     "decision",
		CapabilityConsumptionDigestSHA256: "capability",
		ChangeSetDigestSHA256:             "changeset",
		ScopeVerificationDigestSHA256:     "scope",
		PatchDigestSHA256:                 "patch",
		ResultTreeDigestSHA256:            "tree",
		ResultGraphDigestSHA256:           "graph",
		PipelinePolicyID:                  "pipeline.result_transition.v1",
		RecordedAt:                        "2026-07-16T00:15:00Z",
		Status:                            ReceiptValid,
	}
	d1, err := ResultTransitionReceiptDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	base.ReceiptDigestSHA256 = "different"
	d2, err := ResultTransitionReceiptDigest(base)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatal("self digest field changed semantic digest")
	}
}

func TestResultTransitionArtifactSetOrderIgnored(t *testing.T) {
	a := ResultTransitionReceipt{
		GeneratedArtifactReceipts: []ArtifactReceipt{
			{Path: "a", DigestSHA256: "1"},
			{Path: "b", DigestSHA256: "2"},
		},
	}
	b := ResultTransitionReceipt{
		GeneratedArtifactReceipts: []ArtifactReceipt{
			{Path: "b", DigestSHA256: "2"},
			{Path: "a", DigestSHA256: "1"},
		},
	}
	if MustSemanticDigest(a) != MustSemanticDigest(b) {
		t.Fatal("generated artifact receipts must canonicalize as an unordered set")
	}
}

