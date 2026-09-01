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
	// THE CORPUS REVISION IS RESOLVED FROM THE ADMITTED BASE, NOT LOCAL HEAD.
	//
	// Resolving from HEAD counts a reviewer's own unmerged docs/awareness edit
	// as "admitted", so the published graph is reported stale during the exact
	// pre-merge review where Sensei is being used -- a false staleness warning
	// produced by the branch under review. Admitted means merged: prefer the
	// upstream base, and fall back to HEAD only when there is no upstream to
	// compare against, which a solo checkout legitimately has.
	// THE ADMISSION BRANCH IS RESOLVED, NOT ASSUMED.
	//
	// Hardcoding origin/main silently fell through to local HEAD on any
	// checkout tracking something else -- upstream/main, origin/master, a
	// release branch -- which reinstates the false-staleness bug the fallback
	// was added to avoid, on exactly the repositories that do not look like
	// this one. Ask git which branch this one tracks, then the remote's default
	// head, and use origin/main only as one guess among several.
	// NOT @{upstream}. A feature branch's upstream is its OWN pushed ref --
	// origin/feat/whatever -- so resolving the corpus from it counts the
	// branch's unmerged awareness edits as admitted, which is the precise bug
	// this is meant to avoid. My first attempt used it and the test written for
	// that bug caught it.
	//
	// The admission branch is the remote's DEFAULT HEAD.
	bases := [][]string{}
	if head, ok := git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ok && head != "" {
		bases = append(bases, append([]string{"log", "-1", "--format=%H", head, "--"}, CorpusPaths...))
	}
	bases = append(bases,
		append([]string{"log", "-1", "--format=%H", "origin/main", "--"}, CorpusPaths...),
		// Local HEAD LAST, and only when no admission branch exists at all --
		// a solo checkout legitimately has none.
		append([]string{"log", "-1", "--format=%H", "--"}, CorpusPaths...),
	)
	// A ROOT THAT DOES NOT OWN THE CORPUS ANSWERS NOTHING, and that is enforced
	// here rather than by a separate guard.
	//
	// GraphBuildCommit is the AWARENESS-GRAPH repository's SHA; measuring it
	// against a governed checkout that merely happens to be a git repository
	// would compare unrelated histories. Every lookup below is scoped to
	// CorpusPaths, so a root without them resolves nothing and falls through to
	// Unknown. An explicit holdsCorpus() check was written and then removed: no
	// mutation could distinguish it from this, which makes it duplication
	// rather than defence.
	corpus := ""
	for _, base := range bases {
		if v, ok := git(base...); ok && v != "" {
			corpus = v
			break
		}
	}
	if corpus == "" {
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
		// The published revision is not an ancestor of the corpus revision.
		// Before concluding anything, check the OTHER direction: a graph built
		// from a code-only or merge commit AFTER the last corpus change
		// contains every authored change, and reporting that as Unknown made
		// an ordinary up-to-date build unable to report current at all.
		if _, ahead := git("merge-base", "--is-ancestor", corpus, in.PublishedCommit); ahead {
			in.Contains = true
			in.AheadKnown = true
			in.CommitsAhead = 0
			return Assess(in)
		}
		// Neither is an ancestor of the other: a sibling branch, or a graph
		// built from work never merged here. Unorderable, so Unknown.
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
