// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	repairClassificationValidRepair             = "valid_repair"
	repairClassificationMissingContract         = "missing_contract"
	repairClassificationStaleAuthority          = "stale_authority"
	repairClassificationInsufficientEvidence    = "insufficient_evidence"
	repairClassificationScopeDrift              = "scope_drift"
	repairClassificationForbiddenMoveDetected   = "forbidden_move_detected"
	repairClassificationProjectPlaneUnavailable = "project_plane_unavailable"
	repairClassificationBackingStoreUnavailable = "backing_store_unavailable"
)

type repairReportTarget struct {
	TaskSummary  string `json:"task_summary,omitempty"`
	IssueSummary string `json:"issue_summary,omitempty"`
}

type repairContractSummary struct {
	Status      string   `json:"status"`
	Summary     []string `json:"summary,omitempty"`
	ContractIDs []string `json:"contract_ids,omitempty"`
}

type repairAuthoritySummary struct {
	State                 string `json:"state"`
	Authoritative         bool   `json:"authoritative"`
	GraphFreshnessState   string `json:"graph_freshness_state,omitempty"`
	BuildProvenanceState  string `json:"build_provenance_state,omitempty"`
	CoverageState         string `json:"coverage_state,omitempty"`
	Detail                string `json:"detail,omitempty"`
	LiveGraphDigestSha256 string `json:"live_graph_digest_sha256,omitempty"`
}

type repairEvidenceSummary struct {
	Status               string   `json:"status"`
	RequiredTests        []string `json:"required_tests,omitempty"`
	TestsRun             []string `json:"tests_run,omitempty"`
	MissingRequiredTests []string `json:"missing_required_tests,omitempty"`
	ChecksPassed         []string `json:"checks_passed,omitempty"`
	ChecksFailed         []string `json:"checks_failed,omitempty"`
	Summary              []string `json:"summary,omitempty"`
}

type repairFinding struct {
	ID          string `json:"id,omitempty"`
	File        string `json:"file,omitempty"`
	Message     string `json:"message"`
	Detail      string `json:"detail,omitempty"`
	Enforcement string `json:"enforcement,omitempty"`
}

type governedRepairReport struct {
	SchemaVersion         string                 `json:"schema_version"`
	RepairTarget          repairReportTarget     `json:"repair_target"`
	TouchedFiles          []string               `json:"touched_files,omitempty"`
	ExplicitScope         []string               `json:"explicit_scope,omitempty"`
	GuardedPaths          []string               `json:"guarded_paths,omitempty"`
	GoverningContract     repairContractSummary  `json:"governing_contract"`
	Authority             repairAuthoritySummary `json:"authority"`
	Evidence              repairEvidenceSummary  `json:"evidence"`
	RequiredActions       []string               `json:"required_actions,omitempty"`
	ForbiddenMoveFindings []repairFinding        `json:"forbidden_move_findings,omitempty"`
	ScopeDriftFindings    []repairFinding        `json:"scope_drift_findings,omitempty"`
	BlindSpots            []string               `json:"blind_spots,omitempty"`
	PreflightStatus       string                 `json:"preflight_status,omitempty"`
	RiskClass             string                 `json:"risk_class,omitempty"`
	Confidence            string                 `json:"confidence,omitempty"`
	FinalClassification   string                 `json:"final_classification"`
}

type repairGateVerdict struct {
	Classification string `json:"classification"`
	Pass           bool   `json:"pass"`
	Reason         string `json:"reason"`
}

var repairReportMetadata = metadataRPC
var repairReportPreflight = fetchRepairPlanPreflight
var repairReportEditCheck = func(ctx context.Context, addr string, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
	client, err := connectAWG(addr)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return client.Stub().EditCheck(ctx, req)
}

