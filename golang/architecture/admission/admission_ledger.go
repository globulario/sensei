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

func recordAdmissionEvent(store *ledger.Store, expectedHead, taskID, sessionID string, eventType closureprotocol.LedgerEventType, records map[string]any, producedAt time.Time) (ledger.AppendResult, error) {
	artifacts := make(map[string]closureprotocol.LedgerPayloadRef, len(records))
	for key, record := range records {
		data, err := closureprotocol.CanonicalJSON(record)
		if err != nil {
			return ledger.AppendResult{}, err
		}
		ref, err := store.StoreArtifactBytes(data, "application/json")
		if err != nil {
			return ledger.AppendResult{}, err
		}
		artifacts[key] = ref
	}
	payload := ledger.TaskEventPayload{
		SchemaVersion: ledger.EventPayloadSchemaVersion,
		EventType:     eventType,
		TaskID:        taskID,
		SessionID:     sessionID,
		Artifacts:     artifacts,
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

// RecordAuthorityResolved appends an authority_resolved event carrying the
// resolution together with the exact actor binding, typed change plan, and base
// binding it was computed for, so downstream admission can load them as verified
// task records rather than reconstructing them from caller flags.
func RecordAuthorityResolved(store *ledger.Store, expectedHead string, task closureprotocol.TaskBinding, resolution closureprotocol.AuthorityResolution, actor closureprotocol.ActorBinding, changePlan closureprotocol.ChangePlan, base closureprotocol.BaseBinding, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, task.ID, task.SessionID, closureprotocol.LedgerEventAuthorityResolved, map[string]any{
		"authority_resolution": resolution,
		"actor_binding":        actor,
		"change_plan":          changePlan,
		"base_binding":         base,
	}, producedAt)
}

// RecordAdmissionDecided appends an admission_decided event carrying the typed
// decision as a bound artifact. expectedHead is the current ledger head digest.
func RecordAdmissionDecided(store *ledger.Store, expectedHead string, decision closureprotocol.AdmissionDecision, task closureprotocol.TaskBinding, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, task.ID, task.SessionID, closureprotocol.LedgerEventAdmissionDecided, map[string]any{"admission_decision": decision}, producedAt)
}

// RecordAdmissionConsumed appends an admission_consumed event carrying the
// single-use capability consumption receipt.
func RecordAdmissionConsumed(store *ledger.Store, expectedHead string, consumption closureprotocol.CapabilityConsumption, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, consumption.Task.ID, consumption.Task.SessionID, closureprotocol.LedgerEventAdmissionConsumed, map[string]any{"capability_consumption": consumption}, producedAt)
}

// RecordChangeObserved appends a change_observed event carrying the exact
// observed change set as a bound artifact, between admission_consumed and
// scope_verified. It records the observed mutation itself (not merely a result
// tree digest) so scope verification and the later result transition bind the
// same observed change.
func RecordChangeObserved(store *ledger.Store, expectedHead string, task closureprotocol.TaskBinding, observed ObservedChangeSet, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, task.ID, task.SessionID, closureprotocol.LedgerEventChangeObserved, map[string]any{"observed_change_set": observed}, producedAt)
}

// RecordScopeVerified appends a scope_verified event carrying the typed scope
// verification receipt.
func RecordScopeVerified(store *ledger.Store, expectedHead string, task closureprotocol.TaskBinding, verification ScopeVerification, producedAt time.Time) (ledger.AppendResult, error) {
	return recordAdmissionEvent(store, expectedHead, task.ID, task.SessionID, closureprotocol.LedgerEventScopeVerified, map[string]any{"scope_verification": verification}, producedAt)
}
