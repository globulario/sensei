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
