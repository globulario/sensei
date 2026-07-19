// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

const recordingProducerID = GeneratedBy

// RecordOutcome is the closed set of disposition-recording dispositions.
type RecordOutcome string

const (
	OutcomeRecorded   RecordOutcome = "recorded"
	OutcomeReplayed   RecordOutcome = "replayed"
	OutcomeReconciled RecordOutcome = "reconciled"
	OutcomeContested  RecordOutcome = "contested"
)

// RecordRequest records a prepared disposition candidate.
type RecordRequest struct {
	TaskDirectory string
	Candidate     DispositionCandidate
}

// RecordResult reports the durable identity of the recorded disposition.
type RecordResult struct {
	Outcome                  RecordOutcome
	QuestionID               string
	ReceiptDigestSHA256      string
	EntryDigestSHA256        string
	PreviousLedgerHeadSHA256 string
	CurrentLedgerHeadSHA256  string
	LedgerSequence           int
	ReceiptRef               closureprotocol.LedgerPayloadRef
	// ContestedPriorDigests lists prior conflicting disposition receipts for the
	// same question+result. When non-empty the question is contested: both this
	// and the prior records are immutable and preserved; nothing is overwritten.
	ContestedPriorDigests []string
	ProjectionState       string
}

// RecordDisposition appends a question_disposition_recorded event under
// expected-head protection. An exact replay creates no second event. A
// conflicting second disposition for the same question+result is appended as a
// new immutable record and reported as contested — never a replacement. Once the
// entry is durable, every failure is a PostCommitError.
func RecordDisposition(ctx context.Context, req RecordRequest) (RecordResult, error) {
	c := req.Candidate
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if taskDir == "" {
		return RecordResult{}, qdErr(CodeInvalidRequest, "task directory is required")
	}
	if strings.TrimSpace(c.ReceiptByteDigestSHA256) == "" || len(c.ReceiptBytes) == 0 {
		return RecordResult{}, qdErr(CodeInvalidRequest, "candidate receipt bytes are required")
	}
	if strings.TrimSpace(c.ExpectedLedgerHeadDigestSHA256) == "" {
		return RecordResult{}, qdErr(CodeExpectedHeadRequired, "expected ledger head is required")
	}
	if err := Validate(c.Receipt); err != nil {
		return RecordResult{}, qdErr(CodeInvalidReceipt, "%v", err)
	}

	store := newStore(taskDir)
	chain, err := store.VerifyChain()
	if err != nil {
		return RecordResult{}, qdErr(CodeChainVerifyFailed, "%v", err)
	}

	// Replay vs contested is decided from the CURRENT verified chain, not the
	// Prepare-time snapshot.
	priors := dispositionsForQuestionResult(taskDir, chain,
		c.Receipt.QuestionID, c.Receipt.ResultTransitionReceiptDigestSHA256)
	var contested []string
	for _, p := range priors {
		if p.ReceiptDigestSHA256 == c.Receipt.ReceiptDigestSHA256 {
			// Exact replay: the disposition is already durable. Reconcile and
			// report it without appending a second event.
			return reconcileExisting(taskDir, store, c, OutcomeReplayed, contestedDigests(priors, c.Receipt.ReceiptDigestSHA256))
		}
		contested = append(contested, p.ReceiptDigestSHA256)
	}
	sort.Strings(contested)

	if strings.TrimSpace(chain.Head.EntryDigestSHA256) != c.ExpectedLedgerHeadDigestSHA256 {
		return RecordResult{}, qdErr(CodeStaleExpectedHead, "expected head does not match the current ledger head")
	}

	receiptRef, err := store.StoreArtifactBytes(c.ReceiptBytes, c.ReceiptMediaType)
	if err != nil {
		return RecordResult{}, qdErr(CodeArtifactStoreFailed, "store receipt: %v", err)
	}
	if receiptRef.DigestSHA256 != c.ReceiptByteDigestSHA256 {
		return RecordResult{}, qdErr(CodeArtifactStoreFailed, "stored receipt digest does not match the candidate")
	}

	payload := ledger.TaskEventPayload{
		SchemaVersion: ledger.EventPayloadSchemaVersion,
		EventType:     closureprotocol.LedgerEventQuestionDispositionRecorded,
		TaskID:        c.Receipt.Task.ID,
		SessionID:     c.Receipt.Task.SessionID,
		Artifacts:     map[string]closureprotocol.LedgerPayloadRef{ArtifactKeyReceipt: receiptRef},
	}
	if err := validateDispositionEventPayload(payload); err != nil {
		return RecordResult{}, qdErr(CodeEventPayloadInvalid, "%v", err)
	}

	// DisposedAt is ledger-anchored, so the append time is deterministic and a
	// retry produces a byte-identical entry.
	producedAt, perr := time.Parse(time.RFC3339, c.Receipt.DisposedAt)
	if perr != nil {
		return RecordResult{}, qdErr(CodeInvalidReceipt, "disposed_at is not RFC3339")
	}

	appended, err := store.Append(ctx, ledger.AppendRequest{
		TaskID:                   c.Receipt.Task.ID,
		SessionID:                c.Receipt.Task.SessionID,
		ExpectedHeadDigestSHA256: c.ExpectedLedgerHeadDigestSHA256,
		EventType:                closureprotocol.LedgerEventQuestionDispositionRecorded,
		Payload:                  payload,
		PayloadMediaType:         eventPayloadMediaType,
		ProducerID:               recordingProducerID,
		ProducedAt:               producedAt,
	})
	var durable ledger.ErrEntryDurable
	var stale ledger.ErrStaleHead
	switch {
	case err == nil:
		// appended, or a ledger-level exact replay; handled below.
	case errors.As(err, &durable):
		// Durable entry but HEAD write failed — a post-commit condition. Carry the
		// entry identity forward so postCommit reconciles HEAD.
		appended = ledger.AppendResult{Entry: durable.Entry, Head: durable.Head}
	case errors.As(err, &stale):
		return RecordResult{}, qdErr(CodeStaleExpectedHead, "ledger head moved during recording")
	default:
		return RecordResult{}, qdErr(CodeAppendFailed, "%v", err)
	}

	if appended.Replay {
		return reconcileExisting(taskDir, store, c, OutcomeReconciled, contested)
	}

	outcome := OutcomeRecorded
	if len(contested) > 0 {
		outcome = OutcomeContested
	}
	result := RecordResult{
		Outcome:                  outcome,
		QuestionID:               c.Receipt.QuestionID,
		ReceiptDigestSHA256:      c.Receipt.ReceiptDigestSHA256,
		EntryDigestSHA256:        appended.Entry.EntryDigestSHA256,
		PreviousLedgerHeadSHA256: c.ExpectedLedgerHeadDigestSHA256,
		CurrentLedgerHeadSHA256:  appended.Head.EntryDigestSHA256,
		LedgerSequence:           appended.Head.Sequence,
		ReceiptRef:               receiptRef,
		ContestedPriorDigests:    contested,
	}
	if err := postCommit(taskDir, store, &result); err != nil {
		return result, err
	}
	return result, nil
}

