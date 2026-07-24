// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"
	"path/filepath"
	"strings"
)

// joinRepo joins a normalized repo-relative path onto an OS filesystem root.
func joinRepo(repoRoot, relPath string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(relPath))
}

// walkFiles walks dir (an OS path) and returns every regular file found
// under it, as repo-relative normalized paths (relative to repoRoot). A
// missing dir yields (nil, nil) — absence is not an error.
func walkFiles(repoRoot, dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var out []string
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, p)
		if relErr != nil {
			return nil
		}
		if norm, ok := NormalizePath(rel); ok {
			out = append(out, norm)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// isUnderAny reports whether normalizedPath is under any of the given
// normalized directory prefixes (segment-boundary safe).
func isUnderAny(normalizedPath string, dirs ...string) bool {
	return AnyInPathScope(normalizedPath, dirs)
}

// splitTestID splits a "path/to/file_test.go:TestName" required_tests entry
// into its file portion. Entries without a ':' are returned unchanged (the
// whole string is treated as a file path).
func splitTestID(id string) string {
	if idx := strings.LastIndex(id, ":"); idx > 0 {
		// Guard against a bare Windows drive-letter colon (`C:...`), which
		// NormalizePath rejects anyway, but keep this local split honest.
		if idx != 1 {
			return id[:idx]
		}
	}
	return id
}
