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
// is repairable by rerunning the transition, because the append is idempotent and
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
	// contradicts it outright.
	//
	// TRI-STATE, because the first repair asked the wrong question. It asked
	// latestResultBinding -- "is there a current result binding" -- and a
	// recorded transition whose payload carries no resolvable binding answers
	// no. That is not evidence that nothing was produced; it is evidence that
	// the record cannot be read. Absence of a readable binding and absence of a
	// transition are different facts and only the second permits abandonment.
	_, hasBinding := latestResultBinding(chain)
	switch {
	case tf.resultTransitionCount > 0 && hasBinding:
		return refuseAbandon(OutcomeConflictingCompletion,
			"this session recorded a result transition, so it did not stop without producing a result; "+
				"abandonment would write a receipt claiming otherwise")
	case tf.resultTransitionCount > 0:
		return refuseAbandon(OutcomeIntegrityFailure,
			"this session recorded %d result transition(s) whose current binding cannot be resolved; "+
				"an unreadable result record is not evidence that no result was produced",
			tf.resultTransitionCount)
	case hasBinding:
		// A binding with no transition event is itself incoherent; refuse rather
		// than choose which half to believe.
		return refuseAbandon(OutcomeIntegrityFailure,
			"a current result binding exists with no recorded result transition; the terminal history is not readable")
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
	if _, _, resErr := resolveAbandonmentAuthority(ctx, index, binding, verified, now, taskDir); resErr != nil {
		return refuseAbandon(OutcomeAuthorityRefusal, "%v", resErr)
	}

	// ONLY NOW may an interrupted abandonment be finished. The durable record
	// already stands; what remains is the pointer, and reaching it required the
	// same freshness and authority the original write required.
	if tf.abandonedCount == 1 {
		prior, priorRef, rerr := loadAbandonmentReceipt(taskDir, tf.abandoned)
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
			ReceiptPath:          priorRef.Path,
			ActivePointerCleared: cleared,
		}, nil
	}

	// The lifecycle transition must be legal, and this guard sits AFTER the replay
	// branch on purpose. A task that is already abandoned reads PhaseAbandoned,
	// which has no outgoing transition -- so running this first would refuse the
	// idempotent replay the interruption contract depends on. The guard is about
	// the transition this call is about to WRITE, so it belongs immediately
	// before the write and nowhere earlier. A task already folded to refused,
	// stale or uncertifiable carries no completed, revoked, abandoned or
	// result-transition event, so every guard above passes -- and
	// AllowedTaskTransitions gives those phases NO outgoing transition. Appending
	// here would move an already-final task to abandoned and have terminal
	// inspection report it as a clean stop.
	from, haveFrom, phaseErr := latestTaskPhase(chain)
	if phaseErr != nil {
		return refuseAbandon(OutcomeIntegrityFailure,
			"the task's current lifecycle phase cannot be established: %v; "+
				"an unreadable lifecycle record is not evidence that no phase was reached", phaseErr)
	}
	if haveFrom {
		if terr := closureprotocol.ValidateTaskTransition(from, closureprotocol.PhaseAbandoned); terr != nil {
			return refuseAbandon(OutcomeConflictingCompletion,
				"%v: the task is already in a terminal phase and abandonment would overwrite it", terr)
		}
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
	// Stamp BEFORE serialising, the order completion already uses. Serialising
	// first stored an artifact that did not contain its own identity, so every
	// replay reconstructed a receipt with an empty digest while a fresh success
	// reported one -- the same receipt described two different ways depending on
	// which path produced it.
	dig, derr := AbandonmentReceiptDigest(receipt)
	if derr != nil {
		return refuseAbandon(OutcomeIntegrityFailure, "receipt digest: %v", derr)
	}
	receipt.ReceiptDigestSHA256 = dig
	receiptBytes, berr := closureprotocol.CanonicalJSON(receipt)
	if berr != nil {
		return refuseAbandon(OutcomeIntegrityFailure, "canonical receipt: %v", berr)
	}

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
		var durable ledger.ErrEntryDurable
		switch {
		case errors.As(appErr, &durable):
			// POST-COMMIT. The entry is durable, and Store.Append already retried
			// HEAD publication under its own lock and failed every attempt. HEAD
			// recovery belongs to Append alone, so this caller repairs nothing,
			// rebuilds nothing, and does not read the chain through an unpublished
			// HEAD: the ledger fails closed, and the pointer is left in place
			// because retiring it on an unverifiable ledger would strand the task.
			return AbandonResult{
				Outcome: OutcomeIntegrityFailure,
				Detail: fmt.Sprintf("the abandoned entry is durable but its ledger HEAD was not published (%v); "+
					"the ledger fails verification and the active pointer is left in place deliberately", durable),
				Receipt:     &receipt,
				ReceiptPath: ref.Path,
			}, nil
		case errors.As(appErr, &stale):
			return refuseAbandon(OutcomeStaleExpectedHead, "ledger head advanced during abandonment")
		default:
			return refuseAbandon(OutcomeLedgerInvalid, "append abandoned: %v", appErr)
		}
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

// clearPointer delegates the whole decision to the pointer's owner.
//
// It deliberately does NOT read the pointer, compare it, and decide here. That
// sequence was racy however carefully written: a writer arriving after the read
// could make the pointer name the task being retired, and this function would
// have already returned "nothing to do". The owner performs read, decision and
// unlink under one lock and reports what it did, because a caller cannot learn
// that afterwards without re-reading -- and re-reading is the race.
func clearPointer(root, taskID string) (bool, error) {
	return tasksession.RetireActivePointer(root, taskID)
}

// latestTaskPhase returns the most recent lifecycle phase recorded on the chain.
//
// AN UNREADABLE ENTRY IS AN ERROR, NEVER A SKIP. The previous walk continued
// past anything it could not parse, so an unreadable task_marked_stale head fell
// through to an earlier non-terminal phase and the caller happily transitioned a
// task that had already folded. "This entry says nothing about the phase" and
// "this entry could not be read" are different facts, and the second one is not
// evidence for the first.
//
// The store used here attaches no payload validator, so a semantically invalid
// payload can sit in a chain that verifies. That is exactly the case this must
// refuse rather than walk past.
func latestTaskPhase(chain ledger.VerifiedChain) (closureprotocol.TaskPhase, bool, error) {
	for i := len(chain.Entries) - 1; i >= 0; i-- {
		entry := chain.Entries[i]
		data, err := ledger.ReadVerifiedPayload(entry)
		if err != nil {
			return "", false, fmt.Errorf("lifecycle event %s payload unreadable: %w", entry.Entry.EventType, err)
		}
		payload, perr := ledger.ParseTaskEventPayload(data)
		if perr != nil {
			return "", false, fmt.Errorf("lifecycle event %s payload malformed: %w", entry.Entry.EventType, perr)
		}
		if strings.TrimSpace(string(payload.TaskPhase)) != "" {
			return payload.TaskPhase, true, nil
		}
	}
	return "", false, nil
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
	// The stored artifact must carry its own identity and that identity must
	// recompute. A receipt whose digest is absent or wrong is not this receipt.
	if strings.TrimSpace(receipt.ReceiptDigestSHA256) == "" {
		return zero, ref, errors.New("abandonment receipt carries no digest")
	}
	recomputed, rderr := AbandonmentReceiptDigest(receipt)
	if rderr != nil {
		return zero, ref, fmt.Errorf("recompute receipt digest: %w", rderr)
	}
	if recomputed != receipt.ReceiptDigestSHA256 {
		return zero, ref, errors.New("abandonment receipt digest does not recompute")
	}
	// And it must belong to the event that references it. A structurally valid
	// receipt from another task is still a valid receipt -- for a different
	// task. completedEventMatches makes exactly this check for completion, and
	// omitting it here let one task be reported cleanly abandoned on another
	// task's record, and let a replay retire the wrong task's pointer.
	if merr := abandonedEventMatches(entry, receipt, ref); merr != nil {
		return zero, ref, merr
	}
	return receipt, ref, nil
}

// AbandonmentReceiptDigest is the self-excluding identity of an abandonment
// receipt, computed the way TerminalReceiptDigest computes completion's.
func AbandonmentReceiptDigest(in closureprotocol.AbandonmentReceipt) (string, error) {
	in.ReceiptDigestSHA256 = ""
	return closureprotocol.SemanticDigest(in)
}

// abandonedEventMatches binds a receipt to the abandoned event referencing it.
//
// Modelled on completedEventMatches. The completion analogue additionally
// compares result bindings; abandonment has none by construction, so the
// conjunction here is task, session, and a referenced digest that is actually
// present.
func abandonedEventMatches(entry ledger.VerifiedEntry, receipt closureprotocol.AbandonmentReceipt, ref closureprotocol.LedgerPayloadRef) error {
	if entry.Entry.Task.ID != receipt.Task.ID || entry.Entry.Task.SessionID != receipt.Task.SessionID {
		return fmt.Errorf("abandoned event task %s/%s does not match the receipt task %s/%s",
			entry.Entry.Task.ID, entry.Entry.Task.SessionID, receipt.Task.ID, receipt.Task.SessionID)
	}
	if strings.TrimSpace(ref.DigestSHA256) == "" {
		return errors.New("abandoned event references no receipt digest")
	}
	return nil
}
