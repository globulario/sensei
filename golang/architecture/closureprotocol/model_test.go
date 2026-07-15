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

