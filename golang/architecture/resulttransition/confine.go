// SPDX-License-Identifier: Apache-2.0

package resulttransition

import (
	"fmt"
	"path"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// confineResult fails closed if the result cannot be honestly bound to the
// repository boundary: a rename (protocol v1 cannot represent it), a path that
// escapes the repository root, or a symlink whose target leaves the repository.
// A submodule gitlink is permitted — its commit object id is captured in the
// canonical tree material, so it is bound deterministically rather than
// flattened.
func confineResult(repoRoot, treeish string, observed admission.ObservedChangeSet) error {
	for _, f := range observed.Files {
		if f.ChangeType == "rename" {
			return fmt.Errorf("resulttransition: rename_unsupported: renamed %q -> %q cannot be represented in protocol v1", f.FromPath, f.ToPath)
		}
		for _, p := range []string{f.Path, f.FromPath, f.ToPath} {
			if p == "" {
				continue
			}
			if err := confinedPath(p); err != nil {
				return fmt.Errorf("resulttransition: path_escape: %w", err)
			}
		}
	}
	return confineSymlinks(repoRoot, treeish)
}

// confinedPath rejects absolute paths and any `..` traversal segment.
func confinedPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || strings.Contains(p, ":\\") {
		return fmt.Errorf("path %q must be repository-relative, not absolute", p)
	}
	for _, seg := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if seg == ".." {
			return fmt.Errorf("path %q must not contain a .. traversal", p)
		}
	}
	return nil
}

// confineSymlinks enumerates the result tree and refuses any symlink whose
// target escapes the repository root, so a bound result cannot smuggle a write
// outside the governed tree when later materialized.
func confineSymlinks(repoRoot, treeish string) error {
	out, err := runGit(repoRoot, "ls-tree", "-r", "-z", "--full-tree", treeish)
	if err != nil {
		return fmt.Errorf("resulttransition: enumerate result tree: %w", err)
	}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		meta, filePath, ok := strings.Cut(entry, "\t")
		if !ok {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 {
			continue
		}
		mode, object := fields[0], fields[2]
		if mode != "120000" {
			continue
		}
		target, err := runGit(repoRoot, "cat-file", "blob", object)
		if err != nil {
			return fmt.Errorf("resulttransition: read symlink %q: %w", filePath, err)
		}
		if symlinkEscapes(path.Clean(filePath), strings.TrimRight(target, "\n")) {
			return fmt.Errorf("resulttransition: symlink_escape: symlink %q -> %q leaves the repository root", filePath, strings.TrimRight(target, "\n"))
		}
	}
	return nil
}

// symlinkEscapes reports whether a symlink at linkPath pointing at target
// resolves outside the repository root. An absolute target always escapes; a
// relative target escapes when it climbs above the root.
func symlinkEscapes(linkPath, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "/") || strings.HasPrefix(target, "\\") || strings.Contains(target, ":\\") {
		return true
	}
	resolved := path.Clean(path.Join(path.Dir(linkPath), strings.ReplaceAll(target, "\\", "/")))
	return resolved == ".." || strings.HasPrefix(resolved, "../")
}
