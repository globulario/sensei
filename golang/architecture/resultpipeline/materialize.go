// SPDX-License-Identifier: AGPL-3.0-only

// Package resultpipeline composes the complete result architecture pipeline for
// an admitted, scope-verified task: from the exact repository result tree (bound
// by the resulttransition package) through every mandatory derivation stage —
// governed source manifest, generated repository artifacts, architecture graph,
// inferred claims, maintained claims, plane assessment, closure assessment,
// architect questions, proof requirements, and the canonical artifact manifest —
// into one deterministic, in-memory, independently inspectable bundle bound to a
// complete frozen closureprotocol.ResultBinding.
//
// It is pure and offline. It never writes repository files, loads or mutates a
// live graph store, performs network or gRPC calls, resolves the current working
// directory, reads mutable task projections, appends ledger events, creates a
// ResultTransitionReceipt, moves a task to proving, collects runtime evidence,
// discharges proof, certifies correctness, or completes a task. Those belong to
// later slices.
package resultpipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runGit runs `git -C repoRoot <args...>` and returns stdout.
func runGit(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// runGitEnv runs git with an explicit environment so an isolated GIT_INDEX_FILE
// keeps the repository's real index and working tree untouched.
func runGitEnv(repoRoot string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// materializeTree checks the exact Git tree object treeish out into a fresh
// temporary directory, using a private temporary index so the repository's real
// index, working tree, and HEAD are never touched and the live working tree is
// never copied. It returns the absolute path of the materialized root and a
// cleanup that removes it; cleanup must always be invoked.
//
// Properties, verified by tests:
//   - tracked files match the tree exactly (content and executable bit);
//   - symlinks are preserved as symlinks and never followed;
//   - gitlinks (submodule commits) are not blobs and are simply not written —
//     they are never silently flattened into directories;
//   - no ignored, untracked, or temporary live-worktree file leaks in;
//   - the root contains no .git directory and no identity-bearing absolute path.
func materializeTree(ctx context.Context, repoRoot, treeish string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", func() {}, err
	}
	treeish = strings.TrimSpace(treeish)
	if treeish == "" {
		return "", func() {}, fmt.Errorf("resultpipeline: empty treeish")
	}

	idxDir, err := os.MkdirTemp("", "sensei-p7-idx-")
	if err != nil {
		return "", func() {}, err
	}
	defer os.RemoveAll(idxDir)

	destDir, err := os.MkdirTemp("", "sensei-p7-root-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(destDir) }

	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(idxDir, "index"))
	if _, err := runGitEnv(repoRoot, env, "read-tree", treeish); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("resultpipeline: read-tree %s: %w", treeish, err)
	}
	// checkout-index writes every indexed blob under the prefix, preserving mode
	// bits and symlinks. Gitlinks carry no blob and are skipped. The trailing
	// separator makes --prefix a directory prefix.
	prefix := destDir + string(os.PathSeparator)
	if _, err := runGitEnv(repoRoot, env, "checkout-index", "-a", "-f", "--prefix="+prefix); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("resultpipeline: checkout-index: %w", err)
	}
	return destDir, cleanup, nil
}
