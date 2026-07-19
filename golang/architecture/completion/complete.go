// SPDX-License-Identifier: Apache-2.0

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
	bindingpkg "github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/identity"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// Outcome is the closed set of terminal-completion results. Refusals write nothing
// authoritative.
type Outcome string

const (
	OutcomeCommitted             Outcome = "committed"
	OutcomeExactReplay           Outcome = "exact_replay"
	OutcomeNotReady              Outcome = "not_ready"
	OutcomeStaleExpectedHead     Outcome = "stale_expected_head"
	OutcomeAuthorityRefusal      Outcome = "authority_refusal"
	OutcomeIntegrityFailure      Outcome = "integrity_failure"
	OutcomeConflictingCompletion Outcome = "conflicting_completion"
	OutcomeLedgerInvalid         Outcome = "ledger_invalid"
	OutcomeInputInvalid          Outcome = "input_invalid"
)

// CompleteRequest drives the sole authoritative terminal-completion mutation. The
// caller may identify the repository/task, the expected head, and the actor
// identity — but never a readiness object; readiness is recomputed under the lock.
type CompleteRequest struct {
	RepositoryRoot                 string
	TaskDirectory                  string
	IdentityRoot                   string
	ExpectedLedgerHeadDigestSHA256 string
}

// CompleteResult carries the typed outcome and, on success/replay, the terminal
// receipt and the readiness that justified it.
type CompleteResult struct {
	Outcome     Outcome
	Detail      string
	Assessment  *ReadinessAssessment
	Receipt     *TerminalCompletionReceipt
	ReceiptPath string
}

func refuse(o Outcome, format string, a ...any) (CompleteResult, error) {
	return CompleteResult{Outcome: o, Detail: fmt.Sprintf(format, a...)}, nil
}