func runRepairReport(args []string) int {
	fs := flag.NewFlagSet("awg repair-report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	task := fs.String("task", "", "task or issue summary")
	issue := fs.String("issue", "", "issue summary override (defaults to --task)")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	repoRoot := fs.String("repo-root", ".", "repository root")
	diff := fs.String("diff", "", "git diff range used to discover touched files")
	outPath := fs.String("out", "", "write the machine-readable JSON report artifact to this path")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "deprecated alias for --format json")
	domain := fs.String("domain", "", "domain/repo scope passed through to preflight/edit-check")
	mode := fs.String("mode", "standard", "preflight mode: standard | compact")
	var files stringSlice
	var scopeFiles stringSlice
	var testsRun stringSlice
	var checksPass stringSlice
	var checksFail stringSlice
	fs.Var(&files, "file", "touched file path (repeatable)")
	fs.Var(&scopeFiles, "scope-file", "explicit allowed scope file path (repeatable)")
	fs.Var(&testsRun, "test-run", "executed test id, function, or file path (repeatable)")
	fs.Var(&checksPass, "check-pass", "named check that passed (repeatable)")
	fs.Var(&checksFail, "check-fail", "named check that failed (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg repair-report [--file <path>]... [flags]

Build a governed repair report for the current post-edit state. The report
turns preflight, authority, forbidden-move checks, scope, and test evidence
into one stable repair classification and an exportable JSON artifact.

Touched files come from --file, then --diff, then git status.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}

	report, err := generateRepairReport(repairReportOptions{
		Task:       strings.TrimSpace(*task),
		Issue:      strings.TrimSpace(*issue),
		Addr:       *addr,
		RepoRoot:   *repoRoot,
		Diff:       strings.TrimSpace(*diff),
		Domain:     strings.TrimSpace(*domain),
		Mode:       strings.TrimSpace(*mode),
		Files:      dedupeStrings(files),
		ScopeFiles: dedupeStrings(scopeFiles),
		TestsRun:   dedupeStrings(testsRun),
		ChecksPass: dedupeStrings(checksPass),
		ChecksFail: dedupeStrings(checksFail),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg repair-report: %v\n", err)
		return 1
	}
	if *outPath != "" {
		if err := writeRepairReportArtifact(*outPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "awg repair-report: write artifact: %v\n", err)
			return 1
		}
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	default:
		fmt.Print(renderRepairReportText(report))
	}
	return 0
}

