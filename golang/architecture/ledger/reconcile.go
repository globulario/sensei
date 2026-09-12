// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"path/filepath"
)

// Derived-state repair codes. There is no head-repair code: no public path
// rewrites HEAD, so none can be emitted.
const (
	// CodeProjectionReconciliationFailed reports that projections could not be
	// rebuilt from the verified chain.
	CodeProjectionReconciliationFailed = "ledger.projection_reconciliation_failed"
)

// ReconcileResult reports what derived-state repair did. Derived state (HEAD,
// projections) is never authority; it is always reconstructable from the verified
// entry chain.
type ReconcileResult struct {
	HeadRewritten      bool
	ProjectionsRebuilt bool
	ProjectionState    string
}

// ReconcileDerivedState rebuilds the projection files from the verified entry
// chain, under the append lock. It never rewrites an entry or a payload artifact,
// and it is safe to run repeatedly.
//
// IT REQUIRES A FULLY VALID CHAIN, HEAD INCLUDED, AND REPAIRS NO HEAD. It used to
// recompute a HEAD that disagreed with the entries, which reads as a convenience
// until you ask what produced the disagreement. Deleting the highest-sequence
// entry leaves the task directory in exactly the state a HEAD publication that
// never happened leaves it: entries that verify, and a HEAD that does not name
// the last one. A repair that accepts both republishes the truncated prefix as
// the whole history, the chain verifies clean again, and the deletion has been
// laundered by the recovery path -- a spent mutation capability comes back for
// the cost of one extra call. Nothing in the derived state distinguishes the two
// cases, so this cannot choose between them and refuses.
//
// NO PUBLIC PATH REPAIRS HEAD, and that is the whole point. The evidence that
// separates the two cases -- that THIS process committed the entry -- exists only
// inside the Append that created it, which is where the single bounded HEAD retry
// now lives. Handing that identity to a later call as an argument does not carry
// the evidence with it: the argument is caller-supplied, so after a truncation a
// caller names the SURVIVING tip, the equality check passes, and the deletion is
// laundered. A capability that anyone can forge the proof for is not a proof.
//
// Reconciliation failures surface as ledger.projection_reconciliation_failed.
func (s *Store) ReconcileDerivedState() (ReconcileResult, error) {
	release, err := acquireLock(context.Background(), s.lockDir())
	if err != nil {
		return ReconcileResult{}, err
	}
	defer release()

	chain, err := loadVerifiedChain(context.Background(), s.taskDir, s.payloadValidator)
	if err != nil {
		return ReconcileResult{}, err
	}
	if len(chain.Entries) == 0 {
		return ReconcileResult{}, nil
	}
	return writeProjections(s, chain, ReconcileResult{})
}

// writeProjections rebuilds every projection file from a verified chain and
// reports the resulting projection state, which must be current.
func writeProjections(s *Store, chain VerifiedChain, result ReconcileResult) (ReconcileResult, error) {
	set, err := Project(chain)
	if err != nil {
		return result, &ReconcileError{Code: CodeProjectionReconciliationFailed, Detail: err.Error()}
	}
	for path, data := range set.Files {
		if err := writeFileAtomic(filepath.Join(s.taskDir, filepath.FromSlash(path)), data); err != nil {
			return result, &ReconcileError{Code: CodeProjectionReconciliationFailed, Detail: err.Error()}
		}
	}
	result.ProjectionsRebuilt = true

	state := ProjectionState(s.taskDir, set)
	result.ProjectionState = state
	if state != "current" {
		return result, &ReconcileError{Code: CodeProjectionReconciliationFailed, Detail: "projection state is " + state + " after rebuild"}
	}
	return result, nil
}

// ReconcileError is a typed derived-state repair failure.
type ReconcileError struct {
	Code   string
	Detail string
}

func (e *ReconcileError) Error() string { return e.Code + ": " + e.Detail }
