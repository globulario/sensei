// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/awareness-graph/golang/extractor/coldsource"
	"gopkg.in/yaml.v3"
)

// applyBar is the certainty a strong_intent grounding must clear to be written
// straight into the awareness corpus by --apply. Below it (or any finding) the
// candidate is parked for human review instead.
const applyBar = 0.80

// runIntentMine grounds architectural-intent candidates against a repo tree and
// prints a dry-run report grouped by output class. Two modes:
//
//   - --candidates <yaml>: ground externally-supplied candidates (debug/replay);
//   - --sources <kinds> --drafter <echo|llm>: EXTRACT candidates from the repo's
//     own stated charter (docs/comments/tests/prs/commits/schemas), then ground.
//
// It never writes a graph, promotes nothing, and mints no principles. The
// proposer (echo or LLM) PROPOSES; GroundIntent GROUNDS; a human APPROVES. The
// LLM drafter is opt-in and reads ANTHROPIC_API_KEY from the environment only.
func runIntentMine(args []string) int {
	fs := flag.NewFlagSet("awg intent-mine", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", ".", "repo working tree for grounding + extraction")
	candidates := fs.String("candidates", "", "YAML file of proposed candidates (skips extraction)")
	fromColdsource := fs.String("from-coldsource", "", "YAML of coldsource candidates to lift as scar-derived intent (bridge)")
	sources := fs.String("sources", "docs,comments,schemas,tests", "comma list: docs,comments,schemas,tests,commits,prs")
	drafter := fs.String("drafter", "echo", "proposer: echo (deterministic, no key) | llm (ANTHROPIC_API_KEY)")
	prComments := fs.String("pr-comments", "", "JSON file of PR review comments (for --sources prs)")
	model := fs.String("model", "", "LLM model override (default "+coldsource.DefaultModel+")")
	maxN := fs.Int("max", 12, "max candidates to propose")
	apply := fs.Bool("apply", false, "write results into the repo's awareness corpus: each strong_intent grounding ≥0.80 → docs/awareness/intent_<id>.yaml (graph knowledge on the next build); every finding / sub-bar candidate → docs/awareness/candidates/intents.yaml for human review")
	_ = fs.Bool("dry-run", true, "report only (default); accepted for back-compat — use --apply to write")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage:
  awg intent-mine --repo . --sources docs,comments,schemas,tests [--drafter echo|llm] [--max N]
  awg intent-mine --repo . --sources ... --drafter llm --apply        # land passing intents in the graph
  awg intent-mine --candidates <file.yaml> --repo .

Extract architectural-intent candidates from a repo's stated charter (or read
proposed candidates from YAML), ground them against the tree, and print output
classes (strong/stale/hidden/missing/ambiguous/ungrounded), trust tiers,
certainty, and the >80% routing decision. The proposer proposes; AWG grounds.

Default is report-only. With --apply, the >80% rule is acted on: each grounded
strong_intent at certainty ≥0.80 is written as an importable single-entity intent
file (graph knowledge on the next build); everything below the bar — and every
divergence finding — is parked under candidates/ for a human to review and
promote. Nothing below the bar is ever auto-written into the graph.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	git := coldsource.NewGitVerifier(*repo)
	var cands []coldsource.IntentCandidate

	if *candidates != "" {
		loaded, code := loadIntentCandidatesYAML(*candidates)
		if code != 0 {
			return code
		}
		cands = loaded
	} else if *fromColdsource == "" {
		extracted, code := extractIntentCandidates(*repo, *sources, *drafter, *prComments, *model, *maxN)
		if code != 0 {
			return code
		}
		cands = extracted
	}
	// Bridge (coldsource → intent): lift scar-mined candidates into the grounding
	// set; GroundIntent classifies each as hidden_intent (encoded, undocumented)
	// or missing_invariant (scars imply, nothing encodes it).
	if *fromColdsource != "" {
		scars, err := coldsource.LoadColdsourceAsIntent(*fromColdsource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: load coldsource candidates: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "lifted %d coldsource scar candidate(s) as intent\n", len(scars))
		cands = append(cands, scars...)
	}
	if len(cands) == 0 {
		fmt.Fprintln(os.Stderr, "no candidates to ground")
		return 1
	}

	rep := coldsource.IntentReport{Repo: *repo, Total: len(cands)}
	for _, c := range cands {
		rep.Groundings = append(rep.Groundings, coldsource.GroundIntent(c, *repo, git))
	}
	coldsource.RenderIntentReport(os.Stdout, rep)
	// Bridge (intent → coldsource): emit finder hints from divergence findings.
	coldsource.RenderFinderHints(os.Stdout, coldsource.FinderHintsFromGroundings(rep.Groundings))

	if *apply {
		landed, parked, skipped, err := applyIntentGroundings(*repo, cands, rep.Groundings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "apply: %v\n", err)
			return 1
		}
		fmt.Printf("\nApplied to %s\n", filepath.Join(*repo, "docs", "awareness"))
		fmt.Printf("  %d intent(s) → graph corpus (intent_<id>.yaml, ≥%.2f strong)\n", landed, applyBar)
		fmt.Printf("  %d parked as candidate(s) for review (candidates/intents.yaml)\n", parked)
		if skipped > 0 {
			fmt.Printf("  %d already present (skipped)\n", skipped)
		}
		if landed > 0 {
			fmt.Printf("Run `awg build` (or `awg rebuild`) to emit the triples.\n")
		}
	}
	return 0
}