func runRepairGate(args []string) int {
	fs := flag.NewFlagSet("awg repair-gate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	reportPath := fs.String("report", "", "path to a JSON repair report artifact")
	task := fs.String("task", "", "task or issue summary")
	issue := fs.String("issue", "", "issue summary override (defaults to --task)")
	addr := fs.String("addr", "localhost:10120", "AWG gRPC server address")
	repoRoot := fs.String("repo-root", ".", "repository root")
	diff := fs.String("diff", "", "git diff range used to discover touched files")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "deprecated alias for --format json")
	domain := fs.String("domain", "", "domain/repo scope passed through to preflight/edit-check")
	mode := fs.String("mode", "standard", "preflight mode: standard | compact")
	var files stringSlice
	var scopeFiles stringSlice
	var testsRun stringSlice
	var checksPass stringSlice
	var checksFail stringSlice
	var allowClassifications stringSlice
	fs.Var(&files, "file", "touched file path (repeatable)")
	fs.Var(&scopeFiles, "scope-file", "explicit allowed scope file path (repeatable)")
	fs.Var(&testsRun, "test-run", "executed test id, function, or file path (repeatable)")
	fs.Var(&checksPass, "check-pass", "named check that passed (repeatable)")
	fs.Var(&checksFail, "check-fail", "named check that failed (repeatable)")
	fs.Var(&allowClassifications, "allow-classification", "classification allowed to pass as a warning state (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg repair-gate [--report <path>] [flags]

Fail-closed governed repair gate. Consumes a repair-report artifact or computes
the same classification directly, then exits non-zero unless the classification
is valid_repair or explicitly allowed.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON {
		*format = "json"
	}

	var report governedRepairReport
	if strings.TrimSpace(*reportPath) != "" {
		data, err := os.ReadFile(*reportPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg repair-gate: read report: %v\n", err)
			return 1
		}
		if err := json.Unmarshal(data, &report); err != nil {
			fmt.Fprintf(os.Stderr, "awg repair-gate: parse report: %v\n", err)
			return 1
		}
	} else {
		var err error
		report, err = generateRepairReport(repairReportOptions{
			Task:       strings.TrimSpace(*task),
			Issue:      strings.TrimSpace(*issue),
			Addr:       *addr,
			RepoRoot:   *repoRoot,
			Diff:       strings.TrimSpace(*diff),
			Domain:     strings.TrimSpace(*domain),
			Mode:       strings.TrimSpace(*mode),
			Files:      dedupeStrings(files),
			ScopeFiles: dedupeStrings(scopeFiles),
			TestsRun:   dedupeStrings(testsRun),
			ChecksPass: dedupeStrings(checksPass),
			ChecksFail: dedupeStrings(checksFail),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg repair-gate: %v\n", err)
			return 1
		}
	}

	verdict := evaluateRepairGate(report, dedupeStrings(allowClassifications))
	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(verdict)
	default:
		label := "FAIL"
		if verdict.Pass {
			label = "PASS"
		}
		fmt.Printf("AWG repair gate: %s — %s\n", label, verdict.Classification)
		fmt.Printf("  %s\n", verdict.Reason)
	}
	if verdict.Pass {
		return 0
	}
	return 1
}

type repairReportOptions struct {
	Task       string
	Issue      string
	Addr       string
	RepoRoot   string
	Diff       string
	Domain     string
	Mode       string
	Files      []string
	ScopeFiles []string
	TestsRun   []string
	ChecksPass []string
	ChecksFail []string
}

func generateRepairReport(opts repairReportOptions) (governedRepairReport, error) {
	root, err := filepath.Abs(opts.RepoRoot)
	if err != nil {
		return governedRepairReport{}, fmt.Errorf("resolve repo root: %w", err)
	}
	touchedFiles, err := collectTouchedFiles(root, opts.Diff, opts.Files)
	if err != nil {
		return governedRepairReport{}, err
	}
	if len(touchedFiles) == 0 {
		return governedRepairReport{}, fmt.Errorf("no touched files found; provide --file, --diff, or run inside a git worktree with changes")
	}
	scopeFiles := dedupeStrings(opts.ScopeFiles)
	guarded := guardedTouchedFiles(root, touchedFiles)

	report := governedRepairReport{
		SchemaVersion: "repair_report.v1",
		RepairTarget: repairReportTarget{
			TaskSummary:  opts.Task,
			IssueSummary: firstNonEmpty(opts.Issue, opts.Task),
		},
		TouchedFiles:  touchedFiles,
		ExplicitScope: scopeFiles,
		GuardedPaths:  guarded,
		Evidence: repairEvidenceSummary{
			TestsRun:     append([]string(nil), opts.TestsRun...),
			ChecksPassed: append([]string(nil), opts.ChecksPass...),
			ChecksFailed: append([]string(nil), opts.ChecksFail...),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	metadataResp, metadataErr := repairReportMetadata(ctx, opts.Addr)
	if metadataErr != nil {
		if isProjectPlaneUnavailable(metadataErr) {
			report.Authority = repairAuthoritySummary{State: repairClassificationProjectPlaneUnavailable}
			report.FinalClassification = repairClassificationProjectPlaneUnavailable
			report.GoverningContract = repairContractSummary{Status: repairClassificationMissingContract}
			report.Evidence.Status = "not_evaluated"
			return report, nil
		}
		if isBackingStoreUnavailable(metadataErr) {
			report.Authority = repairAuthoritySummary{State: repairClassificationBackingStoreUnavailable, Detail: formatReadSurfaceError("metadata", metadataErr)}
			report.FinalClassification = repairClassificationBackingStoreUnavailable
			report.GoverningContract = repairContractSummary{Status: repairClassificationMissingContract}
			report.Evidence.Status = "not_evaluated"
			return report, nil
		}
		return governedRepairReport{}, fmt.Errorf("metadata: %w", metadataErr)
	}

	pfMode := awarenesspb.PreflightMode_PREFLIGHT_STANDARD
	if strings.EqualFold(opts.Mode, "compact") {
		pfMode = awarenesspb.PreflightMode_PREFLIGHT_COMPACT
	}
	preflightResp, err := repairReportPreflight(ctx, opts.Addr, &awarenesspb.PreflightRequest{
		Task:   opts.Task,
		Files:  touchedFiles,
		Mode:   pfMode,
		Domain: opts.Domain,
	})
	if err != nil {
		if isStaleAuthorityError(err) {
			report.Authority = buildRepairAuthoritySummary(metadataResp, nil)
			report.Authority.State = "stale"
			report.GoverningContract = repairContractSummary{Status: repairClassificationMissingContract}
			report.Evidence.Status = "not_evaluated"
			report.Evidence.Summary = []string{"preflight refused to classify the repair because graph authority is stale"}
			report.BlindSpots = []string{strings.TrimSpace(err.Error())}
			report.FinalClassification = repairClassificationStaleAuthority
			return report, nil
		}
		if isBackingStoreUnavailable(err) {
			report.Authority = buildRepairAuthoritySummary(metadataResp, nil)
			report.Authority.State = repairClassificationBackingStoreUnavailable
			report.Authority.Detail = formatReadSurfaceError("preflight", err)
			report.FinalClassification = repairClassificationBackingStoreUnavailable
			report.GoverningContract = repairContractSummary{Status: repairClassificationMissingContract}
			report.Evidence.Status = "not_evaluated"
			return report, nil
		}
		return governedRepairReport{}, fmt.Errorf("preflight: %w", err)
	}

	report.PreflightStatus = strings.ToLower(strings.TrimPrefix(preflightResp.GetStatus().String(), "PREFLIGHT_STATUS_"))
	report.RiskClass = strings.ToLower(strings.TrimPrefix(preflightResp.GetRiskClass().String(), "RISK_CLASS_"))
	report.Confidence = strings.ToLower(strings.TrimPrefix(preflightResp.GetConfidence().String(), "CONFIDENCE_"))
	report.RequiredActions = dedupeStrings(preflightResp.GetRequiredActions())
	report.BlindSpots = dedupeStrings(preflightResp.GetBlindSpots())
	report.Authority = buildRepairAuthoritySummary(metadataResp, preflightResp.GetAuthority())
	report.GoverningContract = buildRepairContractSummary(preflightResp)
	report.Evidence = buildRepairEvidenceSummary(preflightResp, report.Evidence.TestsRun, report.Evidence.ChecksPassed, report.Evidence.ChecksFailed)
	report.ScopeDriftFindings = buildScopeDriftFindings(touchedFiles, scopeFiles)

	report.ForbiddenMoveFindings, err = collectForbiddenMoveFindings(ctx, opts.Addr, root, opts.Domain, touchedFiles)
	if err != nil {
		if isBackingStoreUnavailable(err) {
			report.Authority.State = repairClassificationBackingStoreUnavailable
			report.Authority.Detail = formatReadSurfaceError("edit-check", err)
			report.FinalClassification = repairClassificationBackingStoreUnavailable
			return report, nil
		}
		return governedRepairReport{}, fmt.Errorf("edit-check: %w", err)
	}

	report.FinalClassification = classifyRepairReport(report)
	return report, nil
}

func collectTouchedFiles(repoRoot, diff string, explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		return sortNormalizedPaths(explicit), nil
	}
	if strings.TrimSpace(diff) != "" {
		return gitChangedFilesForDiff(repoRoot, diff)
	}
	return sortNormalizedPaths(gitChangedFiles(repoRoot)), nil
}

func gitChangedFilesForDiff(repoRoot, diff string) ([]string, error) {
	changes, err := gitAddedLinesByFile(repoRoot, diff)
	if err != nil {
		return nil, err
	}
	var files []string
	for file := range changes {
		files = append(files, file)
	}
	return sortNormalizedPaths(files), nil
}

func sortNormalizedPaths(items []string) []string {
	set := map[string]bool{}
	var out []string
	for _, item := range items {
		trimmed := filepath.ToSlash(strings.TrimSpace(item))
		if trimmed == "" || set[trimmed] {
			continue
		}
		set[trimmed] = true
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}

func guardedTouchedFiles(root string, touched []string) []string {
	prefixes := readHighRiskPrefixes(root)
	if len(prefixes) == 0 {
		return nil
	}
	var guarded []string
	for _, file := range touched {
		for _, prefix := range prefixes {
			p := filepath.ToSlash(strings.TrimSpace(prefix))
			if p == "" {
				continue
			}
			if strings.HasPrefix(file, p) {
				guarded = append(guarded, file)
				break
			}
		}
	}
	return dedupeStrings(guarded)
}

func buildRepairAuthoritySummary(metadataResp *awarenesspb.MetadataResponse, authority *awarenesspb.GraphAuthority) repairAuthoritySummary {
	out := repairAuthoritySummary{
		State:                 "current",
		GraphFreshnessState:   strings.ToLower(strings.TrimPrefix(metadataResp.GetGraphFreshnessState().String(), "GRAPH_FRESHNESS_STATE_")),
		BuildProvenanceState:  strings.ToLower(strings.TrimPrefix(metadataResp.GetBuildProvenanceState().String(), "BUILD_PROVENANCE_STATE_")),
		CoverageState:         strings.ToLower(strings.TrimPrefix(metadataResp.GetCoverageState().String(), "COVERAGE_STATE_")),
		Detail:                strings.TrimSpace(metadataResp.GetGraphFreshnessDetail()),
		LiveGraphDigestSha256: metadataResp.GetLiveStoreGraphDigestSha256(),
	}
	if authority != nil {
		out.Authoritative = authority.GetAuthoritative()
		if state := strings.ToLower(strings.TrimPrefix(authority.GetGraphFreshnessState().String(), "GRAPH_FRESHNESS_STATE_")); state != "" && state != "graph_freshness_state_unspecified" {
			out.GraphFreshnessState = state
		}
		if detail := strings.TrimSpace(authority.GetGraphFreshnessDetail()); detail != "" {
			out.Detail = detail
		}
		if digest := authority.GetLiveStoreGraphDigestSha256(); digest != "" {
			out.LiveGraphDigestSha256 = digest
		}
	} else {
		out.Authoritative = false
	}
	if out.GraphFreshnessState != "current" || !out.Authoritative {
		out.State = "stale"
	}
	return out
}

func buildRepairContractSummary(resp *awarenesspb.PreflightResponse) repairContractSummary {
	summarySet := map[string]bool{}
	idSet := map[string]bool{}
	var summary []string
	var ids []string
	for _, node := range resp.GetDirectArchitecture() {
		if strings.EqualFold(node.GetClass(), "Contract") {
			addContractSummaryLine(summarySet, &summary, fmt.Sprintf("contract: %s", firstNonEmpty(node.GetLabel(), node.GetId())))
			addContractID(idSet, &ids, node.GetId())
		}
	}
	for _, node := range resp.GetDirectInvariants() {
		addContractSummaryLine(summarySet, &summary, fmt.Sprintf("invariant: %s", firstNonEmpty(node.GetLabel(), node.GetId())))
		addContractID(idSet, &ids, node.GetId())
	}
	for _, node := range resp.GetDirectIntents() {
		addContractSummaryLine(summarySet, &summary, fmt.Sprintf("intent: %s", firstNonEmpty(node.GetLabel(), node.GetId())))
		addContractID(idSet, &ids, node.GetId())
	}
	sort.Strings(summary)
	sort.Strings(ids)
	if len(summary) == 0 {
		return repairContractSummary{Status: repairClassificationMissingContract}
	}
	return repairContractSummary{
		Status:      "present",
		Summary:     summary,
		ContractIDs: ids,
	}
}

func addContractSummaryLine(seen map[string]bool, dst *[]string, item string) {
	item = strings.TrimSpace(item)
	if item == "" || seen[item] {
		return
	}
	seen[item] = true
	*dst = append(*dst, item)
}

func addContractID(seen map[string]bool, dst *[]string, item string) {
	item = strings.TrimSpace(item)
	if item == "" || seen[item] {
		return
	}
	seen[item] = true
	*dst = append(*dst, item)
}

func buildRepairEvidenceSummary(resp *awarenesspb.PreflightResponse, testsRun, checksPassed, checksFailed []string) repairEvidenceSummary {
	requiredTests := benchmarkJudgeRequiredTests([]benchmarkBriefFile{{RequiredTests: dedupeStrings(resp.GetTestsToRun())}})
	requiredTests = dedupeStrings(append(requiredTests, resp.GetTestsToRun()...))
	missingTests := benchmarkMissingRequiredTests(requiredTests, testsRun)
	out := repairEvidenceSummary{
		RequiredTests:        requiredTests,
		TestsRun:             dedupeStrings(testsRun),
		MissingRequiredTests: missingTests,
		ChecksPassed:         dedupeStrings(checksPassed),
		ChecksFailed:         dedupeStrings(checksFailed),
	}
	switch {
	case len(out.ChecksFailed) > 0:
		out.Status = "checks_failed"
		out.Summary = append(out.Summary, "one or more named checks failed")
	case len(requiredTests) > 0 && len(missingTests) > 0:
		out.Status = "missing_required_tests"
		out.Summary = append(out.Summary, "required test evidence is incomplete")
	case len(requiredTests) > 0:
		out.Status = "required_tests_satisfied"
		out.Summary = append(out.Summary, "required test evidence is present")
	default:
		out.Status = "not_required"
		out.Summary = append(out.Summary, "no contract-required test evidence was declared for this scope")
	}
	sort.Strings(out.Summary)
	return out
}

func buildScopeDriftFindings(touchedFiles, scopeFiles []string) []repairFinding {
	if len(scopeFiles) == 0 {
		return nil
	}
	scope := map[string]bool{}
	for _, file := range scopeFiles {
		scope[file] = true
	}
	var findings []repairFinding
	for _, file := range touchedFiles {
		if scope[file] {
			continue
		}
		findings = append(findings, repairFinding{
			File:    file,
			Message: "touched file is outside the explicit allowed scope",
		})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].File < findings[j].File })
	return findings
}

func collectForbiddenMoveFindings(ctx context.Context, addr, repoRoot, domain string, touchedFiles []string) ([]repairFinding, error) {
	var findings []repairFinding
	for _, file := range touchedFiles {
		content, err := readWorkingTreeContent(repoRoot, file)
		if err != nil {
			continue
		}
		resp, err := repairReportEditCheck(ctx, addr, &awarenesspb.EditCheckRequest{
			File:            file,
			ProposedContent: content,
			Domain:          domain,
		})
		if err != nil {
			return nil, err
		}
		for _, warning := range resp.GetWarnings() {
			if !strings.EqualFold(warning.GetClass(), "ForbiddenFix") && !strings.EqualFold(warning.GetEnforcement(), "block") {
				continue
			}
			findings = append(findings, repairFinding{
				ID:          warning.GetRuleId(),
				File:        file,
				Message:     warning.GetMessage(),
				Detail:      warning.GetDetail(),
				Enforcement: warning.GetEnforcement(),
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].ID < findings[j].ID
	})
	return findings, nil
}

func readWorkingTreeContent(repoRoot, relPath string) (string, error) {
	path := filepath.Join(repoRoot, filepath.FromSlash(relPath))
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", fmt.Errorf("read %s: %w", relPath, err)
	}
	return string(data), nil
}

func classifyRepairReport(report governedRepairReport) string {
	switch report.Authority.State {
	case repairClassificationProjectPlaneUnavailable:
		return repairClassificationProjectPlaneUnavailable
	case repairClassificationBackingStoreUnavailable:
		return repairClassificationBackingStoreUnavailable
	}
	if len(report.ForbiddenMoveFindings) > 0 {
		return repairClassificationForbiddenMoveDetected
	}
	if report.Authority.State == "stale" {
		return repairClassificationStaleAuthority
	}
	if len(report.ScopeDriftFindings) > 0 {
		return repairClassificationScopeDrift
	}
	if report.GoverningContract.Status == repairClassificationMissingContract {
		return repairClassificationMissingContract
	}
	if report.Evidence.Status == "checks_failed" || report.Evidence.Status == "missing_required_tests" {
		return repairClassificationInsufficientEvidence
	}
	return repairClassificationValidRepair
}

func evaluateRepairGate(report governedRepairReport, allowed []string) repairGateVerdict {
	classification := strings.TrimSpace(report.FinalClassification)
	allow := map[string]bool{}
	for _, item := range allowed {
		allow[strings.TrimSpace(item)] = true
	}
	if classification == repairClassificationValidRepair || allow[classification] {
		reason := "repair classified as valid"
		if classification != repairClassificationValidRepair {
			reason = "classification explicitly allowed as a warning state"
		}
		return repairGateVerdict{Classification: classification, Pass: true, Reason: reason}
	}
	switch classification {
	case repairClassificationStaleAuthority:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "authority is stale or non-authoritative; gate fails closed"}
	case repairClassificationMissingContract:
		if len(report.GuardedPaths) > 0 {
			return repairGateVerdict{Classification: classification, Pass: false, Reason: "guarded path has no governing contract; gate fails closed"}
		}
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "repair has no governing contract"}
	case repairClassificationForbiddenMoveDetected:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "a forbidden move was detected in the touched files"}
	case repairClassificationScopeDrift:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "the repair drifted outside the explicit allowed scope"}
	case repairClassificationInsufficientEvidence:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "required repair evidence is incomplete or failed"}
	case repairClassificationProjectPlaneUnavailable:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "the local project plane is unavailable"}
	case repairClassificationBackingStoreUnavailable:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "the backing store is unavailable"}
	default:
		return repairGateVerdict{Classification: classification, Pass: false, Reason: "repair classification is not permitted to pass"}
	}
}

