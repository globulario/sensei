// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ContentDigest computes a canonical sha256 digest over this repository's
// git-tracked and untracked-but-not-ignored files (git ls-files -co
// --exclude-standard), excluding any path with one of excludePrefixes as a
// path-component prefix or exactly equal to one of excludePaths.
//
// This deliberately mirrors tasksession.outsideModifyDigest's algorithm
// (golang/architecture/tasksession/session.go) byte-for-byte -- same git
// invocation, same sort/filter shape, same sha256(path\0bytes\0...)
// construction -- but is an independent implementation. This package must
// never require modifying golang/architecture/tasksession, an existing
// admission-critical path flagged in docs/awareness/high_risk_files.yaml,
// for a net-new, non-critical reporting feature. If real duplication pain
// shows up later (a third caller needing the same algorithm), that is the
// right trigger to extract a shared helper -- not this package.
func ContentDigest(repoRoot string, excludePrefixes, excludePaths []string) (string, error) {
	excludedPath := map[string]bool{}
	for _, p := range excludePaths {
		excludedPath[filepath.ToSlash(filepath.Clean(p))] = true
	}

	cmd := exec.Command("git", "ls-files", "-co", "--exclude-standard", "-z")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var paths []string
	for _, raw := range strings.Split(string(out), "\x00") {
		rel := filepath.ToSlash(strings.TrimSpace(raw))
		if rel == "" || excludedPath[rel] {
			continue
		}
		excluded := false
		for _, prefix := range excludePrefixes {
			if strings.HasPrefix(rel, prefix) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		paths = append(paths, rel)
	}
	paths = sortedUnique(paths)

	h := sha256.New()
	for _, rel := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sortedUnique(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