// applyIntentGroundings acts on the >80% rule: each strong_intent grounding at
// certainty ≥ applyBar is written as an importable single-entity intent file in
// docs/awareness (graph knowledge on the next build); everything else — findings
// and sub-bar candidates — is parked under candidates/ for human review. The
// candidates/ subtree is skipped by the importer, so parked entries never reach
// the graph until a human promotes them.
func applyIntentGroundings(repo string, cands []coldsource.IntentCandidate, groundings []coldsource.IntentGrounding) (landed, parked, skipped int, err error) {
	awDir := filepath.Join(repo, "docs", "awareness")
	if mkErr := os.MkdirAll(awDir, 0o755); mkErr != nil {
		return 0, 0, 0, mkErr
	}
	var parkedEntries []map[string]any
	for i, g := range groundings {
		if i >= len(cands) {
			break
		}
		c := cands[i]
		if g.OutputClass == coldsource.StrongIntent && g.Certainty >= applyBar {
			wrote, werr := writeAppliedIntent(awDir, c, g)
			if werr != nil {
				return landed, parked, skipped, werr
			}
			if wrote {
				landed++
			} else {
				skipped++
			}
			continue
		}
		parkedEntries = append(parkedEntries, parkedIntentCandidate(c, g))
		parked++
	}
	if len(parkedEntries) > 0 {
		if werr := writeParkedCandidates(filepath.Join(awDir, "candidates", "intents.yaml"), parkedEntries); werr != nil {
			return landed, parked, skipped, werr
		}
	}
	return landed, parked, skipped, nil
}

// appliedIntent is the single-entity canonical intent the importer reads
// (detected by id + level). Field order is the YAML key order.
type appliedIntent struct {
	ID                string         `yaml:"id"`
	Level             string         `yaml:"level"`
	Title             string         `yaml:"title"`
	Intent            string         `yaml:"intent"`
	Status            string         `yaml:"status"`
	ExpressedBy       []string       `yaml:"expressed_by,omitempty"`
	RelatedInvariants []string       `yaml:"related_invariants,omitempty"`
	Provenance        map[string]any `yaml:"provenance"`
}