// CompleteTask establishes terminal completion for a task, and only by re-proving
// the accepted readiness conjunction under one lock and durably recording the exact
// evidence. It is fail-closed and idempotent: exact retry on an unchanged completed
// world returns the existing completion; a conflicting or revoked fact fails closed;
// it never mutates correctness, disposition, promotion, question-resolution, or
// governed-source truth.
func CompleteTask(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	ctx, _ = ledger.WithVerificationScope(ctx)
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	idRoot := strings.TrimSpace(req.IdentityRoot)
	expected := strings.TrimSpace(req.ExpectedLedgerHeadDigestSHA256)
	if root == "" || taskDir == "" || idRoot == "" || expected == "" {
		return refuse(OutcomeInputInvalid, "repository root, task dir, identity root, and expected head are required")
	}
	// Repository and task must name one world before any lock, authority resolution,
	// receipt write, or append: the lock and authority are computed from the root while
	// the ledger is read and mutated under the task directory.
	if berr := validateRepositoryTaskBinding(root, taskDir); berr != nil {
		return refuse(OutcomeInputInvalid, "%s", berr.Error())
	}
	now := time.Now().UTC()

	// The outermost composable lock serializes completion attempts, so concurrent
	// attempts yield at most one authoritative completion; the rest replay or refuse.
	release, err := governedmutation.AcquireLock(ctx, root, "terminal_completion", now)
	if err != nil {
		return refuse(OutcomeAuthorityRefusal, "acquire lock: %v", err)
	}
	defer release()

	store := ledger.NewStore(taskDir)
	report, verr := store.VerifyCtx(ctx)
	if verr != nil || !report.Valid || report.EntryCount == 0 {
		return refuse(OutcomeLedgerInvalid, "task ledger did not verify")
	}
	chain, cerr := store.VerifyChainCtx(ctx)
	if cerr != nil || len(chain.Entries) == 0 {
		return refuse(OutcomeLedgerInvalid, "task ledger chain unavailable")
	}
	task := chain.Entries[len(chain.Entries)-1].Entry.Task
	currentRB, haveRB := latestResultBinding(chain)
	governedManifest, _ := governedmutation.GovernedManifestDigest(root)

	// Terminal-history cardinality is part of truth: completion may proceed only when
	// there are zero completed and zero revoked facts; exactly one completed (and no
	// revoked) is eligible for replay/conflict; anything else fails closed. This is
	// classified from the verified chain and must not depend on the strict
	// authority-resolution reload, so a terminal task carrying downstream re-work
	// events is still classified (not treated as an invalid ledger).
	if res, ok := terminalStateDecision(taskDir, chain, expected, currentRB, governedManifest); ok {
		return res, nil
	}

	if !haveRB {
		return refuse(OutcomeNotReady, "no current result binding for this task")
	}

	// Freshness: the caller's view must be current before we do authority work.
	if expected != report.HeadDigestSHA256 {
		return refuse(OutcomeStaleExpectedHead, "expected head %s, current %s", short(expected), short(report.HeadDigestSHA256))
	}

	ra, aerr := admission.LoadRecordedAuthorityCtx(ctx, taskDir)
	if aerr != nil {
		return refuse(OutcomeLedgerInvalid, "load recorded authority: %v", aerr)
	}

	// Resolve the SEPARATE completion actor + the exact completion operation.
	index, ierr := authority.LoadPolicyIndex(root)
	if ierr != nil {
		return refuse(OutcomeAuthorityRefusal, "load policy index: %v", ierr)
	}
	id, enrolled, lerr := identity.LoadManifest(idRoot)
	if lerr != nil || !enrolled {
		return refuse(OutcomeAuthorityRefusal, "completion actor is not enrolled")
	}
	binding := id.ActorBinding()
	verified, verifyErr := authority.VerifyActorBinding(binding, identity.Resolver(idRoot), index, now)
	if verifyErr != nil || verified.Status != closureprotocol.ReceiptValid {
		return refuse(OutcomeAuthorityRefusal, "completion actor not verified")
	}
	grantID, roleID, resErr := resolveCompletionAuthority(ctx, index, binding, verified, now, taskDir)
	if resErr != nil {
		return refuse(OutcomeAuthorityRefusal, "%v", resErr)
	}
	actorDigest, _ := closureprotocol.SemanticDigest(binding)

	// Recompute readiness from durable evidence INSIDE the lock — never a caller object.
	ev := &evidence{task: task, resultBinding: currentRB, haveResultBinding: haveRB, headDigest: report.HeadDigestSHA256, governedManifest: governedManifest}
	loadCorrectnessEvidence(taskDir, chain, currentRB, haveRB, ev)
	loadQuestionResolutionEvidence(root, ev)
	detectConflictingCompletion(chain, ev)
	assessment := assess(ev)
	if adig, aderr := AssessmentDigest(assessment); aderr == nil {
		assessment.DigestSHA256 = adig
	}
	if assessment.Readiness != ReadinessReady {
		return CompleteResult{Outcome: OutcomeNotReady, Detail: "readiness conjunction not satisfied", Assessment: &assessment}, nil
	}

	// Anchor the produced-at deterministically to the pre-completion head entry.
	completedAt := chain.Entries[len(chain.Entries)-1].Entry.ProducedAt
	producedAt, perr := time.Parse(time.RFC3339, completedAt)
	if perr != nil {
		return refuse(OutcomeLedgerInvalid, "head produced_at not RFC3339: %v", perr)
	}

	protocol := closureprotocol.CompletionReceipt{
		Task:                          task,
		TerminalStatus:                closureprotocol.TerminalCompleted,
		BaseBinding:                   ra.Base,
		ResultBinding:                 currentRB,
		ClosureAssessmentDigestSHA256: ra.Resolution.ClosureAssessmentDigestSHA256,
		CertificationDigestSHA256:     ev.correctnessDigest,
		CompletionPolicy:              CompletionPolicyID,
		CompletedAt:                   completedAt,
		CompletingActor:               binding.PrincipalID,
	}
	if pverr := closureprotocol.ValidateCompletionReceipt(protocol); pverr != nil {
		return refuse(OutcomeIntegrityFailure, "protocol completion receipt invalid: %v", pverr)
	}

	receipt := TerminalCompletionReceipt{
		SchemaVersion:                             TerminalReceiptSchemaVersion,
		Completion:                                protocol,
		PreCompletionLedgerHeadDigestSHA256:       report.HeadDigestSHA256,
		ReadinessAssessmentDigestSHA256:           assessment.DigestSHA256,
		Obligations:                               assessment.Obligations,
		CorrectnessReceiptDigestSHA256:            ev.correctnessDigest,
		QuestionResolutionCertificateDigestSHA256: ev.qrDigest,
		GovernedManifestDigestSHA256:              governedManifest,
		AuthorityGrantID:                          grantID,
		AuthorityRoleID:                           roleID,
		CompletionActorBindingDigestSHA256:        actorDigest,
		OperationID:                               completionOperationID,
		CausalIdentitySHA256:                      causalIdentity(task, currentRB, ev.correctnessDigest, ev.qrDigest, governedManifest, grantID, roleID),
		Bound:                                     completionBoundStatement(),
	}
	digest, derr := TerminalReceiptDigest(receipt)
	if derr != nil {
		return refuse(OutcomeIntegrityFailure, "receipt digest: %v", derr)
	}
	receipt.ReceiptDigestSHA256 = digest
	if rverr := ValidateTerminalReceipt(receipt); rverr != nil {
		return refuse(OutcomeIntegrityFailure, "terminal receipt invalid: %v", rverr)
	}

	// Persist the receipt content-addressed (idempotent), then append exactly one
	// completed event referencing it. The append is CAS on the expected head, so a
	// concurrent completion appended in between fails the append and is reconciled.
	receiptBytes, berr := closureprotocol.CanonicalJSON(receipt)
	if berr != nil {
		return refuse(OutcomeIntegrityFailure, "canonical receipt: %v", berr)
	}
	ref, srerr := store.StoreArtifactBytes(receiptBytes, "application/json")
	if srerr != nil {
		return refuse(OutcomeLedgerInvalid, "store receipt: %v", srerr)
	}
	appendResult, appErr := store.Append(ctx, ledger.AppendRequest{
		TaskID:                   task.ID,
		SessionID:                task.SessionID,
		ExpectedHeadDigestSHA256: expected,
		EventType:                closureprotocol.LedgerEventCompleted,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion,
			EventType:     closureprotocol.LedgerEventCompleted,
			TaskID:        task.ID,
			SessionID:     task.SessionID,
			TaskPhase:     closureprotocol.PhaseCompleted,
			Status:        string(closureprotocol.TerminalCompleted),
			ResultBinding: &currentRB,
			Artifacts:     map[string]closureprotocol.LedgerPayloadRef{completionArtifactKey: ref},
		},
		PayloadMediaType: "application/yaml",
		ProducerID:       GeneratedBy,
		ProducedAt:       producedAt,
	})
	if appErr != nil {
		var stale ledger.ErrStaleHead
		if errors.As(appErr, &stale) {
			// A completion raced ahead of us. Reconcile deterministically against the
			// terminal-history cardinality, never the latest fact alone.
			if fresh, ferr := store.VerifyChainCtx(ctx); ferr == nil {
				if res, ok := terminalStateDecision(taskDir, fresh, expected, currentRB, governedManifest); ok {
					return res, nil
				}
			}
			return refuse(OutcomeStaleExpectedHead, "ledger head advanced during completion")
		}
		return refuse(OutcomeLedgerInvalid, "append completed: %v", appErr)
	}
	if _, prerr := ledger.RebuildProjections(taskDir, nil); prerr != nil {
		return refuse(OutcomeLedgerInvalid, "rebuild projections: %v", prerr)
	}
	final, ferr := store.VerifyCtx(ctx)
	if ferr != nil || !final.Valid {
		return refuse(OutcomeLedgerInvalid, "ledger invalid after append")
	}
	// Verify the durable conjunction: the completed event and its receipt both
	// present and matching. A receipt alone is never completion.
	if _, verifyErr := verifyDurableConjunction(ctx, taskDir, currentRB); verifyErr != nil {
		return refuse(OutcomeIntegrityFailure, "durable conjunction: %v", verifyErr)
	}
	out := CompleteResult{Outcome: OutcomeCommitted, Assessment: &assessment, Receipt: &receipt, ReceiptPath: ref.Path}
	if appendResult.Replay {
		out.Outcome = OutcomeExactReplay
	}
	return out, nil
}

