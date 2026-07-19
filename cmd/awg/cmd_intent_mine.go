// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/extractor/coldsource"
	"gopkg.in/yaml.v3"
)

// applyBar is the certainty a valid strong_intent must clear for delegated
// machine adoption. Adoption is not human governance: the emitted intent remains
// explicitly model_inferred, machine_adopted, and not_human_reviewed.
const applyBar = 0.80

// runIntentMine grounds architectural-intent candidates against a repo tree and
// prints a dry-run report grouped by output class. Two modes:
//
//   - --candidates <yaml>: ground externally-supplied candidates (debug/replay);
//   - --sources <kinds> --drafter <echo|llm|claude-cli|codex-cli>: EXTRACT candidates from the repo's
//     own stated charter (docs/comments/tests/prs/commits/schemas), then ground.
//
// It never writes a graph, promotes nothing, and mints no governed principles.
// The proposer (echo or LLM) drafts; GroundIntent grounds; an explicit adoption
// policy decides whether valid strong intent may become machine_adopted
// knowledge. The LLM drafter backends are opt-in unless the caller chooses auto.
func runIntentMine(args []string) int {
	fs := flag.NewFlagSet("sensei intent-mine", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := "."
	fs.StringVar(&repo, "path", ".", "repo working tree for grounding + extraction")
	fs.StringVar(&repo, "repo", ".", "deprecated alias for --path")
	candidates := fs.String("candidates", "", "YAML file of proposed candidates (skips extraction)")
	fromColdsource := fs.String("from-coldsource", "", "YAML of coldsource candidates to lift as scar-derived intent (bridge)")
	sources := fs.String("sources", "docs,comments,schemas,tests", "comma list: docs,comments,schemas,tests,commits,prs")
	drafter := fs.String("drafter", "echo", "proposer: echo (deterministic, no key) | llm (ANTHROPIC_API_KEY/AUTH_TOKEN) | claude-cli (authed Claude CLI, no key) | codex-cli (authed Codex CLI, no key) | auto")
	prComments := fs.String("pr-comments", "", "JSON file of PR review comments (for --sources prs)")
	model := fs.String("model", "", "LLM model override (default "+coldsource.DefaultModel+")")
	maxN := fs.Int("max", 12, "max candidates to propose")
	adopt := fs.Bool("adopt", false, "machine-adopt valid strong_intent groundings and stage the rest; never marks model output as governed")
	stage := fs.Bool("stage", false, "stage every grounded intent candidate under docs/awareness/candidates/intents.yaml; no machine adoption")
	apply := fs.Bool("apply", false, "deprecated alias for --adopt; writes machine_adopted, not governed active, intent files")
	_ = fs.Bool("dry-run", true, "report only (default); accepted for back-compat — use --adopt or --stage to write")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage:
  sensei intent-mine --path . --sources docs,comments,schemas,tests [--drafter echo|llm|claude-cli|codex-cli|auto] [--max N]
  sensei intent-mine --path . --sources ... --drafter llm --adopt       # machine-adopt valid strong intents
  sensei intent-mine --path . --sources ... --drafter llm --stage       # candidate-only staging
  sensei intent-mine --candidates <file.yaml> --path .

Extract architectural-intent candidates from a repo's stated charter (or read
proposed candidates from YAML), ground them against the tree, and print output
classes (strong/stale/hidden/missing/ambiguous/ungrounded), trust tiers,
certainty, and the review routing decision. The proposer proposes; AWG grounds.

Default is report-only. With --adopt, valid strong_intent groundings at certainty
≥0.80 are written as machine_adopted, model_inferred intents and every hidden,
weak, contradictory, or invalid result is staged under candidates/ for review.
With --stage, every result stays candidate-only. Neither mode writes governed
human-authored architecture or promotes candidates.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	warnDeprecatedRepoPathAlias(fs, "intent-mine")
	warnIfDomainLikeExtractorPath("intent-mine", repo)

	git := coldsource.NewGitVerifier(repo)
	var cands []coldsource.IntentCandidate

	if *candidates != "" {
		loaded, code := loadIntentCandidatesYAML(*candidates)
		if code != 0 {
			return code
		}
		cands = loaded
	} else if *fromColdsource == "" {
		extracted, code := extractIntentCandidates(repo, *sources, *drafter, *prComments, *model, *maxN)
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

	rep := coldsource.IntentReport{Repo: repo, Total: len(cands)}
	for _, c := range cands {
		rep.Groundings = append(rep.Groundings, coldsource.GroundIntent(c, repo, git))
	}
	coldsource.RenderIntentReport(os.Stdout, rep)
	// Bridge (intent → coldsource): emit finder hints from divergence findings.
	coldsource.RenderFinderHints(os.Stdout, coldsource.FinderHintsFromGroundings(rep.Groundings))

	if *adopt || *stage || *apply {
		policy := intentAdoptionPolicy{AllowMachineAdoption: *adopt || *apply, Drafter: strings.TrimSpace(*drafter)}
		res, err := applyIntentGroundings(repo, cands, rep.Groundings, policy)
		if err != nil {
			fmt.Fprintf(os.Stderr, "intent write: %v\n", err)
			return 1
		}
		fmt.Printf("\nIntent adoption report for %s\n", filepath.Join(repo, "docs", "awareness"))
		fmt.Printf("  Drafted: %d\n", res.Drafted)
		fmt.Printf("  Grounded strong: %d\n", res.GroundedStrong)
		fmt.Printf("  Grounded hidden: %d\n", res.GroundedHidden)
		fmt.Printf("  Staged candidates: %d\n", res.Staged)
		fmt.Printf("  Parked invalid: %d\n", res.ParkedInvalid)
		fmt.Printf("  Machine adopted: %d\n", res.MachineAdopted)
		fmt.Printf("  Governed: 0\n")
		fmt.Printf("  Human-reviewed promotions: 0\n")
		if res.Skipped > 0 {
			fmt.Printf("  Already present: %d\n", res.Skipped)
		}
		if policy.AllowMachineAdoption {
			fmt.Printf("Machine-adopted intents are model_inferred and not_human_reviewed; promote separately for governed authority.\n")
		} else {
			fmt.Printf("Candidate-only staging mode: no machine adoption was performed.\n")
		}
	}
	return 0
}

type intentAdoptionPolicy struct {
	AllowMachineAdoption bool
	Drafter              string
}

type intentApplyResult struct {
	Drafted        int
	GroundedStrong int
	GroundedHidden int
	Staged         int
	ParkedInvalid  int
	MachineAdopted int
	Skipped        int
}

// applyIntentGroundings routes grounded intent under the declared policy.
// Valid strong_intent groundings may become machine_adopted knowledge. Every
// other result remains candidate-only. No path writes human-governed authority.
func applyIntentGroundings(repo string, cands []coldsource.IntentCandidate, groundings []coldsource.IntentGrounding, policy intentAdoptionPolicy) (intentApplyResult, error) {
	var res intentApplyResult
	awDir := filepath.Join(repo, "docs", "awareness")
	if mkErr := os.MkdirAll(awDir, 0o755); mkErr != nil {
		return res, mkErr
	}
	var entries []map[string]any
	seenIntentFingerprints := map[string]string{}
	for i, g := range groundings {
		if i >= len(cands) {
			break
		}
		res.Drafted++
		if g.OutputClass == coldsource.StrongIntent {
			res.GroundedStrong++
		}
		if g.OutputClass == coldsource.HiddenIntent {
			res.GroundedHidden++
		}
		c := cands[i]
		id := canonicalCandidateIntentID(c)
		fp := coldsource.IntentSemanticFingerprint(intentCandidateTitle(c), c.Claim, intentCandidateScope(c))
		violations := validateIntentWriteCandidate(c)
		if prev, ok := seenIntentFingerprints[id]; ok && prev != fp {
			violations = append(violations, "candidate.identity.collision")
		}
		if len(violations) == 0 {
			seenIntentFingerprints[id] = fp
		}
		if len(violations) > 0 {
			res.ParkedInvalid++
			entries = append(entries, stagedIntentCandidate(c, g, policy, violations...))
			continue
		}
		if policy.AllowMachineAdoption && g.OutputClass == coldsource.StrongIntent && g.Certainty >= applyBar {
			wrote, werr := writeMachineAdoptedIntent(awDir, repo, c, g, policy)
			if werr != nil {
				return res, werr
			}
			if wrote {
				res.MachineAdopted++
			} else {
				res.Skipped++
			}
			continue
		}
		entries = append(entries, stagedIntentCandidate(c, g, policy, violations...))
		res.Staged++
	}
	if len(entries) > 0 {
		_, skipped, werr := writeStagedCandidates(filepath.Join(awDir, "candidates", "intents.yaml"), entries)
		if werr != nil {
			return res, werr
		}
		res.Skipped = skipped
	}
	return res, nil
}

// machineAdoptedIntent is importable as aw:Intent while preserving the
// jurisdiction boundary: this is supported, model-inferred knowledge, not a
// human-governed architectural law.
type machineAdoptedIntent struct {
	adoption.Receipt  `yaml:",inline"`
	ID                string         `yaml:"id"`
	Level             string         `yaml:"level"`
	Title             string         `yaml:"title"`
	Intent            string         `yaml:"intent"`
	RevisionStatus    string         `yaml:"revision_status,omitempty"`
	ExpressedBy       []string       `yaml:"expressed_by,omitempty"`
	RelatedInvariants []string       `yaml:"related_invariants,omitempty"`
	Provenance        map[string]any `yaml:"provenance"`
}

func writeMachineAdoptedIntent(awDir, repo string, c coldsource.IntentCandidate, g coldsource.IntentGrounding, policy intentAdoptionPolicy) (bool, error) {
	id := canonicalCandidateIntentID(c)
	path := filepath.Join(awDir, "intent_"+strings.TrimPrefix(intentSlug(id), "intent_")+".yaml")
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	}
	rev, revStatus := gitHeadRevision(repo)
	mi := machineAdoptedIntent{
		Receipt: adoption.Receipt{
			Status:             adoption.PromotionMachineAdopted,
			PromotionStatus:    adoption.PromotionMachineAdopted,
			AssertionOrigin:    "model_inferred",
			EpistemicStatus:    "supported",
			ArchitecturalPlane: "intended",
			ReviewStatus:       adoption.ReviewNotHumanReviewed,
			DecisionActor:      "sensei.intent_mine",
			DecisionContext:    "delegated_machine_adoption",
			DecisionPolicy:     "adoption.intent.strong_grounding.v1",
			DecisionTimestamp:  time.Now().UTC().Format(time.RFC3339),
			ValidForRevision:   rev,
			AdoptionBasis:      []string{"valid strong intent grounded against current repository sources"},
			SourceReceipts:     intentCandidateScope(c),
			CorroborationKinds: []string{"model_draft", "source_file"},
		},
		ID:                id,
		Level:             machineAdoptedIntentLevel(c.Category),
		Title:             intentCandidateTitle(c),
		Intent:            normalizeWS(c.Claim),
		RevisionStatus:    revStatus,
		ExpressedBy:       stripCitationPrefixes(c.Evidence.Code),
		RelatedInvariants: c.RelatedInvariants,
		Provenance: map[string]any{
			"adoption_policy":        "adoption.intent.strong_grounding.v1",
			"decision_actor":         "sensei.intent_mine",
			"decision_context":       "delegated_machine_adoption",
			"discovered_from":        intentDiscoverySource(policy),
			"confidence_at_adoption": confidenceFromCertainty(g.Certainty),
			"grounding_class":        string(g.OutputClass),
			"grounding_tier":         g.GroundingTier.String(),
			"certainty":              g.Certainty,
			"category":               c.Category,
		},
	}
	var buf bytes.Buffer
	buf.WriteString("# Machine-adopted by `sensei intent-mine --adopt`.\n")
	buf.WriteString("# This is model_inferred, supported, and not_human_reviewed; promote separately for governed authority.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if encErr := enc.Encode(mi); encErr != nil {
		return false, encErr
	}
	enc.Close()
	return true, os.WriteFile(path, buf.Bytes(), 0o644)
}

