// SPDX-License-Identifier: AGPL-3.0-only

package architecture

// Binding a scan to a revision.
//
// ResolveRevision answers "what commit is HEAD". That is not the same question
// as "were the bytes I just read the bytes at that commit". A scanner that
// walks the working tree and then stamps HEAD onto what it found asserts the
// second while having only established the first: on a dirty checkout every
// provenance field reads resolved while the facts cite files the named commit
// does not contain.
//
// UncommittedSourceFiles establishes the second question directly, over the
// files a scan actually read, so a caller can bind honestly or refuse to bind
// at all.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// UncommittedSourceFiles returns the repo-relative source files, from the given
// set, whose working-tree bytes are not the bytes committed at revision: files
// the revision does not contain at all (untracked or ignored additions) and
// files it contains with different content (staged or unstaged modifications).
//
// An empty result means every named file was read from the committed revision,
// so a binding to that revision is true of them. An error means the question
// could not be answered; callers must treat that as "not established" rather
// than as "clean", since a scan that cannot verify its binding has not verified
// it.
//
// Paths are interpreted relative to root, which must be the repository top
// level (git reports paths from the top level, and ResolveRevision only reports
// a resolved revision for a root that holds .git).
func UncommittedSourceFiles(root, revision string, files []string) ([]string, error) {
	root = strings.TrimSpace(root)
	revision = strings.TrimSpace(revision)
	if root == "" || revision == "" {
		return nil, fmt.Errorf("repository root and revision are required")
	}
	wanted := map[string]bool{}
	for _, f := range files {
		f = filepath.ToSlash(strings.TrimSpace(f))
		if f == "" || filepath.IsAbs(f) || f == ".." || strings.HasPrefix(f, "../") {
			continue
		}
		// Only regular files carry bytes a scan can have read. Facts also cite
		// directories and synthetic sources; a directory is absent from
		// `git ls-tree`'s blob listing, so counting one as uncommitted would
		// report every tree as dirty forever.
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(f))); err != nil || !info.Mode().IsRegular() {
			continue
		}
		wanted[f] = true
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	committed, err := gitPathSet(root, "ls-tree", "-r", "--name-only", "-z", revision)
	if err != nil {
		return nil, err
	}
	modified, err := gitPathSet(root, "diff", "--name-only", "-z", revision, "--")
	if err != nil {
		return nil, err
	}
	var out []string
	for f := range wanted {
		if !committed[f] || modified[f] {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

// gitPathSet runs a NUL-delimited path-listing git command and returns its
// paths as a set. NUL delimiting is required: the default output quotes and
// escapes unusual path names, which would silently fail to match a scanned
// path.
func gitPathSet(root string, args ...string) (map[string]bool, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	set := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		p = strings.TrimSpace(p)
		if p != "" {
			set[filepath.ToSlash(p)] = true
		}
	}
	return set, nil
}
