// SPDX-License-Identifier: Apache-2.0

// Package resultrecording commits a validated resultpipeline.TransitionCandidate
// into a task's authoritative append-only ledger history. It is the only Phase 7
// package that performs side effects. The ledger entry is the sole commit point:
// content-addressed artifacts are not authoritative merely because their files
// exist. Recording establishes no certification and no completion.
package resultrecording

import "fmt"

// Stable error codes. Tests assert codes, never prose.
const (
	CodeInvalidRequest             = "resultrecording.invalid_request"
	CodeInvalidCandidate           = "resultrecording.invalid_candidate"
	CodeTaskMismatch               = "resultrecording.task_mismatch"
	CodeSessionMismatch            = "resultrecording.session_mismatch"
	CodeStaleExpectedHead          = "resultrecording.stale_expected_head"
	CodeTransitionIDConflict       = "resultrecording.transition_id_conflict"
	CodeArtifactContractInvalid    = "resultrecording.artifact_contract_invalid"
	CodeArtifactStoreFailed        = "resultrecording.artifact_store_failed"
	CodeEventPayloadInvalid        = "resultrecording.event_payload_invalid"
	CodeAppendFailed               = "resultrecording.append_failed"
	CodePostCommitValidationFailed = "resultrecording.post_commit_validation_failed"
	CodeRecordedTransitionInvalid  = "resultrecording.recorded_transition_invalid"
	CodeReceiptMismatch            = "resultrecording.receipt_mismatch"
	CodeStageMismatch              = "resultrecording.stage_mismatch"
	CodeImpactMismatch             = "resultrecording.impact_mismatch"
	CodeProducerSummaryMismatch    = "resultrecording.producer_summary_mismatch"
	CodeBlockedUnprojectable       = "resultrecording.blocked_disposition_unprojectable"
	CodeProjectionRebuildFailed    = "resultrecording.projection_rebuild_failed"
	CodeProjectionDrift            = "resultrecording.projection_drift"
	CodeReloadFailed               = "resultrecording.reload_failed"
)

// Error is a typed pre-commit recording failure.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func recErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// PostCommitError is returned when the ledger entry is already durable but
// derived-state reconciliation or post-commit verification did not complete. It
// never pretends nothing was appended. The recovery action is always: retry the
// exact same candidate.
type PostCommitError struct {
	Code                   string
	TransitionID           string
	EntryDigestSHA256      string
	LedgerHeadDigestSHA256 string
	RecoveryAction         string
	Detail                 string
}

func (e *PostCommitError) Error() string {
	return fmt.Sprintf("%s [committed entry %s]: %s (recovery: %s)", e.Code, e.EntryDigestSHA256, e.Detail, e.RecoveryAction)
}
