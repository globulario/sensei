// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor"
)

// GROUNDING — the verification phase between a cited draft and acceptance.
//
// The citation cage (citation_check.go) proves a citation RESOLVES (file exists,
// line in range, commit resolves). It does NOT prove the cited line holds the
// claimed invariant, nor whether the evidence is a test, a landed commit, or a
// review-only suggestion. Grounding closes that gap MECHANICALLY — git + file
// reads + a shallow identifier match. No LLM, no key.
//
// Design: docs/coldsource-grounding-design.md (§2–§5). This file implements the
// mechanical core only — NOT the §6 confidence-gated auto-decision (which is
// opt-in and LLM-backed) and NOT meta.* mapping.

// ProvenanceTier ranks how strongly a citation — or a whole candidate — is
// grounded in the target tree. Higher is stronger. The strict order is:
//
//	test_encoded > landed_commit > review_suggestion > unresolved
//
// "No test" must not mean "no candidate": a landed commit grounds a real
// invariant. Test evidence is strongest, not mandatory.
type ProvenanceTier int

const (
	// TierUnresolved: the citation does not resolve (missing file, out-of-range
	// line, missing commit, unknown form) OR the cited file is present but holds
	// NONE of the claimed symbols — the line exists, but not the claim.
	TierUnresolved ProvenanceTier = iota
	// TierReviewSuggestion: the only evidence is a PR discussion (a proposal), or
	// a commit that does not touch any cited file. A lead, not an invariant.
	TierReviewSuggestion
	// TierLandedCommit: the invariant is present in shipped (non-test) code, or a
	// commit that landed and touches the cited file.
	TierLandedCommit
	// TierTestEncoded: a test asserts the invariant (cited file is a test and the
	// claimed symbol resolves there). Gold.
	TierTestEncoded
)

// String renders the tier as the stable token used in reports and (future)
// candidate provenance.
func (t ProvenanceTier) String() string {
	switch t {
	case TierTestEncoded:
		return "test_encoded"
	case TierLandedCommit:
		return "landed_commit"
	case TierReviewSuggestion:
		return "review_suggestion"
	default:
		return "unresolved"
	}
}

// CitationGrounding is the per-citation verdict.
type CitationGrounding struct {
	Citation string
	Tier     ProvenanceTier
	Note     string // how it resolved, or why it was demoted
}

// Grounding is a candidate's overall verdict: the strongest tier among its
// citations, plus per-citation detail and a symbol-mismatch flag for the human.
type Grounding struct {
	Overall        ProvenanceTier
	Citations      []CitationGrounding
	Drifted        bool // a file citation's symbol was found, but far from the cited line
	SymbolMismatch bool // a resolving file citation held NONE of the claimed symbols
}

// commitPathVerifier is an OPTIONAL capability on top of GitVerifier: it reports
// whether a commit touched a given path. Grounding uses it to stop a
// real-but-unrelated SHA from laundering a claim. A verifier that does not
// implement it simply skips that cross-check (benefit of the doubt).
type commitPathVerifier interface {
	CommitTouchesPath(sha, path string) bool
}

// CommitTouchesPath implements commitPathVerifier for the exec-backed verifier.
func (g execGitVerifier) CommitTouchesPath(sha, path string) bool {
	if strings.TrimSpace(sha) == "" || strings.TrimSpace(path) == "" {
		return false
	}
	out, err := exec.Command("git", "-C", g.repo, "show", "--name-only", "--format=", sha+"^{commit}").Output()
	if err != nil {
		return false
	}
	want := filepath.ToSlash(filepath.Clean(path))
	for _, line := range strings.Split(string(out), "\n") {
		if filepath.ToSlash(filepath.Clean(strings.TrimSpace(line))) == want {
			return true
		}
	}
	return false
}

// driftWindow is how far a found symbol may sit from the cited line before the
// citation is flagged as drifted (still grounded, but the line moved).
const driftWindow = 25

// GroundCandidate classifies each of a candidate's citations against the target
// tree and returns the candidate's overall provenance tier. Pure except for the
// injected GitVerifier and repo file reads; deterministic for a fixed tree.
func GroundCandidate(p *extractor.PromotionProposal, repoRoot string, git GitVerifier) Grounding {
	symbols := claimedSymbols(p.Reason)
	cpv, _ := git.(commitPathVerifier)
	filePaths := citedFilePaths(p.SourcePaths)

	g := Grounding{Overall: TierUnresolved}
	for _, c := range p.SourcePaths {
		cg := groundOne(c, repoRoot, git, cpv, symbols, filePaths)
		switch cg.Note {
		case "drift":
			g.Drifted = true
		case "symbol_absent":
			g.SymbolMismatch = true
		}
		g.Citations = append(g.Citations, cg)
		if cg.Tier > g.Overall {
			g.Overall = cg.Tier
		}
	}
	return g
}