func machineAdoptedIntentLevel(category string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	category = strings.NewReplacer("-", "_", " ", "_").Replace(category)
	if strings.Contains(category, "contract") {
		return "contract"
	}
	return "constraint"
}

// stagedIntentCandidate is a reviewable candidate entry written under
// candidates/ (skipped by the importer). Invalid entries carry rejection_reasons
// and are parked for repair rather than promotion.
func stagedIntentCandidate(c coldsource.IntentCandidate, g coldsource.IntentGrounding, policy intentAdoptionPolicy, reasons ...string) map[string]any {
	id := canonicalCandidateIntentID(c)
	out := map[string]any{
		"id":               id,
		"class":            "intent",
		"status":           "candidate",
		"promotion_status": "candidate",
		"review_state":     "staged",
		"confidence":       confidenceFromCertainty(g.Certainty),
		"level":            "constraint",
		"title":            intentCandidateTitle(c),
		"label":            intentCandidateTitle(c),
		"statement":        normalizeWS(c.Claim),
		"summary":          normalizeWS(c.Claim),
		"evidence":         intentEvidenceString(c, g),
		"grounding_class":  string(g.OutputClass),
		"grounding_tier":   g.GroundingTier.String(),
		"certainty":        g.Certainty,
		"discovered_from":  intentDiscoverySource(policy),
	}
	if len(reasons) > 0 {
		sort.Strings(reasons)
		out["review_state"] = "parked_invalid"
		out["rejection_reasons"] = reasons
		if stringSliceContains(reasons, "candidate.identity.collision") {
			out["conflicting_id"] = id
			out["id"] = id + ".collision_" + shortDigest(coldsource.IntentSemanticFingerprint(intentCandidateTitle(c), c.Claim, intentCandidateScope(c)))
		}
	}
	return out
}

