// SPDX-License-Identifier: Apache-2.0

package skillimport

import (
	"regexp"
	"sort"
	"strings"
)

const (
	defaultStatus     = "candidate"
	defaultConfidence = "medium"
)

var (
	mustKeywords = []string{
		"must", "should", "always", "before", "do not proceed",
		"completion criterion", "done when", "run", "read", "confirm", "verify",
	}
	forbiddenKeywords = []string{
		"do not", "don't", "never", "no amount", "wrong bug = wrong fix", "not implementation details",
	}
	mustRejectKeywords = []string{
		"do not", "don't", "never", "no amount", "wrong bug = wrong fix",
	}
	whenPhrases = []string{
		"use when", "use if", "a discipline for", "test-driven development", "plan a huge chunk of work",
	}
	requiredEvidenceKeywords = []string{
		"paste the invocation", "output", "captured", "evidence", "red-capable command",
	}
	mustPriorityKeywords = []string{
		"red-capable command",
		"actual bug code path",
		"no phase 2",
		"red-capable",
		"do not proceed",
		"tests verify behavior through public interfaces",
		"before writing any test",
		"write the failing test first",
		"one seam, one test",
		"completion criterion",
		"done when",
	}
	nonSlugRE = regexp.MustCompile(`[^a-z0-9_]+`)
	slugSepRE = regexp.MustCompile(`_+`)
	linkRE    = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	boldRE    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	italicRE  = regexp.MustCompile(`_([^_]+)_`)
	codeRE    = regexp.MustCompile("`([^`]+)`")
	numberRE  = regexp.MustCompile(`^\d+\.\s+`)
)

func ExtractCandidates(skills []Skill, opts ImportOptions) []SkillCandidate {
	candidates := make([]SkillCandidate, 0, len(skills))
	for _, skill := range skills {
		sourcePath := strings.TrimSpace(skill.SourcePath)
		candidate := SkillCandidate{
			ID:          skillCandidateID(skill),
			Class:       string(CandidateImplementationPattern),
			Label:       "Skill: " + skill.Name,
			Status:      defaultStatus,
			SourceSkill: skill.Name,
			SourcePath:  sourcePath,
			Confidence:  confidenceOrDefault(opts.DefaultConfidence),
			Rationale:   rationale(skill),
			WhenToUse:   extractWhenToUse(skill),
			MustFollow: mergeLimited(12,
				extractMustFollow(skill.Body),
				evidenceAsMustFollow(extractRequiredEvidence(skill.Body)),
			),
			RequiredEvidence:   extractRequiredEvidence(skill.Body),
			ForbiddenShortcuts: extractForbiddenShortcuts(skill.Body),
			ReferenceFiles: []ReferenceFile{
				{Path: sourcePath, Role: "source_skill"},
			},
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})
	return candidates
}

func skillCandidateID(skill Skill) string {
	category := slug(skill.Category)
	if category == "" {
		category = "general"
	}
	return "imported.skill." + category + "." + slug(skill.Name)
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "_")
	s = nonSlugRE.ReplaceAllString(s, "_")
	s = slugSepRE.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

func confidenceOrDefault(confidence string) string {
	confidence = strings.TrimSpace(confidence)
	if confidence == "" {
		return defaultConfidence
	}
	return confidence
}

func rationale(skill Skill) string {
	return "Imported from an external agent skill as a reviewable candidate.\n" +
		"This candidate is procedural guidance, not live authority.\n" +
		"It must be reviewed before promotion.\n" +
		"Source skill: " + skill.Name + "\n" +
		"Source path: " + skill.SourcePath
}