// writeAppliedIntent writes one importable intent file. Returns wrote=false if a
// file for that id already exists (idempotent re-apply).
func writeAppliedIntent(awDir string, c coldsource.IntentCandidate, g coldsource.IntentGrounding) (bool, error) {
	id := canonicalIntentID(c.IntentID)
	path := filepath.Join(awDir, "intent_"+strings.TrimPrefix(intentSlug(id), "intent_")+".yaml")
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	}
	ai := appliedIntent{
		ID:                id,
		Level:             "constraint", // base aw:Intent + ConstraintIntent; a human may refine the level
		Title:             humanizeIntentTitle(c.IntentID),
		Intent:            normalizeWS(c.Claim),
		Status:            "active",
		ExpressedBy:       stripCitationPrefixes(c.Evidence.Code),
		RelatedInvariants: c.RelatedInvariants,
		Provenance: map[string]any{
			"promoted_from":           "candidate",
			"discovered_from":         "awg intent-mine --drafter llm (coldsource); grounded against the live tree",
			"confidence_at_promotion": confidenceFromCertainty(g.Certainty),
			"grounding_tier":          g.GroundingTier.String(),
			"category":                c.Category,
		},
	}
	var buf bytes.Buffer
	buf.WriteString("# Applied by `awg intent-mine --apply` — a grounded strong_intent (≥0.80).\n")
	buf.WriteString("# Mined from the repo's charter, grounded against the live tree. Refine by hand as needed.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if encErr := enc.Encode(ai); encErr != nil {
		return false, encErr
	}
	enc.Close()
	return true, os.WriteFile(path, buf.Bytes(), 0o644)
}

// parkedIntentCandidate is a promote-ready candidate entry for a sub-bar/finding
// grounding, written under candidates/ (skipped by the importer).
func parkedIntentCandidate(c coldsource.IntentCandidate, g coldsource.IntentGrounding) map[string]any {
	return map[string]any{
		"id":              canonicalIntentID(c.IntentID),
		"class":           "intent",
		"status":          "candidate",
		"confidence":      confidenceFromCertainty(g.Certainty),
		"level":           "constraint",
		"label":           humanizeIntentTitle(c.IntentID),
		"summary":         normalizeWS(c.Claim),
		"evidence":        intentEvidenceString(c, g),
		"discovered_from": "awg intent-mine --drafter llm (coldsource); grounded against the live tree",
	}
}

// writeParkedCandidates appends entries to a candidates/ file under the
// `candidates:` key, deduped by id and sorted (deterministic).
func writeParkedCandidates(path string, entries []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	doc := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(raw, &doc)
	}
	existing, _ := doc["candidates"].([]any)
	seen := map[string]bool{}
	for _, e := range existing {
		if m, ok := e.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				seen[id] = true
			}
		}
	}
	for _, e := range entries {
		if id, _ := e["id"].(string); !seen[id] {
			existing = append(existing, e)
			seen[id] = true
		}
	}
	sort.SliceStable(existing, func(i, j int) bool {
		a, _ := existing[i].(map[string]any)
		b, _ := existing[j].(map[string]any)
		ai, _ := a["id"].(string)
		bi, _ := b["id"].(string)
		return ai < bi
	})
	doc["candidates"] = existing
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	header := "# Parked by `awg intent-mine --apply` — sub-bar / divergence candidates.\n" +
		"# The importer SKIPS candidates/. Review and `awg promote` to make them graph knowledge.\n"
	return os.WriteFile(path, append([]byte(header), out...), 0o644)
}

// ── small pure helpers ───────────────────────────────────────────────────────

