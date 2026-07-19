// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=cli.propose
// @awareness file_role=typed_graph_feedback_writer
// @awareness risk=high
//
// sensei propose is the typed write path for the awareness graph. It is a THIN
// ADAPTER over the reusable governed-mutation owner
// (golang/architecture/governedmutation): it parses input, delegates the
// complete mutation policy (kind routing, schema validation, canonical ID +
// collision/contradiction rules, manifest CAS, atomic write) to that owner, then
// rebuilds the seed, reloads the local Oxigraph store, and stages the touched
// files — but it NEVER commits and holds NO mutation policy of its own. The
// durable authority stays the YAML sources + generated artifacts + a
// human-reviewed git commit.
//
// This is deliberately a local CLI command, not an MCP/gRPC surface: the MCP
// bridge exposes read-only safe tools (briefing/impact/query/resolve), and the
// graph service must never silently mutate durable truth. Mutation lives behind
// this explicit, diffable, human-committed step, and its policy lives in the
// production owner that the promotion transaction also calls.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/propose"
)

// ProposeRequest is the shared feedback-entry type. It aliases propose.Request
// so the CLI and the server's Propose RPC validate identical input through the
// same code — there is exactly one validator, no drifting copy.
type ProposeRequest = propose.Request

// ProposeResult is the structured outcome returned to the caller.
type ProposeResult struct {
	Status           string   `json:"status"` // created | duplicate | validation_failed | dry_run
	Kind             string   `json:"kind"`
	NodeIDs          []string `json:"node_ids"`
	FilesChanged     []string `json:"files_changed"`
	Validation       string   `json:"validation"` // ok | errors
	ValidationErrors []string `json:"validation_errors,omitempty"`
	Reload           string   `json:"reload"` // ok | skipped | failed | n/a
	ReloadDetail     string   `json:"reload_detail,omitempty"`
	DiffSummary      string   `json:"diff_summary,omitempty"`
	NextCommand      string   `json:"next_command,omitempty"`
	Note             string   `json:"note,omitempty"`
}

// proposeRebuild is the rebuild entry point. It is a package var so tests can
// stand in a stub and assert the yaml2nt/loadnt pipeline is invoked with the
// expected arguments without touching Oxigraph.
var proposeRebuild = runRebuild

// proposeOptions controls side effects (file targeting, rebuild, staging).
type proposeOptions struct {
	targetRepo  string
	agRepo      string
	svcRepo     string
	dryRun      bool
	noRebuild   bool
	noStage     bool
	oxigraphURL string
}

