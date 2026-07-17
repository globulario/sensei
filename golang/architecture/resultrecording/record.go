// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedimpact"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
)

const recordingProducerID = "sensei.resultrecording/v1"

// RecordDisposition names the outcome of a recording attempt.
type RecordDisposition string

const (
	DispositionRecorded   RecordDisposition = "recorded"
	DispositionReplayed   RecordDisposition = "replayed"
	DispositionReconciled RecordDisposition = "reconciled"
)

// RecordRequest asks to record one validated candidate into a task ledger.
type RecordRequest struct {
	TaskDirectory string
	Candidate     resultpipeline.TransitionCandidate
}

// RecordedStageRef binds a canonical stage to its content-addressed reference.
type RecordedStageRef struct {
	Stage closureprotocol.ResultPipelineStage
	Ref   closureprotocol.LedgerPayloadRef
}

// RecordResult is the outcome of recording, derived — not a stored flag.
type RecordResult struct {
	Disposition RecordDisposition

	TransitionID             string
	ReceiptDigestSHA256      string
	EntryDigestSHA256        string
	PreviousLedgerHeadSHA256 string
	CurrentLedgerHeadSHA256  string
	LedgerSequence           int

	ReceiptRef      closureprotocol.LedgerPayloadRef
	ImpactReportRef closureprotocol.LedgerPayloadRef
	StageRefs       []RecordedStageRef

	TaskPhase         closureprotocol.TaskPhase
	OperationalStatus string
	NextAction        string

	ProjectionState string
}

// RecordTransition commits a validated candidate under expected-ledger-head
// protection. The ledger entry write is the sole authority commit; artifacts are
// content-addressed first but are authoritative only once the entry references
// them. Exact replay of an already-recorded transition appends no second event.
func RecordTransition(ctx context.Context, req RecordRequest) (RecordResult, error) {
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if taskDir == "" {
		return RecordResult{}, recErr(CodeInvalidRequest, "task directory is required")
	}
	c := req.Candidate
	if err := resultpipeline.ValidateTransitionCandidate(c); err != nil {
		return RecordResult{}, recErr(CodeInvalidCandidate, "%v", err)
	}
	taskID := strings.TrimSpace(c.Receipt.Task.ID)
	sessionID := strings.TrimSpace(c.Receipt.Task.SessionID)
	if taskID == "" || sessionID == "" {
		return RecordResult{}, recErr(CodeInvalidCandidate, "candidate task and session id are required")
	}
	if !isHex64(c.ExpectedLedgerHeadDigestSHA256) {
		return RecordResult{}, recErr(CodeInvalidCandidate, "candidate expected ledger head is not canonical")
	}

	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(recordingPayloadValidator))
	chain, err := store.VerifyChain()
	if err != nil {
		return RecordResult{}, recErr(CodeInvalidRequest, "verify chain: %v", err)
	}
	if strings.TrimSpace(chain.TaskID) != "" && chain.TaskID != taskID {
		return RecordResult{}, recErr(CodeTaskMismatch, "ledger task %q does not match candidate %q", chain.TaskID, taskID)
	}
	if err := verifyChainIdentity(chain, taskID, sessionID); err != nil {
		return RecordResult{}, err
	}

	// Existing-transition semantics: exact replay reconciles; a different receipt
	// for the same logical transition is a conflict.
	existing, found, err := findRecordedTransition(taskDir, chain, c.Receipt.TransitionID)
	if err != nil {
		return RecordResult{}, err
	}
	if found {
		if existing.receiptDigest != c.Receipt.ReceiptDigestSHA256 {
			return RecordResult{}, recErr(CodeTransitionIDConflict, "transition %q already recorded with a different receipt", c.Receipt.TransitionID)
		}
		return reconcileAndReport(taskDir, store, c, existing, DispositionReplayed)
	}

	// No replay: the expected head must be the current head.
	if strings.TrimSpace(chain.Head.EntryDigestSHA256) != c.ExpectedLedgerHeadDigestSHA256 {
		return RecordResult{}, recErr(CodeStaleExpectedHead, "expected head does not match the current ledger head")
	}

	next, err := ClassifyNextState(c.BuildResult.ProofRequirements)
	if err != nil {
		return RecordResult{}, err
	}

	artifacts, stageRefs, receiptRef, impactRef, err := storeArtifacts(store, c, next)
	if err != nil {
		return RecordResult{}, err
	}

	payload := ledger.TaskEventPayload{
		SchemaVersion: ledger.EventPayloadSchemaVersion,
		EventType:     closureprotocol.LedgerEventResultTransitionRecorded,
		TaskID:        taskID,
		SessionID:     sessionID,
		TaskPhase:     next.TaskPhase,
		Status:        next.OperationalStatus,
		ResultBinding: &c.Receipt.ResultBinding,
		Artifacts:     artifacts,
		Limitations:   c.Receipt.Limitations,
	}
	if err := ValidateResultTransitionEventPayload(payload); err != nil {
		return RecordResult{}, err
	}

	producedAt, perr := time.Parse(time.RFC3339, c.Receipt.RecordedAt)
	if perr != nil {
		return RecordResult{}, recErr(CodeInvalidCandidate, "recorded_at is not RFC3339")
	}
	appended, err := store.Append(ctx, ledger.AppendRequest{
		TaskID:                   taskID,
		SessionID:                sessionID,
		ExpectedHeadDigestSHA256: c.ExpectedLedgerHeadDigestSHA256,
		EventType:                closureprotocol.LedgerEventResultTransitionRecorded,
		Payload:                  payload,
		PayloadMediaType:         "application/yaml",
		ProducerID:               recordingProducerID,
		ProducedAt:               producedAt,
	})
	var durable ledger.ErrEntryDurable
	var stale ledger.ErrStaleHead
	switch {
	case err == nil:
		// appended (or ledger-level exact replay); handled below.
	case errors.As(err, &durable):
		// Durable-entry-but-HEAD-failed is a POST-commit condition: the entry is
		// authoritative. Carry its identity forward so postCommit reconciles HEAD.
		appended = ledger.AppendResult{Entry: durable.Entry, Head: durable.Head}
	case errors.As(err, &stale):
		// A concurrent writer moved the head first; there is no second event.
		return RecordResult{}, recErr(CodeStaleExpectedHead, "ledger head moved during recording")
	default:
		return RecordResult{}, recErr(CodeAppendFailed, "%v", err)
	}

	// A ledger-level exact replay (a concurrent identical writer appended first)
	// creates no second event; reconcile and report it as such.
	if appended.Replay {
		return reconcileAndReport(taskDir, store, c, recordedRef{entry: appended.Entry}, DispositionReconciled)
	}

	// --- commit point passed: the entry is durable. Any failure below is a typed
	// post-commit error, never an ordinary "not appended" error. ---
	result := RecordResult{
		Disposition:              DispositionRecorded,
		TransitionID:             c.Receipt.TransitionID,
		ReceiptDigestSHA256:      c.Receipt.ReceiptDigestSHA256,
		EntryDigestSHA256:        appended.Entry.EntryDigestSHA256,
		PreviousLedgerHeadSHA256: c.ExpectedLedgerHeadDigestSHA256,
		CurrentLedgerHeadSHA256:  appended.Head.EntryDigestSHA256,
		LedgerSequence:           appended.Head.Sequence,
		ReceiptRef:               receiptRef,
		ImpactReportRef:          impactRef,
		StageRefs:                stageRefs,
		TaskPhase:                next.TaskPhase,
		OperationalStatus:        next.OperationalStatus,
		NextAction:               next.NextAction,
	}
	if err := postCommit(taskDir, store, c, &result); err != nil {
		return result, err
	}
	return result, nil
}