func groundOne(c, repoRoot string, git GitVerifier, cpv commitPathVerifier, symbols, filePaths []string) CitationGrounding {
	switch {
	case strings.HasPrefix(c, "file:"):
		path, line := parseFileCitation(strings.TrimPrefix(c, "file:"))
		abs := safeJoin(repoRoot, path)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return CitationGrounding{c, TierUnresolved, "file does not exist"}
		}
		if line > 0 && line > countLines(abs) {
			return CitationGrounding{c, TierUnresolved, "line out of range"}
		}
		// Symbol re-resolution: if the draft names code-like symbols, at least one
		// must appear in the cited file. None present → the line exists but the
		// claim does not live here (the k8s validation.go / churnCancels miss).
		note := "resolved"
		if len(symbols) > 0 {
			found, at := fileContainsAnySymbol(abs, symbols)
			if !found {
				return CitationGrounding{c, TierUnresolved, "symbol_absent"}
			}
			if line > 0 && absInt(at-line) > driftWindow {
				note = "drift"
			}
		}
		if isTestPath(path) {
			return CitationGrounding{c, TierTestEncoded, note}
		}
		return CitationGrounding{c, TierLandedCommit, note}

	case strings.HasPrefix(c, "commit:"):
		sha := strings.TrimPrefix(c, "commit:")
		if git == nil || !git.CommitExists(sha) {
			return CitationGrounding{c, TierUnresolved, "commit does not resolve"}
		}
		// A real SHA that touches none of the cited files cannot launder a claim.
		if cpv != nil && len(filePaths) > 0 {
			touches := false
			for _, fp := range filePaths {
				if cpv.CommitTouchesPath(sha, fp) {
					touches = true
					break
				}
			}
			if !touches {
				return CitationGrounding{c, TierReviewSuggestion, "commit unrelated to cited files"}
			}
		}
		return CitationGrounding{c, TierLandedCommit, "resolved"}

	case strings.HasPrefix(c, "pr:"):
		// A PR comment is a proposal, never sufficient on its own. It caps a
		// candidate at review_suggestion unless stronger evidence outranks it.
		return CitationGrounding{c, TierReviewSuggestion, "review discussion (preserved)"}

	default:
		return CitationGrounding{c, TierUnresolved, "unknown citation form"}
	}
}

// parseFileCitation splits "<path>[:line]" — a trailing :<int> is the line.
func parseFileCitation(rest string) (path string, line int) {
	path = rest
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		if n, err := strconv.Atoi(rest[i+1:]); err == nil {
			path, line = rest[:i], n
		}
	}
	return path, line
}

// citedFilePaths returns the repo-relative paths of the file: citations.
func citedFilePaths(citations []string) []string {
	var out []string
	for _, c := range citations {
		if strings.HasPrefix(c, "file:") {
			p, _ := parseFileCitation(strings.TrimPrefix(c, "file:"))
			out = append(out, p)
		}
	}
	return out
}

// fileContainsAnySymbol reports whether any claimed symbol appears in the file,
// and the 1-based line of the first match (for drift detection).
func fileContainsAnySymbol(abs string, symbols []string) (bool, int) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return false, 0
	}
	content := string(data)
	for _, s := range symbols {
		if idx := strings.Index(content, s); idx >= 0 {
			return true, 1 + strings.Count(content[:idx], "\n")
		}
	}
	return false, 0
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// claimedSymbols extracts code-like identifiers from a draft's prose: tokens
// that are mixed-case with an interior capital (camelCase / PascalCase, e.g.
// makeMounts, WhenDeleted, PodStatusPatchCall, dropHTTPProbeProtocol). English
// prose words and all-caps acronyms (HTTPS, RBAC) are excluded, so this is a
// low-false-positive signal of "the symbol this rule is about". Used ONLY to
// demote a citation when the cited file holds none of them — never to invent
// grounding, so a draft that names no symbol is left to its citation tiers.
func claimedSymbols(reason string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range identRe.FindAllString(reason, -1) {
		if !looksLikeSymbol(tok) || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

func looksLikeSymbol(tok string) bool {
	if len(tok) < 4 {
		return false
	}
	hasInteriorUpper, hasLower := false, false
	for i, r := range tok {
		if r >= 'a' && r <= 'z' {
			hasLower = true
		}
		if i > 0 && r >= 'A' && r <= 'Z' {
			hasInteriorUpper = true
		}
	}
	return hasInteriorUpper && hasLower
}

// isTestPath reports whether a repo-relative path is a test, across the
// languages cold-source has been run on (Go, TS/JS, Python, Rust) plus a
// directory convention.
func isTestPath(p string) bool {
	base := strings.ToLower(filepath.Base(p))
	switch {
	case strings.HasSuffix(base, "_test.go"): // Go
		return true
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"),
		strings.HasSuffix(base, ".test.js"), strings.HasSuffix(base, ".spec.ts"),
		strings.HasSuffix(base, ".spec.js"): // TS/JS
		return true
	case strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"),
		strings.HasSuffix(base, "_test.py"): // Python
		return true
	case strings.HasSuffix(base, "_test.rs"): // Rust (file convention)
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(strings.ToLower(p)), "/") {
		switch seg {
		case "test", "tests", "__tests__":
			return true
		}
	}
	return false
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
