// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"regexp"
	"sort"

	"github.com/globulario/sensei/golang/extractor"
)

// minThreadComments is the density bar: a component must accumulate at least
// this many review comments before a high-engagement thread signal fires. One
// or two comments is normal review; sustained discussion on one component is the
// "architectural friction" we want to surface.
const minThreadComments = 3

// reArchFriction broadens the rule lexicon beyond the strict per-comment
// matchers (reForbid/reFail/reInvariant) with friction terms that mark
// architecture debate. Used ONLY for the thread signal, which already requires
// density — so these softer words cannot fire on a lone stylistic nit.
var reArchFriction = regexp.MustCompile(`(?i)\b(unsafe|real path|invariant|compat(ibility|ible)?|backwards?[\s-]?compat|race|leak|breaks?)\b`)

// hasRuleLanguage reports whether a comment body carries rule/architecture
// language, using the strict per-comment matchers plus the thread-only friction
// lexicon.
func hasRuleLanguage(body string) bool {
	if ok, _ := classifyComment(body); ok {
		return true
	}
	return reArchFriction.MatchString(body)
}

// ExtractReviewThreads surfaces HIGH-ENGAGEMENT review threads as review-channel
// ColdSignals: a component (file-concept theme) with sustained discussion
// (>= minThreadComments comments) where at least one comment carries rule
// language. It emits exactly ONE signal per qualifying theme — not one per
// comment — so a hot thread is a single corroborating data point, not a flood.
//
// Conservative by construction: density AND rule language are both required, so
// neither a single rule comment (already covered by ExtractPRReviews) nor a busy
// but purely stylistic thread qualifies. Excluded surfaces never form a theme.
func ExtractReviewThreads(comments []ReviewComment) []ColdSignal {
	type theme struct {
		count    int
		rep      ReviewComment // first rule-language comment — the anchor/citation
		repClass string
		hasRep   bool
	}
	byTheme := map[string]*theme{}
	order := []string{}
	for _, c := range comments {
		key := surfaceTheme(c.Path)
		if key == "" {
			continue // unanchored or excluded surface — cannot form a thread theme
		}
		t, ok := byTheme[key]
		if !ok {
			t = &theme{}
			byTheme[key] = t
			order = append(order, key)
		}
		t.count++
		if t.hasRep {
			continue
		}
		// The representative is the FIRST comment carrying rule language, so the
		// emitted signal always has a real file/PR/comment citation. Prefer a
		// strict-matched class; fall back to invariant for friction-only matches.
		if strict, class := classifyComment(c.Body); strict {
			t.rep, t.repClass, t.hasRep = c, class, true
		} else if hasRuleLanguage(c.Body) {
			t.rep, t.repClass, t.hasRep = c, extractor.CandidateInvariant, true
		}
	}

	sort.Strings(order)
	var out []ColdSignal
	for _, key := range order {
		t := byTheme[key]
		// Require BOTH density and at least one rule-language comment (hasRep).
		if t.count < minThreadComments || !t.hasRep {
			continue
		}
		out = append(out, ColdSignal{
			SourceType:    SourceReviewThread,
			ThemeKey:      key,
			ProposedClass: t.repClass,
			FilePath:      t.rep.Path,
			Line:          t.rep.Line,
			PRID:          t.rep.PRID,
			CommentID:     t.rep.CommentID,
			MatchedText:   squash(t.rep.Body),
		})
	}
	return out
}
