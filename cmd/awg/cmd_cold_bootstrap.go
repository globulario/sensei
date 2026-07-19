// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/extractor/coldsource"
)

// shallowHistoryNote returns a clear, one-line explanation when the repo is a
// shallow clone — the common cause of the cryptic "exit status 128" the
// commit-channel extractors emit (HEAD~200 doesn't exist under `git clone
// --depth 1`). Returns "" for a normal full-history repo.
func shallowHistoryNote(repo string) string {
	out, err := exec.Command("git", "-C", repo, "rev-parse", "--is-shallow-repository").Output()
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		return "shallow clone detected — commit-history mining needs full history. " +
			"Run `git fetch --unshallow` (or clone without --depth) to enable revert/conventional-commit signals; skipping them for now."
	}
	return ""
}

// runColdBootstrap is the cold-source / history-signal candidate MINER — it
// learns from a repository's scars. It is NOT the product bootstrap command:
// `sensei bootstrap` is the production repo-initialization path and may call this
// miner as one optional candidate stage. This drafts awareness candidates from
// two deterministic cold signals (revert/regression commits + PR review
// comments), triangulates them, and emits status:candidate YAML under
// docs/awareness/candidates/. It NEVER promotes, never edits active knowledge,
// and never touches the promotion gate.
func runColdBootstrap(args []string) int {
	fs := flag.NewFlagSet("sensei cold-bootstrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := "."
	fs.StringVar(&repo, "path", ".", "path to the target git repo working tree")
	fs.StringVar(&repo, "repo", ".", "deprecated alias for --path")
	since := fs.String("since", "", "git range to scan, e.g. v1.0.0..HEAD (default HEAD~200..HEAD)")
	out := fs.String("out", "", "output dir for candidate YAML (default: <repo>/docs/awareness/candidates)")
	dryRun := fs.Bool("dry-run", false, "print the scoring report without writing candidates")
	maxN := fs.Int("max", 10, "bound: emit at most N top-ranked candidates")
	prFile := fs.String("pr-comments", "", "offline JSON file of PR review comments (replaces gh)")
	repoSlug := fs.String("repo-slug", "", "owner/name for gh PR review fetch (omit to skip PR extraction)")
	drafterName := fs.String("drafter", "echo", "candidate drafter: echo (deterministic default, no LLM) | llm (ANTHROPIC_API_KEY) | claude-cli (authed Claude CLI, no key) | codex-cli (authed Codex CLI, no key) | auto")
	model := fs.String("model", "", "LLM model override (default depends on selected backend)")
	bundlesOut := fs.String("bundles-out", "", "with --drafter export: write the bundle envelope here (default: stdout)")
	autoWindow := fs.Bool("auto-window", false, "plan the revert-scan window automatically: widen (bounded) until enough revert/regression signals are found; overrides --since. Never scans full history.")
	awTarget := fs.Int("auto-window-target", coldsource.DefaultWindowTargetReverts, "auto-window: stop widening once this many revert/regression commits are in the window")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei cold-bootstrap --path <checkout> [--since <range>] [--out <dir>] [--dry-run] [--max N]
                        [--pr-comments <file.json> | --repo-slug owner/name]

Drafts awareness candidates from cold day-0 signals (revert/regression commits +
PR review comments). Triangulates (>=2 distinct source types), enforces a
citation contract, bounds to the top N, and writes status:candidate YAML only.
Never promotes; never changes active graph.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	warnDeprecatedRepoPathAlias(fs, "cold-bootstrap")
	warnIfDomainLikeExtractorPath("cold-bootstrap", repo)

	// Default the candidate output UNDER the target repo, not the current working
	// directory — otherwise running the miner from an unrelated checkout writes the
	// scanned repo's candidates into the CWD repo. Every other extractor
	// (extract-authority, bootstrap, intent-mine) anchors output to <repo>.
	if strings.TrimSpace(*out) == "" {
		*out = filepath.Join(repo, "docs", "awareness", "candidates")
	}

	// Bound guard: a non-positive cap would disable the top-N limit and could
	// flood the repo with candidate files. Clamp to the default instead.
	if *maxN <= 0 {
		fmt.Fprintf(os.Stderr, "note: --max=%d is not positive; clamping to 10 to keep output bounded\n", *maxN)
		*maxN = 10
	}

	// Auto-window planning: widen the revert-scan window (bounded by the safe-max
	// ladder) until enough revert/regression signals are present, then report and
	// apply the selected range. This is dry-run planning over commit metadata only
	// — it never fetches PR comments and never scans full history.
	var windowPlanNote string
	if *autoWindow {
		plan, perr := coldsource.PlanWindow(repo, coldsource.DefaultWindowLadder, *awTarget)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "warn: auto-window planning failed (%v); falling back to --since\n", perr)
		} else if plan.Range == "" {
			fmt.Fprintf(os.Stderr, "note: auto-window found history shorter than the smallest window; using --since/default\n")
		} else {
			density := 0.0
			if plan.Scanned > 0 {
				density = 100 * float64(plan.Reverts) / float64(plan.Scanned)
			}
			status := "target met"
			if !plan.HitTarget {
				status = fmt.Sprintf("scar-sparse: only %d reverts at safe-max", plan.Reverts)
			}
			windowPlanNote = fmt.Sprintf("auto %s — %d reverts in %d commits (%.1f%% scar density; %s)",
				plan.Range, plan.Reverts, plan.Depth, density, status)
			fmt.Fprintf(os.Stderr, "auto-window: selected %s\n", windowPlanNote)
			*since = plan.Range
		}
	}

	// Select the drafter. The LLM drafter is OPT-IN and fails clearly when no
	// API key is present — never a silent fallback to echo.
	var drafter coldsource.Drafter
	var drafterLabel string
	switch *drafterName {
	case "echo", "":
		drafter, drafterLabel = coldsource.EchoDrafter{}, "echo"
	case "llm", "claude-cli", "codex-cli", "auto":
		client, receipt, cerr := coldsource.SelectLLMClient(coldsource.DrafterBackend(*drafterName), *model)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", cerr)
			return 2
		}
		drafter, drafterLabel = coldsource.LLMDrafter{Client: client}, receipt.Label()
	case "export":
		// Handled after triangulation: exports bundles, no drafting/validation/emit.
		drafterLabel = "export"
	case "stdin":
		// Session/manual drafter: candidates come from stdin keyed by bundle_id.
		// They flow through the SAME cage as echo/llm; binding is enforced because
		// bundles are re-extracted live below and matched by bundle_id.
		drafts, perr := coldsource.ParseSubmittedDrafts(os.Stdin)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "error: read drafts from stdin: %v\n", perr)
			return 2
		}
		drafter, drafterLabel = coldsource.NewStdinDrafter(drafts), "stdin"
	default:
		fmt.Fprintf(os.Stderr, "error: unknown --drafter %q (use echo|llm|claude-cli|codex-cli|auto|export|stdin)\n", *drafterName)
		return 2
	}

	rep := coldsource.Report{Range: *since, Drafter: drafterLabel, Cap: *maxN, DryRun: *dryRun, WindowPlan: windowPlanNote}
	if rep.Range == "" {
		rep.Range = "HEAD~200..HEAD"
	}

	// ── Extract (deterministic) ────────────────────────────────────────────
	var signals []coldsource.ColdSignal
	var err error

	// A shallow clone can't satisfy the commit range (HEAD~200), which surfaces
	// as a cryptic "exit status 128" from each extractor. Detect it once and
	// explain it, then skip the commit-channel extractors that would just fail.
	shallowNote := shallowHistoryNote(repo)
	if shallowNote != "" {
		fmt.Fprintf(os.Stderr, "note: %s\n", shallowNote)
	}

	if shallowNote == "" {
		revertSignals, rerr := coldsource.LoadRevertSignals(repo, *since)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "warn: revert extraction failed for range %s (continuing): %v\n", *since, rerr)
		}
		signals = append(signals, revertSignals...)

		// Conventional fix/perf/refactor commits — the WEAK commit-channel signal
		// for repos where explicit reverts are sparse (e.g. TypeScript projects
		// using conventional-commits + squash-merge). On their own they never
		// triangulate; they only corroborate with a review-channel signal.
		convSignals, cerr := coldsource.LoadConventionalSignals(repo, *since)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "warn: conventional-commit extraction failed for range %s (continuing): %v\n", *since, cerr)
		}
		signals = append(signals, convSignals...)
	}

	// PR review comments feed TWO review-channel extractors over the same input:
	// per-comment rules (ExtractPRReviews) and high-engagement threads
	// (ExtractReviewThreads).
	var comments []coldsource.ReviewComment
	switch {
	case *prFile != "":
		comments, err = coldsource.LoadPRComments(*prFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: PR comment file failed (continuing): %v\n", err)
		}
	case *repoSlug != "":
		comments, err = coldsource.LoadPRCommentsFromSlug(*repoSlug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: gh PR review fetch failed (continuing): %v\n", err)
		}
	default:
		fmt.Fprintln(os.Stderr, "note: no --pr-comments or --repo-slug — PR review extraction skipped; "+
			"single-channel themes will all be held back (triangulation needs a commit AND a review signal)")
	}
	signals = append(signals, coldsource.ExtractPRReviews(comments)...)
	signals = append(signals, coldsource.ExtractReviewThreads(comments)...)
	rep.TotalSignals = len(signals)

	// ── Triangulate (deterministic) ────────────────────────────────────────
	eligible, heldBack := coldsource.Triangulate(signals)
	rep.Themes = len(eligible) + len(heldBack)
	rep.TriangulatedThemes = len(eligible)
	rep.HeldBackSingleSource = len(heldBack)

	// ── Export mode: write bundles for external/session drafting, then exit ──
	// No drafting, no validation, no candidates, no mutation. Bounded to top-N.
	if *drafterName == "export" {
		bundles := eligible
		if *maxN > 0 && len(bundles) > *maxN {
			bundles = bundles[:*maxN]
		}
		data, merr := json.MarshalIndent(coldsource.NewExportEnvelope(bundles), "", "  ")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "error: marshal export: %v\n", merr)
			return 1
		}
		if *bundlesOut == "" {
			fmt.Println(string(data))
		} else {
			if werr := os.WriteFile(*bundlesOut, append(data, '\n'), 0o644); werr != nil {
				fmt.Fprintf(os.Stderr, "error: write %s: %v\n", *bundlesOut, werr)
				return 1
			}
			fmt.Fprintf(os.Stderr, "wrote %d bundle(s) to %s\n", len(bundles), *bundlesOut)
		}
		return 0
	}

	// ── Draft + contract + citation check ──────────────────────────────────
	git := coldsource.NewGitVerifier(repo)
	ctx := context.Background()

	var admissible []*extractor.PromotionProposal
	seen := map[string]bool{}
	grounded := map[string]coldsource.Grounding{}
	for _, b := range eligible {
		rep.Drafted++
		p, derr := drafter.Draft(ctx, b)
		if derr != nil {
			var de coldsource.DraftError
			errors.As(derr, &de)
			switch de.Kind {
			case "no_draft_supplied":
				// stdin: this triangulated bundle had no submitted draft. Expected
				// when drafting a subset; also the binding guard — a draft bound to
				// a different/stale bundle_id never matches a live bundle.
				rep.SkippedNoDraft++
			case "no_evidence", "untriangulated":
				rep.RejectedMissingCitation++
				fmt.Fprintf(os.Stderr, "draft %s: %v\n", b.ThemeKey, derr)
			default: // malformed, bad_class, llm_error
				rep.RejectedMalformed++
				fmt.Fprintf(os.Stderr, "draft %s: %v\n", b.ThemeKey, derr)
			}
			continue
		}
		// Citation enforcement — exactly as hard as for the echo drafter:
		// every cited source must be present in the bundle, and resolve on disk/git.
		if violations := coldsource.ValidateDraft(p, b); len(violations) > 0 {
			rep.RejectedMissingCitation++
			continue
		}
		if shallow, _ := coldsource.IsShallow(p, b); shallow {
			rep.RejectedShallow++
			continue
		}
		// GROUND: tier each citation's provenance against the tree, then gate on
		// the candidate's overall tier (test_encoded > landed_commit >
		// review_suggestion > unresolved). Accept only at >= landed_commit;
		// review-only is segregated as a lead, unresolved is rejected.
		g := coldsource.GroundCandidate(p, repo, git)
		switch g.Overall {
		case coldsource.TierUnresolved:
			rep.RejectedUnresolved++
			continue
		case coldsource.TierReviewSuggestion:
			rep.SegregatedReviewOnly++
			rep.ReviewOnlyLeads = append(rep.ReviewOnlyLeads, coldsource.CandidateSummary{
				CandidateID: p.CandidateID, Class: p.CandidateClass, Theme: p.Theme,
				Confidence: p.Confidence, Citations: p.SourcePaths, Reason: p.Reason,
				Tier: g.Overall.String(),
			})
			continue
		}
		if seen[p.CandidateID] {
			rep.RejectedDuplicate++
			continue
		}
		seen[p.CandidateID] = true
		grounded[p.CandidateID] = g
		admissible = append(admissible, p)
	}

	// ── Rank + bound (top-N) ───────────────────────────────────────────────
	sort.SliceStable(admissible, func(i, j int) bool {
		if ci, cj := confRank(admissible[i].Confidence), confRank(admissible[j].Confidence); ci != cj {
			return ci > cj
		}
		if li, lj := len(admissible[i].SourcePaths), len(admissible[j].SourcePaths); li != lj {
			return li > lj
		}
		return admissible[i].Theme < admissible[j].Theme
	})
	if *maxN > 0 && len(admissible) > *maxN {
		rep.Dropped = len(admissible) - *maxN
		admissible = admissible[:*maxN]
	}

	// ── Emit (unless dry-run) + report ─────────────────────────────────────
	for _, p := range admissible {
		g := grounded[p.CandidateID]
		rep.Candidates = append(rep.Candidates, coldsource.CandidateSummary{
			CandidateID: p.CandidateID, Class: p.CandidateClass, Theme: p.Theme,
			Confidence: p.Confidence, Citations: p.SourcePaths, Reason: p.Reason,
			Tier: g.Overall.String(), Drift: g.Drifted, SymbolMismatch: g.SymbolMismatch,
		})
		if *dryRun {
			continue
		}
		path, eerr := coldsource.EmitCandidate(*out, p)
		if eerr != nil {
			fmt.Fprintf(os.Stderr, "warn: emit %s failed: %v\n", p.CandidateID, eerr)
			continue
		}
		rep.CandidatesEmitted++
		fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	}

	coldsource.RenderReport(os.Stdout, rep)
	return 0
}

func confRank(c string) int {
	switch c {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
