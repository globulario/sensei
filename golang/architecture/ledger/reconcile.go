// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"path/filepath"
)

// ReconcileResult reports what derived-state repair did. Derived state (HEAD,
// projections) is never authority; it is always reconstructable from the verified
// entry chain.
type ReconcileResult struct {
	HeadRewritten      bool
	ProjectionsRebuilt bool
	ProjectionState    string
}

// ReconcileDerivedState repairs the derived files (HEAD, projections) from the
// verified entry chain, under the same append lock. It never rewrites an entry or
// a payload artifact, and it is safe to run repeatedly. It is the correct recovery
// for a durable-entry-but-stale-HEAD condition (RebuildProjections alone does not
// repair HEAD).
//
// Derived-state repair errors surface as ledger.derived_head_repair_failed or
// ledger.projection_reconciliation_failed.
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

	// Canonical HEAD derived from the last valid entry. Repair only when missing or
	// stale; never touch the entries themselves.
	derived := chain.Head
	current, herr := readHead(s.headPath())
	needRepair := herr != nil ||
		current.EntryDigestSHA256 != derived.EntryDigestSHA256 ||
		current.Sequence != derived.Sequence ||
		current.EntryPath != derived.EntryPath
	if needRepair {
		if err := writeHead(s.headPath(), derived); err != nil {
			return result, &ReconcileError{Code: "ledger.derived_head_repair_failed", Detail: err.Error()}
		}
		result.HeadRewritten = true
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

// ReconcileError is a typed derived-state repair failure.
type ReconcileError struct {
	Code   string
	Detail string
}

func (e *ReconcileError) Error() string { return e.Code + ": " + e.Detail }
