// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"sort"
	"strings"
)

// invariant_from_test.go infers Invariant CANDIDATES from tests whose NAME
// encodes a rule — the "tests are here to protect invariants" layer.
//
// The discipline is conservative on purpose. A test is only lifted to a
// candidate invariant when its name carries RULE language (must/never/rejects/
// idempotent/isolation/roundtrip/nopanic/regression/…). An example-based test
// (TestAddReturnsSum) is NOT an invariant — it proves a behavior, not a law — so
// it stays a plain required_test and is skipped here. This keeps Sensei from
// manufacturing invariants it cannot justify (its own contract-first rule).
//
// Every candidate carries the test that names it as its required_test: the proof
// is built in. status: candidate — nothing is promoted.

type invariantCandidateProtects struct {
	Files []string `yaml:"files,omitempty"`
}

type invariantCandidate struct {
	ID            string                    `yaml:"id"`
	Title         string                    `yaml:"title"`
	Severity      string                    `yaml:"severity"`
	Status        string                    `yaml:"status"`
	Protects      invariantCandidateProtects `yaml:"protects,omitempty"`
	RequiredTests []string                  `yaml:"required_tests"`
}

type invariantCandidateDoc struct {
	Invariants []invariantCandidate `yaml:"invariants"`
}

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

// extractInvariantCandidates lifts rule-signaling tests to candidate invariants.
// It reuses the tests bootstrap already discovered (id = "<path>:TestName"),
// so it does no extra filesystem walk.
func extractInvariantCandidates(tests []bootstrapTest) []invariantCandidate {
	seen := map[string]bool{}
	var out []invariantCandidate
	for _, t := range tests {
		file, name := splitTestID(t.ID)
		if name == "" {
			continue // file-level test entry (non-Go); no function name to read
		}
		words := splitCamel(strings.TrimPrefix(name, "Test"))
		if !anyRuleToken(words) {
			continue
		}
		id := "invariant.candidate." + invariantSlug(name)
		if seen[id] {
			continue
		}
		seen[id] = true
		inv := invariantCandidate{
			ID:            id,
			Title:         humanizeWords(words),
			Severity:      "warning",
			Status:        "candidate",
			RequiredTests: []string{t.ID},
		}
		if src := testFileToSource(file); src != "" {
			inv.Protects.Files = []string{src}
		}
		out = append(out, inv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// splitTestID splits "path/x_test.go:TestFoo" into ("path/x_test.go", "TestFoo").
// Returns name="" when there is no ":Test..." suffix.
func splitTestID(id string) (file, name string) {
	i := strings.LastIndex(id, ":")
	if i < 0 {
		return id, ""
	}
	return id[:i], id[i+1:]
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

func anyRuleToken(words []string) bool {
	for i, w := range words {
		if ruleTokens[w] {
			return true
		}
		// Negated modal: "...should not...", "...does not...", "...can not...".
		// Only a modal + "not" counts as a prohibition; "not" after a noun
		// ("NotFound") is description, not a rule.
		if w == "not" && i > 0 && modalWords[words[i-1]] {
			return true
		}
	}
	return false
}

func humanizeWords(words []string) string {
	if len(words) == 0 {
		return "unnamed invariant"
	}
	joined := strings.Join(words, " ")
	return strings.ToUpper(joined[:1]) + joined[1:]
}

func invariantSlug(name string) string {
	words := splitCamel(strings.TrimPrefix(name, "Test"))
	return strings.Join(words, "_")
}

// testFileToSource maps a test file path to its likely non-test sibling so the
// candidate invariant points at the code it guards. Best-effort; returns "" when
// the mapping is not obvious.
func testFileToSource(file string) string {
	switch {
	case strings.HasSuffix(file, "_test.go"):
		return strings.TrimSuffix(file, "_test.go") + ".go"
	case strings.HasSuffix(file, ".test.ts"):
		return strings.TrimSuffix(file, ".test.ts") + ".ts"
	case strings.HasSuffix(file, ".spec.ts"):
		return strings.TrimSuffix(file, ".spec.ts") + ".ts"
	case strings.HasSuffix(file, "_test.py"):
		return strings.TrimSuffix(file, "_test.py") + ".py"
	}
	return ""
}
