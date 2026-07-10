// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// ReviewComment is one PR review comment, decoupled from the GitHub API so the
// matcher is unit-testable.
type ReviewComment struct {
	PRID      string
	CommentID string
	Path      string // repo-relative file the comment is anchored to
	Line      int    // line in the file (0 if unknown)
	Body      string
}

// Rule-phrase matchers, checked in order. The first match wins and assigns the
// candidate-class hint. A comment matching none is not a rule statement and is
// skipped — most review comments are not rules.
var (
	reForbid    = regexp.MustCompile(`(?i)\b(do not|don't|never|must not|mustn't|shall not|should not|shouldn't|avoid|don’t)\b`)
	reFail      = regexp.MustCompile(`(?i)\b(breaks|broke|broken|regression|races?|deadlocks?|leaks?|corrupt(s|ed)?)\b`)
	reInvariant = regexp.MustCompile(`(?i)\b(must|always|has to|have to|needs to|required|ensure|invariant|guarantee)\b`)
)

// classifyComment returns (matched, classHint). Negative rules win first so
// "must not" is a ForbiddenFix, not an Invariant.
func classifyComment(body string) (bool, string) {
	switch {
	case reForbid.MatchString(body):
		return true, extractor.CandidateForbiddenFix
	case reFail.MatchString(body):
		return true, extractor.CandidateFailureMode
	case reInvariant.MatchString(body):
		return true, extractor.CandidateInvariant
	default:
		return false, ""
	}
}

// ExtractPRReviews turns rule-stating review comments into ColdSignals, themed
// by the file the comment is anchored to.
func ExtractPRReviews(comments []ReviewComment) []ColdSignal {
	var out []ColdSignal
	for _, c := range comments {
		matched, class := classifyComment(c.Body)
		if !matched {
			continue
		}
		// surfaceTheme withholds a file theme for dependency/build/vendor/
		// generated/example surfaces, so a review comment on (e.g.) go.mod can
		// never triangulate into a standalone architecture-rule candidate.
		theme := surfaceTheme(c.Path)
		if theme == "" {
			// Unanchored or excluded-surface comments cannot triangulate by
			// file; theme on the PR (single-source → held back).
			theme = "pr." + c.PRID
		}
		out = append(out, ColdSignal{
			SourceType:    SourcePRReview,
			ThemeKey:      theme,
			ProposedClass: class,
			FilePath:      c.Path,
			Line:          c.Line,
			PRID:          c.PRID,
			CommentID:     c.CommentID,
			MatchedText:   squash(c.Body),
		})
	}
	return out
}

// LoadPRComments reads a JSON array of ReviewComment (for offline runs and
// tests). Exposed so callers can run BOTH the per-comment (ExtractPRReviews) and
// the thread-density (ExtractReviewThreads) extractors over the same comments.
func LoadPRComments(path string) ([]ReviewComment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var comments []ReviewComment
	if err := json.Unmarshal(data, &comments); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return comments, nil
}

// LoadPRReviewSignalsFromFile reads a JSON array of ReviewComment (for offline
// runs and tests). This is the deterministic path the experiment can replay.
func LoadPRReviewSignalsFromFile(path string) ([]ColdSignal, error) {
	comments, err := LoadPRComments(path)
	if err != nil {
		return nil, err
	}
	return ExtractPRReviews(comments), nil
}

// ghReviewComment mirrors the subset of the GitHub pulls/comments API we read.
type ghReviewComment struct {
	ID             int64  `json:"id"`
	Path           string `json:"path"`
	Line           int    `json:"line"`
	OriginalLine   int    `json:"original_line"`
	Body           string `json:"body"`
	PullRequestURL string `json:"pull_request_url"`
}

// LoadPRCommentsFromSlug fetches review comments via `gh api` for repoSlug
// ("owner/name") and returns them raw. Exposed so callers can run both the
// per-comment and thread-density extractors over the same comments.
func LoadPRCommentsFromSlug(repoSlug string) ([]ReviewComment, error) {
	if repoSlug == "" {
		return nil, fmt.Errorf("repo slug (owner/name) required for PR review extraction")
	}
	cmd := exec.Command("gh", "api", "--paginate",
		fmt.Sprintf("repos/%s/pulls/comments?per_page=100", repoSlug))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api pulls/comments: %w", err)
	}
	var raw []ghReviewComment
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse gh response: %w", err)
	}
	comments := make([]ReviewComment, 0, len(raw))
	for _, r := range raw {
		line := r.Line
		if line == 0 {
			line = r.OriginalLine
		}
		comments = append(comments, ReviewComment{
			PRID:      prNumberFromURL(r.PullRequestURL),
			CommentID: strconv.FormatInt(r.ID, 10),
			Path:      r.Path,
			Line:      line,
			Body:      r.Body,
		})
	}
	return comments, nil
}

// LoadPRReviewSignals fetches review comments via `gh api` for repoSlug and
// extracts per-comment ColdSignals. Best-effort: if gh is unavailable or errors,
// returns the error so the caller can continue with other extractors.
func LoadPRReviewSignals(repoSlug string) ([]ColdSignal, error) {
	comments, err := LoadPRCommentsFromSlug(repoSlug)
	if err != nil {
		return nil, err
	}
	return ExtractPRReviews(comments), nil
}

func prNumberFromURL(u string) string {
	if u == "" {
		return ""
	}
	i := strings.LastIndexByte(u, '/')
	if i < 0 {
		return ""
	}
	return u[i+1:]
}

// squash collapses whitespace and trims a comment body to a citable quote.
func squash(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, 280)
}
