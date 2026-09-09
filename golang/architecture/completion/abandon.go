// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// Governed abandonment: the producer for a terminal state the vocabulary already
// named and nothing could write.
//
// TerminalAbandoned and PhaseAbandoned have existed in closureprotocol since the
// vocabulary was written. TaskTerminalStatus appeared in exactly three places --
// the constant, one struct field, and a validator -- so the value was declarable,
// storable and checkable, and unreachable. A closed vocabulary member with no
// producer is not a smaller version of a feature; it is a state the system can
// recognise and never enter.
//
// The case that forced it: a task whose work landed through an ordinary pull
// request, outside its governed session. CompleteTask refuses it correctly with
// "no current result binding" -- there is no result the session can attest -- and
// the only alternatives were to fabricate a result binding or to delete the
// pointer by hand. Both are forbidden, and both were reachable only because the
// honest third answer had no implementation.
//
// # WHAT THIS DELIBERATELY DOES NOT DO
//
// It does not assess readiness. Abandonment is the transition for a task that
// cannot satisfy the readiness conjunction, so requiring readiness would make it
// unreachable for its only purpose.
//
// It does not carry a ResultBinding, and AbandonmentReceipt has no field for one.
//
// It does not certify, promote, revoke, or touch correctness, disposition or
// question-resolution truth. It records that a task stopped and why.
const (
	// AbandonmentPolicyID names the policy under which abandonment is recorded.
	AbandonmentPolicyID = "sensei.completion.abandonment/v1"
	// AbandonedBy names the producer that wrote an abandonment event.
	AbandonedBy = "sensei.completion.abandon/v1"

	abandonmentOperationID = "task.abandon"
	abandonmentArtifactKey = "abandonment_receipt"
)

// AbandonRequest drives the sole authoritative abandonment mutation.
//
// Reason is required by the caller and by the receipt validator both. A terminal
// state whose cause is not recorded lets "we stopped" later be read as "we
// finished", which is the substitution this whole transition exists to prevent.
type AbandonRequest struct {
	RepositoryRoot                 string
	TaskDirectory                  string
	IdentityRoot                   string
	ExpectedLedgerHeadDigestSHA256 string
	Reason                         string
}

// AbandonResult carries the typed outcome and, on success or replay, the receipt.
//
// It reuses Outcome rather than declaring a parallel vocabulary: a caller
// switching on completion outcomes and one switching on abandonment outcomes are
// answering the same question about the same ledger.
type AbandonResult struct {
	Outcome     Outcome
	Detail      string
	Receipt     *closureprotocol.AbandonmentReceipt
	ReceiptPath string
	// ActivePointerCleared reports whether the active-task pointer was cleared in
	// this call. It is false on a replay that found it already clear, and true on
	// a replay that found it still set -- which is exactly the interrupted case,
	// and the caller can tell the two apart.
	ActivePointerCleared bool
}

func refuseAbandon(o Outcome, format string, a ...any) (AbandonResult, error) {
	return AbandonResult{Outcome: o, Detail: fmt.Sprintf(format, a...)}, nil
}

