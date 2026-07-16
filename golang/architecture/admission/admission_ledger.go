// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"context"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// Phase 3 admission v2, slice 4a: record the typed admission producers onto the
// append-only task ledger. Each record is stored as a digest-bound artifact and
// referenced from a task event payload, so the decision, its single-use
// consumption, and the scope verification become hash-chained, non-repudiable
// events on the task's truth surface.

const admissionProducerID = "admission-v2"

func recordAdmissionEvent(store *ledger.Store, expectedHead, taskID, sessionID string, eventType closureprotocol.LedgerEventType, artifactKey string, record any, producedAt time.Time) (ledger.AppendResult, error) {
	data, err := closureprotocol.CanonicalJSON(record)
	if err != nil {
		return ledger.AppendResult{}, err
	}
	ref, err := store.StoreArtifactBytes(data, "application/json")
	if err != nil {
		return ledger.AppendResult{}, err
	}
	payload := ledger.TaskEventPayload{
		SchemaVersion: ledger.EventPayloadSchemaVersion,
		EventType:     eventType,
		TaskID:        taskID,
		SessionID:     sessionID,
		Artifacts:     map[string]closureprotocol.LedgerPayloadRef{artifactKey: ref},
	}
	return store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   taskID,
		SessionID:                sessionID,
		ExpectedHeadDigestSHA256: expectedHead,
		EventType:                eventType,
		Payload:                  payload,
		PayloadMediaType:         "application/yaml",
		ProducerID:               admissionProducerID,
		ProducedAt:               producedAt,
	})
}

// RecordAdmissionDecided appends an admission_decided event carrying the typed
// decision as a bound artifact. expectedHead is the current ledger head digest.
func RecordAdmissionDecided(store *ledger.Store, expectedHead string, decision closureprotocol.AdmissionDecision, task closureprotocol.TaskBinding, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, task.ID, task.SessionID, closureprotocol.LedgerEventAdmissionDecided, "admission_decision", decision, producedAt)
}

// RecordAdmissionConsumed appends an admission_consumed event carrying the
// single-use capability consumption receipt.
func RecordAdmissionConsumed(store *ledger.Store, expectedHead string, consumption closureprotocol.CapabilityConsumption, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, consumption.Task.ID, consumption.Task.SessionID, closureprotocol.LedgerEventAdmissionConsumed, "capability_consumption", consumption, producedAt)
}

// RecordScopeVerified appends a scope_verified event carrying the typed scope
// verification receipt.
func RecordScopeVerified(store *ledger.Store, expectedHead string, task closureprotocol.TaskBinding, verification ScopeVerification, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, task.ID, task.SessionID, closureprotocol.LedgerEventScopeVerified, "scope_verification", verification, producedAt)
}
