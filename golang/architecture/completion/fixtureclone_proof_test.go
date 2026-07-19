// SPDX-License-Identifier: AGPL-3.0-only

package completion

import (
	"sync/atomic"
	"testing"
)

// The canonical world is built exactly once through the real pipeline, and every
// consumer gets a unique directory — the setup-call counter is the contract, not
// wall-clock. constructions is a sync.Once guard, so it is 1 after any number of
// clones; clones grows one per handout.
func TestFixture_ConstructedOncePerClassCloneDirsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		rw, _ := cloneReady(t)
		cw := cloneCommitted(t)
		for _, dir := range []string{rw.Repo, cw.Repo} {
			if seen[dir] {
				t.Fatalf("clone directory reused: %s", dir)
			}
			seen[dir] = true
		}
	}
	if got := atomic.LoadInt64(&readyBase.constructions); got != 1 {
		t.Fatalf("ready pipeline construction count = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&committedBase.constructions); got != 1 {
		t.Fatalf("committed pipeline construction count = %d, want 1", got)
	}
	if atomic.LoadInt64(&readyBase.clones) < 8 || atomic.LoadInt64(&committedBase.clones) < 8 {
		t.Fatal("expected at least 8 clones of each class")
	}
}

// Mutating one clone must not alter the immutable base or any sibling clone — the
// isolation that makes a shared base safe. A tampered completion receipt breaks only
// its own world; a freshly cloned world remains authoritative.
func TestFixture_CloneMutationDoesNotPoisonBaseOrSiblings(t *testing.T) {
	victim := cloneCommitted(t)
	sibling := cloneCommitted(t)

	// Baseline: both clones project an authoritative completion.
	if victim.project(t).ClosureVerdict != ClosureAuthoritativeCompletion {
		t.Fatal("victim must start authoritative")
	}
	if sibling.project(t).ClosureVerdict != ClosureAuthoritativeCompletion {
		t.Fatal("sibling must start authoritative")
	}

	// Corrupt only the victim's terminal completion receipt.
	tamperCompletionReceipt(t, victim.TaskDir)

	if got := victim.project(t).ClosureVerdict; got == ClosureAuthoritativeCompletion {
		t.Fatal("tampering the victim clone must break its projection")
	}
	if got := sibling.project(t).ClosureVerdict; got != ClosureAuthoritativeCompletion {
		t.Fatalf("sibling clone must be unaffected by the victim's mutation, got %s", got)
	}
	// A brand-new clone proves the immutable base itself was not poisoned.
	fresh := cloneCommitted(t)
	if got := fresh.project(t).ClosureVerdict; got != ClosureAuthoritativeCompletion {
		t.Fatalf("base fixture was poisoned: fresh clone projects %s", got)
	}
}