// reconcileExisting reports an already-durable disposition (exact replay or a
// concurrent identical writer) after reconciling derived state; it appends no
// second event.
func reconcileExisting(taskDir string, store *ledger.Store, c DispositionCandidate, outcome RecordOutcome, contested []string) (RecordResult, error) {
	recorded, err := LoadRecordedDisposition(taskDir, c.Receipt.ReceiptDigestSHA256)
	if err != nil {
		return RecordResult{}, qdErr(CodeReloadFailed, "reload existing disposition: %v", err)
	}
	rec, err := store.ReconcileDerivedState()
	if err != nil {
		return RecordResult{}, qdErr(CodeProjectionRebuild, "%v", err)
	}
	return RecordResult{
		Outcome:                  outcome,
		QuestionID:               c.Receipt.QuestionID,
		ReceiptDigestSHA256:      c.Receipt.ReceiptDigestSHA256,
		EntryDigestSHA256:        recorded.EntryDigestSHA256,
		PreviousLedgerHeadSHA256: c.ExpectedLedgerHeadDigestSHA256,
		CurrentLedgerHeadSHA256:  recorded.EntryDigestSHA256,
		LedgerSequence:           recorded.LedgerSequence,
		ReceiptRef:               recorded.ReceiptRef,
		ContestedPriorDigests:    contested,
		ProjectionState:          rec.ProjectionState,
	}, nil
}

// postCommit reconciles derived state after the entry is durable. Any failure
// here is a PostCommitError: the entry is authoritative and the caller retries
// the same candidate.
func postCommit(taskDir string, store *ledger.Store, result *RecordResult) error {
	post := func(code, detail string) *PostCommitError {
		return &PostCommitError{
			Code:                   code,
			QuestionID:             result.QuestionID,
			ReceiptDigestSHA256:    result.ReceiptDigestSHA256,
			EntryDigestSHA256:      result.EntryDigestSHA256,
			LedgerHeadDigestSHA256: result.CurrentLedgerHeadSHA256,
			RecoveryAction:         "retry the same candidate",
			Detail:                 detail,
		}
	}
	rec, err := store.ReconcileDerivedState()
	if err != nil {
		return post(CodeProjectionRebuild, err.Error())
	}
	recorded, err := LoadRecordedDisposition(taskDir, result.ReceiptDigestSHA256)
	if err != nil {
		return post(CodePostCommitValidation, err.Error())
	}
	if err := Validate(recorded.Receipt); err != nil {
		return post(CodePostCommitValidation, err.Error())
	}
	result.ProjectionState = rec.ProjectionState
	result.EntryDigestSHA256 = recorded.EntryDigestSHA256
	result.CurrentLedgerHeadSHA256 = recorded.EntryDigestSHA256
	return nil
}

func contestedDigests(priors []QuestionDispositionReceipt, exclude string) []string {
	var out []string
	for _, p := range priors {
		if p.ReceiptDigestSHA256 != exclude {
			out = append(out, p.ReceiptDigestSHA256)
		}
	}
	sort.Strings(out)
	return out
}
