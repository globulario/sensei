// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/ledger"
)

// RecoverOutcome is the closed set of projection-recovery results.
type RecoverOutcome string

const (
	// RecoverProjectionsRebuilt: stale/missing projections were restored from a valid
	// durable conjunction.
	RecoverProjectionsRebuilt RecoverOutcome = "projections_rebuilt"
	// RecoverAlreadyCurrent: a valid completion whose projections are already current.
	RecoverAlreadyCurrent RecoverOutcome = "already_current"
	// RecoverNothingToRecover: no completion to recover (not completed, or harmless
	// receipt-only residue — retry through CompleteTask is the only append path).
	RecoverNothingToRecover RecoverOutcome = "nothing_to_recover"
	// RecoverContradictory: contradictory terminal history — never normalized here.
	RecoverContradictory RecoverOutcome = "contradictory_terminal_history"
	// RecoverBrokenCompletion: an event without a valid receipt, integrity failure, or
	// wrong binding — not repairable by a projection rebuild.
	RecoverBrokenCompletion RecoverOutcome = "broken_completion"
	// RecoverUnsupported: the ledger could not be verified.
	RecoverUnsupported RecoverOutcome = "unsupported"
	// RecoverInputInvalid: the request was malformed.
	RecoverInputInvalid RecoverOutcome = "input_invalid"
)

// RecoverResult carries the recovery outcome and the before/after reconstructions.
type RecoverResult struct {
	Outcome RecoverOutcome
	Detail  string
	Before  TerminalStateAssessment
	After   *TerminalStateAssessment
}

// RecoverProjections restores derived projections from an already-valid unique
// durable conjunction. It is derived-state maintenance ONLY: it appends no ledger
// event, rewrites no receipt, resolves no completion authority, and can never
// create, replace, supersede, or bless a terminal fact. Contradictory terminal
// history is never normalized; receipt-only residue is never blessed; retry through
// CompleteTask is the only path that may append a completion.
func RecoverProjections(ctx context.Context, req Request) (RecoverResult, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	taskDir := strings.TrimSpace(req.TaskDirectory)
	// Repository and task must name one world before the lock or the projection rebuild:
	// the lock is acquired under the root while projections are rebuilt under the task
	// directory, so a mismatched pair would lock one world and mutate another.
	if berr := validateRepositoryTaskBinding(root, taskDir); berr != nil {
		return RecoverResult{Outcome: RecoverInputInvalid, Detail: berr.Error()}, nil
	}

	// Serialize with completion attempts so a rebuild never races an append. The lock
	// operation is distinct from terminal completion and grants no completion authority.
	release, err := governedmutation.AcquireLock(ctx, root, "terminal_projection_recovery", time.Now().UTC())
	if err != nil {
		return RecoverResult{Outcome: RecoverUnsupported, Detail: "acquire lock: " + err.Error()}, nil
	}
	defer release()

	before, ierr := InspectTerminalState(ctx, req)
	if ierr != nil {
		return RecoverResult{Outcome: RecoverInputInvalid, Detail: ierr.Error()}, nil
	}
	switch before.State {
	case TerminalCommitted:
		return RecoverResult{Outcome: RecoverAlreadyCurrent, Before: before}, nil
	case TerminalProjectionStaleOrMissing:
		// Rebuild derived projections only — no ledger event, no receipt write.
		if _, rerr := ledger.RebuildProjections(taskDir, nil); rerr != nil {
			return RecoverResult{Outcome: RecoverUnsupported, Detail: "rebuild projections: " + rerr.Error(), Before: before}, nil
		}
		after, aerr := InspectTerminalState(ctx, req)
		if aerr != nil {
			return RecoverResult{Outcome: RecoverUnsupported, Detail: aerr.Error(), Before: before}, nil
		}
		if after.State != TerminalCommitted {
			return RecoverResult{Outcome: RecoverUnsupported, Detail: fmt.Sprintf("rebuild did not reconcile: %s", after.State), Before: before, After: &after}, nil
		}
		return RecoverResult{Outcome: RecoverProjectionsRebuilt, Before: before, After: &after}, nil
	case TerminalContradictoryHistory:
		return RecoverResult{Outcome: RecoverContradictory, Detail: before.Detail, Before: before}, nil
	case TerminalEventWithoutValidReceipt, TerminalIntegrityFailure, TerminalWrongBinding:
		return RecoverResult{Outcome: RecoverBrokenCompletion, Detail: before.Detail, Before: before}, nil
	case TerminalNotCompleted, TerminalReceiptWithoutEvent:
		return RecoverResult{Outcome: RecoverNothingToRecover, Detail: before.Detail, Before: before}, nil
	default:
		return RecoverResult{Outcome: RecoverUnsupported, Detail: before.Detail, Before: before}, nil
	}
}
