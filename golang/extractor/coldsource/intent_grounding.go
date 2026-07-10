// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"sort"
	"strings"
)

// INTENT GROUNDING — the mechanical core of docs/intent-mining-design.md (§2–§6).
//
// Coldsource grounds candidate RULES mined from scars. Intent mining grounds
// candidate INTENTS proposed from a project's stated charter (docs, ADRs,
// comments, tests, commits, schemas). This file implements the MECHANICAL
// grounding only — it REUSES the coldsource grounding spine verbatim
// (groundOne / claimedSymbols / isTestPath / ProvenanceTier / GitVerifier) and
// adds:
//   - a trust-tier classifier for intent sources (design §2);
//   - a divergence check (does the code agree / stay silent / contradict the
//     stated intent — mechanically: anchor resolves / no anchor / symbol absent);
//   - the six output classes (design §6);
//   - a deterministic certainty score and the >80% router (design §6) — which
//     may AUTO-MAP a grounded intent to an EXISTING invariant/meta-principle, but
//     NEVER auto-creates new intent or a new principle (that stays human).
//
// It does NOT extract intent (that is the LLM phase), write any graph, promote,
// or mint principles. Extraction proposes; this grounds; humans approve.

// TrustTier ranks an intent SOURCE or ANCHOR by how strongly it PROVES a rule —
// not how confidently it states one (a README can shout a rule and still be
// Tier 4). Higher is stronger. Mirrors design §2.
type TrustTier int

const (
	TierIntentUnresolved TrustTier = iota // no anchor of any kind
	TierWeakHint                          // naming, folders, examples, conventions   (T5)
	TierDocsOnly                          // README, tutorials, user guides, comments  (T4)
	TierMaintainerIntent                  // ADRs, design docs, maintainer PR reviews   (T3)
	TierLandedBehavior                    // implementation code, landed/revert commits (T2)
	TierExecutableTruth                   // tests, schema/proto constraints, CI gates  (T1)
)

func (t TrustTier) String() string {
	switch t {
	case TierExecutableTruth:
		return "executable_truth"
	case TierLandedBehavior:
		return "landed_behavior"
	case TierMaintainerIntent:
		return "maintainer_intent"
	case TierDocsOnly:
		return "docs_only"
	case TierWeakHint:
		return "weak_hint"
	default:
		return "unresolved"
	}
}

// groundedBar is the minimum anchor tier at which an intent counts as GROUNDED
// (design §2: "the grounding bar is Tier 2"). Below it, an intent is proposed,
// not grounded.
const groundedBar = TierLandedBehavior

// IntentOutputClass is the relationship between STATED intent and ENCODED
// reality (design §6). strong_intent is the only clean "accept"; the rest are
// findings/leads.
type IntentOutputClass string

const (
	StrongIntent     IntentOutputClass = "strong_intent"     // doc + code/test agree
	StaleIntent      IntentOutputClass = "stale_intent"      // docs say X, code does not-X
	HiddenIntent     IntentOutputClass = "hidden_intent"     // code/test encode it, docs don't explain it
	MissingInvariant IntentOutputClass = "missing_invariant" // scars imply it; no doc, no test
	AmbiguousOwner   IntentOutputClass = "ambiguous_owner"   // ≥2 sources imply different owners
	UngroundedClaim  IntentOutputClass = "ungrounded_claim"  // stated/LLM, no anchor
)

// IsFinding reports whether a class is a divergence/lead that ALWAYS goes to a
// human (never auto-mapped), as opposed to a clean accept candidate.
func (c IntentOutputClass) IsFinding() bool {
	switch c {
	case StaleIntent, MissingInvariant, AmbiguousOwner, UngroundedClaim:
		return true
	default:
		return false
	}
}

// IntentRoute is the §6 routing decision.
type IntentRoute string

const (
	RouteAutoMap IntentRoute = "auto_map" // ≥80% certainty + maps to EXISTING — advisory, audited, reversible
	RouteHuman   IntentRoute = "human"    // below threshold, a finding, OR creating NEW intent
)

// IntentCandidate is the input schema (design §5). It is PROPOSED (by an LLM, a
// human, or a YAML file) and must be grounded before it is trusted.
type IntentCandidate struct {
	IntentID              string   `yaml:"intent_id"`
	Claim                 string   `yaml:"claim"`
	Category              string   `yaml:"category"`
	Sources               Sources  `yaml:"sources"`  // WHERE it was stated (T3–T5)
	Evidence              Evidence `yaml:"evidence"` // WHAT grounds it (T1–T2)
	RelatedInvariants     []string `yaml:"related_invariants"`
	RelatedMetaPrinciples []string `yaml:"related_meta_principles"`
	Owners                []string `yaml:"owners"`      // ≥2 distinct → ambiguous_owner
	Divergence            string   `yaml:"divergence"`  // "contradicts" asserts stale (counter-anchor in Evidence)
	ScarsImply            bool     `yaml:"scars_imply"` // coldsource scars imply it (→ missing_invariant when unanchored)
	ExtractedByLLM        bool     `yaml:"extracted_by_llm"`
	Status                string   `yaml:"status"` // always "candidate"
}

