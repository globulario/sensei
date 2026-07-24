// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"path"
	"strings"
)

// NormalizePath converts a caller-supplied path into the canonical
// repo-relative, slash-separated form used throughout this package, or
// reports ok=false when the path escapes the repository (absolute outside
// the root, `..` traversal, or a Windows-style absolute/drive path).
//
// Callers MUST use this before comparing, storing, or matching a path — raw
// strings.HasPrefix over unnormalized input is exactly the bug class this
// function exists to close (contract §3.7).
func NormalizePath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	p = filepathToSlash(p)
	// Reject Windows-style absolute/drive/UNC paths outright — this package
	// only ever reasons about repo-relative POSIX-style paths.
	if len(p) >= 2 && p[1] == ':' {
		return "", false
	}
	if strings.HasPrefix(p, "//") || strings.HasPrefix(p, "\\\\") {
		return "", false
	}
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	clean := path.Clean(p)
	if clean == "." {
		return "", false
	}
	// path.Clean collapses ".." segments syntactically but does not forbid a
	// result that still climbs above the root (e.g. "../x" cleans to
	// "../x"). Reject any remaining escape explicitly.
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// filepathToSlash converts OS-specific separators to '/'. Implemented
// locally (rather than importing path/filepath's Separator-dependent
// ToSlash) so normalization is platform-independent: a registry authored on
// Windows and evaluated on Linux (or vice versa) must normalize identically.
func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// InPathScope reports whether normalizedPath is exactly prefix, or lives
// under it as a path-segment-boundary-safe descendant. Both arguments must
// already be normalized (NormalizePath). This is segment-aware: prefix
// "src/auth" does NOT match "src/authorization/x.go" — only an exact
// component boundary counts (contract §3.7).
func InPathScope(normalizedPath, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return false
	}
	if normalizedPath == prefix {
		return true
	}
	return strings.HasPrefix(normalizedPath, prefix+"/")
}

// AnyInPathScope reports whether normalizedPath is in scope of any prefix in
// prefixes (each compared via InPathScope).
func AnyInPathScope(normalizedPath string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if InPathScope(normalizedPath, prefix) {
			return true
		}
	}
	return false
}
