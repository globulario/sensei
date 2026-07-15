// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=cli.propose
// @awareness file_role=typed_graph_feedback_writer
// @awareness risk=high
//
// sensei propose is the typed write path for the awareness graph. It turns a
// structured feedback payload (a failure_mode, invariant, required_test,
// forbidden_fix, decision, or a contract_unknown queue entry) into an appended YAML
// source entry, rebuilds the seed, reloads the local Oxigraph store, and
// stages the touched files — but it NEVER commits. The durable authority stays
// the YAML sources + generated artifacts + a human-reviewed git commit.
//
// This is deliberately a local CLI command, not an MCP/gRPC surface: the MCP
// bridge exposes read-only safe tools (briefing/impact/query/resolve), and the
// graph service must never silently mutate durable truth. Mutation lives here,
// behind an explicit, diffable, human-committed step.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

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

// proposeKind describes where a kind's entry is written and under which key.
type proposeKind struct {
	file string // file name under docs/awareness/
	key  string // top-level list key inside that file
}

var proposeKindToFile = map[string]proposeKind{
	"failure_mode":  {"failure_modes.yaml", "failure_modes"},
	"invariant":     {"invariants.yaml", "invariants"},
	"required_test": {"required_tests.yaml", "required_tests"},
	"forbidden_fix": {"forbidden_fixes.yaml", "forbidden_fixes"},
	"decision":      {filepath.Join("architecture", "decisions.yaml"), "decisions"},
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

// ── core: validation + planning ───────────────────────────────────────────

// proposalPlan is the resolved, validated write target for a request.
type proposalPlan struct {
	Kind        string
	ID          string
	RelPath     string // path under docs/awareness/
	TopKey      string
	Item        interface{}
	IsCandidate bool // contract_unknown: written to candidates/, not rebuilt
	Preview     string
}

// applyProposal validates, then (unless dry-run) writes, rebuilds, reloads and
// stages. It performs NO file mutation when validation fails.
func applyProposal(req *ProposeRequest, opt proposeOptions) (ProposeResult, int) {
	normalizeProposeRequest(req)
	res := ProposeResult{Kind: req.Kind}

	if errs := validateProposal(req); len(errs) > 0 {
		res.Status = "validation_failed"
		res.Validation = "errors"
		res.ValidationErrors = errs
		return res, 1
	}
	res.Validation = "ok"

	plan, err := planProposal(req)
	if err != nil {
		res.Status = "validation_failed"
		res.Validation = "errors"
		res.ValidationErrors = []string{err.Error()}
		return res, 1
	}
	res.NodeIDs = []string{plan.ID}

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
	path := filepath.Join(target, "docs", "awareness", plan.RelPath)

	// Deterministic duplicate handling: if the id already exists, do not mutate.
	if exists, derr := proposalIDExists(path, plan.TopKey, plan.ID); derr != nil {
		res.Status = "validation_failed"
		res.Validation = "errors"
		res.ValidationErrors = []string{fmt.Sprintf("read %s: %v", plan.RelPath, derr)}
		return res, 1
	} else if exists {
		res.Status = "duplicate"
		res.Reload = "n/a"
		res.Note = fmt.Sprintf("id %q already exists in %s — no file changed", plan.ID, plan.RelPath)
		return res, 0
	}

	if opt.dryRun {
		res.Status = "dry_run"
		res.Reload = "skipped"
		res.DiffSummary = fmt.Sprintf("would append to docs/awareness/%s under %q:\n%s", plan.RelPath, plan.TopKey, plan.Preview)
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

	if _, err := appendProposalEntry(path, plan.TopKey, plan.Item); err != nil {
		res.Status = "validation_failed"
		res.Validation = "errors"
		res.ValidationErrors = []string{fmt.Sprintf("write %s: %v", plan.RelPath, err)}
		return res, 1
	}
	res.FilesChanged = append(res.FilesChanged, filepath.Join("docs", "awareness", plan.RelPath))

	if !opt.noStage {
		_ = gitAdd(target, path)
	}

	// contract_unknown lives in candidates/ — outside the build — so there is
	// nothing to compile or reload yet.
	if plan.IsCandidate || opt.noRebuild {
		res.Reload = "skipped"
		if plan.IsCandidate {
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
	res.DiffSummary = composeDiffSummary(target, filepath.Join("docs", "awareness", plan.RelPath))
	res.NextCommand = buildCommitCommand(target, opt.agRepo, req.Kind, plan.ID)
	return res, 0
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

// validateProposal enforces the schema and the contract-first rule. It returns
// human-readable error strings; a non-empty slice means "do not mutate files".
// validateProposal delegates to the shared package so the CLI and the Propose
// RPC enforce identical contract-first rules.
func validateProposal(req *ProposeRequest) []string {
	return propose.Validate(*req)
}

// planProposal derives the id, target file, and the typed entry to render.
func planProposal(req *ProposeRequest) (proposalPlan, error) {
	id := deriveProposalID(req)
	if req.Kind == "contract_unknown" {
		item := proposeContractUnknown{
			ID:               id,
			Kind:             "contract_unknown",
			Title:            req.Title,
			Description:      req.Description,
			ProposedContract: req.ProposedContract,
			RevisionRequest:  req.RevisionRequest,
			SourceFiles:      req.SourceFiles,
			Evidence:         req.Evidence,
			Domain:           req.Domain,
		}
		preview, err := renderListItem(item)
		if err != nil {
			return proposalPlan{}, err
		}
		return proposalPlan{
			Kind:        req.Kind,
			ID:          id,
			RelPath:     filepath.Join("candidates", "contract_unknown_"+slugify(id)+".yaml"),
			TopKey:      "contract_unknown",
			Item:        item,
			IsCandidate: true,
			Preview:     preview,
		}, nil
	}

	target, ok := proposeKindToFile[req.Kind]
	if !ok {
		return proposalPlan{}, fmt.Errorf("no canonical file mapping for kind %q", req.Kind)
	}
	item := buildCanonicalItem(req, id)
	preview, err := renderListItem(item)
	if err != nil {
		return proposalPlan{}, err
	}
	return proposalPlan{
		Kind:    req.Kind,
		ID:      id,
		RelPath: target.file,
		TopKey:  target.key,
		Item:    item,
		Preview: preview,
	}, nil
}

// ── typed entry shapes (ordered, omitempty) ───────────────────────────────

type proposeFiles struct {
	Files []string `yaml:"files,omitempty"`
}

type proposeFailureMode struct {
	ID                string        `yaml:"id"`
	Title             string        `yaml:"title"`
	Severity          string        `yaml:"severity,omitempty"`
	Description       string        `yaml:"description,omitempty"`
	Protects          *proposeFiles `yaml:"protects,omitempty"`
	RelatedInvariants []string      `yaml:"related_invariants,omitempty"`
	RequiredTests     []string      `yaml:"required_tests,omitempty"`
	ForbiddenFix      []string      `yaml:"forbidden_fix,omitempty"`
	Evidence          []string      `yaml:"evidence,omitempty"`
	Contract          string        `yaml:"contract,omitempty"`
}

type proposeInvariant struct {
	ID                  string        `yaml:"id"`
	Title               string        `yaml:"title"`
	Severity            string        `yaml:"severity,omitempty"`
	Status              string        `yaml:"status"`
	Description         string        `yaml:"description,omitempty"`
	Protects            *proposeFiles `yaml:"protects,omitempty"`
	RelatedFailureModes []string      `yaml:"related_failure_modes,omitempty"`
	RelatedInvariants   []string      `yaml:"related_invariants,omitempty"`
	ForbiddenFixes      []string      `yaml:"forbidden_fixes,omitempty"`
	RequiredTests       []string      `yaml:"required_tests,omitempty"`
	Evidence            []string      `yaml:"evidence,omitempty"`
	Contract            string        `yaml:"contract,omitempty"`
}

type proposeRequiredTestProtects struct {
	Invariants   []string `yaml:"invariants,omitempty"`
	FailureModes []string `yaml:"failure_modes,omitempty"`
	Files        []string `yaml:"files,omitempty"`
}

type proposeRequiredTest struct {
	ID       string                      `yaml:"id"`
	Title    string                      `yaml:"title"`
	Protects proposeRequiredTestProtects `yaml:"protects"`
}

type proposeForbiddenFix struct {
	ID                string        `yaml:"id"`
	Title             string        `yaml:"title"`
	Summary           string        `yaml:"summary,omitempty"`
	Protects          *proposeFiles `yaml:"protects,omitempty"`
	RelatedInvariants []string      `yaml:"related_invariants,omitempty"`
	Reason            string        `yaml:"reason,omitempty"`
	Evidence          []string      `yaml:"evidence,omitempty"`
	Contract          string        `yaml:"contract,omitempty"`
}

type proposeContractUnknown struct {
	ID               string   `yaml:"id"`
	Kind             string   `yaml:"kind"`
	Title            string   `yaml:"title"`
	Description      string   `yaml:"description,omitempty"`
	ProposedContract string   `yaml:"proposed_contract,omitempty"`
	RevisionRequest  string   `yaml:"revision_request,omitempty"`
	SourceFiles      []string `yaml:"source_files,omitempty"`
	Evidence         []string `yaml:"evidence,omitempty"`
	Domain           string   `yaml:"domain,omitempty"`
}

type proposeDecision struct {
	ID                 string   `yaml:"id"`
	Title              string   `yaml:"title"`
	Status             string   `yaml:"status"`
	ArchitecturalPlane string   `yaml:"architectural_plane,omitempty"`
	Rationale          string   `yaml:"rationale,omitempty"`
	Context            string   `yaml:"context,omitempty"`
	Consequences       string   `yaml:"consequences,omitempty"`
	RelatedInvariants  []string `yaml:"related_invariants,omitempty"`
	DefinesBoundaries  []string `yaml:"defines_boundaries,omitempty"`
	DefinesContracts   []string `yaml:"defines_contracts,omitempty"`
	AffectsComponents  []string `yaml:"affects_components,omitempty"`
	Mitigates          []string `yaml:"mitigates,omitempty"`
	Rejects            []string `yaml:"rejects,omitempty"`
	SupportedEvidence  []string `yaml:"supported_by_evidence,omitempty"`
	SourceFiles        []string `yaml:"source_files,omitempty"`
}

func buildCanonicalItem(req *ProposeRequest, id string) interface{} {
	files := protectsOrNil(req.SourceFiles)
	switch req.Kind {
	case "failure_mode":
		return proposeFailureMode{
			ID:                id,
			Title:             req.Title,
			Severity:          req.Severity,
			Description:       req.Description,
			Protects:          files,
			RelatedInvariants: req.RelatedInvariants,
			RequiredTests:     req.RequiredTests,
			ForbiddenFix:      req.ForbiddenFixes,
			Evidence:          req.Evidence,
			Contract:          req.Contract,
		}
	case "invariant":
		return proposeInvariant{
			ID:                  id,
			Title:               req.Title,
			Severity:            req.Severity,
			Status:              "active",
			Description:         req.Description,
			Protects:            files,
			RelatedFailureModes: req.RelatedFailures,
			RelatedInvariants:   req.RelatedInvariants,
			ForbiddenFixes:      req.ForbiddenFixes,
			RequiredTests:       req.RequiredTests,
			Evidence:            req.Evidence,
			Contract:            req.Contract,
		}
	case "required_test":
		return proposeRequiredTest{
			ID:    id,
			Title: req.Title,
			Protects: proposeRequiredTestProtects{
				Invariants:   req.RelatedInvariants,
				FailureModes: req.RelatedFailures,
				Files:        req.SourceFiles,
			},
		}
	case "forbidden_fix":
		return proposeForbiddenFix{
			ID:                id,
			Title:             req.Title,
			Summary:           req.Description,
			Protects:          files,
			RelatedInvariants: req.RelatedInvariants,
			Reason:            firstNonEmpty(req.Contract, req.Description),
			Evidence:          req.Evidence,
			Contract:          req.Contract,
		}
	case "decision":
		status := req.Status
		if strings.TrimSpace(status) == "" {
			status = "accepted"
		}
		return proposeDecision{
			ID:                 id,
			Title:              req.Title,
			Status:             status,
			ArchitecturalPlane: req.ArchitecturalPlane,
			Rationale:          req.Description,
			Context:            req.Context,
			Consequences:       req.Consequences,
			RelatedInvariants:  req.RelatedInvariants,
			DefinesBoundaries:  req.DefinesBoundaries,
			DefinesContracts:   req.DefinesContracts,
			AffectsComponents:  req.AffectsComponents,
			Mitigates:          req.RelatedFailures,
			Rejects:            req.ForbiddenFixes,
			SupportedEvidence:  req.SupportedEvidence,
			SourceFiles:        req.SourceFiles,
		}
	}
	return nil
}

func protectsOrNil(files []string) *proposeFiles {
	if len(files) == 0 {
		return nil
	}
	return &proposeFiles{Files: files}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ── id derivation ─────────────────────────────────────────────────────────

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

var idPrefixByKind = map[string]string{
	"failure_mode":     "failure",
	"invariant":        "invariant",
	"forbidden_fix":    "forbidden_fix",
	"decision":         "decision",
	"contract_unknown": "contract_unknown",
}

// deriveProposalID returns the explicit id, or a deterministic one derived from
// kind + a domain/repo hint + the title slug. required_test always carries an
// explicit id (validated earlier).
func deriveProposalID(req *ProposeRequest) string {
	if req.ID != "" {
		return req.ID
	}
	prefix := idPrefixByKind[req.Kind]
	if prefix == "" {
		prefix = "feedback"
	}
	if hint := domainHint(req); hint != "" {
		prefix = prefix + "." + hint
	}
	return prefix + "." + slugify(req.Title)
}

// domainHint extracts a short, stable token from the domain/repo (e.g.
// "github.com/globulario/sensei" -> "awareness_graph") to namespace
// derived ids. Returns "" when nothing usable is present.
func domainHint(req *ProposeRequest) string {
	src := firstNonEmpty(req.Domain, req.Repo)
	if src == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(src, "/"), "/")
	last := parts[len(parts)-1]
	return slugify(last)
}

// ── YAML append + duplicate detection ─────────────────────────────────────

// renderListItem marshals one entry as a 2-space-indented YAML list item under
// a top-level key, producing minimal, human-reviewable diffs.
func renderListItem(item interface{}) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(item); err != nil {
		return "", err
	}
	_ = enc.Close()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	var b strings.Builder
	for i, ln := range lines {
		if i == 0 {
			b.WriteString("  - " + ln + "\n")
		} else {
			b.WriteString("    " + ln + "\n")
		}
	}
	return b.String(), nil
}

var topKeyLine = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:`)
}

// appendProposalEntry appends one rendered list item to the target file under
// topKey. The canonical feedback files carry a single top-level list that runs
// to EOF, so appending at end-of-file yields a minimal diff and preserves the
// existing entries (and their comments) untouched.
func appendProposalEntry(path, topKey string, item interface{}) (bool, error) {
	itemText, err := renderListItem(item)
	if err != nil {
		return false, err
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			if mErr := os.MkdirAll(filepath.Dir(path), 0o755); mErr != nil {
				return false, mErr
			}
			return true, os.WriteFile(path, []byte(topKey+":\n"+itemText), 0o644)
		}
		return false, rerr
	}
	text := string(data)
	if !topKeyLine(topKey).MatchString(text) {
		if len(text) > 0 && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += topKey + ":\n" + itemText
		return true, os.WriteFile(path, []byte(text), 0o644)
	}
	if len(text) > 0 && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += itemText
	return true, os.WriteFile(path, []byte(text), 0o644)
}

// proposalIDExists reports whether topKey already contains an entry with id.
// A missing file is not an error — it simply has no ids yet.
func proposalIDExists(path, topKey, id string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parse: %w", err)
	}
	return collectEntryIDs(doc[topKey])[id], nil
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
