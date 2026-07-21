// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"os/exec"
	"regexp"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
)

// CommitRecord is one commit's metadata, decoupled from git so the matcher is
// unit-testable.
type CommitRecord struct {
	SHA     string
	Subject string
	Body    string
	Files   []string // repo-relative paths touched
}

var (
	reRevert     = regexp.MustCompile(`(?i)^revert[\s"]|\brevert(s|ed|ing)?\b`)
	reRegression = regexp.MustCompile(`(?i)\bregression\b|\bre-?introduc|\bbroke(n)?\b|\bbreaks\b|\bINC-\d{4}-\d{4}\b`)
)

// matchCommit reports whether a commit is a revert/regression signal and the
// candidate-class hint it carries. Shared by ExtractReverts and the loader so
// file enrichment only happens for matched commits.
func matchCommit(c CommitRecord) (bool, string) {
	text := c.Subject + "\n" + c.Body
	switch {
	case reRevert.MatchString(c.Subject):
		// A reverted change is, by definition, a fix that was wrong — the
		// reverted pattern is a forbidden fix.
		return true, extractor.CandidateForbiddenFix
	case reRegression.MatchString(text):
		return true, extractor.CandidateFailureMode
	default:
		return false, ""
	}
}

// ExtractReverts turns revert/regression commits into ColdSignals — one per
// touched file so they can triangulate with file-anchored signals (PR review
// comments on the same component). A matched commit with no files yields a
// single commit-only signal (which will be held back unless something else
// shares its theme).
func ExtractReverts(commits []CommitRecord) []ColdSignal {
	var out []ColdSignal
	for _, c := range commits {
		matched, class := matchCommit(c)
		if !matched {
			continue
		}
		quote := strings.TrimSpace(firstLine(c.Subject))
		// Drop dependency/build/vendor/generated/example surfaces: they must not
		// seed standalone architecture themes (the cross-repo noise source). A
		// commit's real source files still triangulate; a commit touching ONLY
		// excluded surfaces becomes a single commit-only signal, held back.
		var targets []string
		for _, f := range c.Files {
			if isExcludedSurface(f) {
				continue
			}
			targets = append(targets, f)
		}
		if len(targets) == 0 {
			targets = []string{""}
		}
		for _, f := range targets {
			theme := themeFromPath(f)
			if theme == "" {
				theme = "commit." + shortSHA(c.SHA)
			}
			out = append(out, ColdSignal{
				SourceType:    SourceRevertCommit,
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

// LoadRevertSignals reads commits in the given range, keeps only
// revert/regression commits, enriches them with their touched files (bounded:
// only matched commits incur a git call), and extracts ColdSignals.
//
// since is a git range like "v1.0.0..HEAD" or "HEAD~200..HEAD". Empty defaults
// to the last 200 commits.
func LoadRevertSignals(repo, since string) ([]ColdSignal, error) {
	commits, err := loadCommitMetadata(repo, since)
	if err != nil {
		return nil, err
	}
	var matched []CommitRecord
	for _, c := range commits {
		if ok, _ := matchCommit(c); !ok {
			continue
		}
		c.Files = commitFiles(repo, c.SHA) // bounded: only matched commits
		matched = append(matched, c)
	}
	return ExtractReverts(matched), nil
}

// loadCommitMetadata runs one `git log` for sha/subject/body (no file lists).
func loadCommitMetadata(repo, since string) ([]CommitRecord, error) {
	rangeArg := since
	if rangeArg == "" {
		rangeArg = "HEAD~200..HEAD"
	}
	const recSep = "\x1e"
	const fldSep = "\x1f"
	cmd := exec.Command("git", "-C", repo, "log", "--no-merges",
		"--pretty=format:%H"+fldSep+"%s"+fldSep+"%b"+recSep, rangeArg)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var recs []CommitRecord
	for _, raw := range strings.Split(string(out), recSep) {
		raw = strings.TrimLeft(raw, "\n")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		parts := strings.SplitN(raw, fldSep, 3)
		if len(parts) < 2 {
			continue
		}
		rec := CommitRecord{SHA: strings.TrimSpace(parts[0]), Subject: parts[1]}
		if len(parts) == 3 {
			rec.Body = parts[2]
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

// commitFiles returns the repo-relative files a commit touched (best-effort).
func commitFiles(repo, sha string) []string {
	cmd := exec.Command("git", "-C", repo, "show", "--name-only", "--pretty=format:", sha)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			files = append(files, ln)
		}
	}
	return files
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
