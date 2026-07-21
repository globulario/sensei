// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// TaskCertifyOptions drives one ledger-integrated certification run.
type TaskCertifyOptions struct {
	TaskDir string
	// ExpectedHeadDigestSHA256 is required: the caller must state which ledger
	// head it certified against; a stale head is refused, never merged.
	ExpectedHeadDigestSHA256 string
	// RequestPath overrides the default <TaskDir>/certification-request.yaml.
	RequestPath string
	// RepoRoot points at the repository whose governed authority sources
	// (docs/awareness/*) are loaded independently to re-verify delegated
	// authority. When empty, no governed grants are available and any
	// delegated operation fails closed in the authority lane.
	RepoRoot string
	// ProducerID defaults to GeneratedBy.
	ProducerID string
	// ProducedAt stamps the appended ledger event. Required (the engine never
	// reads the wall clock; the CLI adapter supplies it).
	ProducedAt time.Time
}

// TaskCertifyResult reports one ledger-integrated run. Appended is true only
// when a `certified` event was written; a blocked/uncertifiable/stale/
// review_required evaluation returns the deterministic Result with the ledger
// untouched. No path in this package ever appends a `completed` event —
// completion is Phase 8's transaction.
type TaskCertifyResult struct {
	Result       Result                           `json:"result" yaml:"result"`
	Appended     bool                             `json:"appended" yaml:"appended"`
	Head         ledger.Head                      `json:"head" yaml:"head"`
	ReceiptRef   closureprotocol.LedgerPayloadRef `json:"receipt_ref,omitempty" yaml:"receipt_ref,omitempty"`
	Verification ledger.VerificationReport        `json:"verification" yaml:"verification"`
}

func taskPayloadValidator(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
	return ledger.ValidateTaskEventPayload(eventType, data)
}

// CertifyTask runs the full ledger-integrated certification flow:
//
//	verify ledger -> require expected head -> load request -> resolve
//	digest-verified records -> evaluate -> (on a certifying verdict) store the
//	receipt content-addressed, append the frozen `certified` event, rebuild
//	projections, and verify the ledger again.
//
// The CLI adapter contains none of these rules.
func CertifyTask(opts TaskCertifyOptions) (TaskCertifyResult, error) {
	taskDir := strings.TrimSpace(opts.TaskDir)
	if taskDir == "" {
		return TaskCertifyResult{}, fmt.Errorf("%w: task directory is required", ErrRequestInvalid)
	}
	if opts.ProducedAt.IsZero() {
		return TaskCertifyResult{}, fmt.Errorf("%w: produced_at is required", ErrRequestInvalid)
	}
	producer := strings.TrimSpace(opts.ProducerID)
	if producer == "" {
		producer = GeneratedBy
	}

	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(taskPayloadValidator))
	report, err := store.Verify()
	if err != nil {
		return TaskCertifyResult{}, err
	}
	if !report.Valid {
		return TaskCertifyResult{}, fmt.Errorf("%w: %s", ErrLedgerInvalid, summarizeErrors(report))
	}
	if report.EntryCount == 0 {
		return TaskCertifyResult{}, fmt.Errorf("%w: task ledger has no entries", ErrLedgerInvalid)
	}
	expected := strings.TrimSpace(opts.ExpectedHeadDigestSHA256)
	if expected == "" {
		return TaskCertifyResult{}, fmt.Errorf("%w: expected head digest is required", ErrRequestInvalid)
	}
	if report.HeadDigestSHA256 != expected {
		return TaskCertifyResult{}, fmt.Errorf("%w: expected %s, head is %s", ErrStaleExpectedHead, expected, report.HeadDigestSHA256)
	}

	requestPath := strings.TrimSpace(opts.RequestPath)
	if requestPath == "" {
		requestPath = filepath.Join(taskDir, RequestFileName)
	}
	req, err := LoadRequest(requestPath)
	if err != nil {
		return TaskCertifyResult{}, err
	}
	if req.TaskID != report.TaskID {
		return TaskCertifyResult{}, fmt.Errorf("%w: request task %q, ledger task %q", ErrTaskMismatch, req.TaskID, report.TaskID)
	}
	policy, ok := PolicyByID(req.PolicyID)
	if !ok {
		return TaskCertifyResult{}, fmt.Errorf("%w: %q", ErrPolicyUnknown, req.PolicyID)
	}
	records, err := ResolveRecords(DirSource{Dir: taskDir}, req)
	if err != nil {
		return TaskCertifyResult{}, err
	}
	// Load the governed authority sources independently so the authority lane
	// re-verifies any delegated authority against the repository's grants and
	// policies, never against the request bundle. A missing repo root leaves the
	// governed index empty, and delegated operations fail closed.
	if root := strings.TrimSpace(opts.RepoRoot); root != "" {
		index, err := authority.LoadPolicyIndex(root)
		if err != nil {
			return TaskCertifyResult{}, err
		}
		records.GovernedAuthority = index
	}
	result, err := Evaluate(req, records, policy)
	if err != nil {
		return TaskCertifyResult{}, err
	}

	out := TaskCertifyResult{Result: result, Verification: report}
	verdict := result.Receipt.CertificationVerdict
	if verdict != closureprotocol.Certified && verdict != closureprotocol.CertifiedWithConditions {
		// A non-certifying evaluation is a deterministic report; it never
		// pretends a certified event occurred.
		return out, nil
	}

	receiptBytes, err := CanonicalReceiptBytes(result.Receipt)
	if err != nil {
		return TaskCertifyResult{}, err
	}
	receiptRef, err := store.StoreArtifactBytes(receiptBytes, "application/json")
	if err != nil {
		return TaskCertifyResult{}, err
	}

	sessionID, err := ledgerSessionID(store)
	if err != nil {
		return TaskCertifyResult{}, err
	}
	resultBinding := result.Receipt.ResultBinding
	appendResult, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID:                   report.TaskID,
		SessionID:                sessionID,
		ExpectedHeadDigestSHA256: expected,
		EventType:                closureprotocol.LedgerEventCertified,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventCertified,
			TaskID:        report.TaskID,
			SessionID:     sessionID,
			TaskPhase:     closureprotocol.PhaseCertified,
			Status:        string(verdict),
			ResultBinding: &resultBinding,
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{
				"certification_receipt": receiptRef,
			},
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       producer,
		ProducedAt:       opts.ProducedAt,
	})
	if err != nil {
		return TaskCertifyResult{}, err
	}
	if _, err := ledger.RebuildProjections(taskDir, taskPayloadValidator); err != nil {
		return TaskCertifyResult{}, err
	}
	final, err := store.Verify()
	if err != nil {
		return TaskCertifyResult{}, err
	}
	if !final.Valid {
		return TaskCertifyResult{}, fmt.Errorf("%w after append: %s", ErrLedgerInvalid, summarizeErrors(final))
	}

	out.Appended = !appendResult.Replay
	out.Head = appendResult.Head
	out.ReceiptRef = receiptRef
	out.Verification = final
	return out, nil
}

func ledgerSessionID(store *ledger.Store) (string, error) {
	chain, err := store.VerifyChain()
	if err != nil {
		return "", err
	}
	if len(chain.Entries) == 0 {
		return "", fmt.Errorf("%w: task ledger has no entries", ErrLedgerInvalid)
	}
	return chain.Entries[len(chain.Entries)-1].Entry.Task.SessionID, nil
}

func summarizeErrors(report ledger.VerificationReport) string {
	var parts []string
	for _, e := range report.Errors {
		parts = append(parts, e.Code)
	}
	if len(parts) == 0 {
		return "no detail"
	}
	return strings.Join(parts, ", ")
}
