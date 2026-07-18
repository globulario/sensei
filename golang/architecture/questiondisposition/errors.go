// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition

import "fmt"

// Stable error codes for the question-disposition owner. Pre-commit failures use
// Error; once the ledger entry is durable, every failure is a PostCommitError.
const (
	CodeInvalidRequest       = "questiondisposition.invalid_request"
	CodeChainVerifyFailed    = "questiondisposition.chain_verify_failed"
	CodeNoResultTransition   = "questiondisposition.no_result_transition"
	CodeArtifactReadFailed   = "questiondisposition.artifact_read_failed"
	CodeQuestionNotFound     = "questiondisposition.question_not_found"
	CodeDigestMismatch       = "questiondisposition.digest_mismatch"
	CodeActorNotEnrolled     = "questiondisposition.actor_not_enrolled"
	CodeActorNotVerified     = "questiondisposition.actor_not_verified"
	CodeAuthorityUnresolved  = "questiondisposition.authority_unresolved"
	CodeAuthorityNotGranted  = "questiondisposition.authority_not_granted"
	CodeScopeBroadened       = "questiondisposition.scope_broadened"
	CodeInvalidReceipt       = "questiondisposition.invalid_receipt"
	CodeAnchorMissing        = "questiondisposition.anchor_missing"
	CodeExpectedHeadRequired = "questiondisposition.expected_head_required"
	CodeStaleExpectedHead    = "questiondisposition.stale_expected_head"
	CodeReceiptConflict      = "questiondisposition.receipt_conflict"
	CodeArtifactStoreFailed  = "questiondisposition.artifact_store_failed"
	CodeAppendFailed         = "questiondisposition.append_failed"
	CodeEventPayloadInvalid  = "questiondisposition.event_payload_invalid"
	CodeReloadFailed         = "questiondisposition.reload_failed"
	CodeProjectionRebuild    = "questiondisposition.projection_rebuild_failed"
	CodePostCommitValidation = "questiondisposition.post_commit_validation_failed"
)

// Error is a typed pre-commit failure. Nothing was appended to the ledger.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func qdErr(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// PostCommitError signals that the disposition entry is durable on the ledger
// but a derived-state step failed afterward. The entry is authoritative; the
// caller reconciles by retrying the same candidate.
type PostCommitError struct {
	Code                   string
	QuestionID             string
	ReceiptDigestSHA256    string
	EntryDigestSHA256      string
	LedgerHeadDigestSHA256 string
	RecoveryAction         string
	Detail                 string
}

func (e *PostCommitError) Error() string {
	return fmt.Sprintf("%s: disposition %s durable at entry %s: %s",
		e.Code, e.QuestionID, e.EntryDigestSHA256, e.Detail)
}
