// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/extractor/coldsource"
	"github.com/globulario/sensei/golang/extractor/protoscan"
	"github.com/globulario/sensei/golang/propose"
)

// runOnboard is the zero-hand-authoring onboarding entry point (Pillar 1.3). It
// is deliberately agent-agnostic: rather than call a model itself, it EXPORTS a
// self-contained brief (the repo's architecture + the candidate schema + a
// drafting prompt) that the client's own agent — Claude, Codex, Cursor, a CI
// bot — drafts against, then IMPORTS those drafts into the candidate review
// queue (docs/awareness/candidates/proposals/) after the same contract-first
// validation the Propose RPC uses. Nothing lands live; a human promotes.
//
// It also supports an opt-in enrichment path: `--drafter llm`, `claude-cli`, or
// `codex-cli`
// runs the LLM against the very same brief `export` emits and lands the drafted
// candidates through the identical validator + review queue that `import` uses —
// so the model is a drop-in for the client's own agent, with AWG as validator.
// The default (`--drafter none`) keeps export keyless and byte-for-byte the same.
//
//	sensei onboard export [--repo .] [--out brief.md]         # brief for your agent
//	sensei onboard export [--repo .] --drafter auto [--max N] # draft candidates directly
//	sensei onboard import [--repo .] [--from drafts.json|-]   # land drafts as candidates
func runOnboard(args []string) int {
	mode := "export"
	if len(args) > 0 && (args[0] == "export" || args[0] == "import") {
		mode, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("sensei onboard "+mode, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", ".", "repository to onboard")
	out := fs.String("out", "", "export: write the brief here (default: stdout)")
	from := fs.String("from", "-", "import: read the agent's drafted candidates (JSON) here ('-' = stdin)")
	drafter := fs.String("drafter", "none", "export enrichment: none (brief only, no key) | auto | llm (needs ANTHROPIC_API_KEY/AUTH_TOKEN) | claude-cli (authed Claude CLI, no key) | codex-cli (authed Codex CLI, no key)")
	model := fs.String("model", "", "drafter LLM model override (default "+coldsource.DefaultModel+")")
	maxN := fs.Int("max", 15, "drafter: max candidates to propose")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei onboard [export|import] [flags]

Zero-hand-authoring onboarding. export builds a self-contained brief (your repo's
architecture + the candidate schema + a drafting prompt) for the AI agent of your
choice; import validates the agent's drafted candidates and writes them to the
docs/awareness/candidates/proposals/ review queue. Nothing goes live — promote
the good ones with a human/CI step.

Enrichment (opt-in): export --drafter auto runs an authenticated drafter against
that same brief and lands the drafted candidates through the identical validator,
skipping the manual hand-off. --drafter none (default) requires no credential.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard: %v\n", err)
		return 1
	}
	if mode == "import" {
		return onboardImport(root, *from)
	}
	if d := strings.TrimSpace(*drafter); d != "" && d != "none" {
		return onboardDraft(root, d, *model, *maxN)
	}
	return onboardExport(root, *out)
}

// onboardExport builds the agent brief from a deterministic structural scan and
// writes it to stdout or --out.
func onboardExport(root, outPath string) int {
	data := []byte(buildOnboardBrief(root))
	if strings.TrimSpace(outPath) == "" {
		os.Stdout.Write(data)
		return 0
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard export: write %s: %v\n", outPath, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "sensei onboard: wrote brief to %s — hand it to your agent, then `sensei onboard import`\n", outPath)
	return 0
}

// buildOnboardBrief renders the self-contained drafting brief (architecture +
// candidate schema + task) from a deterministic structural scan. It is both the
// human-facing export and the prompt the --drafter llm path sends to the model,
// so the two paths propose against identical grounding.
func buildOnboardBrief(root string) string {
	comps := extractComponents(root)
	highRisk := readHighRiskPrefixes(root)
	var contractCount int
	if protoFiles, _ := protoscan.FindProtoFiles(root); len(protoFiles) > 0 {
		for _, pf := range protoFiles {
			if cs, perr := protoscan.ScanProto(pf, root, nil); perr == nil {
				contractCount += len(cs)
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# AWG onboarding brief — %s\n\n", filepath.Base(root))
	b.WriteString("You are proposing a STARTER set of awareness rules for this repository. ")
	b.WriteString("Ground every rule in the architecture below — do not invent facts. ")
	b.WriteString("Output a JSON array of candidate objects matching the schema, then a human runs `sensei onboard import`.\n\n")

	b.WriteString("## Architecture (deterministic scan)\n\n")
	fmt.Fprintf(&b, "- components: %d\n- proto contracts: %d\n- high-risk path prefixes: %d\n\n", len(comps), contractCount, len(highRisk))
	if len(comps) > 0 {
		b.WriteString("### Components (name — representative files)\n")
		sort.Slice(comps, func(i, j int) bool { return comps[i].Name < comps[j].Name })
		for _, c := range comps {
			files := c.SourceFiles
			if len(files) > 3 {
				files = files[:3]
			}
			fmt.Fprintf(&b, "- **%s** (%s) — %s\n", c.Name, c.Kind, strings.Join(files, ", "))
		}
		b.WriteString("\n")
	}
	if len(highRisk) > 0 {
		b.WriteString("### High-risk paths (edits here cost the most — prioritize invariants covering these)\n")
		for _, p := range highRisk {
			fmt.Fprintf(&b, "- %s\n", p)
		}
		b.WriteString("\n")
	}

	b.WriteString(candidateSchemaSection())

	b.WriteString("## Your task\n\n")
	b.WriteString("Propose 5–15 high-value starter rules grounded in the architecture above, favoring the high-risk paths. ")
	b.WriteString("Prefer invariants (rules that must hold) and the failure_modes they guard. ")
	b.WriteString("Every entry MUST be contract-first: link a related_invariant/related_failure or set `contract`. ")
	b.WriteString("Output ONLY a JSON array of candidate objects (the schema above), no prose. ")
	b.WriteString("Then a human runs: `sensei onboard import --from your-drafts.json`.\n")

	return b.String()
}

// candidateSchemaSection describes the propose.Request shape + the contract-first
// rules, kept in prose so the drafting agent knows exactly what to emit.
func candidateSchemaSection() string {
	var b strings.Builder
	b.WriteString("## Candidate schema (JSON array of these)\n\n")
	b.WriteString("Each object: `{ \"kind\", \"title\", \"description\", \"severity\", \"source_files\": [], ")
	b.WriteString("\"related_invariants\": [], \"related_failures\": [], \"required_tests\": [], ")
	b.WriteString("\"forbidden_fixes\": [], \"evidence\": [], \"contract\" }`.\n\n")
	b.WriteString("kind is one of: " + strings.Join(propose.Kinds(), ", ") + ".\n\n")
	b.WriteString("Contract-first rules (enforced on import):\n")
	b.WriteString("- **invariant**: needs ≥1 `source_files` and a `related_failure`/`forbidden_fix`/`required_test`.\n")
	b.WriteString("- **failure_mode**: needs a `related_invariant` (or `contract`) and `evidence` (or `required_test`).\n")
	b.WriteString("- **forbidden_fix**: needs a `related_invariant` (or `contract`) and a `description` of why it's forbidden.\n")
	b.WriteString("- **required_test**: needs `id` as `path/to/file_test.go:TestName` and something it protects.\n")
	b.WriteString("- **contract_unknown**: needs `description`, `evidence`, and a `proposed_contract`.\n\n")
	return b.String()
}

// onboardImport validates the agent's drafted candidates and writes the valid
// ones to the candidate review queue, using the same validator + renderer as the
// Propose RPC. Invalid drafts are reported, not written.
func onboardImport(root, fromPath string) int {
	raw, err := readOnboardDrafts(fromPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard import: %v\n", err)
		return 2
	}
	var drafts []propose.Request
	if err := json.Unmarshal(raw, &drafts); err != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard import: parse drafts JSON (expected an array of candidate objects): %v\n", err)
		return 2
	}
	if len(drafts) == 0 {
		fmt.Fprintln(os.Stderr, "sensei onboard import: no candidates in the drafts")
		return 1
	}

	accepted, rejected, werr := writeOnboardCandidates(root, drafts)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard import: %v\n", werr)
		return 1
	}

	fmt.Printf("\nonboard import: %d accepted, %d rejected.\n", accepted, rejected)
	if accepted > 0 {
		fmt.Println("Review the proposals in docs/awareness/candidates/proposals/ and promote the good ones into the corpus.")
	}
	if rejected > 0 && accepted == 0 {
		return 1
	}
	return 0
}

// writeOnboardCandidates validates each drafted request with the same
// contract-first validator the Propose RPC uses and writes the accepted ones to
// the candidate review queue (docs/awareness/candidates/proposals/). Rejections
// are reported (not written). It is shared by the human-agent import path and
// the --drafter llm enrichment path so the model and a human go through one gate.
func writeOnboardCandidates(root string, drafts []propose.Request) (accepted, rejected int, err error) {
	awarenessDir := filepath.Join(root, "docs", "awareness")
	for i := range drafts {
		propose.Normalize(&drafts[i])
		if errs := propose.Validate(drafts[i]); len(errs) > 0 {
			rejected++
			fmt.Printf("REJECTED [%s] %q:\n", drafts[i].Kind, drafts[i].Title)
			for _, e := range errs {
				fmt.Printf("    - %s\n", e)
			}
			continue
		}
		cand, rerr := propose.RenderCandidate(drafts[i])
		if rerr != nil {
			rejected++
			fmt.Printf("REJECTED [%s] %q: render: %v\n", drafts[i].Kind, drafts[i].Title, rerr)
			continue
		}
		dest := filepath.Join(awarenessDir, filepath.FromSlash(cand.RelPath))
		if e := os.MkdirAll(filepath.Dir(dest), 0o755); e != nil {
			return accepted, rejected, e
		}
		if e := os.WriteFile(dest, cand.Content, 0o644); e != nil {
			return accepted, rejected, fmt.Errorf("write %s: %w", dest, e)
		}
		accepted++
		fmt.Printf("candidate  %s\n", cand.RelPath)
	}
	return accepted, rejected, nil
}

// onboardDraft is the opt-in enrichment path: it selects an LLM backend, runs it
// against the export brief, and lands the drafted candidates through the same
// validator + queue as import. It fails clearly (exit 2) when the requested
// backend has no credential — never a silent fallback.
func onboardDraft(root, drafter, model string, maxN int) int {
	client, code := selectOnboardClient(drafter, model)
	if client == nil {
		return code
	}
	return onboardDraftWith(root, client, maxN)
}

// selectOnboardClient mirrors intent-mine / cold-bootstrap through the shared
// backend selector. Explicit backends fail clear; auto prefers Claude CLI, then
// direct API, then fails clear.
func selectOnboardClient(drafter, model string) (coldsource.LLMClient, int) {
	switch drafter {
	case "llm", "claude-cli", "codex-cli", "auto":
		client, _, err := coldsource.SelectLLMClient(coldsource.DrafterBackend(drafter), model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei onboard: %v\n", err)
			return nil, 2
		}
		return client, 0
	default:
		fmt.Fprintf(os.Stderr, "sensei onboard: unknown --drafter %q (use none|auto|llm|claude-cli|codex-cli)\n", drafter)
		return nil, 2
	}
}

// onboardDraftWith is the construct-free core of onboardDraft, taking an injected
// LLMClient so tests can drive it with a deterministic fake (no network).
func onboardDraftWith(root string, client coldsource.LLMClient, maxN int) int {
	brief := buildOnboardBrief(root)
	raw, err := coldsource.DraftOnboardingCandidates(context.Background(), client, brief, maxN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard --drafter: %v\n", err)
		return 1
	}
	var drafts []propose.Request
	if err := json.Unmarshal(raw, &drafts); err != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard --drafter: parse drafted candidates: %v\n", err)
		return 1
	}
	if len(drafts) == 0 {
		fmt.Fprintln(os.Stderr, "sensei onboard --drafter: model proposed no candidates")
		return 1
	}
	accepted, rejected, werr := writeOnboardCandidates(root, drafts)
	if werr != nil {
		fmt.Fprintf(os.Stderr, "sensei onboard --drafter: %v\n", werr)
		return 1
	}
	fmt.Printf("\nonboard draft: %d accepted, %d rejected (of %d proposed).\n", accepted, rejected, len(drafts))
	if accepted > 0 {
		fmt.Println("Review the proposals in docs/awareness/candidates/proposals/ and promote the good ones into the corpus.")
	}
	if accepted == 0 {
		return 1
	}
	return 0
}

func readOnboardDrafts(path string) ([]byte, error) {
	if path == "-" || path == "" {
		return os.ReadFile("/dev/stdin")
	}
	return os.ReadFile(path)
}
