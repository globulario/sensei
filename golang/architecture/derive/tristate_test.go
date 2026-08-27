// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"strings"
	"testing"
)

// A faithful reduction of golang/sync's semaphore.Weighted at 3ffd83cb: the
// lock is taken, conditionally released on early-return paths, re-taken inside
// a select case, and a helper (notifyWaiters) touches protected state without
// locking because every caller already holds mu. v1 reported six
// counterexamples here. On inspection every access is under mu.
const semaphoreLike = `package semaphore

import ("container/list"; "context"; "sync")

type waiter struct { n int64; ready chan<- struct{} }

type Weighted struct {
	size    int64
	cur     int64
	mu      sync.Mutex
	waiters list.List
}

func (s *Weighted) Acquire(ctx context.Context, n int64) error {
	done := ctx.Done()
	s.mu.Lock()
	select {
	case <-done:
		s.mu.Unlock()
		return ctx.Err()
	default:
	}
	if s.size-s.cur >= n && s.waiters.Len() == 0 {
		s.cur += n
		s.mu.Unlock()
		return nil
	}
	if n > s.size {
		s.mu.Unlock()
		<-done
		return ctx.Err()
	}
	ready := make(chan struct{})
	w := waiter{n: n, ready: ready}
	elem := s.waiters.PushBack(w)
	s.mu.Unlock()
	select {
	case <-done:
		s.mu.Lock()
		select {
		case <-ready:
			s.cur -= n
			s.notifyWaiters()
		default:
			isFront := s.waiters.Front() == elem
			s.waiters.Remove(elem)
			if isFront && s.size > s.cur {
				s.notifyWaiters()
			}
		}
		s.mu.Unlock()
		return ctx.Err()
	case <-ready:
		select {
		case <-done:
			s.Release(n)
			return ctx.Err()
		default:
		}
		return nil
	}
}

func (s *Weighted) TryAcquire(n int64) bool {
	s.mu.Lock()
	success := s.size-s.cur >= n && s.waiters.Len() == 0
	if success {
		s.cur += n
	}
	s.mu.Unlock()
	return success
}

func (s *Weighted) Release(n int64) {
	s.mu.Lock()
	s.cur -= n
	if s.cur < 0 {
		s.mu.Unlock()
		panic("semaphore: released more than held")
	}
	s.notifyWaiters()
	s.mu.Unlock()
}

func (s *Weighted) notifyWaiters() {
	for {
		next := s.waiters.Front()
		if next == nil {
			break
		}
		w := next.Value.(waiter)
		if s.size-s.cur < w.n {
			break
		}
		s.cur += w.n
		s.waiters.Remove(next)
		close(w.ready)
	}
}
`

func semaphoreProp() Proposition {
	return Proposition{Kind: KindFieldAccessUnderLock, Dir: "semaphore", Type: "Weighted", Field: "cur", Lock: "mu"}
}

// THE SPECIMEN. A true discipline the v1 reader called false.
func TestASemaphoreShapedDisciplineIsDerivedNotRefuted(t *testing.T) {
	src := pinned(t, map[string]string{"semaphore/semaphore.go": semaphoreLike})
	receipt, est := Derive(src, semaphoreProp(), at("2026-08-27T02:00:00Z"))
	if receipt.Outcome != Derived {
		t.Fatalf("outcome %s; every access to Weighted.cur is under mu on inspection:\n%s",
			receipt.Outcome, receipt.Detail)
	}
	if est == nil {
		t.Fatal("DERIVED without an Established")
	}
}

// REFUTED: a real counterexample the reader can point at -- a helper called
// from one site with the lock and from another without it.
func TestAHelperCalledWithoutTheLockIsRefutedAtTheCallSite(t *testing.T) {
	leaky := strings.Replace(semaphoreLike,
		"func (s *Weighted) TryAcquire(n int64) bool {\n\ts.mu.Lock()",
		"func (s *Weighted) Drain() { s.notifyWaiters() }\n\nfunc (s *Weighted) TryAcquire(n int64) bool {\n\ts.mu.Lock()", 1)
	src := pinned(t, map[string]string{"semaphore/semaphore.go": leaky})
	receipt, est := Derive(src, semaphoreProp(), at("2026-08-27T02:00:00Z"))
	if receipt.Outcome != Refuted {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	if est != nil {
		t.Fatal("a refuted proposition produced an Established")
	}
	if !strings.Contains(receipt.Detail, "Drain") || !strings.Contains(receipt.Detail, "not held") {
		t.Fatalf("the refutation must name the unlocked call site: %q", receipt.Detail)
	}
}

// UNRESOLVED: a stored closure. The reader cannot say when it runs, so it may
// neither assume the lock nor claim a counterexample.
func TestAnAccessInsideAStoredClosureIsUnresolvedNotRefuted(t *testing.T) {
	stored := strings.Replace(semaphoreLike,
		"func (s *Weighted) TryAcquire(n int64) bool {",
		"func (s *Weighted) Later() func() int64 { return func() int64 { return s.cur } }\n\nfunc (s *Weighted) TryAcquire(n int64) bool {", 1)
	src := pinned(t, map[string]string{"semaphore/semaphore.go": stored})
	receipt, est := Derive(src, semaphoreProp(), at("2026-08-27T02:00:00Z"))
	if receipt.Outcome != Unresolved {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	if est != nil {
		t.Fatal("an unresolved proposition produced an Established")
	}
	if strings.Contains(receipt.Detail, "not held") {
		t.Fatalf("UNRESOLVED must never be phrased as a counterexample: %q", receipt.Detail)
	}
	if !strings.Contains(receipt.Detail, "closure") {
		t.Fatalf("the detail must say what the reader could not follow: %q", receipt.Detail)
	}
}

// An exported helper that never locks is a counterexample, not a boundary:
// the type's API permits any caller to reach the access unlocked, and nothing
// in the package can prevent it. Whether an in-package caller happens to hold
// the lock does not change what the API allows.
func TestAnExportedHelperThatNeverLocksIsRefutedBecauseTheAPIPermitsIt(t *testing.T) {
	exported := strings.Replace(semaphoreLike, "notifyWaiters", "NotifyWaiters", -1)
	src := pinned(t, map[string]string{"semaphore/semaphore.go": exported})
	receipt, _ := Derive(src, semaphoreProp(), at("2026-08-27T02:00:00Z"))
	if receipt.Outcome != Refuted {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	if !strings.Contains(receipt.Detail, "exported") || !strings.Contains(receipt.Detail, "permits") {
		t.Fatalf("the detail must say WHY it is a counterexample: %q", receipt.Detail)
	}
}

// A genuinely unlocked access in a locking method is still REFUTED, and the
// detail names the site.
func TestAnAccessAfterUnlockIsRefuted(t *testing.T) {
	after := strings.Replace(semaphoreLike,
		"\ts.mu.Unlock()\n\treturn success\n}",
		"\ts.mu.Unlock()\n\ts.cur++\n\treturn success\n}", 1)
	src := pinned(t, map[string]string{"semaphore/semaphore.go": after})
	receipt, _ := Derive(src, semaphoreProp(), at("2026-08-27T02:00:00Z"))
	if receipt.Outcome != Refuted {
		t.Fatalf("outcome %s: %s", receipt.Outcome, receipt.Detail)
	}
	if !strings.Contains(receipt.Detail, "TryAcquire") {
		t.Fatalf("the counterexample must be located: %q", receipt.Detail)
	}
}