// Sources are the stated-intent locations (Tier 3–5).
type Sources struct {
	Docs     []string `yaml:"docs"`
	Comments []string `yaml:"comments"`
	Hints    []string `yaml:"hints"` // naming/folder/example/convention (Tier 5)
}

// Evidence are the code anchors (Tier 1–2), in coldsource citation form
// (file:<path>[:line], commit:<sha>) or bare paths/shas (normalized).
type Evidence struct {
	Code    []string `yaml:"code"`
	Tests   []string `yaml:"tests"`
	Commits []string `yaml:"commits"`
}

// IntentGrounding is the result of grounding one candidate.
type IntentGrounding struct {
	IntentID       string
	StatedTier     TrustTier // strongest doc/source tier
	GroundingTier  TrustTier // strongest code/test anchor tier
	OutputClass    IntentOutputClass
	Certainty      float64
	Route          IntentRoute
	RouteReason    string
	DecidedBy      string // "auto" (auto-map to existing) | "human"
	SymbolMismatch bool   // a cited code anchor held none of the claim's symbols
	Anchors        []CitationGrounding
}

// GroundIntent classifies one intent candidate against the target tree. Pure
// except for the injected GitVerifier and repo file reads; deterministic for a
// fixed tree. Reuses groundOne (the coldsource per-citation grounder) for every
// code/test/commit anchor.
func GroundIntent(c IntentCandidate, repoRoot string, git GitVerifier) IntentGrounding {
	symbols := claimedSymbols(c.Claim)
	cpv, _ := git.(commitPathVerifier)

	// ── stated tier: strongest of the doc/comment/hint sources ──────────────
	stated := TierIntentUnresolved
	for _, d := range c.Sources.Docs {
		stated = maxTier(stated, classifySourceTier(d))
	}
	if len(c.Sources.Comments) > 0 {
		stated = maxTier(stated, TierDocsOnly)
	}
	if len(c.Sources.Hints) > 0 {
		stated = maxTier(stated, TierWeakHint)
	}

	// ── grounding tier: run every code anchor through the coldsource spine ───
	anchorCits := evidenceCitations(c.Evidence)
	filePaths := citedFilePaths(anchorCits)
	g := IntentGrounding{IntentID: c.IntentID, StatedTier: stated}
	grounding := TierIntentUnresolved
	hasCodeAnchor := false
	for _, cit := range anchorCits {
		hasCodeAnchor = true
		cg := groundOne(cit, repoRoot, git, cpv, symbols, filePaths)
		g.Anchors = append(g.Anchors, cg)
		if cg.Note == "symbol_absent" {
			g.SymbolMismatch = true
		}
		grounding = maxTier(grounding, anchorTier(cit, cg.Tier))
	}
	g.GroundingTier = grounding

	hasStated := stated >= TierWeakHint
	isGrounded := grounding >= groundedBar

	// ── output class (design §6) ────────────────────────────────────────────
	g.OutputClass = classifyOutput(c, hasStated, isGrounded, hasCodeAnchor, g.SymbolMismatch)
	g.Certainty = certainty(grounding, isGrounded, hasStated, g.SymbolMismatch, len(g.Anchors))
	g.Route, g.RouteReason, g.DecidedBy = route(c, g.OutputClass, g.Certainty)
	return g
}

// classifyOutput derives the output class from the stated/encoded relationship
// plus the structural signals (owners, asserted divergence, scars). Mechanical
// contradiction detection beyond "the doc names symbols the code lacks" is left
// to the LLM/human phase — honestly scoped.
func classifyOutput(c IntentCandidate, hasStated, isGrounded, hasCodeAnchor, symbolMismatch bool) IntentOutputClass {
	switch {
	case len(distinct(c.Owners)) >= 2:
		return AmbiguousOwner
	case c.Divergence == "contradicts" && isGrounded:
		return StaleIntent
	case hasStated && hasCodeAnchor && !isGrounded && symbolMismatch:
		// docs assert symbols the code does not contain → docs vs code disagree.
		return StaleIntent
	case isGrounded && hasStated:
		return StrongIntent
	case isGrounded && !hasStated:
		return HiddenIntent
	case !isGrounded && c.ScarsImply && !hasStated:
		return MissingInvariant
	default:
		return UngroundedClaim
	}
}

