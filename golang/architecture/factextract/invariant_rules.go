// SPDX-License-Identifier: AGPL-3.0-only

package factextract

import "strings"

// invariant_rules.go is the shared rule-signal vocabulary for recognizing when a
// test NAME encodes an architectural law rather than an example. It is the single
// home for that judgement, consumed by the invariant extractor
// (`testAssertionFact` in cmd_extract_invariants.go): a rule-signaling test
// becomes an `asserts_architectural_rule` fact, which
// `synthesizeTestAttestedCandidates` then promotes to a medium-confidence
// candidate invariant. An example-only test stays a plain behavior fact.
//
// Keeping the vocabulary here — not inline in the extractor — means the "tests
// protect invariants" heuristic has one definition and one place to tune.

// ruleTokens are the CamelCase words in a test name that signal the test guards
// a rule (an invariant), not just an example. Curated to strong signals only —
// widening this floods the candidate queue with example tests.
var ruleTokens = map[string]bool{
	// modal / prohibition (single strong words)
	"must": true, "never": true, "always": true, "cannot": true,
	"refuses": true, "rejects": true, "forbid": true, "forbids": true,
	"forbidden": true, "denies": true,
	// property / safety laws
	"idempotent": true, "isolation": true, "isolated": true, "preserves": true,
	"consistent": true, "consistency": true, "concurrent": true, "race": true,
	"roundtrip": true, "nopanic": true, "panics": true,
	"failsclosed": true, "failclosed": true, "atomic": true,
	"overwrite": true, "leak": true, "stale": true,
	// provenance of a fixed law
	"regression": true, "repro": true, "invariant": true,
}

// modalWords precede "not" in a genuine prohibition ("should not", "does not").
// This distinguishes a rule ("ShouldNotCancel") from a description ("NotFound",
// where "not" follows a noun) — the key false-positive filter.
var modalWords = map[string]bool{
	"should": true, "must": true, "will": true, "would": true, "can": true,
	"could": true, "does": true, "do": true, "is": true, "are": true,
	"has": true, "have": true, "shall": true, "may": true,
}

// splitCamel breaks a CamelCase identifier into lowercase words. Digits attach
// to the preceding word. A trailing "/subtest" segment is dropped first.
func splitCamel(s string) []string {
	if idx := strings.IndexByte(s, '/'); idx >= 0 {
		s = s[:idx]
	}
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			flush()
		}
		if r == '_' {
			flush()
			continue
		}
		cur.WriteRune(r)
	}
	flush()
	return words
}

// anyRuleToken reports whether the words of a test name carry rule language: a
// strong single token, or a negated modal ("should not", "does not"). "not"
// after a noun ("NotFound") is description, not a rule, and is rejected.
func anyRuleToken(words []string) bool {
	for i, w := range words {
		if ruleTokens[w] {
			return true
		}
		if w == "not" && i > 0 && modalWords[words[i-1]] {
			return true
		}
	}
	return false
}

// humanizeWords joins lowercased words into a readable phrase, capitalized.
func humanizeWords(words []string) string {
	if len(words) == 0 {
		return "unnamed invariant"
	}
	joined := strings.Join(words, " ")
	return strings.ToUpper(joined[:1]) + joined[1:]
}