// intentSlug lowercases s and collapses every non-alphanumeric run to one
// underscore ("build-shell-once" → "build_shell_once").
func intentSlug(s string) string {
	var b strings.Builder
	prevU := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevU = false
		} else if !prevU {
			b.WriteByte('_')
			prevU = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// canonicalIntentID maps a mined id ("build-shell-once" or "intent.build-shell")
// to the canonical dotted form "intent.<slug>".
func canonicalIntentID(id string) string {
	return "intent." + strings.TrimPrefix(intentSlug(id), "intent_")
}

func humanizeIntentTitle(id string) string {
	s := strings.TrimPrefix(intentSlug(id), "intent_")
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return id
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func confidenceFromCertainty(c float64) string {
	switch {
	case c >= 0.80:
		return "high"
	case c >= 0.60:
		return "medium"
	default:
		return "low"
	}
}

// stripCitationPrefixes drops the coldsource "file:" citation prefix so anchors
// read as bare repo paths (the expressed_by convention).
func stripCitationPrefixes(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, strings.TrimPrefix(strings.TrimSpace(s), "file:"))
	}
	return out
}

func normalizeWS(s string) string { return strings.Join(strings.Fields(s), " ") }

func intentEvidenceString(c coldsource.IntentCandidate, g coldsource.IntentGrounding) string {
	var parts []string
	if n := len(c.Sources.Docs) + len(c.Sources.Comments); n > 0 {
		parts = append(parts, fmt.Sprintf("stated in %d source(s)", n))
	}
	if anchors := stripCitationPrefixes(c.Evidence.Code); len(anchors) > 0 {
		parts = append(parts, fmt.Sprintf("grounded (%s, %.2f) at: %s", g.GroundingTier.String(), g.Certainty, strings.Join(anchors, ", ")))
	}
	if len(parts) == 0 {
		return "mined by awg intent-mine; see claim"
	}
	return strings.Join(parts, "; ")
}

func loadIntentCandidatesYAML(path string) ([]coldsource.IntentCandidate, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read candidates: %v\n", err)
		return nil, 1
	}
	var doc struct {
		Candidates []coldsource.IntentCandidate `yaml:"candidates"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "error: parse candidates YAML: %v\n", err)
		return nil, 1
	}
	return doc.Candidates, 0
}

// extractIntentCandidates gathers stated-intent excerpts, drafts candidates via
// the chosen proposer, applies the intent cage, and materializes grounded-ready
// candidates. Nothing is written.
func extractIntentCandidates(repo, sources, drafter, prCommentsPath, model string, maxN int) ([]coldsource.IntentCandidate, int) {
	kinds := splitCSV(sources)
	var prComments []coldsource.ReviewComment
	if prCommentsPath != "" {
		c, err := coldsource.LoadPRComments(prCommentsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: load pr-comments: %v\n", err)
			return nil, 1
		}
		prComments = c
	}
	excerpts := coldsource.GatherIntentExcerpts(repo, kinds, prComments, 0)
	fmt.Fprintf(os.Stderr, "gathered %d rule-bearing excerpt(s) from sources: %s\n", len(excerpts), sources)
	if len(excerpts) == 0 {
		return nil, 0
	}
	// Bound what the proposer sees so the prompt stays focused; the cage's allowed
	// set is exactly this bounded list.
	const maxExcerptsToDraft = 120
	if len(excerpts) > maxExcerptsToDraft {
		fmt.Fprintf(os.Stderr, "bounding proposer input to the first %d excerpt(s)\n", maxExcerptsToDraft)
		excerpts = excerpts[:maxExcerptsToDraft]
	}

	var dr coldsource.IntentDrafter
	switch drafter {
	case "echo":
		dr = coldsource.EchoIntentDrafter{Max: maxN}
	case "llm":
		client, err := coldsource.NewAnthropicClientFromEnv(model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return nil, 2
		}
		dr = coldsource.LLMIntentDrafter{Client: client, Max: maxN}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown drafter %q (use echo|llm)\n", drafter)
		return nil, 2
	}

	cands, rejected, err := coldsource.DraftAndCageIntents(context.Background(), dr, excerpts, maxN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: intent drafter: %v\n", err)
		return nil, 1
	}
	if rejected > 0 {
		fmt.Fprintf(os.Stderr, "intent cage: rejected %d uncited/fabricated draft(s)\n", rejected)
	}
	return cands, 0
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
