// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// BuildManifest walks root -- a real directory, either a pinned snapshot or
// a final candidate buffer -- and returns its canonical manifest, per hard
// law 9's tree-encoding rules and the same no-follow discipline
// CandidateWorkspace enforces. filepath.WalkDir does not follow symlinks:
// each symlink it encounters is reported via its own fs.DirEntry without
// ever being dereferenced, so a symlink anywhere in the tree becomes its
// own opaque manifest entry (its target read via os.Readlink, never
// resolved) rather than being traversed into.
//
// root itself must be a real directory, not a symlink -- WalkDir does not
// follow a symlink even at the root, which would otherwise silently return
// an empty manifest rather than an error.
//
// The returned manifest is already canonical (CanonicalizeManifest is
// called internally): a path that cannot be represented under
// ValidateCandidatePath's rules (e.g. a real file whose name happens to
// contain a backslash or newline byte) fails the build rather than being
// silently mangled.
func BuildManifest(root string) ([]CandidateManifestEntry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("BuildManifest: stat %q: %w", root, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("BuildManifest: root %q must not itself be a symlink", root)
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("BuildManifest: root %q is not a directory", root)
	}

	var entries []CandidateManifestEntry
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %q: %w", p, err)
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("relativize %q: %w", p, err)
		}
		posixPath := filepath.ToSlash(rel)

		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return fmt.Errorf("readlink %q: %w", p, err)
			}
			entries = append(entries, CandidateManifestEntry{
				Path:                posixPath,
				Mode:                ModeSymlink,
				Content:             []byte{},
				SymlinkTarget:       target,
				ContentDigestSHA256: sha256Hex([]byte(target)),
			})
			return nil
		case d.IsDir():
			// Directories are implicit -- reconstructed from their
			// children's paths, never their own manifest entry.
			return nil
		case !d.Type().IsRegular():
			// A FIFO, socket, device, or other special file. None has a
			// representation in the closed regular/executable/symlink
			// mode vocabulary, and opening one (e.g. os.ReadFile on a
			// FIFO with no writer) can block indefinitely rather than
			// fail promptly. Reject outright rather than either hanging
			// or silently treating it as an ordinary file.
			return fmt.Errorf("%q is not a regular file, directory, or symlink (mode %v) -- BuildManifest has no representation for it", p, d.Type())
		default:
			content, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read %q: %w", p, err)
			}
			info, err := d.Info()
			if err != nil {
				return fmt.Errorf("info %q: %w", p, err)
			}
			mode := ModeRegular
			if info.Mode()&0o111 != 0 {
				mode = ModeExecutable
			}
			entries = append(entries, CandidateManifestEntry{
				Path:                posixPath,
				Mode:                mode,
				Content:             content,
				ContentDigestSHA256: sha256Hex(content),
			})
			return nil
		}
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return CanonicalizeManifest(entries)
}