// storeArtifacts writes the receipt, ten stage bytes, impact report, and three
// projections as content-addressed artifacts, verifying each returned digest
// against the candidate's own digest. Nothing is authoritative yet.
func storeArtifacts(store *ledger.Store, c resultpipeline.TransitionCandidate, next NextState) (map[string]closureprotocol.LedgerPayloadRef, []RecordedStageRef, closureprotocol.LedgerPayloadRef, closureprotocol.LedgerPayloadRef, error) {
	artifacts := map[string]closureprotocol.LedgerPayloadRef{}

	receiptRef, err := store.StoreArtifactBytes(c.ReceiptBytes, c.ReceiptMediaType)
	if err != nil {
		return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "receipt: %v", err)
	}
	if receiptRef.DigestSHA256 != c.ReceiptByteDigestSHA256 {
		return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "stored receipt digest does not match the candidate")
	}
	artifacts[KeyReceipt] = receiptRef

	byStage := map[closureprotocol.ResultPipelineStage]resultpipeline.PipelineArtifact{}
	for _, a := range c.BuildResult.StageArtifacts {
		byStage[a.Stage] = a
	}
	var stageRefs []RecordedStageRef
	for _, stage := range closureprotocol.ResultPipelineStages {
		a := byStage[stage]
		ref, err := store.StoreArtifactBytes(a.Bytes, a.MediaType)
		if err != nil {
			return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "stage %s: %v", stage, err)
		}
		if ref.DigestSHA256 != a.Receipt.ByteDigestSHA256 {
			return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "stored stage %s digest does not match its receipt", stage)
		}
		artifacts[stageKey(stage)] = ref
		stageRefs = append(stageRefs, RecordedStageRef{Stage: stage, Ref: ref})
	}

	impactBytes, err := governedimpact.MarshalCanonicalReport(c.BuildResult.GovernedKnowledgeImpactReport)
	if err != nil {
		return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "impact report: %v", err)
	}
	impactRef, err := store.StoreArtifactBytes(impactBytes, "application/json")
	if err != nil {
		return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "impact report: %v", err)
	}
	artifacts[KeyImpactReport] = impactRef

	for _, kind := range []string{KeySession, KeyTaskControl, KeyStatus} {
		bytes, err := renderProjection(kind, c, next)
		if err != nil {
			return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "projection %s: %v", kind, err)
		}
		ref, err := store.StoreArtifactBytes(bytes, "application/json")
		if err != nil {
			return nil, nil, closureprotocol.LedgerPayloadRef{}, closureprotocol.LedgerPayloadRef{}, recErr(CodeArtifactStoreFailed, "projection %s: %v", kind, err)
		}
		artifacts[kind] = ref
	}
	return artifacts, stageRefs, receiptRef, impactRef, nil
}

