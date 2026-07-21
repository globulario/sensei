// SPDX-License-Identifier: AGPL-3.0-only

package admission

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Phase 3 admission v2, slice 2: single-use capability consumption. A minted
// capability authorizes exactly one act of mutation; consuming it a second time
// (replay) or after expiry must fail closed. This follows the bootstrap-
// direction consumption pattern (one-use, atomic across tasks).

// ConsumeCapability produces a first-use CapabilityConsumption receipt for an
// admitted decision. It is stateless: enforcing that a capability is consumed
// at most once across calls is the CapabilityRegistry's job. It fails closed
// when the decision did not admit every operation, when the capability has
// expired at consumedAt, or when a consumed operation was not one the decision
// admitted.
func ConsumeCapability(decision closureprotocol.AdmissionDecision, task closureprotocol.TaskBinding, consumer closureprotocol.ActorBinding, consumedOperationIDs []string, consumedAt string) (closureprotocol.CapabilityConsumption, error) {
	if err := closureprotocol.ValidateAdmissionDecision(decision); err != nil {
		return closureprotocol.CapabilityConsumption{}, err
	}
	if !AllAdmitted(decision) {
		return closureprotocol.CapabilityConsumption{}, errors.New("capability refers to a decision that did not admit every operation")
	}
	consumedTime, err := time.Parse(time.RFC3339, consumedAt)
	if err != nil {
		return closureprotocol.CapabilityConsumption{}, errors.New("consumed_at must be RFC3339")
	}
	if strings.TrimSpace(decision.CapabilityExpiry) != "" {
		expiry, err := time.Parse(time.RFC3339, decision.CapabilityExpiry)
		if err != nil {
			return closureprotocol.CapabilityConsumption{}, errors.New("decision capability_expiry must be RFC3339")
		}
		if consumedTime.After(expiry) {
			return closureprotocol.CapabilityConsumption{}, fmt.Errorf("capability %s expired at %s", decision.CapabilityID, decision.CapabilityExpiry)
		}
	}

	admitted := admittedOperationSet(decision)
	consumed := closureprotocol.NormalizeSet(consumedOperationIDs)
	if len(consumed) == 0 {
		return closureprotocol.CapabilityConsumption{}, errors.New("consumed_operation_ids are required")
	}
	for _, op := range consumed {
		if !admitted[op] {
			return closureprotocol.CapabilityConsumption{}, fmt.Errorf("operation %s was not admitted by this capability", op)
		}
	}

	decisionDigest, err := closureprotocol.SemanticDigest(decision)
	if err != nil {
		return closureprotocol.CapabilityConsumption{}, err
	}

	consumption := closureprotocol.CapabilityConsumption{
		CapabilityID:         decision.CapabilityID,
		Task:                 task,
		ConsumerActor:        consumer,
		ConsumedOperationIDs: consumed,
		ConsumedAt:           consumedTime.UTC().Format(time.RFC3339),
		DecisionDigestSHA256: decisionDigest,
		OneUseStatus:         closureprotocol.ReceiptValid,
	}
	if err := closureprotocol.ValidateCapabilityConsumption(consumption); err != nil {
		return closureprotocol.CapabilityConsumption{}, err
	}
	return consumption, nil
}

// CapabilityRegistry enforces single-use across consumption attempts: the first
// consume of a capability id wins; any later attempt on the same id is a replay
// and fails closed. It is safe for concurrent use and is atomic across tasks —
// two tasks racing on the same capability id cannot both succeed.
type CapabilityRegistry struct {
	mu       sync.Mutex
	consumed map[string]closureprotocol.CapabilityConsumption
}

// NewCapabilityRegistry returns an empty registry.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{consumed: make(map[string]closureprotocol.CapabilityConsumption)}
}

// Consume produces and records a first-use receipt, or fails closed if the
// capability was already consumed (replay).
func (r *CapabilityRegistry) Consume(decision closureprotocol.AdmissionDecision, task closureprotocol.TaskBinding, consumer closureprotocol.ActorBinding, consumedOperationIDs []string, consumedAt string) (closureprotocol.CapabilityConsumption, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if prior, ok := r.consumed[decision.CapabilityID]; ok {
		return closureprotocol.CapabilityConsumption{}, fmt.Errorf("capability %s already consumed by task %s", decision.CapabilityID, prior.Task.ID)
	}
	consumption, err := ConsumeCapability(decision, task, consumer, consumedOperationIDs, consumedAt)
	if err != nil {
		return closureprotocol.CapabilityConsumption{}, err
	}
	r.consumed[decision.CapabilityID] = consumption
	return consumption, nil
}

func admittedOperationSet(decision closureprotocol.AdmissionDecision) map[string]bool {
	out := make(map[string]bool, len(decision.OperationVerdicts))
	for _, v := range decision.OperationVerdicts {
		if v.Verdict == AdmissionVerdictAdmitted {
			out[v.OperationID] = true
		}
	}
	return out
}
