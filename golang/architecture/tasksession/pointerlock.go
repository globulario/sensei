// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Serialization for the active-task pointer, owned by the package that owns the
// pointer.
//
// WHY THIS EXISTS. ClearActivePointer used to read the pointer, compare its task
// id, and then unlink the file. Between the comparison and the unlink another
// writer could replace active.yaml with a different task's pointer -- Prepare
// does exactly that, atomically -- and the unlink would then delete a pointer
// that had never been checked. The identity check was real and its conclusion
// was stale by the time it was acted on. A second read immediately before the
// unlink does not fix this; it only shortens the window.
//
// WHY NOT THE GOVERNED-MUTATION LOCK. governedmutation.AcquireLock is the
// obvious candidate and it cannot be used here. It is a single repository-scoped
// lock and it is NOT reentrant, and terminal abandonment already holds it for
// the whole transition -- so a ClearActivePointer that acquired it would
// deadlock against its own caller. Its documented contract is also that owners
// never lock internally, which is the opposite of what the pointer needs.
//
// So the pointer gets its own lock, in the package that owns the file, using the
// same directory-as-mutex technique already proven in governedmutation. It
// serializes pointer writers against pointer writers and nothing else, which is
// exactly the scope of the race. It is not a new lifecycle protocol: no state,
// no vocabulary, no receipt -- one mutex over one file.

// pointerLockWait bounds how long a writer waits for the pointer lock.
//
// Bounded rather than indefinite: a stale lock directory left by a killed
// process must not hang every later task forever, and a caller that cannot get
// the lock quickly is better told so than blocked. The window a legitimate
// holder needs is a read, a compare and a rename.
const pointerLockWait = 5 * time.Second

// ErrPointerLockHeld reports that another pointer writer holds the lock.
var ErrPointerLockHeld = errors.New("the active-task pointer is being written by another operation")

func pointerLockDir(repoRoot string) string {
	return filepath.Join(repoRoot, ".sensei", "tasks", ".active.lock")
}

// acquirePointerLock takes the pointer lock and returns its release.
func acquirePointerLock(repoRoot string) (func(), error) {
	dir := pointerLockDir(repoRoot)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(pointerLockWait)
	for {
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: %s", ErrPointerLockHeld, dir)
		}
		time.Sleep(2 * time.Millisecond)
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		_ = os.RemoveAll(dir)
	}, nil
}

// afterPointerIdentityCheck is a test seam fired between the identity check and
// the unlink, so the window can be driven deterministically instead of raced.
// It is nil in production and the lock is held while it runs -- which is the
// point: a lock-respecting writer cannot act inside it.
var afterPointerIdentityCheck func()