// postCommit reconciles derived state, then independently reloads and validates
// the recorded transition. Any failure is a PostCommitError carrying the durable
// entry identity; the recovery action is to retry the exact same candidate.
func postCommit(taskDir string, store *ledger.Store, c resultpipeline.TransitionCandidate, result *RecordResult) error {
	post := func(code, detail string) *PostCommitError {
		return &PostCommitError{Code: code, TransitionID: result.TransitionID, EntryDigestSHA256: result.EntryDigestSHA256, LedgerHeadDigestSHA256: result.CurrentLedgerHeadSHA256, RecoveryAction: "retry the same candidate", Detail: detail}
	}
	rec, err := store.ReconcileDerivedState()
	if err != nil {
		return post(CodeProjectionRebuildFailed, err.Error())
	}
	recorded, err := LoadRecordedTransition(taskDir, result.TransitionID)
	if err != nil {
		return post(CodePostCommitValidationFailed, err.Error())
	}
	if err := ValidateRecordedTransition(recorded); err != nil {
		return post(CodePostCommitValidationFailed, err.Error())
	}
	result.ProjectionState = rec.ProjectionState
	result.CurrentLedgerHeadSHA256 = recorded.Entry.EntryDigestSHA256
	return nil
}

// reconcileAndReport handles an exact replay: it repairs derived state and reloads
// without appending a second event.
func reconcileAndReport(taskDir string, store *ledger.Store, c resultpipeline.TransitionCandidate, existing recordedRef, disposition RecordDisposition) (RecordResult, error) {
	rec, err := store.ReconcileDerivedState()
	if err != nil {
		return RecordResult{}, recErr(CodeProjectionRebuildFailed, "%v", err)
	}
	recorded, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if err != nil {
		return RecordResult{}, recErr(CodeReloadFailed, "%v", err)
	}
	if err := ValidateRecordedTransition(recorded); err != nil {
		return RecordResult{}, recErr(CodeRecordedTransitionInvalid, "%v", err)
	}
	next, err := ClassifyNextState(c.BuildResult.ProofRequirements)
	if err != nil {
		return RecordResult{}, err
	}
	var stageRefs []RecordedStageRef
	for _, s := range recorded.Stages {
		stageRefs = append(stageRefs, RecordedStageRef{Stage: s.Stage, Ref: s.Ref})
	}
	return RecordResult{
		Disposition:             DispositionReconciled,
		TransitionID:            c.Receipt.TransitionID,
		ReceiptDigestSHA256:     c.Receipt.ReceiptDigestSHA256,
		EntryDigestSHA256:       recorded.Entry.EntryDigestSHA256,
		CurrentLedgerHeadSHA256: recorded.Entry.EntryDigestSHA256,
		LedgerSequence:          recorded.Entry.Sequence,
		ReceiptRef:              recorded.ReceiptRef,
		ImpactReportRef:         recorded.ImpactReportRef,
		StageRefs:               stageRefs,
		TaskPhase:               next.TaskPhase,
		OperationalStatus:       next.OperationalStatus,
		NextAction:              next.NextAction,
		ProjectionState:         rec.ProjectionState,
	}, nil
}

// verifyChainIdentity requires one task id and one session id across the whole
// verified chain and equal to the candidate's task/session.
func verifyChainIdentity(chain ledger.VerifiedChain, taskID, sessionID string) error {
	for _, ve := range chain.Entries {
		if strings.TrimSpace(ve.Entry.Task.ID) != taskID {
			return recErr(CodeTaskMismatch, "ledger entry task %q does not match candidate %q", ve.Entry.Task.ID, taskID)
		}
		if strings.TrimSpace(ve.Entry.Task.SessionID) != sessionID {
			return recErr(CodeSessionMismatch, "ledger entry session %q does not match candidate %q", ve.Entry.Task.SessionID, sessionID)
		}
	}
	return nil
}

// readArtifact reads the confined bytes of a ref and verifies its digest.
func readArtifact(taskDir string, ref closureprotocol.LedgerPayloadRef) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		return nil, err
	}
	if sha256Hex(data) != ref.DigestSHA256 {
		return nil, recErr(CodeReloadFailed, "artifact %q byte digest does not match its ref", ref.Path)
	}
	return data, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
