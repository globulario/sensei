// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// ArtifactKeyReceipt is the ledger artifact key the question_disposition_recorded
// event must carry (enforced by ledger.ValidateTaskEventPayload).
const ArtifactKeyReceipt = "question_disposition_receipt"

// ReceiptMediaType is the canonical media type for a stored disposition receipt.
// A raw []byte payload at this type content-addresses by raw sha256 (ledger
// artifact.renderPayload), so the stored digest equals the receipt byte digest.
const ReceiptMediaType = "application/json"

// eventPayloadMediaType matches the recording boundary: the task-event envelope
// is rendered as canonical YAML.
const eventPayloadMediaType = "application/yaml"

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// dispositionPayloadValidator is the strict ledger PayloadValidator for the
// disposition boundary: generic task-event validation plus the disposition
// event contract (the receipt artifact must be present).
func dispositionPayloadValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	if err := ledger.ValidateTaskEventPayload(eventType, data); err != nil {
		return err
	}
	if eventType != closureprotocol.LedgerEventQuestionDispositionRecorded {
		return nil
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return qdErr(CodeEventPayloadInvalid, "parse: %v", err)
	}
	return validateDispositionEventPayload(payload)
}

// validateDispositionEventPayload enforces that the event names the disposition
// it records and does not smuggle a phase/status transition — 8.1a records a
// question outcome, it never advances the task lifecycle.
func validateDispositionEventPayload(payload ledger.TaskEventPayload) error {
	if payload.EventType != "" && payload.EventType != closureprotocol.LedgerEventQuestionDispositionRecorded {
		return qdErr(CodeEventPayloadInvalid, "event_type must be %q", closureprotocol.LedgerEventQuestionDispositionRecorded)
	}
	if _, ok := payload.Artifacts[ArtifactKeyReceipt]; !ok {
		return qdErr(CodeEventPayloadInvalid, "missing %q artifact", ArtifactKeyReceipt)
	}
	if payload.TaskPhase != "" {
		return qdErr(CodeEventPayloadInvalid, "disposition event must not carry a task_phase")
	}
	if payload.Status != "" {
		return qdErr(CodeEventPayloadInvalid, "disposition event must not carry a status")
	}
	if payload.ResultBinding != nil {
		return qdErr(CodeEventPayloadInvalid, "disposition event must not carry a result_binding")
	}
	return nil
}
