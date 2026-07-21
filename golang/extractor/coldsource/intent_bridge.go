// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// COLDSOURCE <-> INTENT-MINING BRIDGE — the bidirectional wiring of
// docs/intent-mining-design.md §8. Two pure, mechanical converters (no LLM):
//
//   coldsource → intent:  a scar-mined candidate (revert/fix/review) is EVIDENCE
//     that a rule exists but is NOT a stated charter. Lifted to an intent
//     candidate with ScarsImply=true and NO sources, GroundIntent classifies it:
//       - grounded  → hidden_intent     (the code/fix encodes a rule no doc explains)
//       - ungrounded→ missing_invariant (scars imply it; nothing encodes it)
//     i.e. coldsource gives the rule a candidate home; intent mining asks whether
//     the charter ever named it.
//
//   intent → coldsource:  a divergence finding (stale_intent / ambiguous_owner)
//     names the file where stated intent and encoded reality disagree — the most
//     likely place the NEXT scar will land. Emitted as finder hints coldsource
//     can weight.
//
// Both directions are dry-run data transforms. Nothing is promoted, written to a
// graph, or minted.

// ScarToIntent lifts one coldsource scar-mined candidate into an intent
// candidate. The scar's citations become EVIDENCE (file/commit); review-only
// (pr:) citations are dropped — a review of a scar is not the charter. No
// Sources are set: that absence is the point (the rule is unstated).
func ScarToIntent(id, class, theme, reason string, citations []string) IntentCandidate {
	c := IntentCandidate{
		IntentID:   "intent.from_scar." + sanitizeID(firstNonEmpty(id, theme)),
		Claim:      reason,
		Category:   scarCategory(class),
		ScarsImply: true,
		Status:     "candidate",
	}
	for _, cit := range citations {
		switch {
		case strings.HasPrefix(cit, "commit:"):
			c.Evidence.Commits = append(c.Evidence.Commits, cit)
		case strings.HasPrefix(cit, "file:"):
			if isTestPath(citationToPath(cit)) {
				c.Evidence.Tests = append(c.Evidence.Tests, cit)
			} else {
				c.Evidence.Code = append(c.Evidence.Code, cit)
			}
		case strings.HasPrefix(cit, "pr:"):
			// dropped: a PR review of a scar is evidence of pain, not the charter.
		}
	}
	return c
}

// scarCategory maps a coldsource candidate class to an intent category.
func scarCategory(class string) string {
	switch strings.ToLower(strings.TrimSuffix(strings.ToLower(class), "candidate")) {
	case "forbiddenfix", "forbidden_fix", "failuremode", "failure_mode":
		return "failure_response"
	case "invariant":
		return "api_contract"
	default:
		return ""
	}
}

// coldsourceCandidateFile is the YAML shape this bridge reads: a list of
// coldsource candidates (the cold-bootstrap candidate shape, aggregated).
type coldsourceCandidateFile struct {
	Candidates []struct {
		ID          string   `yaml:"id"`
		Class       string   `yaml:"class"`
		Theme       string   `yaml:"theme"`
		Reason      string   `yaml:"reason"`
		SourcePaths []string `yaml:"source_paths"`
	} `yaml:"candidates"`
}

// LoadColdsourceAsIntent reads a coldsource candidates YAML and lifts each into
// a scar-derived intent candidate (the coldsource → intent direction).
func LoadColdsourceAsIntent(path string) ([]IntentCandidate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f coldsourceCandidateFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	out := make([]IntentCandidate, 0, len(f.Candidates))
	for _, c := range f.Candidates {
		out = append(out, ScarToIntent(c.ID, c.Class, c.Theme, c.Reason, c.SourcePaths))
	}
	return out, nil
}

// FinderHint points coldsource at where the next scar is likely: a file where
// stated intent and encoded reality diverge.
type FinderHint struct {
	IntentID string
	File     string
	Class    IntentOutputClass
	Why      string
}

// FinderHintsFromGroundings derives finder hints from divergence findings
// (stale_intent, ambiguous_owner) — the intent → coldsource direction. It pulls
// the cited files out of each finding's anchors. Deterministic, deduplicated.
func FinderHintsFromGroundings(gs []IntentGrounding) []FinderHint {
	seen := map[string]bool{}
	var out []FinderHint
	for _, g := range gs {
		if g.OutputClass != StaleIntent && g.OutputClass != AmbiguousOwner {
			continue
		}
		why := "stated intent diverges from encoded reality — likely next-scar site"
		if g.OutputClass == AmbiguousOwner {
			why = "two sources imply different owners — contested truth, likely next-scar site"
		}
		for _, a := range g.Anchors {
			if !strings.HasPrefix(a.Citation, "file:") {
				continue
			}
			file := citationToPath(a.Citation)
			key := string(g.OutputClass) + "|" + file
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, FinderHint{IntentID: g.IntentID, File: file, Class: g.OutputClass, Why: why})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].IntentID < out[j].IntentID
	})
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return "unnamed"
}
