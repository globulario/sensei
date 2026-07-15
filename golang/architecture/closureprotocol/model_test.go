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
