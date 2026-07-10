// SPDX-License-Identifier: Apache-2.0

// Package coldsource drafts awareness candidates from the "cold" day-0 signals a
// repo already carries — revert/regression commits, conventional commits, and PR
// review threads — without any hand-authored YAML and without runtime
// outcome/incident history. It is how AWG finds knowledge a repo already encodes.
//
// Safety model (always on):
//   - it NEVER writes active knowledge and NEVER bypasses the promotion gate;
//   - every emitted node carries status:candidate under docs/awareness/candidates/;
//   - a draft must cite its evidence and survives a citation check against the
//     live tree, so it can never widen its own allowed-citation set.
//
// Pipeline: deterministic extractors -> ColdSignal -> Triangulate (>=2 distinct
// source types) -> Drafter (behind an interface, must cite) -> citation check ->
// bounded emit + scoring report.
package coldsource

import (
	"fmt"
	"path"
	"strings"
)

// SourceType identifies which deterministic extractor produced a ColdSignal.
// Two extractors only, by design, for the first cut.
type SourceType string

const (
	// SourcePRReview is a human review comment stating a rule ("must", "don't").
	SourcePRReview SourceType = "pr_review"
	// SourceRevertCommit is a revert or regression-marked commit.
	SourceRevertCommit SourceType = "revert_commit"
	// SourceConventionalCommit is a conventional-commit fix/perf/refactor. It is
	// the WEAKEST signal: a routine change, not an explicit scar. It only
	// corroborates when paired with a review signal (see channel()).
	SourceConventionalCommit SourceType = "conventional_commit"
	// SourceReviewThread is a high-engagement review thread on one component:
	// repeated rule-language comments indicating architectural friction. A
	// review-channel signal that catches dense threads strict per-comment
	// matching misses.
	SourceReviewThread SourceType = "review_thread"
)

// channel maps a source type to its evidence CHANNEL — the git commit history
// or the PR review surface. Triangulation requires corroboration across
// channels (one commit signal AND one review signal), not just two signals from
// the same channel. This keeps two routine commits (or two comments on one
// thread) from triangulating on their own, while letting a weak
// conventional-commit pair with a human review signal. For the original two
// source types {revert_commit, pr_review} this is identical to the old
// "2 distinct source types" rule, so existing behaviour is preserved.
func channel(t SourceType) string {
	switch t {
	case SourceRevertCommit, SourceConventionalCommit:
		return "commit"
	case SourcePRReview, SourceReviewThread:
		return "review"
	default:
		return string(t)
	}
}

// ColdSignal is one piece of cold (day-0) evidence with strict provenance.
// Every locator that says WHERE the evidence lives is preserved so a human
// reviewer — and the deterministic citation check — can verify it without
// re-reading the repo. At least one of {FilePath, CommitSHA, PRID} must be set.
type ColdSignal struct {
	SourceType SourceType
	ThemeKey   string // shared key space across extractors (component/dir path)

	// ProposedClass is a non-binding hint about which candidate class the
	// signal might support (extractor.CandidateInvariant / ...). The drafter
	// makes the final call.
	ProposedClass string

	// Provenance.
	FilePath  string // repo-relative path, if anchored to a file
	Line      int    // 1-based line; 0 = unknown
	CommitSHA string // commit sha, if anchored to a commit
	PRID      string // pull-request id/number, if from a PR
	CommentID string // review-comment id, if from a PR review

	MatchedText string // the exact phrase that matched — the raw evidence
}

// Citations returns the canonical citation strings this signal supports. These
// are the ONLY strings a drafted candidate may cite for a bundle. Format
// (parsed verbatim by citation_check):
//
//	file:<path>            file:<path>:<line>
//	commit:<sha>
//	pr:<prid>              pr:<prid>:<commentid>
func (s ColdSignal) Citations() []string {
	var cs []string
	if s.FilePath != "" {
		if s.Line > 0 {
			cs = append(cs, fmt.Sprintf("file:%s:%d", s.FilePath, s.Line))
		} else {
			cs = append(cs, "file:"+s.FilePath)
		}
	}
	if s.CommitSHA != "" {
		cs = append(cs, "commit:"+s.CommitSHA)
	}
	if s.PRID != "" {
		if s.CommentID != "" {
			cs = append(cs, fmt.Sprintf("pr:%s:%s", s.PRID, s.CommentID))
		} else {
			cs = append(cs, "pr:"+s.PRID)
		}
	}
	return cs
}

// themeFromPath maps a repo-relative file path to a shared theme key at
// FILE-CONCEPT granularity: the directory PLUS the file's concept stem.
//
// Why not the directory alone: a broad component directory (e.g.
// modules/caddyhttp with ~20 unrelated source files) collapses every revert
// and review comment under it into one giant bundle. That "theme-conflation"
// buries the real per-concept rules and produces noisy mega-candidates — the
// dominant failure mode observed on the Caddy benchmark. Keying on the file
// concept splits that directory into per-file themes so distinct rules stay
// distinct, while genuinely cross-file scars still triangulate when they touch
// the same file (a fix touches foo.go; a reviewer comments on foo.go).
//
// Test files are normalized to their sibling's concept (foo_test.go -> foo) so
// a fix commit (foo.go) and a review comment on its test (foo_test.go) still
// land on one theme. Returns "" for empty input.
func themeFromPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	clean := path.Clean(p)
	dir := strings.Trim(path.Dir(clean), "/")
	stem := conceptStem(strings.TrimSuffix(path.Base(clean), path.Ext(clean)))

	var key string
	switch dir {
	case ".", "/", "":
		// Top-level file: theme on the concept stem alone.
		key = stem
	default:
		key = dir + "/" + stem
	}
	return strings.ToLower(strings.ReplaceAll(key, "/", "."))
}

// conceptStem strips test/generated affixes so a source file and its test or
// generated sibling share one concept theme. Deterministic; never empty unless
// the input was. Falls back to the original stem if stripping would empty it.
//
// Suffix forms cover Go (foo_test) and the TS/JS convention where the outer
// extension is already removed by the caller (foo.test.ts -> "foo.test" ->
// "foo"; likewise .spec / .test.tsx / .spec.tsx). The test_ PREFIX covers the
// Python convention (test_foo.py -> "foo"), which suffix stripping misses.
func conceptStem(stem string) string {
	s := stem
	for _, suf := range []string{"_test", ".test", ".spec", "_spec", "_gen", "_generated", ".gen"} {
		if t := strings.TrimSuffix(s, suf); t != "" {
			s = t
		}
	}
	for _, pre := range []string{"test_", "test."} {
		if t := strings.TrimPrefix(s, pre); t != "" && t != s {
			s = t
		}
	}
	return s
}