func extractWhenToUse(skill Skill) []string {
	var out []string
	out = appendUniqueLimited(out, skill.Description, 4)
	for _, para := range markdownParagraphs(skill.Body) {
		lower := strings.ToLower(para)
		for _, phrase := range whenPhrases {
			if strings.Contains(lower, phrase) {
				out = appendUniqueLimited(out, normalizeMarkdown(para), 4)
				break
			}
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func extractMustFollow(body string) []string {
	var out []string
	for _, line := range markdownLines(body) {
		clean := normalizeMarkdown(line)
		if clean == "" || isHeading(clean) {
			continue
		}
		lower := strings.ToLower(clean)
		if isBullet(line) || containsAny(lower, mustKeywords) || looksLikeNoNoRule(lower) || looksLikeBehaviorRule(lower) {
			if !containsAny(lower, mustRejectKeywords) {
				out = appendUniqueLimited(out, clean, 200)
			}
		}
	}
	return prioritizeLimited(out, mustPriorityKeywords, 12)
}

func extractForbiddenShortcuts(body string) []string {
	var out []string
	inForbiddenSection := false
	for _, line := range markdownLines(body) {
		clean := normalizeMarkdown(line)
		if clean == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			heading := strings.ToLower(strings.Trim(strings.TrimSpace(line), "# "))
			inForbiddenSection = containsAny(heading, []string{
				"anti-pattern", "do not", "never", "forbidden", "when you genuinely cannot",
			})
			continue
		}
		lower := strings.ToLower(clean)
		if inForbiddenSection || containsAny(lower, forbiddenKeywords) {
			out = appendUniqueLimited(out, clean, 10)
		}
		if len(out) >= 10 {
			break
		}
	}
	return out
}

func extractRequiredEvidence(body string) []string {
	var out []string
	for _, line := range markdownLines(body) {
		clean := normalizeMarkdown(line)
		if clean == "" || isHeading(clean) {
			continue
		}
		lower := strings.ToLower(clean)
		if containsAny(lower, requiredEvidenceKeywords) {
			out = appendUniqueLimited(out, clean, 4)
		}
		if len(out) >= 4 {
			break
		}
	}
	return out
}

func evidenceAsMustFollow(evidence []string) []string {
	out := make([]string, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, "Evidence required: "+item)
	}
	return out
}

func markdownLines(body string) []string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.Split(body, "\n")
}

func markdownParagraphs(body string) []string {
	var paragraphs []string
	var current []string
	flush := func() {
		if len(current) == 0 {
			return
		}
		paragraphs = append(paragraphs, strings.Join(current, " "))
		current = nil
	}
	for _, line := range markdownLines(body) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush()
			continue
		}
		if strings.HasPrefix(trimmed, "#") || isBullet(trimmed) {
			flush()
			continue
		}
		current = append(current, trimmed)
	}
	flush()
	return paragraphs
}

func isBullet(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "- ") ||
		strings.HasPrefix(trimmed, "* ") ||
		strings.HasPrefix(trimmed, "- [ ]") ||
		strings.HasPrefix(trimmed, "* [ ]") ||
		numberRE.MatchString(trimmed)
}

func isHeading(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

func normalizeMarkdown(line string) string {
	s := strings.TrimSpace(line)
	s = numberRE.ReplaceAllString(s, "")
	s = strings.TrimPrefix(s, "- [ ] ")
	s = strings.TrimPrefix(s, "* [ ] ")
	s = strings.TrimPrefix(s, "- ")
	s = strings.TrimPrefix(s, "* ")
	s = strings.TrimSpace(s)
	s = linkRE.ReplaceAllString(s, "$1")
	s = boldRE.ReplaceAllString(s, "$1")
	s = italicRE.ReplaceAllString(s, "$1")
	s = codeRE.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "\\", "")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	return trimRunes(s, 260)
}

func containsAny(s string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

func looksLikeNoNoRule(s string) bool {
	return strings.HasPrefix(s, "no ") && strings.Contains(s, ", no ")
}

func looksLikeBehaviorRule(s string) bool {
	return strings.Contains(s, "verify behavior through public interfaces") ||
		strings.Contains(s, "write the failing test first") ||
		strings.Contains(s, "one seam, one test") ||
		strings.Contains(s, "actual bug code path") ||
		strings.Contains(s, "red-capable")
}

func appendUniqueLimited(out []string, value string, limit int) []string {
	value = normalizeMarkdown(value)
	if value == "" {
		return out
	}
	for _, existing := range out {
		if strings.EqualFold(existing, value) {
			return out
		}
	}
	if len(out) >= limit {
		return out
	}
	return append(out, value)
}

func mergeLimited(limit int, groups ...[]string) []string {
	var out []string
	for _, group := range groups {
		for _, item := range group {
			out = appendUniqueLimited(out, item, limit)
			if len(out) >= limit {
				return out
			}
		}
	}
	return out
}

func prioritizeLimited(items []string, priorityKeywords []string, limit int) []string {
	var out []string
	used := map[int]bool{}
	for _, keyword := range priorityKeywords {
		for i, item := range items {
			if used[i] {
				continue
			}
			if strings.Contains(strings.ToLower(item), keyword) {
				out = appendUniqueLimited(out, item, limit)
				used[i] = true
				if len(out) >= limit {
					return out
				}
			}
		}
	}
	for i, item := range items {
		if used[i] {
			continue
		}
		out = appendUniqueLimited(out, item, limit)
		if len(out) >= limit {
			return out
		}
	}
	return out
}

func trimRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return strings.TrimSpace(string(runes[:limit]))
}
