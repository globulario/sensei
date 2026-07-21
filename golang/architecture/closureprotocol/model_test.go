// SPDX-License-Identifier: AGPL-3.0-only

package closureprotocol

import "testing"

func TestValidateTaskTransition(t *testing.T) {
	if err := ValidateTaskTransition(PhasePrepared, PhaseCompleted); err == nil {
		t.Fatal("expected prepared -> completed to be illegal")
	}
	if err := ValidateTaskTransition(PhasePrepared, PhaseConverging); err != nil {
		t.Fatalf("expected prepared -> converging to be legal: %v", err)
	}
}

func TestValidateActorBindingRejectsUnknownKind(t *testing.T) {
	err := ValidateActorBinding(ActorBinding{PrincipalID: "actor.dave", ActorKind: ActorKind("robot")})
	if err == nil {
		t.Fatal("expected unknown actor kind to fail")
	}
}

func TestValidateAuthenticationReceiptRequiresArtifact(t *testing.T) {
	err := ValidateAuthenticationReceipt(AuthenticationReceipt{
		ReceiptID:       "auth.example",
		PrincipalID:     "actor.dave",
		Issuer:          "issuer.local",
		AuthenticatedAt: "2026-07-15T20:00:00Z",
		Status:          ReceiptValid,
	})
	if err == nil {
		t.Fatal("expected missing authentication artifact to fail")
	}
}

func TestValidateRoleAttestationReceiptRequiresRoles(t *testing.T) {
	err := ValidateRoleAttestationReceipt(RoleAttestationReceipt{
		ReceiptID:   "attestation.example",
		PrincipalID: "actor.dave",
		ActorKind:   ActorHuman,
		Issuer:      "issuer.local",
		IssuedAt:    "2026-07-15T20:00:00Z",
		Status:      ReceiptValid,
	})
	if err == nil {
		t.Fatal("expected missing roles to fail")
	}
}

func TestValidateDelegationReceiptRequiresParent(t *testing.T) {
	err := ValidateDelegationReceipt(DelegationReceipt{
		DelegationID:         "delegation.example",
		DelegatorPrincipalID: "actor.owner",
		DelegatedPrincipalID: "actor.agent",
		PolicyID:             "delegation_policy.example",
		Issuer:               "issuer.local",
		IssuedAt:             "2026-07-15T20:00:00Z",
		ValidFrom:            "2026-07-15T20:00:00Z",
		Status:               ReceiptValid,
	})
	if err == nil {
		t.Fatal("expected missing parent grant/delegation to fail")
	}
}

func TestValidateAuthorityResolutionRequiresBoundMetadata(t *testing.T) {
	err := ValidateAuthorityResolution(AuthorityResolution{
		Status: ReceiptValid,
		OperationResults: []AuthorityResolutionOperation{{
			OperationID:       "op.modify.closure",
			Status:            ReceiptValid,
			SelectedMechanism: MechanismRepositoryEdit,
		}},
	})
	if err == nil {
		t.Fatal("expected missing bound metadata to fail")
	}
}

func TestValidateAuthorityResolutionRequiresOperationResults(t *testing.T) {
	err := ValidateAuthorityResolution(AuthorityResolution{
		ActorBindingDigestSHA256:         "actor123",
		BaseBindingDigestSHA256:          "base123",
		ClosureAssessmentDigestSHA256:    "closure123",
		OperationSetDigestSHA256:         "ops123",
		AuthorityPolicyGraphDigestSHA256: "graph123",
		PolicyID:                         "authority.strict.v1",
		EvaluatedAt:                      "2026-07-15T20:00:00Z",
		Status:                           ReceiptValid,
	})
	if err == nil {
		t.Fatal("expected missing operation results to fail")
	}
}

func TestValidateLedgerEntryRejectsUnknownEventType(t *testing.T) {
	err := ValidateLedgerEntry(LedgerEntry{
		Sequence:   1,
		EventType:  LedgerEventType("bogus"),
		Task:       TaskBinding{ID: "task.example", SessionID: "session.example"},
		Payload:    LedgerPayloadRef{Path: "artifacts/sha256/abc.yaml", MediaType: "application/yaml", DigestSHA256: "abc"},
		Producer:   "sensei",
		ProducedAt: "2026-07-15T12:00:00Z",
	})
	if err == nil {
		t.Fatal("expected unknown event type to fail")
	}
}

func TestValidateLedgerEntryRejectsEscapingPayloadPath(t *testing.T) {
	err := ValidateLedgerEntry(LedgerEntry{
		Sequence:   1,
		EventType:  LedgerEventTaskPrepared,
		Task:       TaskBinding{ID: "task.example", SessionID: "session.example"},
		Payload:    LedgerPayloadRef{Path: "../escape.yaml", MediaType: "application/yaml", DigestSHA256: "abc"},
		Producer:   "sensei",
		ProducedAt: "2026-07-15T12:00:00Z",
	})
	if err == nil {
		t.Fatal("expected escaping payload path to fail")
	}
}