// writeStagedCandidates appends entries to a candidates/ file under the
// `candidates:` key, deduped by id and sorted (deterministic).
func writeStagedCandidates(path string, entries []map[string]any) (added, skipped int, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, 0, err
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
		if id, _ := e["id"].(string); id != "" && !seen[id] {
			existing = append(existing, e)
			seen[id] = true
			added++
		} else {
			skipped++
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
		return added, skipped, err
	}
	header := "# Staged by `sensei intent-mine --stage` — review-only intent candidates.\n" +
		"# The importer SKIPS candidates/. Review and promote explicitly to make graph knowledge.\n"
	return added, skipped, os.WriteFile(path, append([]byte(header), out...), 0o644)
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

func canonicalCandidateIntentID(c coldsource.IntentCandidate) string {
	if c.ExtractedByLLM {
		return coldsource.MintIntentID(c.Title, c.Claim, intentCandidateScope(c))
	}
	if id := strings.TrimSpace(c.IntentID); strings.HasPrefix(id, "intent.") {
		return id
	}
	return canonicalIntentID(c.IntentID)
}

func validateIntentWriteCandidate(c coldsource.IntentCandidate) []string {
	var reasons []string
	id := canonicalCandidateIntentID(c)
	if !coldsource.ValidIntentID(id) {
		reasons = append(reasons, "candidate.identity.invalid")
	}
	if strings.TrimSpace(intentCandidateTitle(c)) == "" || (c.ExtractedByLLM && strings.TrimSpace(c.Title) == "") {
		reasons = append(reasons, "candidate.title.empty")
	}
	if strings.TrimSpace(c.Claim) == "" {
		reasons = append(reasons, "candidate.statement.empty")
	}
	if strings.TrimSpace(c.Category) == "" {
		reasons = append(reasons, "candidate.kind.empty")
	}
	if len(intentCandidateScope(c)) == 0 {
		reasons = append(reasons, "candidate.scope.empty")
	}
	if len(intentCandidateCitations(c)) == 0 {
		reasons = append(reasons, "candidate.citations.empty")
	}
	return reasons
}

func stringSliceContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func shortDigest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:10]
}

