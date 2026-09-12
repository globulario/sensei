// SPDX-License-Identifier: AGPL-3.0-only

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
// is committed) but HEAD publication failed on every bounded attempt Append made
// under its lock. It must never be mistaken for a pre-commit failure: the entry
// exists and is authoritative, so the caller must not assume nothing was appended.
//
// Until HEAD is published the ledger fails closed: Verify reports
// ledger.head_stale or ledger.head_missing and every generic chain read refuses.
// Only Store.Append republishes it, under its lock -- retrying the same append
// does so and resolves as an exact replay. No other API repairs HEAD.
type ErrEntryDurable struct {
	Entry  Entry
	Head   Head
	Detail string
}

func (e ErrEntryDurable) Error() string {
	return fmt.Sprintf("ledger entry %s is durable but HEAD write failed: %s", e.Head.EntryDigestSHA256, e.Detail)
}
