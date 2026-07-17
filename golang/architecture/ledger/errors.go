// SPDX-License-Identifier: Apache-2.0

package ledger

import "fmt"

type ErrStaleHead struct {
	Expected string
	Actual   string
	Sequence int
}

func (e ErrStaleHead) Error() string {
	return fmt.Sprintf("stale ledger head: expected %q actual %q sequence %d", e.Expected, e.Actual, e.Sequence)
}

type ErrLockHeld struct {
	Path string
}

func (e ErrLockHeld) Error() string {
	return "ledger append lock held: " + e.Path
}

// ErrEntryDurable reports that the ledger entry was written durably (the append
// is committed) but a subsequent derived-state write (HEAD) failed. It must never
// be mistaken for a pre-commit failure: the entry exists and is authoritative, so
// the caller must reconcile rather than assume nothing was appended. The recovery
// is to reconcile derived state (or retry the same append, which resolves as an
// exact replay).
type ErrEntryDurable struct {
	Entry  Entry
	Head   Head
	Detail string
}

func (e ErrEntryDurable) Error() string {
	return fmt.Sprintf("ledger entry %s is durable but HEAD write failed: %s", e.Head.EntryDigestSHA256, e.Detail)
}
