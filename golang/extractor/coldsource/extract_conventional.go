// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"regexp"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
)

// reConventional matches a conventional-commit fix/perf/refactor subject, with
// an optional scope ("fix(router):") and optional breaking marker ("fix!:").
// These are the change types that imply a bug, a performance failure, or a
// structural rule — NOT feat/docs/chore/style/test/build/ci, which are not
// architecture scars and would add noise.
//
// `revert:` is intentionally excluded: a revert is a STRONGER scar already
// captured by ExtractReverts as SourceRevertCommit; re-emitting it here would
// downgrade it to the weakest signal.
var reConventional = regexp.MustCompile(`(?i)^(fix|perf|refactor)(\([^)]*\))?!?:`)

// matchConventional reports whether a commit is a conventional fix/perf/refactor
// and the candidate-class hint it carries. Commits that are already revert/
// regression scars are NOT conventional signals (the caller skips them).
func matchConventional(c CommitRecord) (bool, string) {
	m := reConventional.FindStringSubmatch(strings.TrimSpace(c.Subject))
	if m == nil {
		return false, ""
	}
	switch strings.ToLower(m[1]) {
	case "refactor":
		return true, extractor.CandidateInvariant
	default: // fix, perf — a corrected bug / performance failure
		return true, extractor.CandidateFailureMode
	}
}

// ExtractConventionalCommits turns conventional fix/perf/refactor commits into
// WEAK ColdSignals — one per touched (non-excluded) file. They are the commit-
// channel signal for repos where explicit reverts are sparse (e.g. TypeScript
// projects using conventional-commits + squash-merge). On their own they never
// triangulate; they only corroborate with a review-channel signal.
//
// Commits that already match the revert/regression matcher are skipped — that
// scar is the stronger SourceRevertCommit and must not be downgraded here.
func ExtractConventionalCommits(commits []CommitRecord) []ColdSignal {
	var out []ColdSignal
	for _, c := range commits {
		if revert, _ := matchCommit(c); revert {
			continue
		}
		matched, class := matchConventional(c)
		if !matched {
			continue
		}
		quote := strings.TrimSpace(firstLine(c.Subject))
		var targets []string
		for _, f := range c.Files {
			if isExcludedSurface(f) {
				continue
			}
			targets = append(targets, f)
		}
		// A conventional commit touching only excluded surfaces (e.g. a lockfile
		// bump) carries no architecture theme — drop it entirely rather than
		// seeding a commit-only theme. (Reverts keep a commit-only fallback
		// because the scar itself is meaningful; a routine fix is not.)
		for _, f := range targets {
			theme := themeFromPath(f)
			if theme == "" {
				continue
			}
			out = append(out, ColdSignal{
				SourceType:    SourceConventionalCommit,
				ThemeKey:      theme,
				ProposedClass: class,
				FilePath:      f,
				CommitSHA:     c.SHA,
				MatchedText:   quote,
			})
		}
	}
	return out
}

// LoadConventionalSignals reads commits in the range, keeps only conventional
// fix/perf/refactor commits that are not already revert scars, enriches them
// with their touched files (bounded: only matched commits incur a git call),
// and extracts ColdSignals.
func LoadConventionalSignals(repo, since string) ([]ColdSignal, error) {
	commits, err := loadCommitMetadata(repo, since)
	if err != nil {
		return nil, err
	}
	var matched []CommitRecord
	for _, c := range commits {
		if revert, _ := matchCommit(c); revert {
			continue
		}
		if ok, _ := matchConventional(c); !ok {
			continue
		}
		c.Files = commitFiles(repo, c.SHA) // bounded: only matched commits
		matched = append(matched, c)
	}
	return ExtractConventionalCommits(matched), nil
}
