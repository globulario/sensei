// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"path/filepath"
)

// ReconcileResult reports what projection reconciliation did. Projections are
// never authority; they are always reconstructable from the verified entry chain.
type ReconcileResult struct {
	ProjectionsRebuilt bool
	ProjectionState    string
}

// ReconcileDerivedState rebuilds the projections from the verified entry chain,
// under the same append lock. It never rewrites an entry, a payload artifact, or
// HEAD, and it is safe to run repeatedly.
//
// It requires a FULLY valid ledger, a published and current HEAD included. A stale
// or missing HEAD is refused like any other integrity error: this method is
// callable by anyone holding a Store, so repairing the pointer here would be a
// caller-reachable recovery authority. HEAD publication recovery belongs to
// Store.Append alone.
//
// Projection repair errors surface as ledger.projection_reconciliation_failed.
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
	var result ReconcileResult
	if len(chain.Entries) == 0 {
		return result, nil
	}

	set, err := Project(chain)
	if err != nil {
		return result, &ReconcileError{Code: "ledger.projection_reconciliation_failed", Detail: err.Error()}
	}
	for path, data := range set.Files {
		if err := writeFileAtomic(filepath.Join(s.taskDir, filepath.FromSlash(path)), data); err != nil {
			return result, &ReconcileError{Code: "ledger.projection_reconciliation_failed", Detail: err.Error()}
		}
	}
	result.ProjectionsRebuilt = true

	state := ProjectionState(s.taskDir, set)
	result.ProjectionState = state
	if state != "current" {
		return result, &ReconcileError{Code: "ledger.projection_reconciliation_failed", Detail: "projection state is " + state + " after rebuild"}
	}
	return result, nil
}

// ReconcileError is a typed projection reconciliation failure.
type ReconcileError struct {
	Code   string
	Detail string
}

func (e *ReconcileError) Error() string { return e.Code + ": " + e.Detail }
