// SPDX-License-Identifier: AGPL-3.0-only

package graphbuild

import (
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/repodomain"
)

// resolveSourceRootRepositoryIdentity returns the canonical repository
// identity every SourceFile subject compiled from root is scoped to
// (issue #197).
//
// An explicit SourceRoot.RepositoryIdentity wins. Otherwise the identity is
// resolved from the tree itself, by walking up from IdentityRoot (falling
// back to FilesystemPath) for the nearest checkout that configures a
// repository domain. Resolving from the TREE rather than from the process's
// working directory is what makes a cross-repo source root
// (--input ../other-repo/docs/awareness) attribute the other repository's
// files to the other repository: a file belongs to the repository it lives
// in, not to whichever build happened to read it.
//
// An unresolved identity is an error. There is no unscoped fallback,
// because an unscoped SourceFile identity is exactly the collision #197
// retired: every repository's README.md would collapse onto one subject
// again.
func resolveSourceRootRepositoryIdentity(root SourceRoot) (string, error) {
	if explicit := strings.TrimSpace(root.RepositoryIdentity); explicit != "" {
		return explicit, nil
	}
	tree := strings.TrimSpace(root.IdentityRoot)
	if tree == "" {
		tree = root.FilesystemPath
	}
	identity, err := repodomain.IdentityForTree(tree)
	if err != nil {
		return "", err
	}
	if identity == "" {
		return "", fmt.Errorf(
			"no canonical repository identity for this tree, so its SourceFile identities cannot be scoped to the repository that owns them.\n" +
				"Declare it in the owning checkout's sensei-repository.yaml (`sensei init` and `sensei bootstrap` write it), or name it explicitly with --repository-identity")
	}
	return identity, nil
}
