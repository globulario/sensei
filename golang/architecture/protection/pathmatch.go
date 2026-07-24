// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"errors"
	"path"
	"path/filepath"
	"strings"
)

// NormalizePath converts an AUTHORED, repo-relative path declaration (a
// docs/awareness/*.yaml `protects.files` entry, a manual registry prefix, a
// scanner-computed repo-relative path) into the canonical slash-separated
// form used throughout this package, or reports ok=false when the string is
// not a valid repo-relative declaration at all: empty, `..` traversal, a
// Windows-style absolute/drive/UNC path, OR a POSIX absolute path.
//
// This function has no repository root to resolve against, so it NEVER
// accepts a leading "/" by silently reinterpreting it as repo-relative
// (stripping "/etc/passwd" to "etc/passwd" would be exactly the kind of
// silent reinterpretation contract §3.7 forbids) — an authored source
// declaring an absolute path is malformed input, not a repo-relative path
// missing its leading slash.
//
// A real caller-supplied filesystem path (a CLI --file flag, a hook's
// resolved file path) MUST go through ResolveRepoPath instead, which has the
// repository root available to safely resolve and contain absolute paths,
// relative paths, and symlinks.
//
// Callers MUST use this (or ResolveRepoPath) before comparing, storing, or
// matching a path — raw strings.HasPrefix over unnormalized input is exactly
// the bug class this function exists to close (contract §3.7).
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
	// A POSIX absolute path is not a repo-relative declaration — reject
	// rather than silently reinterpret (see doc comment above).
	if strings.HasPrefix(p, "/") {
		return "", false
	}
	p = strings.TrimPrefix(p, "./")
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

// ResolveRepoPath safely resolves a caller-supplied filesystem path — CLI
// --file flag, a hook's resolved file path, absolute or relative — against
// repoRoot, returning the canonical repo-relative slash-separated form.
//
// Unlike NormalizePath (which only validates an already-relative string
// syntactically), ResolveRepoPath is symlink-aware: it resolves repoRoot and
// the candidate path with filepath.EvalSymlinks and verifies the RESOLVED
// location is actually inside the RESOLVED repository root, so a symlink
// that appears to be inside the repo but points outside it is rejected
// (contract §3.7's "fail safely on malformed paths and symlink escapes").
//
// A target that does not exist yet (a file about to be created) is handled
// by resolving the nearest existing ancestor directory and rejoining the
// remaining components — EvalSymlinks itself requires an existing path.
func ResolveRepoPath(repoRoot, p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" || repoRoot == "" {
		return "", false
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", false
	}

	absPath := p
	if !filepath.IsAbs(p) {
		absPath = filepath.Join(absRoot, p)
	}
	absPath = filepath.Clean(absPath)

	// Resolve the longest existing ancestor so a not-yet-created file still
	// resolves symlinks on the directories that DO exist.
	resolvedDir, remainder, err := resolveExistingAncestor(absPath)
	if err != nil {
		return "", false
	}
	resolvedPath := resolvedDir
	if remainder != "" {
		resolvedPath = filepath.Join(resolvedDir, remainder)
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil {
		return "", false
	}
	return NormalizePath(rel)
}

// resolveExistingAncestor walks up from absPath (already filepath.Clean'd,
// absolute) until it finds a path segment that exists on disk, resolves
// THAT segment's symlinks, and returns it plus the remaining path
// components that don't exist yet (joined with "/", empty if absPath itself
// exists).
func resolveExistingAncestor(absPath string) (resolved string, remainder string, err error) {
	current := absPath
	var tail []string
	for {
		if real, evalErr := filepath.EvalSymlinks(current); evalErr == nil {
			return real, strings.Join(tail, "/"), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			// Reached the filesystem root without finding anything real.
			return "", "", errNoExistingAncestor
		}
		tail = append([]string{filepath.Base(current)}, tail...)
		current = parent
	}
}

var errNoExistingAncestor = errors.New("no existing ancestor directory found")

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
