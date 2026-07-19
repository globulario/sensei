// SPDX-License-Identifier: Apache-2.0

// Package resulttransition binds the exact repository result of an admitted,
// scope-verified task to the frozen closure protocol. It owns the repository
// side of the result boundary: loading the verified upstream ledger truth,
// resolving the committed or worktree result tree, deriving the canonical
// Sensei tree identity, re-deriving the observed change and proving it is
// exactly what Phase 3 admitted, and constructing the typed pre-transition
// repository result binding.
//
// It deliberately does NOT rebuild the architecture graph, produce operational
// pipeline artifacts, append a result_transition_recorded event, move a task to
// proving, or certify or complete anything. Those belong to later Phase 7
// slices and to Phase 6 respectively.
package resulttransition

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
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

// runGitEnv runs git with an explicit environment (used to isolate GIT_INDEX_FILE
// so the real repository index and working tree are never touched).
func runGitEnv(repoRoot string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// worktreeResultTree materializes the current working tree as a Git tree object
// WITHOUT mutating the repository's real index or working tree. It seeds an
// isolated index (via GIT_INDEX_FILE in a private temp directory) with the
// admitted base tree, stages every worktree difference (modifications,
// deletions, and untracked additions), and writes the resulting tree object.
//
// The returned cleanup removes the temp index directory and must always be
// invoked. This mirrors the admission-v2 observation exactly so the re-derived
// result tree is byte-identical to the one Phase 3 scope-verified.
func worktreeResultTree(repoRoot, baseRev string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "sensei-result-index-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmpDir) }
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmpDir, "index"))
	if _, err := runGitEnv(repoRoot, env, "read-tree", baseRev); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if _, err := runGitEnv(repoRoot, env, "add", "-A"); err != nil {
		cleanup()
		return "", func() {}, err
	}
	out, err := runGitEnv(repoRoot, env, "write-tree")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return strings.TrimSpace(out), cleanup, nil
}

// resolveResultTreeish returns the Git treeish that names the exact result and a
// cleanup to run when done. For a committed result the treeish is the caller's
// revision; for a worktree result it is the isolated write-tree object id.
func resolveResultTreeish(repoRoot, baseRev, resultRev string) (string, func(), error) {
	resultRev = strings.TrimSpace(resultRev)
	if resultRev != "" {
		return resultRev, func() {}, nil
	}
	return worktreeResultTree(repoRoot, baseRev)
}

// changedFiles derives the observed file set from the base->result diff, using
// the same name-status parsing and change-type mapping as admission-v2 so the
// two observations cannot drift. A rename carries both endpoints so downstream
// verification refuses it honestly instead of mis-reading the destination as an
// ordinary modification.
func changedFiles(repoRoot, baseRev, resultTreeish string) ([]admission.ObservedFile, error) {
	out, err := runGit(repoRoot, "diff", "--name-status", baseRev, resultTreeish)
	if err != nil {
		return nil, err
	}
	var files []admission.ObservedFile
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		changeType := gitChangeType(fields[0])
		if changeType == "rename" && len(fields) >= 3 {
			from := filepath.ToSlash(fields[1])
			to := filepath.ToSlash(fields[2])
			files = append(files, admission.ObservedFile{ChangeType: changeType, Path: to, FromPath: from, ToPath: to})
			continue
		}
		files = append(files, admission.ObservedFile{ChangeType: changeType, Path: filepath.ToSlash(fields[len(fields)-1])})
	}
	return files, nil
}

// gitChangeType maps a Git name-status code to the observed change vocabulary.
func gitChangeType(code string) string {
	switch {
	case strings.HasPrefix(code, "A"):
		return "create"
	case strings.HasPrefix(code, "D"):
		return "delete"
	case strings.HasPrefix(code, "R"):
		return "rename"
	default:
		return "modify"
	}
}

// patchDigest returns the SHA-256 of the canonical binary patch from base to the
// result treeish, matching the admission-v1 CaptureChanges convention (raw
// sha256 of `git diff --no-ext-diff --binary`) but ranging over the exact result
// tree so it is valid for both committed and worktree results.
func patchDigest(repoRoot, baseRev, resultTreeish string) (string, error) {
	out, err := runGit(repoRoot, "diff", "--no-ext-diff", "--binary", baseRev, resultTreeish)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(out))
	return hex.EncodeToString(sum[:]), nil
}