// certainty is a DETERMINISTIC grounding-strength score in [0,1]. The §6 ensemble
// (LLM refute-lens agreement) is an opt-in later augmentation; this mechanical
// score stands alone for now.
func certainty(grounding TrustTier, isGrounded, hasStated, symbolMismatch bool, anchors int) float64 {
	var base float64
	switch grounding {
	case TierExecutableTruth:
		base = 0.9
	case TierLandedBehavior:
		base = 0.8
	case TierMaintainerIntent:
		base = 0.5
	case TierDocsOnly:
		base = 0.3
	case TierWeakHint:
		base = 0.1
	}
	if anchors > 1 {
		base += 0.03 * float64(anchors-1) // corroboration, capped below
	}
	if isGrounded && hasStated {
		base += 0.05 // doc and code agree
	}
	if symbolMismatch {
		base -= 0.3 // a cited anchor did not actually hold the claim
	}
	return clamp(base, 0, 1)
}

// route applies the >80% rule (design §6): a finding always goes to a human; a
// grounded candidate at ≥0.80 that maps to an EXISTING invariant/meta-principle
// is auto-mapped (advisory, audited); everything else — INCLUDING creating NEW
// intent (no existing mapping) at high certainty — goes to a human.
func route(c IntentCandidate, class IntentOutputClass, cert float64) (IntentRoute, string, string) {
	if class.IsFinding() {
		return RouteHuman, "finding/divergence — always human", "human"
	}
	hasExistingMapping := len(c.RelatedInvariants) > 0 || len(c.RelatedMetaPrinciples) > 0
	switch {
	case cert >= 0.80 && hasExistingMapping:
		return RouteAutoMap, "≥0.80 and maps to existing invariant/meta-principle — advisory auto-map", "auto"
	case cert >= 0.80 && !hasExistingMapping:
		return RouteHuman, "≥0.80 but no existing mapping — creating NEW intent stays human", "human"
	default:
		return RouteHuman, "below 0.80 certainty — human review", "human"
	}
}

// classifySourceTier tiers a stated-intent doc path. ADRs / design docs / RFCs /
// decision records are maintainer intent (T3); other docs/comments are
// descriptive (T4); anything that looks like a mere convention is a hint (T5).
func classifySourceTier(path string) TrustTier {
	p := strings.ToLower(path)
	switch {
	case strings.HasPrefix(p, "pr:"):
		return TierMaintainerIntent // a maintainer's PR-review explanation
	case strings.HasSuffix(p, ".proto"), containsAny(p, ".schema.", "openapi"):
		return TierMaintainerIntent // a schema/contract spec
	case containsAny(p, "/adr", "adr/", "decision-record", "/rfc", "rfc-", "design-doc",
		"/design/", "design-note", "/intent/", "architecture/"):
		return TierMaintainerIntent
	case strings.HasSuffix(p, ".md"), containsAny(p, "readme", "tutorial", "guide", "/docs/", "claude.md"):
		return TierDocsOnly
	default:
		return TierWeakHint
	}
}

// anchorTier maps a code-anchor's coldsource ProvenanceTier into a TrustTier,
// promoting schema/proto constraints to executable truth.
func anchorTier(citation string, pt ProvenanceTier) TrustTier {
	switch pt {
	case TierTestEncoded:
		return TierExecutableTruth
	case TierLandedCommit:
		if isExecutableTruthPath(citation) {
			return TierExecutableTruth // a resolving .proto/schema constraint is T1
		}
		return TierLandedBehavior
	default:
		return TierIntentUnresolved // review_suggestion / unresolved do not ground intent
	}
}

func isExecutableTruthPath(citation string) bool {
	p := strings.ToLower(citation)
	return containsAny(p, ".proto", ".schema.", "schema.json", "openapi", ".jsonschema")
}

// evidenceCitations normalizes the intent evidence fields into coldsource
// citation strings. tests/code → file:, commits → commit:; already-prefixed
// entries pass through.
func evidenceCitations(e Evidence) []string {
	var out []string
	add := func(items []string, prefix string) {
		for _, s := range items {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if strings.HasPrefix(s, "file:") || strings.HasPrefix(s, "commit:") || strings.HasPrefix(s, "pr:") {
				out = append(out, s)
				continue
			}
			out = append(out, prefix+s)
		}
	}
	add(e.Tests, "file:")
	add(e.Code, "file:")
	add(e.Commits, "commit:")
	return out
}

func maxTier(a, b TrustTier) TrustTier {
	if a > b {
		return a
	}
	return b
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func distinct(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
