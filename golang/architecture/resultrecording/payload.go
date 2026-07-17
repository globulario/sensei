// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// validTransitionPhaseStatus is the closed set of (phase, status) combinations a
// recording event may carry.
var validTransitionPhaseStatus = map[closureprotocol.TaskPhase]map[string]bool{
	closureprotocol.PhaseProving:                 {StatusReadyForProving: true},
	closureprotocol.PhaseScopeVerified:           {StatusWaitingArchitect: true, StatusWaitingGovernance: true, StatusWaitingMechanicalRepair: true},
	closureprotocol.PhaseWaitingArchitect:        {StatusWaitingArchitect: true},
	closureprotocol.PhaseWaitingGovernance:       {StatusWaitingGovernance: true},
	closureprotocol.PhaseWaitingMechanicalRepair: {StatusWaitingMechanicalRepair: true},
}

// ValidateResultTransitionEventPayload enforces the strong recording-boundary
// contract on a result_transition_recorded payload. The generic historical
// payload validator stays compatible; this is applied only at the new boundary.
func ValidateResultTransitionEventPayload(payload ledger.TaskEventPayload) error {
	if payload.EventType != closureprotocol.LedgerEventResultTransitionRecorded {
		return recErr(CodeEventPayloadInvalid, "event type %q, want result_transition_recorded", payload.EventType)
	}
	if strings.TrimSpace(payload.TaskID) == "" || strings.TrimSpace(payload.SessionID) == "" {
		return recErr(CodeEventPayloadInvalid, "event requires task and session id")
	}
	if payload.ResultBinding == nil {
		return recErr(CodeEventPayloadInvalid, "event requires the exact result binding")
	}
	if err := validateArtifactKeySet(payload.Artifacts); err != nil {
		return err
	}
	// Every ref confined, valid, and non-conflicting.
	seenPath := map[string]string{}
	for key, ref := range payload.Artifacts {
		if err := validateRef(ref); err != nil {
			return recErr(CodeEventPayloadInvalid, "artifact %q: %v", key, err)
		}
		if mt, dup := seenPath[ref.Path]; dup && mt != ref.MediaType {
			return recErr(CodeEventPayloadInvalid, "artifact path %q has conflicting media types", ref.Path)
		}
		seenPath[ref.Path] = ref.MediaType
	}
	// Valid phase/status combination.
	statuses, ok := validTransitionPhaseStatus[payload.TaskPhase]
	if !ok || !statuses[payload.Status] {
		return recErr(CodeEventPayloadInvalid, "invalid phase/status combination %q/%q", payload.TaskPhase, payload.Status)
	}
	return nil
}

func validateRef(ref closureprotocol.LedgerPayloadRef) error {
	p := strings.TrimSpace(ref.Path)
	if p == "" {
		return recErr(CodeEventPayloadInvalid, "artifact ref has no path")
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
		return recErr(CodeEventPayloadInvalid, "artifact ref path is not confined")
	}
	if !isHex64(ref.DigestSHA256) {
		return recErr(CodeEventPayloadInvalid, "artifact ref digest is not a 64-hex sha256")
	}
	if strings.TrimSpace(ref.MediaType) == "" {
		return recErr(CodeEventPayloadInvalid, "artifact ref has no media type")
	}
	return nil
}

// recordingPayloadValidator is the strict ledger PayloadValidator for the recording
// boundary: it applies the generic task-event validation and the stronger
// result-transition contract.
func recordingPayloadValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	if err := ledger.ValidateTaskEventPayload(eventType, data); err != nil {
		return err
	}
	if eventType != closureprotocol.LedgerEventResultTransitionRecorded {
		return nil
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return recErr(CodeEventPayloadInvalid, "parse: %v", err)
	}
	return ValidateResultTransitionEventPayload(payload)
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
