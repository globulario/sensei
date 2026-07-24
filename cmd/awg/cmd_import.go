// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	iofs "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/architecture/claimaudit"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/inference"
	"github.com/globulario/sensei/golang/architecture/knowledgeadoption"
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/extractor/coldsource"
	"github.com/globulario/sensei/golang/extractor/importgraph"
	"github.com/globulario/sensei/golang/rdf"
	yaml "gopkg.in/yaml.v3"
)

// runImport is the one-command foreign-repo onboarding wrapper. It composes the
// existing extractors in the ONE correct order so no caller has to remember it:
//
//	clone -> contract extraction (pristine!) -> structural -> [history] -> reconstruction -> load
//
// The ordering is load-bearing: contract extraction runs on the pristine clone,
// BEFORE `bootstrap` scaffolds Sensei's own charter into the tree (guardrail 7
// of the sensei-import skill; also backstopped in the intent gatherer).
//
// It never promotes: every extractor writes candidates/intents for human review.
// It never touches a store unless an explicit --store-url is given.
func runImport(args []string) int {
	fs := flag.NewFlagSet("sensei import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	domain := fs.String("domain", "", "domain to tag the repo's nodes, e.g. github.com/gin-gonic/gin (default: derived from the URL)")
	depth := fs.String("depth", "full", "extraction depth: basic (structural only) | full (adds LLM contract extraction + optional history)")
	dir := fs.String("dir", "", "checkout destination for a URL (default: a temp dir); ignored when the target is an existing path")
	storeURL := fs.String("store-url", "", "load the slice into this store; when empty, print the exact build command instead of touching any store")
	markerFile := fs.String("graph-marker-file", "", "server's graph marker file; pass this with --store-url so a served store stays fresh for briefing")
	drafter := fs.String("drafter", "auto", "contract drafter for full depth: auto (prefer Claude CLI, then Codex CLI, then direct API) | claude-cli | codex-cli | llm | echo")
	model := fs.String("model", "", "contract drafter model override (default "+coldsource.DefaultModel+")")
	maxN := fs.Int("max", 12, "max contract candidates to propose (full depth)")
	repoSlug := fs.String("repo-slug", "", "owner/name for PR-review history mining (full depth; needs gh auth + full history)")
	refresh := fs.Bool("refresh", false, "re-extract and optionally reload an existing checkout; never clones")
	dryRun := fs.Bool("dry-run", false, "print the plan and stop; run nothing")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage:
  sensei import <git-url | path> --domain <domain> [flags]
  sensei import --refresh <checkout-path> --domain <domain> [flags]

Onboard a foreign repository into Sensei in one command, in the correct order:
clone -> contract extraction (on the pristine clone) -> structural extraction ->
optional history mining -> project reconstruction -> (optionally) load the
domain-scoped slice.

With --refresh, re-extract an existing checkout and optionally reload its
domain-scoped slice. Refresh never clones; it requires a checkout path.

Never auto-promotes: extractors write candidates/intents for you to review and
promote yourself. Never touches a store unless --store-url is given.

Flags:
`)
		fs.PrintDefaults()
	}
	// Parse flags that may appear before OR after the positional target: Go's
	// flag package stops at the first non-flag arg, so pull positionals out and
	// keep parsing the remainder.
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	var positional []string
	for fs.NArg() > 0 {
		positional = append(positional, fs.Arg(0))
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			if err == flag.ErrHelp {
				return 0
			}
			return 2
		}
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "sensei import: exactly one <git-url | path> argument is required")
		fs.Usage()
		return 2
	}
	target := positional[0]

	full := false
	switch strings.ToLower(strings.TrimSpace(*depth)) {
	case "full":
		full = true
	case "basic":
		full = false
	default:
		fmt.Fprintf(os.Stderr, "sensei import: --depth must be 'basic' or 'full', got %q\n", *depth)
		return 2
	}

	// Resolve the checkout: refresh only accepts an existing checkout; normal
	// import uses an existing path in place or clones a URL.
	var checkout string
	var cloned bool
	var code int
	if *refresh {
		checkout, code = resolveImportRefreshCheckout(target)
	} else {
		checkout, cloned, code = resolveImportCheckout(target, strings.TrimSpace(*dir), *dryRun)
	}
	if code != 0 {
		return code
	}

	dom := resolveImportDomain(*domain, target, checkout)
	if dom == "" {
		fmt.Fprintln(os.Stderr, "sensei import: --domain is required (could not derive it from the target or checkout remote)")
		return 2
	}
	slug := strings.TrimSpace(*repoSlug)
	if slug == "" {
		slug = deriveSlug(dom)
	}

	selectedDrafter, contractAuth, contractSkip, contractCode := planImportContractBackend(full, *drafter, *model)
	if contractCode != 0 {
		return contractCode
	}
	wantContracts := full && contractSkip == ""
	skipContractReason := ""
	if full && contractSkip != "" {
		skipContractReason = contractSkip
	}

	mode := "import"
	if *refresh {
		mode = "refresh"
	}
	fmt.Fprintf(os.Stderr, "sensei import: %s\n  mode:     %s\n  domain:   %s\n  checkout: %s\n  depth:    %s\n",
		target, mode, dom, checkout, *depth)
	if *dryRun {
		fmt.Fprintln(os.Stderr, "  (dry-run: nothing executed)")
		printImportPlan(checkout, dom, slug, full, wantContracts, skipContractReason, *storeURL, *refresh, selectedDrafter, contractAuth)
		return 0
	}

	// 1) Contracts FIRST for fresh imports — on the pristine clone, before
	// bootstrap scaffolds. Refresh reuses an existing checkout, so this stage is
	// a re-grounding pass over current files rather than a pristine-clone pass.
	if wantContracts {
		stage := "contract extraction (pristine clone)"
		if *refresh {
			stage = "contract refresh (existing checkout)"
		}
		fmt.Fprintf(os.Stderr, "\n== [1/5] %s ==\n", stage)
		if contractAuth != "" {
			fmt.Fprintf(os.Stderr, "authentication: %s\n", contractAuth)
		}
		ia := []string{"--path", checkout, "--sources", "docs,comments,tests",
			"--drafter", selectedDrafter, "--max", strconv.Itoa(*maxN), "--adopt"}
		if strings.TrimSpace(*model) != "" {
			ia = append(ia, "--model", *model)
		}
		if rc := runIntentMine(ia); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: contract extraction failed — continuing with structure only")
		}
	} else if skipContractReason != "" {
		fmt.Fprintf(os.Stderr, "\n== [1/5] contract extraction: %s ==\n", skipContractReason)
	}

	// 2) Structural extraction — now safe to scaffold the checkout.
	fmt.Fprintln(os.Stderr, "\n== [2/5] structural extraction ==")
	if rc := runBootstrap([]string{"--path", checkout, "--skip-history", "--skip-build"}); rc != 0 {
		fmt.Fprintln(os.Stderr, "sensei import: structural extraction failed")
		return 1
	}

	// 3) History / PR mining — full depth only, best-effort.
	if full && slug != "" {
		fmt.Fprintln(os.Stderr, "\n== [3/5] day-0 history / PR mining ==")
		if rc := runColdBootstrap([]string{"--path", checkout, "--repo-slug", slug, "--auto-window"}); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: history mining produced nothing usable (expected on quiet repos)")
		}
	} else if full {
		fmt.Fprintln(os.Stderr, "\n== [3/5] history mining skipped (no --repo-slug) ==")
	}

	// 4) Compile the repository slice and hand its exact digest to inference.
	awarenessDir := filepath.Join(checkout, "docs", "awareness")
	generatedDir := filepath.Join(awarenessDir, "generated")
	projectDir := filepath.Join(checkout, ".sensei", "project")
	fmt.Fprintln(os.Stderr, "\n== [4/5] project reconstruction ==")
	readiness, err := reconstructImportedProject(checkout, dom, full)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei import: project reconstruction failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "  phase 2 readiness: %s (sources %d/%d, root %d/%d, claims %d)\n",
		readiness.State, readiness.RepresentedSourceFiles, readiness.EligibleSourceFiles,
		readiness.RepresentedRootFiles, readiness.EligibleRootFiles, readiness.ClaimCount)

	// 5) Load the domain-scoped slice, or print the command to do it.
	if strings.TrimSpace(*storeURL) == "" {
		fmt.Fprintln(os.Stderr, "\n== [5/5] load: no --store-url given; run this against your store ==")
		fmt.Fprintf(os.Stderr, "  sensei build --input %s --input %s --input %s --repo %s --store-url <url>\n",
			awarenessDir, generatedDir, projectDir, dom)
		fmt.Fprintln(os.Stderr, "  (fresh store? seed once with `sensei build --all` first.)")
	} else {
		fmt.Fprintln(os.Stderr, "\n== [5/5] load domain-scoped slice ==")
		ba := []string{"--input", awarenessDir, "--input", generatedDir, "--input", projectDir, "--repo", dom, "--store-url", *storeURL}
		if m := strings.TrimSpace(*markerFile); m != "" {
			ba = append(ba, "--graph-marker-file", m)
		} else {
			fmt.Fprintln(os.Stderr, "  note: no --graph-marker-file given; a live/served store may report freshness-stale for briefing until re-certified")
		}
		if rc := runBuild(ba); rc != 0 {
			fmt.Fprintln(os.Stderr, "sensei import: load failed — a scoped --repo update needs a non-empty store; seed with `sensei build --all` first")
			return 1
		}
	}

	printImportSummary(checkout, dom, cloned, wantContracts, *refresh, readiness)
	return 0
}

// resolveImportCheckout returns the working-tree path to extract from. An
// existing directory is used in place; anything else is treated as a git URL and
// cloned. cloned reports whether a fresh clone was made.
func resolveImportCheckout(target, dir string, dryRun bool) (checkout string, cloned bool, code int) {
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		abs, aerr := filepath.Abs(target)
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "sensei import: %v\n", aerr)
			return "", false, 1
		}
		return abs, false, 0
	}
	dest := dir
	if dest == "" {
		dest = filepath.Join(os.TempDir(), "sensei-import-"+sanitizeName(deriveRepoBase(target)))
	}
	if dryRun {
		return dest, true, 0
	}
	if _, err := os.Stat(dest); err == nil {
		fmt.Fprintf(os.Stderr, "sensei import: checkout dir %s already exists; using it in place\n", dest)
		abs, _ := filepath.Abs(dest)
		return abs, false, 0
	}
	fmt.Fprintf(os.Stderr, "sensei import: cloning %s -> %s\n", target, dest)
	cmd := exec.Command("git", "clone", "--depth", "1", target, dest)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sensei import: git clone failed: %v\n", err)
		return "", false, 1
	}
	abs, _ := filepath.Abs(dest)
	return abs, true, 0
}

func resolveImportRefreshCheckout(target string) (checkout string, code int) {
	info, err := os.Stat(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei import --refresh: checkout path %s: %v\n", target, err)
		return "", 2
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei import --refresh: %s is not a directory\n", target)
		return "", 2
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei import --refresh: %v\n", err)
		return "", 1
	}
	return abs, 0
}

func resolveImportDomain(explicit, target, checkout string) string {
	if dom := strings.TrimSpace(explicit); dom != "" {
		return dom
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return gitRemoteDomain(checkout)
	}
	if dom := deriveDomain(target); dom != "" {
		return dom
	}
	return gitRemoteDomain(checkout)
}

// deriveDomain turns a git URL into a domain tag, e.g.
// https://github.com/gin-gonic/gin(.git) -> github.com/gin-gonic/gin.
func deriveDomain(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if at := strings.Index(s, "@"); at >= 0 { // scp-style git@host:owner/repo
		s = s[at+1:]
	}
	s = strings.Replace(s, ":", "/", 1)
	s = strings.Trim(s, "/")
	if s == "" || !strings.Contains(s, "/") {
		return ""
	}
	return s
}

// deriveSlug returns owner/name from a domain like github.com/gin-gonic/gin.
func deriveSlug(domain string) string {
	parts := strings.Split(strings.Trim(domain, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

func deriveRepoBase(url string) string {
	s := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	s = strings.Trim(s, "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "repo"
	}
	return b.String()
}

func printImportPlan(checkout, domain, slug string, full, wantContracts bool, skipReason, storeURL string, refresh bool, drafter, auth string) {
	fmt.Fprintln(os.Stderr, "\nplan:")
	if refresh {
		fmt.Fprintf(os.Stderr, "  0. refresh existing checkout %s for domain %s\n", checkout, domain)
	}
	if full && wantContracts {
		fmt.Fprintf(os.Stderr, "  1. sensei intent-mine --path %s --sources docs,comments,tests --drafter %s --max N --adopt\n", checkout, drafter)
		if auth != "" {
			fmt.Fprintf(os.Stderr, "     authentication: %s\n", auth)
		}
	} else if skipReason != "" {
		fmt.Fprintf(os.Stderr, "  1. (contracts skipped: %s)\n", skipReason)
	} else {
		fmt.Fprintln(os.Stderr, "  1. (basic depth: no contract extraction)")
	}
	fmt.Fprintf(os.Stderr, "  2. sensei bootstrap --path %s --skip-history --skip-build\n", checkout)
	if full && slug != "" {
		fmt.Fprintf(os.Stderr, "  3. sensei cold-bootstrap --path %s --repo-slug %s --auto-window\n", checkout, slug)
	} else {
		fmt.Fprintln(os.Stderr, "  3. (history mining skipped)")
	}
	fmt.Fprintf(os.Stderr, "  4. compile %s/.sensei/project/graph.nt, infer bound claims, and write readiness.yaml\n", checkout)
	store := storeURL
	if store == "" {
		store = "<url>"
	}
	fmt.Fprintf(os.Stderr, "  5. sensei build --input %s/docs/awareness --input %s/docs/awareness/generated --input %s/.sensei/project --repo %s --store-url %s\n",
		checkout, checkout, checkout, domain, store)
}

func planImportContractBackend(full bool, requested, model string) (drafter, auth, skipReason string, code int) {
	if !full {
		return strings.TrimSpace(requested), "", "", 0
	}
	req := strings.TrimSpace(requested)
	if req == "" {
		req = string(coldsource.DrafterAuto)
	}
	switch req {
	case string(coldsource.DrafterEcho):
		return req, "deterministic echo drafter (shallow, no credential)", "", 0
	case string(coldsource.DrafterLLM), string(coldsource.DrafterClaudeCLI), string(coldsource.DrafterCodexCLI):
		_, receipt, err := coldsource.SelectLLMClient(coldsource.DrafterBackend(req), model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei import: --drafter %s unavailable: %v\n", req, err)
			return "", "", "", 2
		}
		return string(receipt.Drafter), importAuthDescription(receipt), "", 0
	case string(coldsource.DrafterAuto):
		_, receipt, err := coldsource.SelectLLMClient(coldsource.DrafterAuto, model)
		if err != nil {
			return "", "", "no authenticated Claude CLI, Codex CLI, or direct API credential found — contract layer skipped (run again with --drafter claude-cli, --drafter codex-cli, --drafter llm, or --drafter echo)", 0
		}
		return string(receipt.Drafter), importAuthDescription(receipt), "", 0
	default:
		fmt.Fprintf(os.Stderr, "sensei import: unknown --drafter %q (use auto|claude-cli|codex-cli|llm|echo)\n", requested)
		return "", "", "", 2
	}
}

func importAuthDescription(receipt coldsource.BackendReceipt) string {
	switch receipt.Drafter {
	case coldsource.DrafterClaudeCLI:
		msg := "Claude CLI subscription login"
		if receipt.DirectAPIEnvironmentIgnored {
			msg += " (direct API env ignored)"
		}
		return msg
	case coldsource.DrafterCodexCLI:
		msg := "Codex CLI subscription login"
		if receipt.DirectAPIEnvironmentIgnored {
			msg += " (direct API env ignored)"
		}
		return msg
	case coldsource.DrafterLLM:
		return "direct Anthropic Messages API via " + receipt.CredentialSource
	default:
		return receipt.CredentialSource
	}
}

func printImportSummary(checkout, domain string, cloned, wantContracts, refresh bool, readiness phase2Readiness) {
	if refresh {
		fmt.Fprintln(os.Stderr, "\nsensei import --refresh: done — nothing was promoted.")
	} else {
		fmt.Fprintln(os.Stderr, "\nsensei import: done — nothing was promoted.")
	}
	if wantContracts {
		fmt.Fprintf(os.Stderr, "  contracts/intents: machine-adopted intents in %s/docs/awareness/intent_*.yaml; review-only material in candidates/\n", checkout)
	}
	fmt.Fprintf(os.Stderr, "  project graph: %s/.sensei/project/graph.nt\n", checkout)
	fmt.Fprintf(os.Stderr, "  project claims: %s/.sensei/project/claims.yaml (%d)\n", checkout, readiness.ClaimCount)
	fmt.Fprintf(os.Stderr, "  claim audit: %s/.sensei/project/claim-audit.yaml (%d distinct propositions)\n", checkout, readiness.DistinctPropositionCount)
	fmt.Fprintf(os.Stderr, "  adoption report: %s/.sensei/project/knowledge/adoption-report.yaml (%d machine adopted)\n", checkout, readiness.MachineAdoptedKnowledge)
	fmt.Fprintf(os.Stderr, "  phase 2 readiness: %s/.sensei/project/readiness.yaml (%s)\n", checkout, readiness.State)
	fmt.Fprintf(os.Stderr, "  candidates for review: %s/docs/awareness/candidates/\n", checkout)
	fmt.Fprintf(os.Stderr, "  next: review, then `sensei promote --repo %s ...`; verify with `sensei briefing --file <f> --domain %s`\n", domain, domain)
	if cloned {
		fmt.Fprintf(os.Stderr, "  checkout kept at %s (delete when done)\n", checkout)
	}
}

const (
	readinessReady            = "ready"
	readinessPartiallyReady   = "partially_ready"
	readinessStructurallyThin = "structurally_thin"
	readinessInferenceMissing = "inference_missing"
	readinessUncertifiable    = "uncertifiable"
)

type phase2Readiness struct {
	SchemaVersion                           string                           `yaml:"schema_version"`
	GeneratedBy                             string                           `yaml:"generated_by"`
	RepositoryDomain                        string                           `yaml:"repository_domain"`
	State                                   string                           `yaml:"state"`
	GraphPath                               string                           `yaml:"graph_path"`
	GraphDigestSHA256                       string                           `yaml:"graph_digest_sha256"`
	ClaimsPath                              string                           `yaml:"claims_path"`
	ClaimAuditPath                          string                           `yaml:"claim_audit_path"`
	AdoptionReportPath                      string                           `yaml:"adoption_report_path"`
	ReconstructionReceiptPath               string                           `yaml:"reconstruction_receipt_path"`
	EligibleSourceFiles                     int                              `yaml:"eligible_source_files"`
	RepresentedSourceFiles                  int                              `yaml:"represented_source_files"`
	StructuralSourceCoverage                string                           `yaml:"structural_source_coverage"`
	EligibleRootFiles                       int                              `yaml:"eligible_root_files"`
	RepresentedRootFiles                    int                              `yaml:"represented_root_files"`
	RootPackageCoverage                     string                           `yaml:"root_package_coverage"`
	CodeSymbolCount                         int                              `yaml:"code_symbol_count"`
	ArchitectureFactCount                   int                              `yaml:"architecture_fact_count"`
	GoSemanticFactCount                     int                              `yaml:"go_semantic_fact_count"`
	ClaimCount                              int                              `yaml:"claim_count"`
	DistinctPropositionCount                int                              `yaml:"distinct_proposition_count"`
	MachineAdoptedKnowledge                 int                              `yaml:"machine_adopted_knowledge_count"`
	AdoptedKnowledgeByClass                 []knowledgeadoption.ClassSummary `yaml:"adopted_knowledge_by_class"`
	MachineAdoptedKnowledgeByClass          []claimaudit.Count               `yaml:"machine_adopted_knowledge_by_class"`
	StagedKnowledgeByClass                  []claimaudit.Count               `yaml:"staged_knowledge_by_class"`
	RejectedKnowledgeByClass                []claimaudit.Count               `yaml:"rejected_knowledge_by_class"`
	ClaimsByRule                            []claimaudit.Count               `yaml:"claims_by_rule"`
	DecisionCount                           int                              `yaml:"decision_count"`
	ContractCount                           int                              `yaml:"contract_count"`
	FailureModeCount                        int                              `yaml:"failure_mode_count"`
	ForbiddenFixCount                       int                              `yaml:"forbidden_fix_count"`
	BoundaryCount                           int                              `yaml:"boundary_count"`
	IncidentCount                           int                              `yaml:"incident_count"`
	ContractLikeIntentsStaged               int                              `yaml:"contract_like_intents_staged_for_missing_structure"`
	HistoryCandidatesStaged                 int                              `yaml:"history_candidates_staged"`
	HistoryCandidatesAdopted                int                              `yaml:"history_candidates_adopted"`
	HistoryCandidatesRejected               int                              `yaml:"history_candidates_rejected"`
	ClaimAuditValid                         bool                             `yaml:"claim_audit_valid"`
	ProjectGraphDeterministic               bool                             `yaml:"project_graph_deterministic"`
	RootCoreFilesWithClaims                 []string                         `yaml:"root_core_files_with_claims,omitempty"`
	TaskReadyCoreFilesWithSemanticKnowledge []string                         `yaml:"task_ready_core_files_with_semantic_knowledge,omitempty"`
	TaskReadyCoreFiles                      []string                         `yaml:"task_ready_core_files,omitempty"`
	UnrepresentedCoreFiles                  []string                         `yaml:"unrepresented_core_files,omitempty"`
	UnrepresentedSourceFiles                []string                         `yaml:"unrepresented_source_files,omitempty"`
	Limitations                             []string                         `yaml:"limitations,omitempty"`
}

type phase2ReadinessEnvelope struct {
	Phase2Readiness phase2Readiness `yaml:"phase_2_readiness"`
}

type phase2SemanticInputs struct {
	Audit               claimaudit.Report
	AuditValid          bool
	Adoption            knowledgeadoption.Report
	AdoptionPath        string
	GraphDeterministic  bool
	FactCount           int
	GoSemanticFactCount int
}

type transactionPhase string

const (
	phaseStaging    transactionPhase = "staging"
	phaseValidated  transactionPhase = "validated"
	phasePriorMoved transactionPhase = "prior_moved"
	phaseActivated  transactionPhase = "activated"
)

type transactionMarker struct {
	TransactionID string              `yaml:"transaction_id"`
	RepoRevision  string              `yaml:"repo_revision"`
	StagingPath   string              `yaml:"staging_path"`
	PriorPath     string              `yaml:"prior_path"`
	PriorStatus   projectFamilyStatus `yaml:"prior_status"`
	Phase         transactionPhase    `yaml:"phase"`
}

var testFailureInjectionPoint string
var testFailureHook func(string)

const (
	failAfterGraph         = "after_graph"
	failAfterClaims        = "after_claims"
	failAfterAudit         = "after_audit"
	failAfterReadiness     = "after_readiness"
	failAfterAdoption      = "after_adoption"
	failAfterReceipt       = "after_receipt"
	failAfterValidation    = "after_validation"
	failBeforeMoveCoherent = "before_move_coherent"
	failAfterMoveCoherent  = "after_move_coherent"
	failBeforeMoveInvalid  = "before_move_invalid"
	failAfterMoveInvalid   = "after_move_invalid"
	failBeforeActivation   = "before_activation"
	failDuringActivation   = "during_activation"
	failAfterActivation    = "after_activation"
)

func injectFailure(point string, cleanupStaging *bool) error {
	if testFailureHook != nil {
		testFailureHook(point)
	}
	if testFailureInjectionPoint == point {
		if cleanupStaging != nil {
			*cleanupStaging = false
		}
		return fmt.Errorf("injected failure at: %s", point)
	}
	return nil
}

func flushFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func flushDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	err = f.Sync()
	if err != nil && strings.Contains(err.Error(), "invalid argument") {
		return nil
	}
	return err
}

func flushStagingFiles(stagingDir string) error {
	return filepath.Walk(stagingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if err := flushFile(path); err != nil {
				return err
			}
		}
		return nil
	})
}

func recoverTransaction(projectParent, txMarkerPath string) error {
	data, err := os.ReadFile(txMarkerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var marker transactionMarker
	if err := yaml.Unmarshal(data, &marker); err != nil {
		os.Remove(txMarkerPath)
		return fmt.Errorf("malformed transaction marker deleted: %w", err)
	}

	activeProjectDir := marker.PriorPath
	backupDir := activeProjectDir + ".previous"

	switch marker.Phase {
	case phaseStaging, phaseValidated:
		if marker.StagingPath != "" {
			os.RemoveAll(marker.StagingPath)
		}
	case phasePriorMoved:
		if marker.PriorStatus == projectFamilyCoherent {
			if _, err := os.Stat(backupDir); err == nil {
				os.RemoveAll(activeProjectDir)
				os.Rename(backupDir, activeProjectDir)
			}
		} else {
			os.RemoveAll(activeProjectDir)
		}
		if marker.StagingPath != "" {
			os.RemoveAll(marker.StagingPath)
		}
	case phaseActivated:
		finalClass, err := classifyActiveProjectFamily(activeProjectDir)
		if err == nil && finalClass.Status == projectFamilyCoherent {
			if marker.PriorStatus == projectFamilyCoherent {
				os.RemoveAll(backupDir)
			}
		} else {
			if marker.PriorStatus == projectFamilyCoherent {
				os.RemoveAll(activeProjectDir)
				if _, statErr := os.Stat(backupDir); statErr == nil {
					os.Rename(backupDir, activeProjectDir)
				}
			} else {
				os.RemoveAll(activeProjectDir)
			}
		}
		if marker.StagingPath != "" {
			os.RemoveAll(marker.StagingPath)
		}
	}

	os.Remove(txMarkerPath)
	return flushDir(projectParent)
}

func reconstructImportedProject(root, domain string, includeHistory bool) (phase2Readiness, error) {
	projectParent := filepath.Join(root, ".sensei")
	activeProjectDir := filepath.Join(projectParent, "project")
	txMarkerPath := filepath.Join(projectParent, "tx-marker.yaml")
	lockPath := filepath.Join(projectParent, "project.lock")

	if err := os.MkdirAll(projectParent, 0o755); err != nil {
		return phase2Readiness{}, fmt.Errorf("create project parent directory: %w", err)
	}

	lockFile, err := acquireProjectLock(lockPath)
	if err != nil {
		return phase2Readiness{}, err
	}
	defer lockFile.Close()

	// 1. Recover any crashed/interrupted transaction
	if err := recoverTransaction(projectParent, txMarkerPath); err != nil {
		return phase2Readiness{}, fmt.Errorf("recover transaction: %w", err)
	}

	// 2. Classify prior active project family
	prior, err := classifyActiveProjectFamily(activeProjectDir)
	if err != nil {
		return phase2Readiness{}, fmt.Errorf("classify active project family: %w", err)
	}

	// 3. Setup Staging
	projectDir, err := os.MkdirTemp(projectParent, ".project-staging-")
	if err != nil {
		return phase2Readiness{}, fmt.Errorf("create project staging directory: %w", err)
	}

	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			os.RemoveAll(projectDir)
		}
	}()

	revision, decisionTimestamp := projectRevisionBinding(root)

	// 4. Create Persistent Transaction Marker
	txID := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	marker := transactionMarker{
		TransactionID: txID,
		RepoRevision:  revision,
		StagingPath:   projectDir,
		PriorPath:     activeProjectDir,
		PriorStatus:   prior.Status,
		Phase:         phaseStaging,
	}
	if err := writeTxMarker(txMarkerPath, marker); err != nil {
		return phase2Readiness{}, fmt.Errorf("write transaction marker: %w", err)
	}

	graphPath := filepath.Join(projectDir, "graph.nt")
	claimsPath := filepath.Join(projectDir, "claims.yaml")
	claimAuditPath := filepath.Join(projectDir, "claim-audit.yaml")
	readinessPath := filepath.Join(projectDir, "readiness.yaml")
	reconstructionReceiptPath := filepath.Join(projectDir, "reconstruction-receipt.yaml")
	knowledgeDir := filepath.Join(projectDir, "knowledge")
	awarenessDir := filepath.Join(root, "docs", "awareness")
	generatedDir := filepath.Join(awarenessDir, "generated")

	raw, _, err := compileAwarenessInputs([]string{awarenessDir, generatedDir}, domain, "", "", false)
	if err != nil {
		return phase2Readiness{}, err
	}
	baseRaw, err := stripMachineAdoptedIntentSubjects(raw)
	if err != nil {
		return phase2Readiness{}, err
	}
	baseGraph, _, _, _ := finalizeBuildArtifact(canonicalProjectNTriples(baseRaw))
	if errs := extractor.ValidateNTriples(strings.NewReader(string(baseGraph))); len(errs) > 0 {
		return phase2Readiness{}, fmt.Errorf("compiled project graph is invalid: %v", errs[0])
	}
	baseSum := sha256.Sum256(baseGraph)
	baseDigest := hex.EncodeToString(baseSum[:])
	if err := finalizeMachineAdoptedIntentReceipts(awarenessDir, revision, baseDigest, decisionTimestamp); err != nil {
		return phase2Readiness{}, fmt.Errorf("finalize machine-adopted Intent receipts: %w", err)
	}
	adoptionResult, err := knowledgeadoption.Run(knowledgeadoption.Options{
		RepositoryRoot: root, RepositoryDomain: domain,
		CandidatesDir: filepath.Join(awarenessDir, "candidates"), OutputDir: knowledgeDir,
		Revision: revision, GraphDigest: baseDigest, DecisionTimestamp: decisionTimestamp,
		ProvisionalGraph: baseGraph,
	})
	if err != nil {
		return phase2Readiness{}, fmt.Errorf("project knowledge adoption: %w", err)
	}

	if err := injectFailure(failAfterAdoption, &cleanupStaging); err != nil {
		return phase2Readiness{}, err
	}

	compileFinal := func() ([]byte, error) {
		finalRaw, _, compileErr := compileAwarenessInputs([]string{awarenessDir, generatedDir, knowledgeDir}, domain, "", "", false)
		if compileErr != nil {
			return nil, compileErr
		}
		finalGraph, _, _, _ := finalizeBuildArtifact(canonicalProjectNTriples(finalRaw))
		if errs := extractor.ValidateNTriples(strings.NewReader(string(finalGraph))); len(errs) > 0 {
			return nil, fmt.Errorf("compiled final project graph is invalid: %v", errs[0])
		}
		return finalGraph, nil
	}
	graphData, err := compileFinal()
	if err != nil {
		return phase2Readiness{}, err
	}
	if _, err := knowledgeadoption.Run(knowledgeadoption.Options{
		RepositoryRoot: root, RepositoryDomain: domain,
		CandidatesDir: filepath.Join(awarenessDir, "candidates"), OutputDir: knowledgeDir,
		Revision: revision, GraphDigest: baseDigest, DecisionTimestamp: decisionTimestamp,
		ProvisionalGraph: baseGraph,
	}); err != nil {
		return phase2Readiness{}, fmt.Errorf("second project knowledge adoption pass: %w", err)
	}
	convergedGraph, err := compileFinal()
	if err != nil {
		return phase2Readiness{}, err
	}
	if !bytes.Equal(graphData, convergedGraph) {
		firstSum := sha256.Sum256(graphData)
		secondSum := sha256.Sum256(convergedGraph)
		return phase2Readiness{}, fmt.Errorf("project reconstruction did not converge byte-identically on the second pass: first=%x second=%x %s", firstSum, secondSum, firstGraphDifference(graphData, convergedGraph))
	}
	graphData = convergedGraph
	if err := writeFileAtomic(graphPath, graphData); err != nil {
		return phase2Readiness{}, err
	}
	if err := injectFailure(failAfterGraph, &cleanupStaging); err != nil {
		return phase2Readiness{}, err
	}
	sum := sha256.Sum256(graphData)
	graphDigest := hex.EncodeToString(sum[:])

	reg, err := inference.DefaultRegistry()
	if err != nil {
		return phase2Readiness{}, err
	}
	inferenceResult, inferenceErr := buildInferClaimsResult(root, inferClaimsOptions{
		Repo:              root,
		RepositoryDomain:  domain,
		Format:            "yaml",
		IncludeDocs:       false,
		IncludeTests:      true,
		IncludeHistory:    includeHistory,
		GraphDigest:       graphDigest,
		GraphDigestStatus: architecture.GraphDigestResolved,
	}, reg)
	claimsData, claims := inferenceResult.Rendered, inferenceResult.Document
	if inferenceErr == nil {
		inferenceErr = writeFileAtomic(claimsPath, claimsData)
	}
	if inferenceErr != nil {
		return phase2Readiness{}, inferenceErr
	}
	if err := injectFailure(failAfterClaims, &cleanupStaging); err != nil {
		return phase2Readiness{}, err
	}

	audit := claimaudit.Report{}
	auditValid := false
	audit = claimaudit.Build(claims, projectClaimAuditOptions(root))
	auditBytes, auditErr := yaml.Marshal(audit)
	if auditErr == nil {
		auditErr = writeFileAtomic(claimAuditPath, auditBytes)
	}
	if auditErr != nil {
		return phase2Readiness{}, fmt.Errorf("write claim audit: %w", auditErr)
	}
	auditValid = true
	if err := injectFailure(failAfterAudit, &cleanupStaging); err != nil {
		return phase2Readiness{}, err
	}

	canonicalGraphPath := filepath.Join(root, ".sensei", "project", "graph.nt")
	canonicalClaimsPath := filepath.Join(root, ".sensei", "project", "claims.yaml")
	canonicalAdoptionPath := filepath.Join(root, ".sensei", "project", "knowledge", "adoption-report.yaml")

	readiness, readinessErr := assessPhase2Readiness(root, domain, canonicalGraphPath, canonicalClaimsPath, graphData, claims, phase2SemanticInputs{
		Audit: audit, AuditValid: auditValid, Adoption: adoptionResult.Report,
		AdoptionPath: canonicalAdoptionPath, GraphDeterministic: true,
		FactCount: inferenceResult.FactCount, GoSemanticFactCount: inferenceResult.GoSemanticFactCount,
	})
	if readinessErr != nil {
		return readiness, readinessErr
	}
	readinessBytes, err := yaml.Marshal(phase2ReadinessEnvelope{Phase2Readiness: readiness})
	if err == nil {
		err = writeFileAtomic(readinessPath, readinessBytes)
	}
	if err != nil {
		return readiness, err
	}
	if err := injectFailure(failAfterReadiness, &cleanupStaging); err != nil {
		return readiness, err
	}

	artifactPaths := []string{graphPath, claimsPath, claimAuditPath, readinessPath}
	for _, path := range adoptionResult.Paths {
		artifactPaths = append(artifactPaths, path)
	}
	if err := writeProjectReconstructionReceipt(reconstructionReceiptPath, projectDir, domain, revision, graphDigest, readiness.State, artifactPaths); err != nil {
		return readiness, err
	}
	if err := injectFailure(failAfterReceipt, &cleanupStaging); err != nil {
		return readiness, err
	}

	// 5. Durability Hardening (flush staged files and directory)
	if err := flushStagingFiles(projectDir); err != nil {
		return readiness, err
	}
	if err := flushDir(projectDir); err != nil {
		return readiness, err
	}

	// 6. Validate Staged Family before rename
	stagedClass, err := classifyActiveProjectFamily(projectDir)
	if err != nil || stagedClass.Status != projectFamilyCoherent {
		detail := ""
		if err != nil {
			detail = err.Error()
		} else {
			detail = stagedClass.Detail
		}
		return readiness, fmt.Errorf("staged project family is not coherent: %s", detail)
	}
	if err := injectFailure(failAfterValidation, &cleanupStaging); err != nil {
		return readiness, err
	}

	// Update Phase to validated
	marker.Phase = phaseValidated
	if err := writeTxMarker(txMarkerPath, marker); err != nil {
		return readiness, err
	}

	// 7. Recheck Repository Revision immediately before activation
	currentRevision, _ := projectRevisionBinding(root)
	if currentRevision != marker.RepoRevision {
		return readiness, fmt.Errorf("repository revision changed during staging (was %s, now %s); activation failed closed", marker.RepoRevision, currentRevision)
	}

	// 8. Move Prior Family (Publication Rename)
	if prior.Status == projectFamilyInvalid {
		if err := injectFailure(failBeforeMoveInvalid, &cleanupStaging); err != nil {
			return readiness, err
		}
		marker.Phase = phasePriorMoved
		if err := writeTxMarker(txMarkerPath, marker); err != nil {
			return readiness, err
		}

		diagnosticDir := filepath.Join(projectParent, "project-invalid-"+txID)
		if err := os.Rename(activeProjectDir, diagnosticDir); err != nil {
			return readiness, fmt.Errorf("preserve invalid project family: %w", err)
		}
		if err := os.WriteFile(filepath.Join(diagnosticDir, "NON_CERTIFYING"), []byte("invalid project artifact family; preserved for diagnostics\n"), 0o600); err != nil {
			return readiness, fmt.Errorf("mark invalid project family: %w", err)
		}
		if err := flushDir(projectParent); err != nil {
			return readiness, err
		}
		if err := injectFailure(failAfterMoveInvalid, &cleanupStaging); err != nil {
			return readiness, err
		}
	} else if prior.Status == projectFamilyCoherent {
		if err := injectFailure(failBeforeMoveCoherent, &cleanupStaging); err != nil {
			return readiness, err
		}
		marker.Phase = phasePriorMoved
		if err := writeTxMarker(txMarkerPath, marker); err != nil {
			return readiness, err
		}

		backupDir := activeProjectDir + ".previous"
		if err := os.RemoveAll(backupDir); err != nil {
			return readiness, err
		}
		if err := os.Rename(activeProjectDir, backupDir); err != nil {
			return readiness, fmt.Errorf("stage prior project generation: %w", err)
		}
		if err := flushDir(projectParent); err != nil {
			return readiness, err
		}
		if err := injectFailure(failAfterMoveCoherent, &cleanupStaging); err != nil {
			return readiness, err
		}
	} else {
		// Prior family was absent. Mark phasePriorMoved so recovery knows we are moving to activate.
		marker.Phase = phasePriorMoved
		if err := writeTxMarker(txMarkerPath, marker); err != nil {
			return readiness, err
		}
	}

	// 9. Activate Staged Directory
	if err := injectFailure(failBeforeActivation, &cleanupStaging); err != nil {
		return readiness, err
	}
	if err := injectFailure(failDuringActivation, &cleanupStaging); err != nil {
		return readiness, err
	}

	if err := os.Rename(projectDir, activeProjectDir); err != nil {
		// If prior coherent existed, roll back rename
		if prior.Status == projectFamilyCoherent {
			backupDir := activeProjectDir + ".previous"
			if rerr := os.Rename(backupDir, activeProjectDir); rerr != nil {
				return readiness, fmt.Errorf("activate project generation failed: %w (rollback failed: %v)", err, rerr)
			}
		}
		return readiness, fmt.Errorf("activate project generation: %w", err)
	}

	// Update Phase to activated
	marker.Phase = phaseActivated
	if err := writeTxMarker(txMarkerPath, marker); err != nil {
		return readiness, err
	}
	if err := flushDir(projectParent); err != nil {
		return readiness, err
	}
	if err := injectFailure(failAfterActivation, &cleanupStaging); err != nil {
		return readiness, err
	}

	// 10. Final validation and cleanup
	finalClass, err := classifyActiveProjectFamily(activeProjectDir)
	if err != nil || finalClass.Status != projectFamilyCoherent {
		// Roll back
		if prior.Status == projectFamilyCoherent {
			backupDir := activeProjectDir + ".previous"
			os.RemoveAll(activeProjectDir)
			os.Rename(backupDir, activeProjectDir)
		} else {
			os.RemoveAll(activeProjectDir)
		}
		detail := ""
		if err != nil {
			detail = err.Error()
		} else {
			detail = finalClass.Detail
		}
		return readiness, fmt.Errorf("final active project validation failed: %s", detail)
	}

	// Remove marker and backup
	os.Remove(txMarkerPath)
	if prior.Status == projectFamilyCoherent {
		backupDir := activeProjectDir + ".previous"
		os.RemoveAll(backupDir)
	}
	flushDir(projectParent)

	cleanupStaging = false
	return readiness, nil
}

func writeTxMarker(path string, marker transactionMarker) error {
	data, err := yaml.Marshal(marker)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	if err := flushFile(path); err != nil {
		return err
	}
	return flushDir(filepath.Dir(path))
}

func stripMachineAdoptedIntentSubjects(raw []byte) ([]byte, error) {
	triples, err := graphsnapshot.Read(strings.NewReader(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("parse provisional project graph: %w", err)
	}
	intentClasses := map[string]bool{
		rdf.ClassIntent: true, rdf.ClassDesignIntent: true, rdf.ClassOperationalIntent: true,
		rdf.ClassProductIntent: true, rdf.ClassConstraintIntent: true,
	}
	intentSubjects, machineSubjects := map[string]bool{}, map[string]bool{}
	for _, triple := range triples {
		if triple.Predicate == rdf.PropType && triple.ObjectIsIRI && intentClasses[triple.Object] {
			intentSubjects[triple.Subject] = true
		}
		if (triple.Predicate == rdf.PropStatus || triple.Predicate == rdf.PropPromotionStatus) && triple.Object == adoption.PromotionMachineAdopted {
			machineSubjects[triple.Subject] = true
		}
	}
	remove := map[string]bool{}
	for subject := range intentSubjects {
		if machineSubjects[subject] {
			remove[subject] = true
		}
	}
	if len(remove) == 0 {
		return raw, nil
	}
	var lines []string
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		removed := false
		for subject := range remove {
			if strings.HasPrefix(line, "<"+subject+"> ") {
				removed = true
				break
			}
		}
		if !removed {
			lines = append(lines, line)
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func finalizeMachineAdoptedIntentReceipts(awarenessDir, revision, graphDigest, decisionTimestamp string) error {
	paths, err := filepath.Glob(filepath.Join(awarenessDir, "intent_*.yaml"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var receipt adoption.Receipt
		if unmarshalErr := yaml.Unmarshal(raw, &receipt); unmarshalErr != nil {
			return unmarshalErr
		}
		if receipt.Status != adoption.PromotionMachineAdopted && receipt.PromotionStatus != adoption.PromotionMachineAdopted {
			continue
		}
		var document yaml.Node
		if unmarshalErr := yaml.Unmarshal(raw, &document); unmarshalErr != nil {
			return unmarshalErr
		}
		if revision == "" || graphDigest == "" || decisionTimestamp == "" {
			setTopLevelYAMLScalar(&document, "status", "staged")
			setTopLevelYAMLScalar(&document, "promotion_status", adoption.PromotionCandidate)
			setTopLevelYAMLScalar(&document, "epistemic_status", "unknown")
		} else {
			setTopLevelYAMLScalar(&document, "decision_timestamp", decisionTimestamp)
			setTopLevelYAMLScalar(&document, "valid_for_revision", revision)
			setTopLevelYAMLScalar(&document, "valid_for_graph_digest", graphDigest)
		}
		updated, marshalErr := yaml.Marshal(&document)
		if marshalErr != nil {
			return marshalErr
		}
		var finalized adoption.Receipt
		if unmarshalErr := yaml.Unmarshal(updated, &finalized); unmarshalErr != nil {
			return unmarshalErr
		}
		if finalized.Status == adoption.PromotionMachineAdopted {
			if validateErr := adoption.ValidateMachineAdoption(finalized); validateErr != nil {
				return fmt.Errorf("%s: %w", path, validateErr)
			}
		}
		if !bytes.Equal(raw, updated) {
			if writeErr := writeFileAtomic(path, updated); writeErr != nil {
				return writeErr
			}
		}
	}
	return nil
}

func setTopLevelYAMLScalar(document *yaml.Node, key, value string) {
	if document == nil || len(document.Content) == 0 {
		return
	}
	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1].Kind = yaml.ScalarNode
			mapping.Content[i+1].Tag = "!!str"
			mapping.Content[i+1].Value = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

type projectReconstructionArtifactReceipt struct {
	Path         string `yaml:"path"`
	SHA256Digest string `yaml:"sha256_digest"`
}

type projectReconstructionReceipt struct {
	SchemaVersion                string                                 `yaml:"schema_version"`
	GeneratedBy                  string                                 `yaml:"generated_by"`
	RepositoryDomain             string                                 `yaml:"repository_domain"`
	Revision                     string                                 `yaml:"revision"`
	FinalGraphDigestSHA256       string                                 `yaml:"final_graph_digest_sha256"`
	ReadinessState               string                                 `yaml:"readiness_state"`
	DeterministicSecondPass      bool                                   `yaml:"deterministic_second_pass"`
	ClaimsBoundToFinalGraph      bool                                   `yaml:"claims_bound_to_final_graph"`
	ExternalProofCreatedByImport bool                                   `yaml:"external_proof_created_by_import"`
	Artifacts                    []projectReconstructionArtifactReceipt `yaml:"artifacts"`
}

type projectFamilyStatus string

type projectFamilyInvalidReason string

const (
	invalidReceipt   projectFamilyInvalidReason = "receipt"
	invalidPath      projectFamilyInvalidReason = "path"
	invalidShape     projectFamilyInvalidReason = "shape"
	invalidDigest    projectFamilyInvalidReason = "digest"
	invalidGraph     projectFamilyInvalidReason = "graph"
	invalidClaims    projectFamilyInvalidReason = "claims"
	invalidReadiness projectFamilyInvalidReason = "readiness"
	invalidRevision  projectFamilyInvalidReason = "revision"
)

type projectFamilyClassification struct {
	Status projectFamilyStatus
	Reason projectFamilyInvalidReason
	Detail string
}

const (
	projectFamilyAbsent   projectFamilyStatus = "absent"
	projectFamilyCoherent projectFamilyStatus = "coherent"
	projectFamilyInvalid  projectFamilyStatus = "invalid"
)

// classifyActiveProjectFamily verifies the published family before it can be
// considered rollback authority. An incomplete or cross-generation family is
// diagnostic residue, never a generation to restore.
func classifyActiveProjectFamily(projectDir string) (projectFamilyClassification, error) {
	receiptPath := filepath.Join(projectDir, "reconstruction-receipt.yaml")
	data, err := os.ReadFile(receiptPath)
	if os.IsNotExist(err) {
		if _, statErr := os.Stat(projectDir); os.IsNotExist(statErr) {
			return projectFamilyClassification{Status: projectFamilyAbsent, Detail: "project directory absent"}, nil
		}
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReceipt, Detail: "missing reconstruction receipt"}, nil
	}
	if err != nil {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReceipt, Detail: err.Error()}, err
	}
	var receipt projectReconstructionReceipt
	if err := yaml.Unmarshal(data, &receipt); err != nil {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReceipt, Detail: "malformed receipt yaml"}, nil
	}
	if receipt.Revision == "" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReceipt, Detail: "missing revision in receipt"}, nil
	}
	if receipt.FinalGraphDigestSHA256 == "" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReceipt, Detail: "missing graph digest in receipt"}, nil
	}
	required := map[string]bool{
		".sensei/project/graph.nt": true, ".sensei/project/claims.yaml": true,
		".sensei/project/claim-audit.yaml": true, ".sensei/project/readiness.yaml": true,
		".sensei/project/knowledge/adoption-report.yaml": true,
	}
	seen := map[string]bool{}
	for _, artifact := range receipt.Artifacts {
		rel := filepath.ToSlash(filepath.Clean(artifact.Path))
		if !strings.HasPrefix(rel, ".sensei/project/") {
			return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidPath, Detail: fmt.Sprintf("non-canonical path: %s", rel)}, nil
		}
		if filepath.IsAbs(rel) || strings.Contains(rel, "../") {
			return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidPath, Detail: fmt.Sprintf("path traversal or absolute path: %s", rel)}, nil
		}
		if seen[rel] {
			return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidPath, Detail: fmt.Sprintf("duplicate receipt path: %s", rel)}, nil
		}
		seen[rel] = true
		subpath := strings.TrimPrefix(rel, ".sensei/project/")
		body, readErr := os.ReadFile(filepath.Join(projectDir, filepath.FromSlash(subpath)))
		if readErr != nil {
			return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidDigest, Detail: fmt.Sprintf("failed to read artifact %s: %v", rel, readErr)}, nil
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != artifact.SHA256Digest {
			return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidDigest, Detail: fmt.Sprintf("digest mismatch for %s: got %s, want %s", rel, hex.EncodeToString(sum[:]), artifact.SHA256Digest)}, nil
		}
	}
	for path := range required {
		if !seen[path] {
			return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidShape, Detail: fmt.Sprintf("missing required artifact: %s", path)}, nil
		}
	}
	graph, err := os.ReadFile(filepath.Join(projectDir, "graph.nt"))
	if err != nil {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidGraph, Detail: fmt.Sprintf("failed to read graph: %v", err)}, nil
	}
	graphSum := sha256.Sum256(graph)
	if hex.EncodeToString(graphSum[:]) != receipt.FinalGraphDigestSHA256 {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidGraph, Detail: fmt.Sprintf("graph digest mismatch against receipt: got %s, receipt has %s", hex.EncodeToString(graphSum[:]), receipt.FinalGraphDigestSHA256)}, nil
	}
	if len(extractor.ValidateNTriples(bytes.NewReader(graph))) != 0 {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidGraph, Detail: "malformed graph NTriples"}, nil
	}
	claims, err := architecture.LoadClaimDocument(filepath.Join(projectDir, "claims.yaml"))
	if err != nil {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidClaims, Detail: fmt.Sprintf("failed to load claims: %v", err)}, nil
	}
	if claims.Binding.RepositoryDomain != receipt.RepositoryDomain {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidClaims, Detail: fmt.Sprintf("claims domain mismatch: got %s, receipt has %s", claims.Binding.RepositoryDomain, receipt.RepositoryDomain)}, nil
	}
	if claims.Binding.Revision != receipt.Revision {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidClaims, Detail: fmt.Sprintf("claims revision mismatch: got %s, receipt has %s", claims.Binding.Revision, receipt.Revision)}, nil
	}
	if claims.Binding.RevisionStatus != architecture.RevisionResolved {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidClaims, Detail: fmt.Sprintf("unresolved claims revision status: %s", claims.Binding.RevisionStatus)}, nil
	}
	if claims.Binding.GraphDigestSHA256 != receipt.FinalGraphDigestSHA256 {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidClaims, Detail: fmt.Sprintf("claims graph digest mismatch: got %s, receipt has %s", claims.Binding.GraphDigestSHA256, receipt.FinalGraphDigestSHA256)}, nil
	}
	if claims.Binding.GraphDigestStatus != architecture.GraphDigestResolved {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidClaims, Detail: fmt.Sprintf("unresolved claims graph digest status: %s", claims.Binding.GraphDigestStatus)}, nil
	}
	readinessBytes, err := os.ReadFile(filepath.Join(projectDir, "readiness.yaml"))
	if err != nil {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("failed to read readiness: %v", err)}, nil
	}
	var readinessEnv phase2ReadinessEnvelope
	if yaml.Unmarshal(readinessBytes, &readinessEnv) != nil {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: "failed to unmarshal readiness"}, nil
	}
	r := readinessEnv.Phase2Readiness
	if r.RepositoryDomain != receipt.RepositoryDomain {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness domain mismatch: got %s, receipt has %s", r.RepositoryDomain, receipt.RepositoryDomain)}, nil
	}
	if r.GraphDigestSHA256 != receipt.FinalGraphDigestSHA256 {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness graph digest mismatch: got %s, receipt has %s", r.GraphDigestSHA256, receipt.FinalGraphDigestSHA256)}, nil
	}
	if r.State != receipt.ReadinessState {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness state mismatch: got %s, receipt has %s", r.State, receipt.ReadinessState)}, nil
	}
	if r.GraphPath != ".sensei/project/graph.nt" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness graph path mismatch: %s", r.GraphPath)}, nil
	}
	if r.ClaimsPath != ".sensei/project/claims.yaml" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness claims path mismatch: %s", r.ClaimsPath)}, nil
	}
	if r.ClaimAuditPath != ".sensei/project/claim-audit.yaml" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness audit path mismatch: %s", r.ClaimAuditPath)}, nil
	}
	if r.AdoptionReportPath != ".sensei/project/knowledge/adoption-report.yaml" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness adoption path mismatch: %s", r.AdoptionReportPath)}, nil
	}
	if r.ReconstructionReceiptPath != ".sensei/project/reconstruction-receipt.yaml" {
		return projectFamilyClassification{Status: projectFamilyInvalid, Reason: invalidReadiness, Detail: fmt.Sprintf("readiness receipt path mismatch: %s", r.ReconstructionReceiptPath)}, nil
	}
	return projectFamilyClassification{Status: projectFamilyCoherent, Detail: "coherent project family"}, nil
}

func writeProjectReconstructionReceipt(path, projectRoot, domain, revision, graphDigest, readinessState string, artifactPaths []string) error {
	unique := map[string]bool{}
	for _, artifactPath := range artifactPaths {
		if strings.TrimSpace(artifactPath) != "" {
			unique[filepath.Clean(artifactPath)] = true
		}
	}
	ordered := make([]string, 0, len(unique))
	for artifactPath := range unique {
		ordered = append(ordered, artifactPath)
	}
	sort.Strings(ordered)
	receipt := projectReconstructionReceipt{
		SchemaVersion: "1", GeneratedBy: "sensei import project reconstruction", RepositoryDomain: domain,
		Revision: revision, FinalGraphDigestSHA256: graphDigest, ReadinessState: readinessState,
		DeterministicSecondPass: true, ClaimsBoundToFinalGraph: true, ExternalProofCreatedByImport: false,
		Artifacts: []projectReconstructionArtifactReceipt{},
	}
	for _, artifactPath := range ordered {
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			return fmt.Errorf("read reconstruction artifact %s: %w", artifactPath, err)
		}
		canonicalPath := canonicalProjectArtifactPath(projectRoot, artifactPath)
		if canonicalPath == "" {
			return fmt.Errorf("reconstruction artifact escapes staging root: %s", artifactPath)
		}
		sum := sha256.Sum256(data)
		receipt.Artifacts = append(receipt.Artifacts, projectReconstructionArtifactReceipt{
			Path: canonicalPath, SHA256Digest: hex.EncodeToString(sum[:]),
		})
	}
	data, err := yaml.Marshal(receipt)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

func canonicalProjectArtifactPath(projectRoot, artifactPath string) string {
	rel, err := filepath.Rel(projectRoot, artifactPath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return ".sensei/project/" + filepath.ToSlash(rel)
}

func projectClaimAuditOptions(root string) claimaudit.Options {
	var coreFiles []string
	if eligible, err := eligibleProjectSourceFiles(root); err == nil {
		for _, file := range eligible {
			if !strings.Contains(file, "/") {
				coreFiles = append(coreFiles, file)
			}
		}
	}
	rootComponentID := ""
	if component, err := importgraph.DetectGoRootComponent(root); err == nil && component != nil {
		rootComponentID = component.ID
	}
	return claimaudit.Options{RootComponentID: rootComponentID, CoreFiles: coreFiles}
}

func canonicalProjectNTriples(raw []byte) []byte {
	lines := make([]string, 0, bytes.Count(raw, []byte{'\n'})+1)
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return nil
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func firstGraphDifference(first, second []byte) string {
	firstLines := strings.Split(string(first), "\n")
	secondLines := strings.Split(string(second), "\n")
	limit := len(firstLines)
	if len(secondLines) < limit {
		limit = len(secondLines)
	}
	for i := 0; i < limit; i++ {
		if firstLines[i] != secondLines[i] {
			return fmt.Sprintf("first difference at line %d: %q != %q", i+1, firstLines[i], secondLines[i])
		}
	}
	return fmt.Sprintf("line counts differ: %d != %d", len(firstLines), len(secondLines))
}

func projectRevisionBinding(root string) (revision, decisionTimestamp string) {
	revision, status := gitHeadRevision(root)
	if status != "resolved" || revision == "" {
		return "", ""
	}
	out, err := exec.Command("git", "-C", root, "show", "-s", "--format=%cI", revision).Output()
	if err != nil {
		return revision, ""
	}
	return revision, strings.TrimSpace(string(out))
}

func assessPhase2Readiness(root, domain, graphPath, claimsPath string, graphData []byte, claims architecture.ClaimDocument, semantic ...phase2SemanticInputs) (phase2Readiness, error) {
	eligible, err := eligibleProjectSourceFiles(root)
	if err != nil {
		return phase2Readiness{}, err
	}
	triples, err := graphsnapshot.Read(strings.NewReader(string(graphData)))
	if err != nil {
		return phase2Readiness{}, err
	}
	typed := map[string]map[string]bool{}
	machineAdopted := map[string]bool{}
	semanticFiles := map[string]bool{}
	knowledgeSubjects := map[string]bool{}
	for _, triple := range triples {
		if triple.Predicate == rdf.PropType && triple.ObjectIsIRI {
			if typed[triple.Object] == nil {
				typed[triple.Object] = map[string]bool{}
			}
			typed[triple.Object][triple.Subject] = true
		}
		if (triple.Predicate == rdf.PropStatus || triple.Predicate == rdf.PropPromotionStatus) && triple.Object == "machine_adopted" {
			machineAdopted[triple.Subject] = true
		}
	}
	for _, class := range []string{rdf.ClassInvariant, rdf.ClassFailureMode, rdf.ClassForbiddenFix, rdf.ClassBoundary, rdf.ClassDecision, rdf.ClassContract, rdf.ClassIncident, rdf.ClassIntent} {
		for subject := range typed[class] {
			knowledgeSubjects[subject] = true
		}
	}
	for _, triple := range triples {
		if triple.ObjectIsIRI && knowledgeSubjects[triple.Object] && triple.Predicate == rdf.PropImplements {
			for fileIRI := range typed[rdf.ClassSourceFile] {
				if triple.Subject == fileIRI {
					semanticFiles[fileIRI] = true
				}
			}
		}
		if triple.ObjectIsIRI && knowledgeSubjects[triple.Subject] && (triple.Predicate == rdf.PropProtects || triple.Predicate == rdf.PropAnchoredIn || triple.Predicate == rdf.PropExpressedBy) {
			semanticFiles[triple.Object] = true
		}
	}

	semanticInput := phase2SemanticInputs{}
	if len(semantic) > 0 {
		semanticInput = semantic[0]
	}

	represented := 0
	rootEligible := 0
	rootRepresented := 0
	var readyCore, missingCore, missing, coreWithClaims, coreWithSemantic []string
	claimCountByFile := map[string]int{}
	for _, count := range semanticInput.Audit.ClaimsAnchoredToCoreFiles {
		claimCountByFile[count.File] = count.Count
	}
	for _, file := range eligible {
		iri := strings.Trim(rdf.MintIRI(rdf.ClassSourceFile, file), "<>")
		found := typed[rdf.ClassSourceFile][iri]
		if found {
			represented++
		} else {
			missing = append(missing, file)
		}
		if !strings.Contains(file, "/") {
			rootEligible++
			if found {
				rootRepresented++
				readyCore = append(readyCore, file)
				if claimCountByFile[file] > 0 {
					coreWithClaims = append(coreWithClaims, file)
				}
				if semanticFiles[iri] || claimCountByFile[file] > 0 {
					coreWithSemantic = append(coreWithSemantic, file)
				}
			} else {
				missingCore = append(missingCore, file)
			}
		}
	}
	sum := sha256.Sum256(graphData)
	report := phase2Readiness{
		SchemaVersion:                           "1",
		GeneratedBy:                             "sensei import",
		RepositoryDomain:                        domain,
		State:                                   readinessReady,
		GraphPath:                               filepath.ToSlash(relativeProjectPath(root, graphPath)),
		GraphDigestSHA256:                       hex.EncodeToString(sum[:]),
		ClaimsPath:                              filepath.ToSlash(relativeProjectPath(root, claimsPath)),
		ClaimAuditPath:                          filepath.ToSlash(relativeProjectPath(root, filepath.Join(root, ".sensei", "project", "claim-audit.yaml"))),
		AdoptionReportPath:                      filepath.ToSlash(relativeProjectPath(root, semanticInput.AdoptionPath)),
		ReconstructionReceiptPath:               ".sensei/project/reconstruction-receipt.yaml",
		EligibleSourceFiles:                     len(eligible),
		RepresentedSourceFiles:                  represented,
		StructuralSourceCoverage:                fmt.Sprintf("%d/%d", represented, len(eligible)),
		EligibleRootFiles:                       rootEligible,
		RepresentedRootFiles:                    rootRepresented,
		RootPackageCoverage:                     fmt.Sprintf("%d/%d", rootRepresented, rootEligible),
		CodeSymbolCount:                         len(typed[rdf.ClassCodeSymbol]),
		ArchitectureFactCount:                   semanticInput.FactCount,
		GoSemanticFactCount:                     semanticInput.GoSemanticFactCount,
		ClaimCount:                              len(claims.Claims),
		DistinctPropositionCount:                semanticInput.Audit.DistinctPropositionKeys,
		MachineAdoptedKnowledge:                 len(machineAdopted),
		AdoptedKnowledgeByClass:                 semanticInput.Adoption.Classes,
		MachineAdoptedKnowledgeByClass:          knowledgeClassCounts(semanticInput.Adoption.Classes, "machine_adopted"),
		StagedKnowledgeByClass:                  knowledgeClassCounts(semanticInput.Adoption.Classes, "staged"),
		RejectedKnowledgeByClass:                knowledgeClassCounts(semanticInput.Adoption.Classes, "rejected"),
		ClaimsByRule:                            semanticInput.Audit.ClaimsByInferenceRule,
		DecisionCount:                           len(typed[rdf.ClassDecision]),
		ContractCount:                           len(typed[rdf.ClassContract]),
		FailureModeCount:                        len(typed[rdf.ClassFailureMode]),
		ForbiddenFixCount:                       len(typed[rdf.ClassForbiddenFix]),
		BoundaryCount:                           len(typed[rdf.ClassBoundary]),
		IncidentCount:                           len(typed[rdf.ClassIncident]),
		ClaimAuditValid:                         semanticInput.AuditValid,
		ProjectGraphDeterministic:               semanticInput.GraphDeterministic,
		RootCoreFilesWithClaims:                 coreWithClaims,
		TaskReadyCoreFilesWithSemanticKnowledge: coreWithSemantic,
		TaskReadyCoreFiles:                      readyCore,
		UnrepresentedCoreFiles:                  missingCore,
		UnrepresentedSourceFiles:                missing,
	}
	for _, decision := range semanticInput.Adoption.Decisions {
		if decision.CandidateSource != "history" {
			if decision.CandidateSource == "intent_materialization" && decision.CandidateClass == knowledgeadoption.ClassContract+"Candidate" && decision.Outcome == knowledgeadoption.OutcomeStaged {
				report.ContractLikeIntentsStaged++
			}
			continue
		}
		switch decision.Outcome {
		case knowledgeadoption.OutcomeMachineAdopted:
			report.HistoryCandidatesAdopted++
		case knowledgeadoption.OutcomeRejected:
			report.HistoryCandidatesRejected++
		default:
			report.HistoryCandidatesStaged++
		}
		for _, reason := range decision.Reasons {
			if strings.Contains(reason, "identity") || strings.Contains(reason, "collid") {
				report.State = readinessUncertifiable
				report.Limitations = append(report.Limitations, "knowledge adoption identity failure: "+decision.CandidateID)
			}
		}
	}
	switch {
	case represented != len(eligible) || rootRepresented != rootEligible:
		report.State = readinessStructurallyThin
		report.Limitations = append(report.Limitations, "not every eligible source file is represented in the compiled graph")
	case len(claims.Claims) == 0:
		report.State = readinessInferenceMissing
		report.Limitations = append(report.Limitations, "deterministic inference produced no architecture claims")
	case report.CodeSymbolCount == 0:
		report.State = readinessPartiallyReady
		report.Limitations = append(report.Limitations, "the compiled graph contains no code symbols")
	case !semanticInput.AuditValid:
		report.State = readinessUncertifiable
		report.Limitations = append(report.Limitations, "claim audit is missing or invalid")
	case !semanticInput.GraphDeterministic:
		report.State = readinessUncertifiable
		report.Limitations = append(report.Limitations, "project graph did not prove deterministic convergence")
	}
	if report.State == readinessReady && len(machineAdopted) == 0 {
		report.Limitations = append(report.Limitations, "ready_for_task_assessment_but_limited_semantic_adoption")
	}
	return report, nil
}

func knowledgeClassCounts(classes []knowledgeadoption.ClassSummary, field string) []claimaudit.Count {
	counts := make([]claimaudit.Count, 0, len(classes))
	for _, summary := range classes {
		count := 0
		switch field {
		case "machine_adopted":
			count = summary.MachineAdopted
		case "staged":
			count = summary.Staged
		case "rejected":
			count = summary.Rejected
		}
		counts = append(counts, claimaudit.Count{Key: summary.Class, Count: count})
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i].Key < counts[j].Key })
	return counts
}

func eligibleProjectSourceFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry iofs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (bootstrapExcludedDir(entry.Name()) || strings.HasPrefix(entry.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(entry.Name()) || isTestFile(entry.Name()) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func relativeProjectPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