func renderRepairReportText(report governedRepairReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repair report\n")
	fmt.Fprintf(&b, "Classification: %s\n", report.FinalClassification)
	if report.RepairTarget.TaskSummary != "" {
		fmt.Fprintf(&b, "Task: %s\n", report.RepairTarget.TaskSummary)
	}
	if len(report.TouchedFiles) > 0 {
		fmt.Fprintf(&b, "Touched files:\n")
		for _, file := range report.TouchedFiles {
			fmt.Fprintf(&b, "  - %s\n", file)
		}
	}
	if len(report.ExplicitScope) > 0 {
		fmt.Fprintf(&b, "Explicit scope:\n")
		for _, file := range report.ExplicitScope {
			fmt.Fprintf(&b, "  - %s\n", file)
		}
	}
	if len(report.GuardedPaths) > 0 {
		fmt.Fprintf(&b, "Guarded paths:\n")
		for _, file := range report.GuardedPaths {
			fmt.Fprintf(&b, "  - %s\n", file)
		}
	}
	fmt.Fprintf(&b, "Governing contract: %s\n", report.GoverningContract.Status)
	for _, item := range report.GoverningContract.Summary {
		fmt.Fprintf(&b, "  - %s\n", item)
	}
	fmt.Fprintf(&b, "Authority: %s", report.Authority.State)
	if report.Authority.GraphFreshnessState != "" {
		fmt.Fprintf(&b, " (%s)", report.Authority.GraphFreshnessState)
	}
	fmt.Fprintf(&b, "\n")
	if report.Authority.Detail != "" {
		fmt.Fprintf(&b, "  %s\n", report.Authority.Detail)
	}
	fmt.Fprintf(&b, "Evidence: %s\n", report.Evidence.Status)
	for _, item := range report.Evidence.RequiredTests {
		fmt.Fprintf(&b, "  required test: %s\n", item)
	}
	for _, item := range report.Evidence.MissingRequiredTests {
		fmt.Fprintf(&b, "  missing required test: %s\n", item)
	}
	for _, item := range report.Evidence.ChecksFailed {
		fmt.Fprintf(&b, "  failed check: %s\n", item)
	}
	if len(report.ForbiddenMoveFindings) > 0 {
		fmt.Fprintf(&b, "Forbidden move findings:\n")
		for _, finding := range report.ForbiddenMoveFindings {
			fmt.Fprintf(&b, "  - %s: %s\n", firstNonEmpty(finding.ID, finding.File), finding.Message)
		}
	}
	if len(report.ScopeDriftFindings) > 0 {
		fmt.Fprintf(&b, "Scope drift findings:\n")
		for _, finding := range report.ScopeDriftFindings {
			fmt.Fprintf(&b, "  - %s\n", finding.File)
		}
	}
	return b.String()
}

func writeRepairReportArtifact(path string, report governedRepairReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func isProjectPlaneUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == codes.Unavailable && strings.Contains(strings.ToLower(st.Message()), "connection")
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "dial") || strings.Contains(msg, "connection refused")
}

func isBackingStoreUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "store is unavailable") || strings.Contains(msg, "backend is unreachable")
}

func isStaleAuthorityError(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		if st.Code() == codes.FailedPrecondition && strings.Contains(strings.ToLower(st.Message()), "graph freshness stale") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "graph freshness stale")
}
