// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"fmt"
	"path/filepath"
	"strings"
)

// validateRepositoryTaskBinding proves that RepositoryRoot and TaskDirectory name ONE
// world: the task directory must resolve inside the repository root.
//
// Every completion owner resolves its lock, governed authority, and governed-manifest
// truth from the repository root while reading and mutating the task directory. If a
// caller pairs one repository with another repository's task directory, those are
// computed for a different world than the one read and mutated — and a legitimate
// operation on the real owning repository, which locks under ITS root, can race. Repository
// identity, lock identity, governed authority, and task identity must be one indivisible
// coordinate.
//
// The check is symlink-aware (a symlinked task directory cannot smuggle in another
// repository's world) and MUST run before any read, lock, authority resolution, receipt
// write, ledger append, or projection rebuild.
// validateRepositoryTaskBinding returns a typed *ProjectionIdentityError for every
// failure: an absent, unresolvable, or out-of-world task identity is an IDENTITY
// failure (block under enforce), never a runtime/availability failure. The typed error
// is what lets a downstream enforcement surface distinguish identity from runtime
// without ever parsing the message.
func validateRepositoryTaskBinding(root, taskDir string) error {
	root = strings.TrimSpace(root)
	taskDir = strings.TrimSpace(taskDir)
	if root == "" || taskDir == "" {
		return identityError("identity_absent", fmt.Errorf("repository root and task directory are required"))
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return identityError("identity_unresolvable", fmt.Errorf("resolve repository root: %w", err))
	}
	absTask, err := filepath.Abs(taskDir)
	if err != nil {
		return identityError("identity_unresolvable", fmt.Errorf("resolve task directory: %w", err))
	}
	realRoot := resolveSymlinks(absRoot)
	realTask := resolveSymlinks(absTask)
	rel, err := filepath.Rel(realRoot, realTask)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return identityError("identity_out_of_scope", fmt.Errorf("task directory %q is outside repository %q: repository and task must name one world", taskDir, root))
	}
	return nil
}

// resolveSymlinks returns the fully symlink-resolved path, or the input unchanged when it
// cannot be resolved (a component does not exist yet). A real cross-repository task
// directory exists and therefore resolves, so the fallback never widens the check: an
// unresolvable path has nothing to read or mutate.
func resolveSymlinks(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}
