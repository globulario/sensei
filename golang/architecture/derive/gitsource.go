// SPDX-License-Identifier: AGPL-3.0-only

package derive

// The pinned source a derivation reads.
//
// Everything here goes through `git` at an immutable revision. The proposer
// selects WHERE to look — a package directory — and supplies none of what is
// found there. That is the rule #298 established for evidence references,
// carried into derivation: a claimant may direct the investigation and may not
// manufacture its result.
//
// Reading the working tree instead would quietly break it. A dirty checkout is
// whatever somebody last typed, so a receipt naming a commit while reading
// unstaged bytes would describe a world that never existed.

import (
	"context"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// GitSource reads one repository at one commit.
type GitSource struct {
	ctx    context.Context
	dir    string
	repo   string
	commit string
}

// NewGitSource pins a repository to a revision, resolving it to a full commit
// id so the receipt records what was actually read rather than a moving name.
func NewGitSource(ctx context.Context, dir, repo, revision string) (*GitSource, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", strings.TrimSpace(revision)+"^{commit}").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %q in %s: %w", revision, dir, err)
	}
	return &GitSource{ctx: ctx, dir: dir, repo: repo, commit: strings.TrimSpace(string(out))}, nil
}

func (g *GitSource) Repository() string { return g.repo }
func (g *GitSource) Commit() string     { return g.commit }

// List returns the repo-relative paths directly under dir at the pinned commit.
func (g *GitSource) List(dir string) ([]string, error) {
	clean := strings.Trim(strings.TrimSpace(dir), "/")
	out, err := exec.CommandContext(g.ctx, "git", "-C", g.dir, "ls-tree", "--name-only",
		g.commit, clean+"/").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree %s at %s: %w", clean, shortCommit(g.commit), err)
	}
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, path.Clean(line))
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s is empty or absent at %s", clean, shortCommit(g.commit))
	}
	return paths, nil
}

// Read returns one path's bytes at the pinned commit.
func (g *GitSource) Read(p string) ([]byte, error) {
	out, err := exec.CommandContext(g.ctx, "git", "-C", g.dir, "show", g.commit+":"+p).Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s at %s: %w", p, shortCommit(g.commit), err)
	}
	return out, nil
}
