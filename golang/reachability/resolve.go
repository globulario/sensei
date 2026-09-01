// SPDX-License-Identifier: AGPL-3.0-only

package reachability

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
)

// CorpusPaths are the authored-knowledge directories whose movement makes a
// published generation stale. Movement anywhere else in the repository does not.
var CorpusPaths = []string{"docs/awareness"}

// ResolveFromGit answers the reachability question for a caller that holds the
// repository. It is the only impure part of this package, and it FAILS TO
// UNKNOWN in every direction it cannot answer: not a repository, git absent,
// the published revision not in this history, the corpus never committed.
//
// It never falls back to "current". An unresolvable question is Unknown, which
// is a member of the state set and not an error dressed as success.
func ResolveFromGit(ctx context.Context, repoRoot, publishedCommit string) Assessment {
	in := Inputs{PublishedCommit: strings.TrimSpace(publishedCommit)}
	if in.PublishedCommit == "" {
		return Assess(in)
	}
	git := func(args ...string) (string, bool) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
		out, err := cmd.Output()
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(string(out)), true
	}
	corpus, ok := git(append([]string{"log", "-1", "--format=%H", "--"}, CorpusPaths...)...)
	if !ok || corpus == "" {
		return Assess(in)
	}
	in.CorpusCommit = corpus

	// Does this history contain the revision the graph was built from? If the
	// object is unknown here, the two cannot be ordered and the answer is
	// Unknown -- not stale.
	if _, ok := git("cat-file", "-e", in.PublishedCommit+"^{commit}"); !ok {
		return Assess(in)
	}
	if _, ok := git("merge-base", "--is-ancestor", in.PublishedCommit, corpus); !ok {
		// Reachable object, but not an ancestor of the corpus revision: a
		// sibling branch, or a graph built from work never merged here.
		return Assess(in)
	}
	in.Contains = true

	// Count only commits that TOUCHED the corpus. A thousand code commits do
	// not make governed knowledge stale, and counting them would cry wolf
	// until the signal was ignored.
	countArgs := append([]string{"rev-list", "--count", in.PublishedCommit + ".." + corpus, "--"}, CorpusPaths...)
	if n, ok := git(countArgs...); ok {
		if v, err := strconv.Atoi(n); err == nil {
			in.CommitsAhead = v
			in.AheadKnown = true
		}
	}
	return Assess(in)
}