// AbandonTask records a task as abandoned and then retires its active pointer.
//
// THE ORDER IS THE CONTRACT, and it is durable-first for a reason that survives a
// crash. Clearing the pointer first would, on interruption, leave a task with no
// pointer and no terminal record: invisible to the readiness check that reports
// it and unreachable by the transition that would retire it, which is a worse
// state than the stale binding this repairs. Recording first leaves the opposite
// residue -- a terminal record with a pointer still naming it -- and that residue
// is repairable by rerunning this function, because the append is idempotent and
// the clear is reached again.
//
// So an interruption between the two steps is not an error state. It is the
// expected intermediate, and TestAnInterruptedAbandonmentIsResumable pins it.
func AbandonTask(ctx context.Context, req AbandonRequest) (AbandonResult, error) {
	ctx, _ = ledger.WithVerificationScope(ctx)
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	idRoot := strings.TrimSpace(req.IdentityRoot)
	expected := strings.TrimSpace(req.ExpectedLedgerHeadDigestSHA256)
	reason := strings.TrimSpace(req.Reason)
	if root == "" || taskDir == "" || idRoot == "" || expected == "" {
		return refuseAbandon(OutcomeInputInvalid, "repository root, task dir, identity root, and expected head are required")
	}
	if reason == "" {
		return refuseAbandon(OutcomeInputInvalid, "a non-empty reason is required to abandon a task")
	}
	// The repository and the task must name one world before any lock, authority
	// resolution or append. Same guard as completion, same reason: the lock and
	// authority come from the root while the ledger is read and mutated under the
	// task directory, and a mismatch would authorize against one and write to
	// another.
	if berr := validateRepositoryTaskBinding(root, taskDir); berr != nil {
		return refuseAbandon(OutcomeInputInvalid, "%s", berr.Error())
	}
	now := time.Now().UTC()

	release, err := governedmutation.AcquireLock(ctx, root, "terminal_abandonment", now)
	if err != nil {
		return refuseAbandon(OutcomeAuthorityRefusal, "acquire lock: %v", err)
	}
	defer release()

	store := ledger.NewStore(taskDir)
	report, verr := store.VerifyCtx(ctx)
	if verr != nil || !report.Valid || report.EntryCount == 0 {
		return refuseAbandon(OutcomeLedgerInvalid, "task ledger did not verify")
	}
	chain, cerr := store.VerifyChainCtx(ctx)
	if cerr != nil || len(chain.Entries) == 0 {
		return refuseAbandon(OutcomeLedgerInvalid, "task ledger chain unavailable")
	}
	task := chain.Entries[len(chain.Entries)-1].Entry.Task
	tf := classifyTerminalFacts(chain)

	// ORDER OF REFUSALS, and every one of them precedes any mutation.
	//
	// The replay branch used to sit at the top and retire the governed pointer
	// before freshness and authority were checked at all, so a caller with an
	// arbitrary head and no enrolled identity could finish the mutation on any
	// interrupted task simply by naming it. Cleanup is a governed write; it is
	// gated exactly like the write that precedes it.

	// A stronger terminal may never be weakened.
	if tf.completedCount > 0 || tf.revokedCount > 0 {
		return refuseAbandon(OutcomeConflictingCompletion,
			"the task already reached a stronger terminal state (completed=%d revoked=%d); abandonment is refused",
			tf.completedCount, tf.revokedCount)
	}
	if tf.abandonedCount > 1 {
		return refuseAbandon(OutcomeConflictingCompletion,
			"multiple abandonment facts on the ledger — terminal history is not unique")
	}

	// A session that PRODUCED something may not record that it produced nothing.
	// This is the receipt's single claim, and a result transition on the ledger
	// contradicts it outright. Checked against the durable chain rather than a
	// caller assertion, and checked for the replay path too: an abandonment that
	// should never have been written is not made true by being repeated.
	if _, hasResult := latestResultBinding(chain); hasResult {
		return refuseAbandon(OutcomeConflictingCompletion,
			"this session recorded a result transition, so it did not stop without producing a result; "+
				"abandonment would write a receipt claiming otherwise")
	}

	// Freshness: the caller's view must be current before any authority work.
	if expected != report.HeadDigestSHA256 {
		return refuseAbandon(OutcomeStaleExpectedHead, "expected head %s, current %s", short(expected), short(report.HeadDigestSHA256))
	}

	// Authority. The same owner completion uses -- no parallel store, no second
	// policy index, and no caller-supplied claim of permission.
	index, ierr := authority.LoadPolicyIndex(root)
	if ierr != nil {
		return refuseAbandon(OutcomeAuthorityRefusal, "load policy index: %v", ierr)
	}
	id, enrolled, lerr := identity.LoadManifest(idRoot)
	if lerr != nil || !enrolled {
		return refuseAbandon(OutcomeAuthorityRefusal, "abandoning actor is not enrolled")
	}
	binding := id.ActorBinding()
	verified, verifyErr := authority.VerifyActorBinding(binding, identity.Resolver(idRoot), index, now)
	if verifyErr != nil || verified.Status != closureprotocol.ReceiptValid {
		return refuseAbandon(OutcomeAuthorityRefusal, "abandoning actor not verified")
	}
	if _, _, resErr := resolveCompletionAuthority(ctx, index, binding, verified, now, taskDir); resErr != nil {
		return refuseAbandon(OutcomeAuthorityRefusal, "%v", resErr)
	}

	// ONLY NOW may an interrupted abandonment be finished. The durable record
	// already stands; what remains is the pointer, and reaching it required the
	// same freshness and authority the original write required.
	if tf.abandonedCount == 1 {
		prior, _, rerr := loadAbandonmentReceipt(taskDir, tf.abandoned)
		if rerr != nil {
			// The event is terminal and its receipt is broken. Retiring the pointer
			// would remove the default route back to a task whose reason and actor
			// can no longer be reconstructed, and blessing the damage as a clean
			// replay would hide it.
			return AbandonResult{
				Outcome: OutcomeIntegrityFailure,
				Detail: fmt.Sprintf("the task is abandoned but its receipt is not usable (%v); "+
					"the active pointer is left in place deliberately", rerr),
			}, nil
		}
		cleared, cerr := clearPointer(root, task.ID)
		if cerr != nil {
			return refuseAbandon(OutcomeIntegrityFailure, "task is abandoned but its active pointer was not retired: %v", cerr)
		}
		return AbandonResult{
			Outcome:              OutcomeExactReplay,
			Detail:               "task was already abandoned; the durable record stands",
			Receipt:              &prior,
			ActivePointerCleared: cleared,
		}, nil
	}

	// The base binding comes from the recorded authority, the same owner
	// completion reads it from. Abandonment records WHICH world the task was
	// bound to when it stopped; it does not re-derive one.
	ra, aerr := admission.LoadRecordedAuthorityCtx(ctx, taskDir)
	if aerr != nil {
		return refuseAbandon(OutcomeLedgerInvalid, "load recorded authority: %v", aerr)
	}

	producedAt, perr := time.Parse(time.RFC3339, chain.Entries[len(chain.Entries)-1].Entry.ProducedAt)
	if perr != nil {
		return refuseAbandon(OutcomeLedgerInvalid, "head produced_at not RFC3339: %v", perr)
	}

	receipt := closureprotocol.AbandonmentReceipt{
		Task:              task,
		TerminalStatus:    closureprotocol.TerminalAbandoned,
		BaseBinding:       ra.Base,
		Reason:            reason,
		NoResultProduced:  true,
		AbandonmentPolicy: AbandonmentPolicyID,
		AbandonedAt:       producedAt.UTC().Format(time.RFC3339),
		AbandoningActor:   binding.PrincipalID,
	}
	if rverr := closureprotocol.ValidateAbandonmentReceipt(receipt); rverr != nil {
		return refuseAbandon(OutcomeIntegrityFailure, "abandonment receipt invalid: %v", rverr)
	}
	receiptBytes, berr := closureprotocol.CanonicalJSON(receipt)
	if berr != nil {
		return refuseAbandon(OutcomeIntegrityFailure, "canonical receipt: %v", berr)
	}
	dig, derr := closureprotocol.SemanticDigest(receipt)
	if derr != nil {
		return refuseAbandon(OutcomeIntegrityFailure, "receipt digest: %v", derr)
	}
	receipt.ReceiptDigestSHA256 = dig

	ref, srerr := store.StoreArtifactBytes(receiptBytes, "application/json")
	if srerr != nil {
		return refuseAbandon(OutcomeLedgerInvalid, "store receipt: %v", srerr)
	}

	// STEP ONE: the durable terminal record, CAS on the expected head.
	if _, appErr := store.Append(ctx, ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: expected,
		EventType:                closureprotocol.LedgerEventAbandoned,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventAbandoned,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
			TaskPhase:     closureprotocol.PhaseAbandoned,
			Status:        string(closureprotocol.TerminalAbandoned),
			// ResultBinding is deliberately nil. The payload has the field; an
			// abandonment has nothing true to put in it.
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{abandonmentArtifactKey: ref},
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       AbandonedBy,
		ProducedAt:       producedAt,
	}); appErr != nil {
		var stale ledger.ErrStaleHead
		if errors.As(appErr, &stale) {
			return refuseAbandon(OutcomeStaleExpectedHead, "ledger head advanced during abandonment")
		}
		return refuseAbandon(OutcomeLedgerInvalid, "append abandoned: %v", appErr)
	}
	if _, prerr := ledger.RebuildProjections(taskDir, nil); prerr != nil {
		return refuseAbandon(OutcomeLedgerInvalid, "rebuild projections: %v", prerr)
	}
	final, ferr := store.VerifyCtx(ctx)
	if ferr != nil || !final.Valid {
		return refuseAbandon(OutcomeLedgerInvalid, "ledger invalid after append")
	}

	// STEP TWO, and only now: retire the pointer through its owner.
	cleared, clrErr := clearPointer(root, task.ID)
	if clrErr != nil {
		// The terminal record stands and is durable. Report the residue precisely
		// rather than rolling back a record that is now true: rerunning this
		// function reaches the replay path above and finishes the job.
		return AbandonResult{
			Outcome: OutcomeIntegrityFailure,
			Detail: fmt.Sprintf("task recorded as abandoned, but the active pointer was not retired: %v; "+
				"rerun to finish -- the durable record is idempotent", clrErr),
			Receipt:     &receipt,
			ReceiptPath: ref.Path,
		}, nil
	}
	return AbandonResult{
		Outcome:              OutcomeCommitted,
		Receipt:              &receipt,
		ReceiptPath:          ref.Path,
		ActivePointerCleared: cleared,
	}, nil
}