func runPropose(args []string) int {
	fs := flag.NewFlagSet("sensei propose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	jsonPath := fs.String("json", "", "read the ProposeRequest from this JSON file (use '-' for stdin)")
	kind := fs.String("kind", "", "entry kind: failure_mode | invariant | required_test | forbidden_fix | decision | contract_unknown")
	id := fs.String("id", "", "stable id (derived from kind+title when omitted; required for required_test)")
	title := fs.String("title", "", "short title")
	description := fs.String("description", "", "what happened / what the entry documents")
	severity := fs.String("severity", "", "critical | high | warning (where applicable)")
	status := fs.String("status", "", "record status override (used by decision proposals; default: accepted)")
	context := fs.String("context", "", "decision: context for the architectural record")
	consequences := fs.String("consequences", "", "decision: consequences of the architectural record")
	architecturalPlane := fs.String("architectural-plane", "", "governed architectural plane for decision proposals: desired | intended | historical")
	repo := fs.String("repo", "", "repo the feedback belongs to")
	domain := fs.String("domain", "", "domain scope, e.g. github.com/globulario/sensei")
	contract := fs.String("contract", "", "the contract that was violated or clarified")
	proposedContract := fs.String("proposed-contract", "", "contract_unknown: the contract you propose")
	revisionRequest := fs.String("revision-request", "", "contract_unknown: a request to revise an existing contract")

	var sourceFiles, relatedInvariants, relatedFailures multiString
	var requiredTests, forbiddenFixes, evidence multiString
	var definesBoundaries, definesContracts, affectsComponents, supportedEvidence multiString
	fs.Var(&sourceFiles, "source-file", "source file the entry anchors to (repeatable)")
	fs.Var(&relatedInvariants, "related-invariant", "related invariant id (repeatable)")
	fs.Var(&relatedFailures, "related-failure", "related failure_mode id (repeatable)")
	fs.Var(&requiredTests, "required-test", "required test ref, file.go:TestName (repeatable)")
	fs.Var(&forbiddenFixes, "forbidden-fix", "forbidden fix id or note (repeatable)")
	fs.Var(&evidence, "evidence", "evidence line (repeatable)")
	fs.Var(&definesBoundaries, "defines-boundary", "decision: boundary id defined by the decision (repeatable)")
	fs.Var(&definesContracts, "defines-contract", "decision: contract id defined by the decision (repeatable)")
	fs.Var(&affectsComponents, "affects-component", "decision: component id affected by the decision (repeatable)")
	fs.Var(&supportedEvidence, "supported-evidence", "decision: evidence id supporting the decision (repeatable)")

	dryRun := fs.Bool("dry-run", false, "validate and render only; do not modify files")
	noRebuild := fs.Bool("no-rebuild", false, "append YAML but skip rebuild/reload")
	noStage := fs.Bool("no-stage", false, "do not 'git add' the touched files")
	targetRepoFlag := fs.String("target-repo", "", "repo whose docs/awareness/ receives the entry (default: awareness-graph repo)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	oxigraphURL := fs.String("oxigraph-url", defaultOxigraphStoreURL(), "Oxigraph Graph Store endpoint for reload")
	format := fs.String("format", "text", "output format: text | json")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei propose --json <file|-> [flags]
       sensei propose --kind <kind> --title ... [flags]

Append one typed feedback entry to the awareness graph YAML sources, rebuild
the seed, reload the local store, and stage the change. Never commits.

Kinds:
  failure_mode      a way the system breaks (links an invariant it violates)
  invariant         a rule that must hold (anchors source files; names tests)
  required_test     a test that proves behavior (protects invariants/failures)
  forbidden_fix     a repair that must never be applied again
  decision          an architectural decision record on the canonical decisions surface
  contract_unknown  queued feedback whose contract is not yet known
                    (requires --proposed-contract or --revision-request)

Contract-first rule: every entry must connect to a contract — link an
invariant/failure_mode (or pass --contract). Vague notes are rejected.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Base request: JSON if provided, else empty; flags override set fields.
	var req ProposeRequest
	if *jsonPath != "" {
		raw, err := readProposeJSON(*jsonPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei propose: %v\n", err)
			return 2
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			fmt.Fprintf(os.Stderr, "sensei propose: parse json: %v\n", err)
			return 2
		}
	}
	applyProposeFlags(&req, fs, map[string]func(){
		"kind":                func() { req.Kind = *kind },
		"id":                  func() { req.ID = *id },
		"title":               func() { req.Title = *title },
		"description":         func() { req.Description = *description },
		"severity":            func() { req.Severity = *severity },
		"status":              func() { req.Status = *status },
		"context":             func() { req.Context = *context },
		"consequences":        func() { req.Consequences = *consequences },
		"architectural-plane": func() { req.ArchitecturalPlane = *architecturalPlane },
		"repo":                func() { req.Repo = *repo },
		"domain":              func() { req.Domain = *domain },
		"contract":            func() { req.Contract = *contract },
		"proposed-contract":   func() { req.ProposedContract = *proposedContract },
		"revision-request":    func() { req.RevisionRequest = *revisionRequest },
		"source-file":         func() { req.SourceFiles = append(req.SourceFiles, sourceFiles...) },
		"related-invariant":   func() { req.RelatedInvariants = append(req.RelatedInvariants, relatedInvariants...) },
		"related-failure":     func() { req.RelatedFailures = append(req.RelatedFailures, relatedFailures...) },
		"required-test":       func() { req.RequiredTests = append(req.RequiredTests, requiredTests...) },
		"forbidden-fix":       func() { req.ForbiddenFixes = append(req.ForbiddenFixes, forbiddenFixes...) },
		"evidence":            func() { req.Evidence = append(req.Evidence, evidence...) },
		"defines-boundary":    func() { req.DefinesBoundaries = append(req.DefinesBoundaries, definesBoundaries...) },
		"defines-contract":    func() { req.DefinesContracts = append(req.DefinesContracts, definesContracts...) },
		"affects-component":   func() { req.AffectsComponents = append(req.AffectsComponents, affectsComponents...) },
		"supported-evidence":  func() { req.SupportedEvidence = append(req.SupportedEvidence, supportedEvidence...) },
	})

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	if agRepo == "" {
		// Final fallback: the project root we are standing in.
		if root, rerr := resolveProjectRoot(""); rerr == nil {
			agRepo = root
		}
	}

	opt := proposeOptions{
		targetRepo:  *targetRepoFlag,
		agRepo:      agRepo,
		svcRepo:     svcRepo,
		dryRun:      *dryRun,
		noRebuild:   *noRebuild,
		noStage:     *noStage,
		oxigraphURL: *oxigraphURL,
	}

	res, code := applyProposal(&req, opt)
	printProposeResult(res, *format)
	return code
}

// readProposeJSON reads a ProposeRequest payload from a file or stdin ("-").
func readProposeJSON(path string) ([]byte, error) {
	if path == "-" {
		return readAll(os.Stdin)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// applyProposeFlags runs the setter for each flag the user actually passed, so
// flags override the JSON base only when explicitly present.
func applyProposeFlags(_ *ProposeRequest, fs *flag.FlagSet, setters map[string]func()) {
	fs.Visit(func(f *flag.Flag) {
		if set, ok := setters[f.Name]; ok {
			set()
		}
	})
}

// ── adapter: delegate the mutation to the governed-mutation owner ───────────

// applyProposal is the thin adapter over governedmutation. It resolves the
// target repo, delegates the complete mutation policy to the owner, and adds the
// propose-specific side effects (rebuild, stage) and the diff/commit rendering.
// It performs NO routing, schema, ID, collision, replay, manifest-CAS, or write
// policy of its own.
func applyProposal(req *ProposeRequest, opt proposeOptions) (ProposeResult, int) {
	normalizeProposeRequest(req)
	res := ProposeResult{Kind: req.Kind}

	target := opt.targetRepo
	if target == "" {
		target = opt.agRepo
	}
	if target == "" {
		res.Status = "validation_failed"
		res.Validation = "errors"
		res.ValidationErrors = []string{"cannot locate target repo (pass --target-repo or run inside the awareness-graph checkout)"}
		return res, 1
	}

	owned := governedmutation.Request{RepositoryRoot: target, Proposal: *req}

	// Plan (validate + route + classify) without writing.
	plan, err := governedmutation.Plan(owned)
	if err != nil {
		return mapMutationError(res, err)
	}
	res.Validation = "ok"
	res.NodeIDs = []string{plan.CanonicalID}
	relPath := plan.TargetRelPath

	// Deterministic duplicate handling: an exact replay (same id + equivalent
	// body) changes nothing.
	if plan.Disposition == governedmutation.DispositionReplay {
		res.Status = "duplicate"
		res.Reload = "n/a"
		res.Note = fmt.Sprintf("id %q already exists in %s — no file changed", plan.CanonicalID, relPath)
		return res, 0
	}

	if opt.dryRun {
		res.Status = "dry_run"
		res.Reload = "skipped"
		res.DiffSummary = fmt.Sprintf("would append to %s under %q:\n%s", relPath, plan.TopKey, plan.Preview)
		res.NextCommand = "(dry-run — re-run without --dry-run to write)"
		return res, 0
	}

	if !plan.IsCandidate && !opt.noRebuild {
		if err := ensureCrossRepoRebuildPrereqs(opt.agRepo, opt.svcRepo); err != nil {
			res.Status = "validation_failed"
			res.Validation = "errors"
			res.ValidationErrors = []string{err.Error()}
			return res, 1
		}
	}

	// Hold the single repository governed-mutation lock across the mutation (and
	// the rebuild), mirroring the promotion owner's continuous-hold discipline.
	release, lerr := governedmutation.AcquireLock(context.Background(), target, "propose", time.Now())
	if lerr != nil {
		res.Status = "validation_failed"
		res.Validation = "errors"
		res.ValidationErrors = []string{fmt.Sprintf("acquire governed-mutation lock: %v", lerr)}
		return res, 1
	}
	defer release()

	applied, err := governedmutation.Apply(owned)
	if err != nil {
		return mapMutationError(res, err)
	}
	relPath = applied.TargetRelPath
	if applied.Disposition == governedmutation.DispositionReplay {
		res.Status = "duplicate"
		res.Reload = "n/a"
		res.Note = fmt.Sprintf("id %q already exists in %s — no file changed", applied.CanonicalID, relPath)
		return res, 0
	}
	res.FilesChanged = append(res.FilesChanged, relPath)

	if !opt.noStage {
		_ = gitAdd(target, filepath.Join(target, filepath.FromSlash(relPath)))
	}

	// contract_unknown lives in candidates/ — outside the build — so there is
	// nothing to compile or reload yet.
	if applied.IsCandidate || opt.noRebuild {
		res.Reload = "skipped"
		if applied.IsCandidate {
			res.Note = "queued in candidates/ for human contract definition; no rebuild (not yet a live node)"
		} else {
			res.Note = "rebuild skipped (--no-rebuild); run 'sensei rebuild' before committing"
		}
	} else {
		rebuildArgs := []string{"--oxigraph-url", opt.oxigraphURL}
		if opt.svcRepo != "" {
			rebuildArgs = append(rebuildArgs, "--services-repo", opt.svcRepo)
		}
		if opt.agRepo != "" {
			rebuildArgs = append(rebuildArgs, "--ag-repo", opt.agRepo)
		}
		if rc := proposeRebuild(rebuildArgs); rc == 0 {
			res.Reload = "ok"
		} else {
			res.Reload = "failed"
			res.ReloadDetail = "sensei rebuild returned a non-zero exit; see output above"
		}
		// The seed is regenerated in the awareness-graph repo; stage it too.
		if opt.agRepo != "" {
			embed := filepath.Join(opt.agRepo, "golang", "server", "embeddata", "awareness.nt")
			if fileExists(embed) {
				res.FilesChanged = append(res.FilesChanged, filepath.Join("golang", "server", "embeddata", "awareness.nt"))
				if !opt.noStage {
					_ = gitAdd(opt.agRepo, embed)
				}
			}
		}
	}

	res.Status = "created"
	res.DiffSummary = composeDiffSummary(target, relPath)
	res.NextCommand = buildCommitCommand(target, opt.agRepo, req.Kind, applied.CanonicalID)
	return res, 0
}

// mapMutationError maps a typed governed-mutation error to the CLI result. The
// CLI renders all mutation-policy failures as validation_failed; the richer
// contradiction/stale distinctions are consumed by the promotion owner.
func mapMutationError(res ProposeResult, err error) (ProposeResult, int) {
	res.Validation = "errors"
	res.Status = "validation_failed"
	var ve *governedmutation.ValidationError
	if errors.As(err, &ve) {
		res.ValidationErrors = ve.Errors
	} else {
		res.ValidationErrors = []string{err.Error()}
	}
	return res, 1
}

// composeDiffSummary returns a concise, human-reviewable diff: the staged
// --stat plus the staged unified diff of the feedback file itself (truncated),
// so the new graph entry is visible in the returned result.
func composeDiffSummary(repo, feedbackRel string) string {
	var parts []string
	if stat := gitDiffStatStaged(repo); stat != "" {
		parts = append(parts, stat)
	}
	if entry := gitDiffStagedPath(repo, feedbackRel, 60); entry != "" {
		parts = append(parts, entry)
	}
	return strings.Join(parts, "\n\n")
}

// normalizeProposeRequest trims whitespace and drops empty list entries.
func normalizeProposeRequest(req *ProposeRequest) {
	propose.Normalize(req)
}

// validateProposal enforces the schema and the contract-first rule via the
// shared package, so the CLI and the Propose RPC enforce identical rules.
func validateProposal(req *ProposeRequest) []string {
	return propose.Validate(*req)
}

// deriveProposalID delegates canonical ID derivation to the governed-mutation
// owner (no CLI-local id policy).
func deriveProposalID(req *ProposeRequest) string {
	return governedmutation.DeriveID(*req)
}

// ── generic string helpers (shared across cmd/awg) ─────────────────────────

var nonSlugRun = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonSlugRun.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "_")
	}
	if s == "" {
		s = "entry"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ── git helpers (stage + diff; never commit) ──────────────────────────────

func gitAdd(repo string, paths ...string) error {
	args := append([]string{"-C", repo, "add", "--"}, paths...)
	return exec.Command("git", args...).Run()
}

func gitDiffStatStaged(repo string) string {
	out, err := exec.Command("git", "-C", repo, "diff", "--cached", "--stat").Output()
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(out), "\n")
}

// gitDiffStagedPath returns the staged unified diff for a single path, capped at
// maxLines so a large hunk does not bloat the result.
func gitDiffStagedPath(repo, path string, maxLines int) string {
	out, err := exec.Command("git", "-C", repo, "diff", "--cached", "--", path).Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... (%d more lines)", len(lines)-maxLines))
	}
	return strings.Join(lines, "\n")
}

// buildCommitCommand returns the exact command the human runs to commit. When
// the YAML target and the regenerated seed live in different repos, both
// commits are returned.
func buildCommitCommand(targetRepo, agRepo, kind, id string) string {
	msg := fmt.Sprintf("awareness: add %s %s", kind, id)
	cmd := fmt.Sprintf("git -C %s commit -m %q", targetRepo, msg)
	if agRepo != "" && !sameDir(agRepo, targetRepo) {
		cmd += fmt.Sprintf(" && git -C %s commit -m %q", agRepo, fmt.Sprintf("awareness: refresh seed for %s", id))
	}
	return cmd
}

func sameDir(a, b string) bool {
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return aa == bb
}

func readAll(f *os.File) ([]byte, error) {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ── output ────────────────────────────────────────────────────────────────

func printProposeResult(res ProposeResult, format string) {
	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		return
	}

	if res.Status == "validation_failed" {
		fmt.Fprintf(os.Stderr, "sensei propose: rejected (%s) — no files changed\n", res.Validation)
		for _, e := range res.ValidationErrors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return
	}

	fmt.Printf("status:      %s\n", res.Status)
	fmt.Printf("kind:        %s\n", res.Kind)
	if len(res.NodeIDs) > 0 {
		fmt.Printf("node ids:    %s\n", strings.Join(res.NodeIDs, ", "))
	}
	fmt.Printf("validation:  %s\n", res.Validation)
	if len(res.FilesChanged) > 0 {
		fmt.Printf("files:       %s\n", strings.Join(res.FilesChanged, ", "))
	}
	if res.Reload != "" {
		line := res.Reload
		if res.ReloadDetail != "" {
			line += " (" + res.ReloadDetail + ")"
		}
		fmt.Printf("reload:      %s\n", line)
	}
	if res.Note != "" {
		fmt.Printf("note:        %s\n", res.Note)
	}
	if res.DiffSummary != "" {
		fmt.Printf("\ndiff:\n%s\n", res.DiffSummary)
	}
	if res.NextCommand != "" {
		fmt.Printf("\nnext (commit when reviewed):\n  %s\n", res.NextCommand)
	}
}