// handleExistingCompletion decides replay vs conflict for a task that already
// carries a completed event, without requiring readiness (which is naturally stale
// once the head advanced). It verifies the durable receipt, then treats the attempt
// as an exact replay only when the caller's expected head is the recorded
// pre-completion head and the current result and governed world are unchanged.
func handleExistingCompletion(taskDir string, completed ledger.VerifiedEntry, expected string, currentRB closureprotocol.ResultBinding, governedManifest string) (CompleteResult, error) {
	receipt, ref, err := loadTerminalReceipt(taskDir, completed)
	if err != nil {
		return refuse(OutcomeIntegrityFailure, "existing completion receipt: %v", err)
	}
	// The completed event must reference the exact receipt and bind the same identity.
	if evtErr := completedEventMatches(completed, receipt, ref); evtErr != nil {
		return refuse(OutcomeIntegrityFailure, "%v", evtErr)
	}
	unchanged := expected == receipt.PreCompletionLedgerHeadDigestSHA256 &&
		bindingpkg.ResultBindingEqual(receipt.Completion.ResultBinding, currentRB) &&
		receipt.GovernedManifestDigestSHA256 == governedManifest
	if unchanged {
		return CompleteResult{Outcome: OutcomeExactReplay, Receipt: &receipt, ReceiptPath: ref.Path}, nil
	}
	return refuse(OutcomeConflictingCompletion, "task already carries a terminal completion for a different world; no supersession")
}

