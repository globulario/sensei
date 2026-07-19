// SPDX-License-Identifier: AGPL-3.0-only

package resultrecording

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
)

// projectionDoc is the canonical, deterministic projection carried on the
// recording event. It references the exact result identity and the honest next
// state; it never claims certification or completion, and it uses only explicit
// candidate timestamps — never the wall clock.
type projectionDoc struct {
	SchemaVersion             string                        `json:"schema_version"`
	Kind                      string                        `json:"kind"`
	TaskID                    string                        `json:"task_id"`
	SessionID                 string                        `json:"session_id"`
	TransitionID              string                        `json:"transition_id"`
	ResultBinding             closureprotocol.ResultBinding `json:"result_binding"`
	ResultBindingDigestSHA256 string                        `json:"result_binding_digest_sha256"`
	ReceiptDigestSHA256       string                        `json:"receipt_digest_sha256"`
	TaskPhase                 closureprotocol.TaskPhase     `json:"task_phase"`
	OperationalStatus         string                        `json:"operational_status"`
	WaitingOn                 []string                      `json:"waiting_on"`
	NextAction                string                        `json:"next_action"`
	EvaluatedAt               string                        `json:"evaluated_at"`
	RecordedAt                string                        `json:"recorded_at"`
	Limitations               []string                      `json:"limitations"`
}

const projectionSchemaVersion = "resultrecording.projection/v1"

func newProjectionDoc(kind string, c resultpipeline.TransitionCandidate, next NextState) projectionDoc {
	waiting := next.WaitingOn
	if waiting == nil {
		waiting = []string{}
	}
	lims := c.Receipt.Limitations
	if lims == nil {
		lims = []string{}
	}
	return projectionDoc{
		SchemaVersion:             projectionSchemaVersion,
		Kind:                      kind,
		TaskID:                    c.Receipt.Task.ID,
		SessionID:                 c.Receipt.Task.SessionID,
		TransitionID:              c.Receipt.TransitionID,
		ResultBinding:             c.Receipt.ResultBinding,
		ResultBindingDigestSHA256: c.Receipt.ResultBindingDigestSHA256,
		ReceiptDigestSHA256:       c.Receipt.ReceiptDigestSHA256,
		TaskPhase:                 next.TaskPhase,
		OperationalStatus:         next.OperationalStatus,
		WaitingOn:                 waiting,
		NextAction:                next.NextAction,
		EvaluatedAt:               c.BuildResult.EvaluatedAt,
		RecordedAt:                c.Receipt.RecordedAt,
		Limitations:               lims,
	}
}

// renderProjection returns deterministic canonical bytes for one projection kind.
func renderProjection(kind string, c resultpipeline.TransitionCandidate, next NextState) ([]byte, error) {
	return closureprotocol.CanonicalJSON(newProjectionDoc(kind, c, next))
}
