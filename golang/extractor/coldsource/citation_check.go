// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// GitVerifier resolves whether a commit SHA exists in a repo. Injectable so the
// check is unit-testable without a real repo.
type GitVerifier interface {
	CommitExists(sha string) bool
}

type execGitVerifier struct{ repo string }

func (g execGitVerifier) CommitExists(sha string) bool {
	if strings.TrimSpace(sha) == "" {
		return false
	}
	cmd := exec.Command("git", "-C", g.repo, "rev-parse", "--verify", "--quiet", sha+"^{commit}")
	return cmd.Run() == nil
}

// NewGitVerifier returns a verifier bound to a repo working tree.
func NewGitVerifier(repo string) GitVerifier { return execGitVerifier{repo: repo} }

// CitationResult is the outcome of verifying one citation string.
type CitationResult struct {
	Citation string
	OK       bool
	Reason   string // failure reason, or how it resolved
}

// CheckCitations verifies every citation resolves against the repo:
//   - file:<path>[:line] — the file must exist; if a line is given it must be
//     within range;
//   - commit:<sha> — must resolve via git;
//   - pr:<...> — provenance is PRESERVED but not failed offline (cheap network
//     verification isn't available; the id is kept for the human reviewer).
//
// Returns overall ok (all non-pr citations resolved) and per-citation results.
func CheckCitations(citations []string, repoRoot string, git GitVerifier) (bool, []CitationResult) {
	ok := true
	results := make([]CitationResult, 0, len(citations))
	for _, c := range citations {
		r := checkOne(c, repoRoot, git)
		if !r.OK {
			ok = false
		}
		results = append(results, r)
	}
	return ok, results
}

func checkOne(c, repoRoot string, git GitVerifier) CitationResult {
	switch {
	case strings.HasPrefix(c, "file:"):
		rest := strings.TrimPrefix(c, "file:")
		p, line := rest, 0
		// file:<path>:<line> — a trailing :<int> is the line.
		if i := strings.LastIndexByte(rest, ':'); i >= 0 {
			if n, err := strconv.Atoi(rest[i+1:]); err == nil {
				p, line = rest[:i], n
			}
		}
		abs := safeJoin(repoRoot, p)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return CitationResult{c, false, "file does not exist: " + p}
		}
		if line > 0 {
			if cnt := countLines(abs); line > cnt {
				return CitationResult{c, false, "line out of range (" + strconv.Itoa(line) + " > " + strconv.Itoa(cnt) + "): " + p}
			}
		}
		return CitationResult{c, true, "resolved"}

	case strings.HasPrefix(c, "commit:"):
		sha := strings.TrimPrefix(c, "commit:")
		if git != nil && git.CommitExists(sha) {
			return CitationResult{c, true, "resolved"}
		}
		return CitationResult{c, false, "commit does not resolve: " + sha}

	case strings.HasPrefix(c, "pr:"):
		return CitationResult{c, true, "pr provenance preserved (not network-verified)"}

	default:
		return CitationResult{c, false, "unknown citation form: " + c}
	}
}

// safeJoin joins a repo-relative path under root, stripping any leading
// traversal so a citation can't point outside the repo.
func safeJoin(root, p string) string {
	clean := filepath.Clean("/" + p)
	return filepath.Join(root, clean[1:])
}

func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		n++
	}
	return n
}