// verifyDurableConjunction independently re-proves the authoritative completion
// fact: EXACTLY one completed event, zero revoked facts, its content-addressed
// receipt present and matching, and the receipt bound to the expected current result.
func verifyDurableConjunction(ctx context.Context, taskDir string, currentRB closureprotocol.ResultBinding) (TerminalCompletionReceipt, error) {
	chain, err := ledger.NewStore(taskDir).VerifyChainCtx(ctx)
	if err != nil {
		return TerminalCompletionReceipt{}, err
	}
	tf := classifyTerminalFacts(chain)
	if tf.revokedCount > 0 {
		return TerminalCompletionReceipt{}, fmt.Errorf("a revocation fact is present")
	}
	if tf.completedCount != 1 {
		return TerminalCompletionReceipt{}, fmt.Errorf("expected exactly one completed event, found %d", tf.completedCount)
	}
	receipt, ref, lerr := loadTerminalReceipt(taskDir, tf.completed)
	if lerr != nil {
		return TerminalCompletionReceipt{}, lerr
	}
	if err := completedEventMatches(tf.completed, receipt, ref); err != nil {
		return TerminalCompletionReceipt{}, err
	}
	if !bindingpkg.ResultBindingEqual(receipt.Completion.ResultBinding, currentRB) {
		return TerminalCompletionReceipt{}, fmt.Errorf("completed receipt binds a different result than the current result")
	}
	return receipt, nil
}

