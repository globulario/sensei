// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/authority"
)

// repoRootFor walks up from a corpus directory to the checkout root.
func repoRootFor(corpus string) string {
	dir, err := filepath.Abs(corpus)
	if err != nil {
		return corpus
	}
	for {
		if info, serr := os.Stat(filepath.Join(dir, ".git")); serr == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return corpus
		}
		dir = parent
	}
}

// policyIndexFor loads governed actor/role policy, returning an empty index when
// it cannot be read. An empty index verifies nothing, so admission still fails
// closed — it never silently widens authority.
func policyIndexFor(repoRoot string) authority.PolicyIndex {
	idx, err := authority.LoadPolicyIndex(repoRoot)
	if err != nil {
		return authority.PolicyIndex{}
	}
	return idx
}
