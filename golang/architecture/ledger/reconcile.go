// SPDX-License-Identifier: AGPL-3.0-only

package ledger

import (
	"context"
	"fmt"
	"path/filepath"
)

// Derived-state repair codes.
const (
	// CodeDerivedHeadRepairFailed reports that a proven HEAD republication could
	// not be written.
	CodeDerivedHeadRepairFailed = "ledger.derived_head_repair_failed"
	// CodeProjectionReconciliationFailed reports that projections could not be
	// rebuilt from the verified chain.
	CodeProjectionReconciliationFailed = "ledger.projection_reconciliation_failed"
	// CodeDurableAppendUnproven reports a refusal to republish HEAD because the
	// chain on disk does not end in the entry the caller proved was committed.
	CodeDurableAppendUnproven = "ledger.durable_append_unproven"
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
// The caller that does hold the distinguishing evidence -- the exact entry an
// Append committed before its HEAD write failed, reported as ErrEntryDurable --
// uses RecoverDurableAppend instead.
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

// RecoverDurableAppend republishes HEAD for one append that committed its entry
// and then failed to publish HEAD -- the condition Append reports as
// ErrEntryDurable -- and rebuilds the projections that publication would have
// refreshed.
//
// This is the only path that may write a HEAD the chain does not already carry,
// and it is bound to proof rather than to a condition. The durable error names
// the exact entry the append committed; this re-derives the chain from disk and
// requires the recomputed tip to BE that entry -- same digest, same sequence,
// same path -- before anything is written. A truncated ledger cannot get through
// that check: once the tail is deleted the recomputed tip is an earlier entry
// than the one the caller can prove was committed, so the refusal stands and the
// ledger stays invalid. Every verification error other than the HEAD's own
// staleness or absence refuses outright, because those are damage to the entry
// chain and no HEAD write repairs them.
//
// Refusals surface as ledger.durable_append_unproven.
func (s *Store) RecoverDurableAppend(ctx context.Context, durable ErrEntryDurable) (ReconcileResult, error) {
	release, err := acquireLock(ctx, s.lockDir())
	if err != nil {
		return ReconcileResult{}, err
	}
	defer release()

	chain, report, err := verifyAndLoadChain(ctx, s.taskDir, s.payloadValidator)
	if err != nil {
		return ReconcileResult{}, err
	}
	for _, e := range report.Errors {
		if e.Code != codeHeadStale && e.Code != codeHeadMissing {
			return ReconcileResult{}, &ReconcileError{Code: CodeDurableAppendUnproven,
				Detail: fmt.Sprintf("the chain is damaged beyond HEAD (%s: %s); no HEAD write repairs that", e.Code, e.Detail)}
		}
	}
	if len(chain.Entries) == 0 {
		return ReconcileResult{}, &ReconcileError{Code: CodeDurableAppendUnproven,
			Detail: "the chain has no entries, so no append committed one"}
	}
	// THE PROOF. The tip re-derived from disk must be the committed entry itself.
	tip := chain.Entries[len(chain.Entries)-1].Entry
	if tip.EntryDigestSHA256 != durable.Entry.EntryDigestSHA256 || chain.Head != durable.Head {
		return ReconcileResult{}, &ReconcileError{Code: CodeDurableAppendUnproven,
			Detail: fmt.Sprintf("the chain ends at entry %d (%s), not at the entry %d (%s) reported durable; HEAD is not published for a chain that does not end in the committed entry",
				chain.Head.Sequence, chain.Head.EntryDigestSHA256, durable.Head.Sequence, durable.Head.EntryDigestSHA256)}
	}

	result := ReconcileResult{}
	// HEAD is written from the chain, not from the caller's copy: the two are now
	// proven equal and the chain is the one that was read off disk.
	if current, herr := readHead(s.headPath()); herr != nil || current != chain.Head {
		if err := writeHead(s.headPath(), chain.Head); err != nil {
			return result, &ReconcileError{Code: CodeDerivedHeadRepairFailed, Detail: err.Error()}
		}
		result.HeadRewritten = true
	}
	return writeProjections(s, chain, result)
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