// loadTerminalReceipt reads and validates the terminal receipt referenced by a
// completed event, checking artifact byte integrity.
func loadTerminalReceipt(taskDir string, completed ledger.VerifiedEntry) (TerminalCompletionReceipt, closureprotocol.LedgerPayloadRef, error) {
	data, err := ledger.ReadVerifiedPayload(completed)
	if err != nil {
		return TerminalCompletionReceipt{}, closureprotocol.LedgerPayloadRef{}, fmt.Errorf("completed payload unreadable: %w", err)
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return TerminalCompletionReceipt{}, closureprotocol.LedgerPayloadRef{}, fmt.Errorf("completed payload malformed: %w", err)
	}
	ref, ok := payload.Artifacts[completionArtifactKey]
	if !ok {
		return TerminalCompletionReceipt{}, closureprotocol.LedgerPayloadRef{}, fmt.Errorf("completed event has no completion_receipt artifact")
	}
	raw, err := os.ReadFile(filepath.Join(taskDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		return TerminalCompletionReceipt{}, ref, fmt.Errorf("completion receipt artifact unreadable: %w", err)
	}
	if sha256Hex(raw) != ref.DigestSHA256 {
		return TerminalCompletionReceipt{}, ref, fmt.Errorf("completion receipt artifact digest mismatch")
	}
	var receipt TerminalCompletionReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return TerminalCompletionReceipt{}, ref, fmt.Errorf("completion receipt unparseable: %w", err)
	}
	if err := ValidateTerminalReceipt(receipt); err != nil {
		return TerminalCompletionReceipt{}, ref, fmt.Errorf("completion receipt invalid: %w", err)
	}
	return receipt, ref, nil
}

// completedEventMatches requires the completed event to reference the exact receipt
// and bind the same task/result as the receipt.
func completedEventMatches(completed ledger.VerifiedEntry, receipt TerminalCompletionReceipt, ref closureprotocol.LedgerPayloadRef) error {
	if completed.Entry.Task.ID != receipt.Completion.Task.ID || completed.Entry.Task.SessionID != receipt.Completion.Task.SessionID {
		return fmt.Errorf("completed event task does not match the receipt")
	}
	data, err := ledger.ReadVerifiedPayload(completed)
	if err != nil {
		return err
	}
	payload, err := ledger.ParseTaskEventPayload(data)
	if err != nil {
		return err
	}
	if payload.ResultBinding == nil || !bindingpkg.ResultBindingEqual(*payload.ResultBinding, receipt.Completion.ResultBinding) {
		return fmt.Errorf("completed event result binding does not match the receipt")
	}
	if ref.DigestSHA256 == "" {
		return fmt.Errorf("completed event references no receipt digest")
	}
	return nil
}

// terminalFacts is the classification of a task ledger's terminal history.
type terminalFacts struct {
	completedCount int
	revokedCount   int
	completed      ledger.VerifiedEntry // the completed entry when completedCount == 1
}

// classifyTerminalFacts counts the completed and revoked events across the whole
// verified chain — terminal uniqueness is proven by cardinality, not by taking the
// latest fact.
func classifyTerminalFacts(chain ledger.VerifiedChain) terminalFacts {
	var tf terminalFacts
	for _, ve := range chain.Entries {
		switch ve.Entry.EventType {
		case closureprotocol.LedgerEventCompleted:
			tf.completedCount++
			tf.completed = ve
		case closureprotocol.LedgerEventRevoked:
			tf.revokedCount++
		}
	}
	return tf
}

// terminalStateDecision applies the terminal-history cardinality rule in strict
// fail-closed order: any revoked fact is contradictory; more than one completed fact
// is non-unique; exactly one completed (and no revoked) is dispatched to
// replay/conflict classification. ok=false means no terminal fact exists and
// completion may proceed. It is used by both the pre-mutation path and the CAS-race
// reconciliation so both share one definition of terminal truth.
func terminalStateDecision(taskDir string, chain ledger.VerifiedChain, expected string, currentRB closureprotocol.ResultBinding, governedManifest string) (CompleteResult, bool) {
	tf := classifyTerminalFacts(chain)
	switch {
	case tf.revokedCount > 0:
		r, _ := refuse(OutcomeConflictingCompletion, "a revocation fact exists — contradictory terminal history")
		return r, true
	case tf.completedCount > 1:
		r, _ := refuse(OutcomeConflictingCompletion, "multiple completed facts on the ledger — terminal history is not unique")
		return r, true
	case tf.completedCount == 1:
		r, _ := handleExistingCompletion(taskDir, tf.completed, expected, currentRB, governedManifest)
		return r, true
	default:
		return CompleteResult{}, false
	}
}

func short(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
