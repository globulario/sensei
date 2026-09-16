// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"gopkg.in/yaml.v3"
)

const EventPayloadSchemaVersion = "1"

type TaskEventPayload struct {
	SchemaVersion string                                      `json:"schema_version" yaml:"schema_version"`
	EventType     closureprotocol.LedgerEventType             `json:"event_type,omitempty" yaml:"event_type,omitempty"`
	TaskID        string                                      `json:"task_id" yaml:"task_id"`
	SessionID     string                                      `json:"session_id" yaml:"session_id"`
	TaskPhase     closureprotocol.TaskPhase                   `json:"task_phase,omitempty" yaml:"task_phase,omitempty"`
	Status        string                                      `json:"status,omitempty" yaml:"status,omitempty"`
	BaseBinding   *closureprotocol.BaseBinding                `json:"base_binding,omitempty" yaml:"base_binding,omitempty"`
	ResultBinding *closureprotocol.ResultBinding              `json:"result_binding,omitempty" yaml:"result_binding,omitempty"`
	Artifacts     map[string]closureprotocol.LedgerPayloadRef `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Limitations   []string                                    `json:"limitations,omitempty" yaml:"limitations,omitempty"`
}

type ImportOptions struct {
	ProducerID string
	ProducedAt time.Time
}

type ImportResult struct {
	TaskID      string
	SessionID   string
	Head        Head
	Replay      bool
	Limitations []string
}

func ParseTaskEventPayload(data []byte) (TaskEventPayload, error) {
	var payload TaskEventPayload
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return TaskEventPayload{}, err
	}
	return payload, nil
}

func ValidateTaskEventPayload(eventType closureprotocol.LedgerEventType, data []byte) error {
	payload, err := ParseTaskEventPayload(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(payload.SchemaVersion) != EventPayloadSchemaVersion {
		return fmt.Errorf("task event payload schema_version must be %q", EventPayloadSchemaVersion)
	}
	if strings.TrimSpace(payload.TaskID) == "" || strings.TrimSpace(payload.SessionID) == "" {
		return fmt.Errorf("task event payload requires task_id and session_id")
	}
	if payload.EventType != "" && payload.EventType != eventType {
		return fmt.Errorf("task event payload event_type %q does not match ledger event %q", payload.EventType, eventType)
	}
	if payload.BaseBinding != nil {
		if err := binding.ValidateBase(*payload.BaseBinding); err != nil {
			return err
		}
	}
	if payload.ResultBinding != nil {
		if err := binding.ValidateResult(*payload.ResultBinding); err != nil {
			return err
		}
	}
	for name, ref := range payload.Artifacts {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("artifact name is required")
		}
		if err := closureprotocol.ValidateLedgerPayloadRef(ref); err != nil {
			return err
		}
	}
	for _, key := range RequiredArtifactKeys(eventType) {
		if _, ok := payload.Artifacts[key]; !ok {
			return fmt.Errorf("%s event requires a %s artifact", eventType, key)
		}
	}
	return nil
}

// requiredEventArtifacts names, per event type, the content-addressed artifacts an event of
// that type MUST reference. The receipts' own fields are validated where they are loaded; the
// event contract here only requires the artifact to be present.
//
// A TABLE RATHER THAN A CHAIN OF IF-BLOCKS, because the set is the thing that was wrong. Two
// of nineteen event types declared a requirement, and admission_consumed -- which records that
// a single-use mutation capability was SPENT -- was not one of them. So an event of that type
// carrying no artifact validated, appended, and became the latest event of its type; the reader
// scanning backwards stopped at it and never reached the real record.
//
// Adding an authority-bearing event type is now a data change in one place that a test can
// enumerate, instead of a rule someone must remember to write.
var requiredEventArtifacts = map[closureprotocol.LedgerEventType][]string{
	closureprotocol.LedgerEventResultTransitionRecorded:    {"result_transition_receipt"},
	closureprotocol.LedgerEventQuestionDispositionRecorded: {"question_disposition_receipt"},
	closureprotocol.LedgerEventAdmissionConsumed:           {"capability_consumption"},
}

// RequiredArtifactKeys returns the artifact keys an event of this type must carry.
func RequiredArtifactKeys(eventType closureprotocol.LedgerEventType) []string {
	return requiredEventArtifacts[eventType]
}
