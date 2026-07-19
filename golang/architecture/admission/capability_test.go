// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const (
	v2ConsumedAt = "2026-07-16T13:00:00Z" // within the decided_at+24h window
	v2ExpiredAt  = "2026-07-18T00:00:00Z" // after the 2026-07-17T12:05:00Z expiry
)

// admittedDecision returns a fully-admitted decision for the standard fixture.
func admittedDecision(t *testing.T) closureprotocol.AdmissionDecision {
	t.Helper()
	req, res := v2Fixture(t)
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatalf("DecideAdmission: %v", err)
	}
	return d
}

func v2Task() closureprotocol.TaskBinding {
	return closureprotocol.TaskBinding{ID: "task.v2", SessionID: "session.v2"}
}

func TestConsumeCapabilityFirstUse(t *testing.T) {
	d := admittedDecision(t)
	c, err := ConsumeCapability(d, v2Task(), v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt)
	if err != nil {
		t.Fatalf("ConsumeCapability: %v", err)
	}
	if c.OneUseStatus != closureprotocol.ReceiptValid {
		t.Fatalf("expected valid one-use status, got %q", c.OneUseStatus)
	}
	if c.CapabilityID != d.CapabilityID || c.DecisionDigestSHA256 == "" {
		t.Fatalf("consumption not bound to the decision: %+v", c)
	}
	if err := closureprotocol.ValidateCapabilityConsumption(c); err != nil {
		t.Fatalf("consumption failed frozen validation: %v", err)
	}
}

func TestConsumeCapabilityRejectsExpired(t *testing.T) {
	d := admittedDecision(t)
	if _, err := ConsumeCapability(d, v2Task(), v2ActorBinding(), []string{"op.modify.admission"}, v2ExpiredAt); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expected expiry rejection, got %v", err)
	}
}

func TestConsumeCapabilityRejectsUnadmittedDecision(t *testing.T) {
	req, res := v2Fixture(t)
	req.ChangePlan.Operations[0].SelectedMechanism = closureprotocol.MechanismOwnerRPC // forces a refusal
	d, err := DecideAdmission(req, res, v2Policy(), v2DecidedAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ConsumeCapability(d, v2Task(), v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt); err == nil || !strings.Contains(err.Error(), "did not admit every operation") {
		t.Fatalf("expected refusal to consume an unadmitted decision, got %v", err)
	}
}

func TestConsumeCapabilityRejectsUnadmittedOperation(t *testing.T) {
	d := admittedDecision(t)
	if _, err := ConsumeCapability(d, v2Task(), v2ActorBinding(), []string{"op.not.admitted"}, v2ConsumedAt); err == nil || !strings.Contains(err.Error(), "was not admitted") {
		t.Fatalf("expected rejection of an operation the capability never admitted, got %v", err)
	}
}

func TestCapabilityRegistryRejectsReplay(t *testing.T) {
	d := admittedDecision(t)
	reg := NewCapabilityRegistry()
	if _, err := reg.Consume(d, v2Task(), v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt); err != nil {
		t.Fatalf("first consume should succeed: %v", err)
	}
	if _, err := reg.Consume(d, v2Task(), v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("expected replay rejection on second consume, got %v", err)
	}
}

func TestCapabilityRegistryAtomicAcrossTasks(t *testing.T) {
	d := admittedDecision(t)
	reg := NewCapabilityRegistry()
	if _, err := reg.Consume(d, closureprotocol.TaskBinding{ID: "task.one", SessionID: "session.one"}, v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt); err != nil {
		t.Fatalf("task.one should win: %v", err)
	}
	// A second, different task cannot consume the same minted capability.
	if _, err := reg.Consume(d, closureprotocol.TaskBinding{ID: "task.two", SessionID: "session.two"}, v2ActorBinding(), []string{"op.modify.admission"}, v2ConsumedAt); err == nil || !strings.Contains(err.Error(), "already consumed") {
		t.Fatalf("expected the second task to be refused, got %v", err)
	}
}
