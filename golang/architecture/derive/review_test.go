// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"strings"
	"testing"
)

// Review reproductions for the reader. Each was a way a false discipline could
// earn DERIVED; each now resolves away from DERIVED.

func TestAPackageLevelUnlockedCallerIsACallSite(t *testing.T) {
	src := strings.Replace(semaphoreLike, "func (s *Weighted) TryAcquire(n int64) bool {",
		"func Drain(w *Weighted) { w.notifyWaiters() }\n\nfunc (s *Weighted) TryAcquire(n int64) bool {", 1)
	r, _ := Derive(pinned(t, map[string]string{"semaphore/semaphore.go": src}), semaphoreProp(), at("2026-08-27T00:00:00Z"))
	if r.Outcome != Unresolved {
		t.Fatalf("outcome %s; a package-level caller's lock state cannot be followed and must not be skipped: %s", r.Outcome, r.Detail)
	}
	if !strings.Contains(r.Detail, "Drain") {
		t.Fatalf("the detail must name the caller: %s", r.Detail)
	}
}

func TestAGoCallSiteIsACounterexample(t *testing.T) {
	src := strings.Replace(semaphoreLike, "func (s *Weighted) TryAcquire(n int64) bool {",
		"func (s *Weighted) Kick() { s.mu.Lock(); go s.notifyWaiters(); s.mu.Unlock() }\n\nfunc (s *Weighted) TryAcquire(n int64) bool {", 1)
	r, _ := Derive(pinned(t, map[string]string{"semaphore/semaphore.go": src}), semaphoreProp(), at("2026-08-27T00:00:00Z"))
	if r.Outcome != Refuted {
		t.Fatalf("outcome %s; a goroutine holds none of the caller's locks: %s", r.Outcome, r.Detail)
	}
}

func TestADeferredCallSiteIsUnresolved(t *testing.T) {
	src := strings.Replace(semaphoreLike, "func (s *Weighted) TryAcquire(n int64) bool {",
		"func (s *Weighted) Kick() { s.mu.Lock(); defer s.notifyWaiters(); s.mu.Unlock() }\n\nfunc (s *Weighted) TryAcquire(n int64) bool {", 1)
	r, _ := Derive(pinned(t, map[string]string{"semaphore/semaphore.go": src}), semaphoreProp(), at("2026-08-27T00:00:00Z"))
	if r.Outcome != Unresolved {
		t.Fatalf("outcome %s; a deferred call runs after unlocks this reader does not order: %s", r.Outcome, r.Detail)
	}
}