// clearPointer retires the active pointer when it names this task, and reports
// whether it actually removed one.
//
// ONLY A NOT-EXIST FILE MEANS "NOTHING TO RETIRE". LoadActivePointer fails for
// absence and equally for a malformed, inaccessible or unsafe-path file, and
// collapsing those into absence reported the abandonment committed while the
// pointer survived to break active-task resolution afterwards. An unreadable
// pointer is a failure to propagate, not a goal state reached.
//
// A pointer naming a DIFFERENT task is left alone and is not an error: this task
// is terminal either way, and another task's pointer is not this transition's to
// retire.
func clearPointer(root, taskID string) (bool, error) {
	path := filepath.Join(root, ".sensei", "tasks", "active.yaml")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("active task pointer is inaccessible: %w", err)
	}
	ptr, err := tasksession.LoadActivePointer(root)
	if err != nil {
		return false, fmt.Errorf("active task pointer exists but could not be read: %w", err)
	}
	if strings.TrimSpace(ptr.TaskID) != strings.TrimSpace(taskID) {
		return false, nil
	}
	if cerr := tasksession.ClearActivePointer(root, taskID); cerr != nil {
		return false, cerr
	}
	return true, nil
}

// loadAbandonmentReceipt reads and validates the receipt an abandoned event
// references.
//
// An error here is an INTEGRITY FAILURE, never "no abandonment". The event is a
// terminal fact whichever way its artifact reads; what a broken artifact costs is
// the reason and the actor, which are the only things the receipt exists to
// carry. Reporting "not abandoned" would let a fresh abandonment overwrite a
// damaged one, and reporting "replayed" would bless the damage as terminal.
func loadAbandonmentReceipt(taskDir string, entry ledger.VerifiedEntry) (closureprotocol.AbandonmentReceipt, closureprotocol.LedgerPayloadRef, error) {
	var zero closureprotocol.AbandonmentReceipt
	data, err := ledger.ReadVerifiedPayload(entry)
	if err != nil {
		return zero, closureprotocol.LedgerPayloadRef{}, fmt.Errorf("abandoned payload unreadable: %w", err)
	}
	payload, perr := ledger.ParseTaskEventPayload(data)
	if perr != nil {
		return zero, closureprotocol.LedgerPayloadRef{}, fmt.Errorf("abandoned payload malformed: %w", perr)
	}
	ref, ok := payload.Artifacts[abandonmentArtifactKey]
	if !ok {
		return zero, closureprotocol.LedgerPayloadRef{}, errors.New("abandoned event has no abandonment_receipt artifact")
	}
	raw, rerr := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if rerr != nil {
		return zero, ref, fmt.Errorf("abandonment receipt artifact unreadable: %w", rerr)
	}
	if sha256Hex(raw) != ref.DigestSHA256 {
		return zero, ref, errors.New("abandonment receipt artifact digest mismatch")
	}
	var receipt closureprotocol.AbandonmentReceipt
	if uerr := json.Unmarshal(raw, &receipt); uerr != nil {
		return zero, ref, fmt.Errorf("abandonment receipt unparseable: %w", uerr)
	}
	if verr := closureprotocol.ValidateAbandonmentReceipt(receipt); verr != nil {
		return zero, ref, fmt.Errorf("abandonment receipt invalid: %w", verr)
	}
	return receipt, ref, nil
}