func intentDiscoverySource(policy intentAdoptionPolicy) string {
	drafter := strings.TrimSpace(policy.Drafter)
	if drafter == "" {
		drafter = "unknown"
	}
	return fmt.Sprintf("sensei intent-mine --drafter %s (coldsource); grounded against the live tree", drafter)
}

func gitHeadRevision(repo string) (revision, status string) {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", "unresolved"
	}
	rev := strings.TrimSpace(string(out))
	if rev == "" {
		return "", "unresolved"
	}
	return rev, "resolved"
}

func intentCandidateTitle(c coldsource.IntentCandidate) string {
	if t := strings.TrimSpace(c.Title); t != "" {
		return t
	}
	return humanizeIntentTitle(c.IntentID)
}

func intentCandidateScope(c coldsource.IntentCandidate) []string {
	var scope []string
	scope = append(scope, stripCitationPrefixes(c.Evidence.Code)...)
	scope = append(scope, stripCitationPrefixes(c.Evidence.Tests)...)
	scope = append(scope, stripCitationPrefixes(c.Sources.Docs)...)
	scope = append(scope, stripCitationPrefixes(c.Sources.Comments)...)
	scope = append(scope, stripCitationPrefixes(c.Sources.Hints)...)
	scope = append(scope, c.Evidence.Commits...)
	clean := make([]string, 0, len(scope))
	seen := map[string]bool{}
	for _, s := range scope {
		t := strings.TrimSpace(s)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
	}
	sort.Strings(clean)
	return clean
}

func intentCandidateCitations(c coldsource.IntentCandidate) []string {
	var citations []string
	citations = append(citations, c.Sources.Docs...)
	citations = append(citations, c.Sources.Comments...)
	citations = append(citations, c.Sources.Hints...)
	citations = append(citations, c.Evidence.Code...)
	citations = append(citations, c.Evidence.Tests...)
	citations = append(citations, c.Evidence.Commits...)
	return citations
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
		return "mined by sensei intent-mine; see claim"
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
	case "llm", "claude-cli", "codex-cli", "auto":
		client, receipt, err := coldsource.SelectLLMClient(coldsource.DrafterBackend(drafter), model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return nil, 2
		}
		fmt.Fprintf(os.Stderr, "intent drafter: %s credential_source=%s", receipt.Drafter, receipt.CredentialSource)
		if receipt.Model != "" {
			fmt.Fprintf(os.Stderr, " model=%s", receipt.Model)
		}
		if receipt.DirectAPIEnvironmentIgnored {
			fmt.Fprintf(os.Stderr, " direct_api_environment_ignored=true")
		}
		fmt.Fprintln(os.Stderr)
		dr = coldsource.LLMIntentDrafter{Client: client, Max: maxN}
	default:
		fmt.Fprintf(os.Stderr, "error: unknown drafter %q (use echo|llm|claude-cli|codex-cli|auto)\n", drafter)
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
