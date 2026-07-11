// SPDX-License-Identifier: Apache-2.0

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
	"time"
)

type benchmarkScoreResult struct {
	RepoRoot               string                          `json:"repo_root"`
	Sequence               []string                        `json:"sequence"`
	RepairSuccess          string                          `json:"repair_success"`
	ContractCertification  string                          `json:"contract_certification"`
	ContractConfidence     string                          `json:"contract_confidence,omitempty"`
	ContractClean          *bool                           `json:"contract_clean,omitempty"`
	ContractCleanReasons   []string                        `json:"contract_clean_reasons,omitempty"`
	ContractCleanWarnings  []string                        `json:"contract_clean_warnings,omitempty"`
	ContractFailureReason  string                          `json:"contract_failure_reason,omitempty"`
	ContractScopeStatus    string                          `json:"contract_scope_status,omitempty"`
	ProofRequired          bool                            `json:"proof_required,omitempty"`
	ProofStatus            string                          `json:"proof_status,omitempty"`
	CertificationBreakdown benchmarkCertificationBreakdown `json:"certification_breakdown"`
	RecommendedFocus       []benchmarkFocusRecommendation  `json:"recommended_focus,omitempty"`
	ScoreCapReason         string                          `json:"score_cap_reason,omitempty"`
	OverallScore           int                             `json:"overall_score"`
	RepairPlanIDs          []string                        `json:"repair_plan_ids,omitempty"`
	RepairProofIDs         []string                        `json:"repair_proof_ids,omitempty"`
	Brief                  benchmarkBriefResult            `json:"brief"`
	Judge                  benchmarkJudgeResult            `json:"judge"`
}

type benchmarkCertificationBreakdown struct {
	Scope     benchmarkCertificationLane `json:"scope"`
	Proof     benchmarkCertificationLane `json:"proof"`
	Authority benchmarkCertificationLane `json:"authority"`
	Evidence  benchmarkCertificationLane `json:"evidence"`
}

type benchmarkCertificationLane struct {
	Status  string   `json:"status"`
	Score   int      `json:"score"`
	Reasons []string `json:"reasons,omitempty"`
}

type benchmarkFocusRecommendation struct {
	Lane     string   `json:"lane"`
	Priority int      `json:"priority"`
	Action   string   `json:"action"`
	Reasons  []string `json:"reasons,omitempty"`
}

type benchmarkScoreTask struct {
	InstanceID                        string   `json:"instance_id"`
	Issue                             string   `json:"issue"`
	F2PTests                          []string `json:"f2p_tests"`
	Files                             []string `json:"files"`
	TestsRun                          []string `json:"tests_run"`
	RepairSuccess                     bool     `json:"repair_success"`
	ContractClean                     *bool    `json:"contract_clean,omitempty"`
	ContractConfidence                string   `json:"contract_confidence,omitempty"`
	ContractCleanReasons              []string `json:"contract_clean_reasons,omitempty"`
	ContractCleanWarnings             []string `json:"contract_clean_warnings,omitempty"`
	ContractFailureReason             string   `json:"contract_failure_reason,omitempty"`
	ContractScopeStatus               string   `json:"contract_scope_status,omitempty"`
	AllowedRelatedScopeCandidateFiles []string `json:"allowed_related_scope_candidate_files,omitempty"`
	MissingRequiredScopeFiles         []string `json:"missing_required_scope_files,omitempty"`
	MissingVerifiedPaths              []string `json:"missing_verified_paths,omitempty"`
	RequiredTestPaths                 []string `json:"required_test_paths,omitempty"`
	MissingRequiredTestPaths          []string `json:"missing_required_test_paths,omitempty"`
	ProofRequired                     bool     `json:"proof_required,omitempty"`
	ProofStatus                       string   `json:"proof_status,omitempty"`
}

func runBenchmarkScore(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-score", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoRoot := fs.String("repo-root", ".", "repository root to analyze")
	addr := fs.String("addr", defaultServiceAddr(), "AWG gRPC server address for authoritative repair-plan resolution")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo for cross-repo atomicity (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo for cross-repo atomicity (auto-detect)")
	taskFile := fs.String("task-file", "", "task JSON containing issue/f2p_tests/files")
	issue := fs.String("issue", "", "issue text when not using --task-file")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	repairSuccess := fs.Bool("repair-success", false, "mark the benchmark repair itself as successful")
	var tests stringSlice
	var files stringSlice
	var testsRun stringSlice
	fs.Var(&tests, "f2p-test", "fail-to-pass test name (repeatable)")
	fs.Var(&files, "file", "changed or judged file path (repeatable)")
	fs.Var(&testsRun, "test-run", "executed test id, function, or file path (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-score [flags]

Run the standard local benchmark sequence:
  1. build a repair brief
  2. judge the patch envelope
  3. emit one combined score/result

This is the workflow wrapper that benchmark agents should call by default.

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

	task, source, err := loadBenchmarkScoreTask(*taskFile, *issue, tests, files, testsRun, *repairSuccess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-score: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(*repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-score: resolve repo root: %v\n", err)
		return 1
	}
	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	guardCtx, guardCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer guardCancel()
	if err := benchmarkAuthorityGuard(guardCtx, *addr, agRepo, svcRepo); err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-score: %v\n", err)
		return 1
	}
	briefTask := benchmarkBriefTask{
		InstanceID: task.InstanceID,
		Issue:      task.Issue,
		F2PTests:   task.F2PTests,
		Files:      task.Files,
	}
	brief, err := buildBenchmarkBrief(root, briefTask, source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-score: %v\n", err)
		return 1
	}
	repairPlan, err := buildAuthoritativeRepairPlan(root, *addr, strings.TrimSpace(task.Issue), brief.LikelyImplementationFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-score: %v\n", err)
		return 1
	}
	brief.RepairPlan = &repairPlan
	judge, err := buildBenchmarkJudge(root, briefTask, task.TestsRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-score: %v\n", err)
		return 1
	}
	breakdown := benchmarkCertificationLanes(task, judge)
	result := benchmarkScoreResult{
		RepoRoot:               root,
		Sequence:               []string{"benchmark-brief", "benchmark-judge"},
		RepairSuccess:          benchmarkRepairSuccess(task.RepairSuccess),
		ContractCertification:  benchmarkContractCertification(task, breakdown),
		ContractConfidence:     strings.TrimSpace(task.ContractConfidence),
		ContractClean:          task.ContractClean,
		ContractCleanReasons:   dedupeStrings(task.ContractCleanReasons),
		ContractCleanWarnings:  dedupeStrings(task.ContractCleanWarnings),
		ContractFailureReason:  strings.TrimSpace(task.ContractFailureReason),
		ContractScopeStatus:    strings.TrimSpace(task.ContractScopeStatus),
		ProofRequired:          task.ProofRequired,
		ProofStatus:            strings.TrimSpace(task.ProofStatus),
		CertificationBreakdown: breakdown,
		RecommendedFocus:       benchmarkRecommendedFocus(task, judge, breakdown),
		RepairPlanIDs:          benchmarkRepairPlanIDs(brief.RepairPlan),
		RepairProofIDs:         benchmarkRepairProofIDs(brief.RepairPlan),
		Brief:                  brief,
		Judge:                  judge,
	}
	result.OverallScore, result.ScoreCapReason = benchmarkOverallScore(task, judge, breakdown)

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(result)
	default:
		fmt.Print(renderBenchmarkScoreText(result))
	}
	return 0
}

func benchmarkRepairPlanIDs(plan *repairPlanResult) []string {
	if plan == nil {
		return nil
	}
	return dedupeStrings(filterStrings(plan.RequiredActions, func(item string) bool {
		return strings.HasPrefix(strings.TrimSpace(item), "repair_plan:")
	}))
}

func benchmarkRepairProofIDs(plan *repairPlanResult) []string {
	if plan == nil {
		return nil
	}
	var ids []string
	for _, obligation := range plan.Proof.Obligations {
		id := strings.TrimSpace(obligation.ID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return dedupeStrings(ids)
}

func benchmarkRepairSuccess(ok bool) string {
	if ok {
		return "pass"
	}
	return "unknown"
}

func loadBenchmarkScoreTask(taskFile, issue string, tests, files, testsRun []string, repairSuccess bool) (benchmarkScoreTask, string, error) {
	if strings.TrimSpace(taskFile) != "" {
		data, err := os.ReadFile(taskFile)
		if err != nil {
			return benchmarkScoreTask{}, "", err
		}
		var task benchmarkScoreTask
		if err := json.Unmarshal(data, &task); err != nil {
			return benchmarkScoreTask{}, "", fmt.Errorf("parse task file: %w", err)
		}
		return task, taskFile, nil
	}
	if strings.TrimSpace(issue) == "" && len(tests) == 0 && len(files) == 0 {
		return benchmarkScoreTask{}, "", fmt.Errorf("provide --task-file or at least one of --issue, --f2p-test, --file")
	}
	return benchmarkScoreTask{
		Issue:         issue,
		F2PTests:      tests,
		Files:         files,
		TestsRun:      testsRun,
		RepairSuccess: repairSuccess,
	}, "flags", nil
}

func benchmarkOverallScore(task benchmarkScoreTask, judge benchmarkJudgeResult, breakdown benchmarkCertificationBreakdown) (int, string) {
	score := 0
	if task.RepairSuccess {
		score += 20
	}
	if judge.ContractPreservation == "ok" {
		score += 20
	}
	if judge.TestDiscipline == "ok" {
		score += 20
	}
	if judge.AuthorityDiscipline == "ok" {
		score += 20
	}
	score += breakdown.Scope.Score
	score += breakdown.Proof.Score
	score += breakdown.Authority.Score
	score += breakdown.Evidence.Score
	cap, reason := benchmarkScoreCap(task, breakdown)
	if cap > 0 && score > cap {
		return cap, reason
	}
	return score, ""
}

func benchmarkScoreCap(task benchmarkScoreTask, breakdown benchmarkCertificationBreakdown) (int, string) {
	if hasCertificationStatus(breakdown, "insufficient") {
		return 79, "insufficient certification lanes cap overall score below high-confidence results"
	}
	if hasCertificationStatus(breakdown, "review_required") {
		return 89, "review-required certification lanes cap overall score below high-confidence results"
	}
	switch strings.TrimSpace(strings.ToLower(task.ContractConfidence)) {
	case "", "unknown", "low", "candidate":
		return 89, "weak contract confidence caps overall score below high-confidence results"
	case "medium":
		return 94, "medium contract confidence caps overall score below top-tier results"
	default:
		return 0, ""
	}
}

func benchmarkContractCertification(task benchmarkScoreTask, breakdown benchmarkCertificationBreakdown) string {
	if task.ContractClean == nil && allCertificationUnknown(breakdown) {
		return "unknown"
	}
	if hasCertificationStatus(breakdown, "insufficient") {
		return "insufficient"
	}
	if hasCertificationStatus(breakdown, "review_required") {
		return "review_required"
	}
	if task.ContractClean != nil && !*task.ContractClean {
		return "insufficient"
	}
	return "ok"
}

func benchmarkCertificationLanes(task benchmarkScoreTask, judge benchmarkJudgeResult) benchmarkCertificationBreakdown {
	reasons := dedupeStrings(task.ContractCleanReasons)
	warnings := dedupeStrings(task.ContractCleanWarnings)
	scopeReasons := filterStrings(reasons, func(item string) bool {
		return stringInSet(item, map[string]bool{
			"scope_underconstrained":                      true,
			"required_scope_missing":                      true,
			"allowed_related_scope_missing":               true,
			"out_of_scope_missing":                        true,
			"edited_file_out_of_scope":                    true,
			"out_of_scope_edit":                           true,
			"allowed_related_scope_candidate_unconfirmed": true,
			"repo_artifact_leak":                          true,
		})
	})
	proofReasons := filterStrings(reasons, func(item string) bool {
		return stringInSet(item, map[string]bool{
			"test_proof_incomplete":        true,
			"missing_required_test_path":   true,
			"missing_required_test_symbol": true,
		})
	})
	authorityReasons := filterStrings(reasons, func(item string) bool {
		return stringInSet(item, map[string]bool{
			"contract_not_found":       true,
			"contract_block_missing":   true,
			"contract_block_invalid":   true,
			"revision_request_missing": true,
		})
	})
	evidenceReasons := filterStrings(reasons, func(item string) bool {
		return stringInSet(item, map[string]bool{
			"verification_missing_required_path":         true,
			"verification_schema_unstable_required_path": true,
			"verification_impossible_required_path":      true,
			"partial_coverage_non_compliant":             true,
			"required_paths_missing":                     true,
		})
	})
	evidenceReview := append([]string{}, warnings...)
	evidenceReview = append(evidenceReview, dedupeStrings(append(task.JudgeLikeEvidenceGaps(judge), task.BriefLikeEvidenceGaps()...))...)
	scopeReview := append([]string{}, benchmarkScopeReviewReasons(task)...)
	proofReasons = append(proofReasons, benchmarkProofReasonsFromTask(task)...)
	evidenceReasons = append(evidenceReasons, benchmarkEvidenceReasonsFromTask(task)...)
	return benchmarkCertificationBreakdown{
		Scope:     benchmarkLaneFrom(task.ContractScopeStatus, dedupeStrings(scopeReasons), dedupeStrings(scopeReview), task.ContractClean),
		Proof:     benchmarkProofLane(task, judge, proofReasons),
		Authority: benchmarkAuthorityLane(judge, authorityReasons, task.ContractClean),
		Evidence:  benchmarkEvidenceLane(judge, evidenceReasons, evidenceReview, task.ContractClean),
	}
}

func benchmarkLaneFrom(scopeStatus string, insufficient, review []string, clean *bool) benchmarkCertificationLane {
	switch {
	case len(insufficient) > 0:
		return benchmarkCertificationLane{Status: "insufficient", Score: 0, Reasons: insufficient}
	case len(review) > 0:
		return benchmarkCertificationLane{Status: "review_required", Score: 2, Reasons: review}
	}
	switch strings.TrimSpace(scopeStatus) {
	case "underconstrained":
		return benchmarkCertificationLane{Status: "insufficient", Score: 0}
	case "contract_incomplete":
		return benchmarkCertificationLane{Status: "review_required", Score: 2}
	}
	if clean == nil {
		return benchmarkCertificationLane{Status: "unknown", Score: 0}
	}
	return benchmarkCertificationLane{Status: "ok", Score: 5}
}

func benchmarkProofLane(task benchmarkScoreTask, judge benchmarkJudgeResult, reasons []string) benchmarkCertificationLane {
	if len(reasons) > 0 || strings.TrimSpace(task.ContractFailureReason) == "test_proof_incomplete" || len(judge.MissingRequiredTests) > 0 {
		out := append([]string{}, reasons...)
		for _, item := range judge.MissingRequiredTests {
			out = append(out, "missing_required_test:"+item)
		}
		return benchmarkCertificationLane{Status: "insufficient", Score: 0, Reasons: dedupeStrings(out)}
	}
	switch strings.TrimSpace(task.ProofStatus) {
	case "incomplete":
		return benchmarkCertificationLane{Status: "insufficient", Score: 0}
	case "not_required", "":
		if task.ProofRequired || len(task.RequiredTestPaths) > 0 {
			return benchmarkCertificationLane{Status: "unknown", Score: 0}
		}
	default:
		return benchmarkCertificationLane{Status: "ok", Score: 5}
	}
	if task.ProofRequired && task.ContractClean == nil {
		return benchmarkCertificationLane{Status: "unknown", Score: 0}
	}
	return benchmarkCertificationLane{Status: "ok", Score: 5}
}

func benchmarkAuthorityLane(judge benchmarkJudgeResult, reasons []string, clean *bool) benchmarkCertificationLane {
	if len(reasons) > 0 {
		return benchmarkCertificationLane{Status: "insufficient", Score: 0, Reasons: reasons}
	}
	if judge.AuthorityDiscipline == "review_required" {
		return benchmarkCertificationLane{Status: "review_required", Score: 2, Reasons: dedupeStrings(judge.AuthorityGaps)}
	}
	if clean == nil && judge.AuthorityDiscipline != "ok" {
		return benchmarkCertificationLane{Status: "unknown", Score: 0}
	}
	return benchmarkCertificationLane{Status: "ok", Score: 5}
}

func benchmarkEvidenceLane(judge benchmarkJudgeResult, reasons, review []string, clean *bool) benchmarkCertificationLane {
	if len(reasons) > 0 {
		return benchmarkCertificationLane{Status: "insufficient", Score: 0, Reasons: reasons}
	}
	if len(review) > 0 || len(judge.EvidenceGaps) > 0 {
		out := append([]string{}, review...)
		out = append(out, judge.EvidenceGaps...)
		return benchmarkCertificationLane{Status: "review_required", Score: 2, Reasons: dedupeStrings(out)}
	}
	if clean == nil && len(judge.EvidenceGaps) == 0 {
		return benchmarkCertificationLane{Status: "unknown", Score: 0}
	}
	return benchmarkCertificationLane{Status: "ok", Score: 5}
}

func hasCertificationStatus(b benchmarkCertificationBreakdown, want string) bool {
	return b.Scope.Status == want || b.Proof.Status == want || b.Authority.Status == want || b.Evidence.Status == want
}

func allCertificationUnknown(b benchmarkCertificationBreakdown) bool {
	return b.Scope.Status == "unknown" && b.Proof.Status == "unknown" && b.Authority.Status == "unknown" && b.Evidence.Status == "unknown"
}

func benchmarkRecommendedFocus(task benchmarkScoreTask, judge benchmarkJudgeResult, breakdown benchmarkCertificationBreakdown) []benchmarkFocusRecommendation {
	var out []benchmarkFocusRecommendation
	appendLane := func(lane string, rec benchmarkCertificationLane, action string, extraReasons []string) {
		if rec.Status == "ok" || rec.Status == "unknown" {
			return
		}
		priority := 2
		if rec.Status == "insufficient" {
			priority = 1
		}
		reasons := append([]string{}, rec.Reasons...)
		reasons = append(reasons, extraReasons...)
		out = append(out, benchmarkFocusRecommendation{
			Lane:     lane,
			Priority: priority,
			Action:   action,
			Reasons:  dedupeStrings(reasons),
		})
	}
	appendLane("scope", breakdown.Scope, benchmarkScopeAction(task, breakdown.Scope), nil)
	appendLane("proof", breakdown.Proof, benchmarkProofAction(task, judge, breakdown.Proof), nil)
	appendLane("authority", breakdown.Authority, benchmarkAuthorityAction(judge, breakdown.Authority), judge.AuthorityGaps)
	appendLane("evidence", breakdown.Evidence, benchmarkEvidenceAction(judge, breakdown.Evidence), judge.EvidenceGaps)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Lane < out[j].Lane
	})
	return out
}

func benchmarkScopeAction(task benchmarkScoreTask, lane benchmarkCertificationLane) string {
	if strings.TrimSpace(task.ContractScopeStatus) == "underconstrained" {
		return "Tighten required and allowed-related scope before broadening any edits."
	}
	if hasReason(lane.Reasons, "edited_file_out_of_scope") || hasReason(lane.Reasons, "out_of_scope_edit") {
		return "Pull edits back into governed files or explicitly authorize the helper path."
	}
	return "Clarify the governed file boundary before changing more code."
}

func benchmarkProofAction(task benchmarkScoreTask, judge benchmarkJudgeResult, lane benchmarkCertificationLane) string {
	if len(judge.MissingRequiredTests) > 0 {
		return "Run or add the required proving tests before treating the repair as certified."
	}
	if strings.TrimSpace(task.ProofStatus) == "incomplete" || hasReason(lane.Reasons, "test_proof_incomplete") {
		return "Complete the proof path with the contract-required test evidence."
	}
	return "Strengthen proof coverage for the claimed behavior change."
}

func benchmarkAuthorityAction(judge benchmarkJudgeResult, lane benchmarkCertificationLane) string {
	if len(judge.AuthorityGaps) > 0 {
		return "Repair missing authority anchors before promoting or trusting the change."
	}
	if hasReason(lane.Reasons, "contract_block_missing") || hasReason(lane.Reasons, "contract_block_invalid") {
		return "Rebuild the contract mapping so the repair is tied to an authoritative frozen contract."
	}
	return "Reconcile the repair against the authoritative contract block and intent sources."
}

func benchmarkEvidenceAction(judge benchmarkJudgeResult, lane benchmarkCertificationLane) string {
	if len(judge.EvidenceGaps) > 0 {
		return "Repair stale or missing evidence links so tests and annotations point to real proof."
	}
	if hasReason(lane.Reasons, "verification_missing_required_path") {
		return "Extend verification to cover the required path evidence the contract demands."
	}
	return "Close the remaining verification gaps before trusting the repair result."
}

func hasReason(reasons []string, want string) bool {
	for _, item := range reasons {
		if item == want {
			return true
		}
	}
	return false
}

func filterStrings(items []string, keep func(string) bool) []string {
	var out []string
	for _, item := range items {
		if keep(item) {
			out = append(out, item)
		}
	}
	return dedupeStrings(out)
}

func stringInSet(item string, set map[string]bool) bool {
	return set[item]
}

func benchmarkScopeReviewReasons(task benchmarkScoreTask) []string {
	var out []string
	for _, path := range dedupeStrings(task.AllowedRelatedScopeCandidateFiles) {
		if !benchmarkCandidateTouched(task.Files, path) {
			continue
		}
		out = append(out, "allowed_related_scope_candidate_unconfirmed:"+path)
	}
	return out
}

func benchmarkCandidateTouched(changedFiles []string, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	for _, changed := range dedupeStrings(changedFiles) {
		changed = strings.TrimSpace(changed)
		if changed == "" {
			continue
		}
		if changed == candidate {
			return true
		}
		if strings.Contains(candidate, "*") || strings.Contains(candidate, "?") {
			if matched, _ := filepath.Match(candidate, changed); matched {
				return true
			}
		}
	}
	return false
}

func benchmarkProofReasonsFromTask(task benchmarkScoreTask) []string {
	var out []string
	for _, path := range dedupeStrings(task.MissingRequiredTestPaths) {
		out = append(out, "missing_required_test_path:"+path)
	}
	return out
}

func benchmarkEvidenceReasonsFromTask(task benchmarkScoreTask) []string {
	var out []string
	for _, path := range dedupeStrings(task.MissingVerifiedPaths) {
		out = append(out, "verification_missing_required_path:"+path)
	}
	for _, path := range dedupeStrings(task.MissingRequiredScopeFiles) {
		out = append(out, "required_scope_missing:"+path)
	}
	return out
}

func (task benchmarkScoreTask) BriefLikeEvidenceGaps() []string {
	return nil
}

func (task benchmarkScoreTask) JudgeLikeEvidenceGaps(judge benchmarkJudgeResult) []string {
	return judge.EvidenceGaps
}

func renderBenchmarkScoreText(res benchmarkScoreResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Repository: %s\n", res.RepoRoot)
	fmt.Fprintf(&b, "Sequence: %s\n", strings.Join(res.Sequence, " -> "))
	fmt.Fprintf(&b, "Repair success: %s\n", res.RepairSuccess)
	fmt.Fprintf(&b, "Contract certification: %s\n", res.ContractCertification)
	if res.ContractConfidence != "" {
		fmt.Fprintf(&b, "Contract confidence: %s\n", res.ContractConfidence)
	}
	fmt.Fprintf(&b, "Overall score: %d/100\n", res.OverallScore)
	if res.ScoreCapReason != "" {
		fmt.Fprintf(&b, "Score cap: %s\n", res.ScoreCapReason)
	}
	fmt.Fprintf(&b, "Certification lanes:\n")
	fmt.Fprintf(&b, "  scope: %s (%d/5)\n", res.CertificationBreakdown.Scope.Status, res.CertificationBreakdown.Scope.Score)
	fmt.Fprintf(&b, "  proof: %s (%d/5)\n", res.CertificationBreakdown.Proof.Status, res.CertificationBreakdown.Proof.Score)
	fmt.Fprintf(&b, "  authority: %s (%d/5)\n", res.CertificationBreakdown.Authority.Status, res.CertificationBreakdown.Authority.Score)
	fmt.Fprintf(&b, "  evidence: %s (%d/5)\n", res.CertificationBreakdown.Evidence.Status, res.CertificationBreakdown.Evidence.Score)
	if len(res.RecommendedFocus) > 0 {
		fmt.Fprintf(&b, "Recommended focus:\n")
		for _, item := range res.RecommendedFocus {
			fmt.Fprintf(&b, "  %d. %s: %s\n", item.Priority, item.Lane, item.Action)
		}
	}
	fmt.Fprintf(&b, "\nBrief summary:\n")
	fmt.Fprintf(&b, "  likely implementation files: %d\n", len(res.Brief.LikelyImplementationFiles))
	fmt.Fprintf(&b, "  tests to run: %d\n", len(res.Brief.TestsToRun))
	fmt.Fprintf(&b, "  authority gaps: %d\n", len(res.Brief.AuthorityGaps))
	fmt.Fprintf(&b, "  evidence gaps: %d\n", len(res.Brief.EvidenceGaps))
	if len(res.RepairPlanIDs) > 0 {
		fmt.Fprintf(&b, "  repair plans: %s\n", strings.Join(res.RepairPlanIDs, ", "))
	}
	if len(res.RepairProofIDs) > 0 {
		fmt.Fprintf(&b, "  repair proof: %s\n", strings.Join(res.RepairProofIDs, ", "))
	}
	fmt.Fprintf(&b, "\nJudge summary:\n")
	fmt.Fprintf(&b, "  contract preservation: %s\n", res.Judge.ContractPreservation)
	fmt.Fprintf(&b, "  test discipline: %s\n", res.Judge.TestDiscipline)
	fmt.Fprintf(&b, "  authority discipline: %s\n", res.Judge.AuthorityDiscipline)
	if len(res.ContractCleanReasons) > 0 {
		fmt.Fprintf(&b, "  contract clean reasons:\n")
		for _, item := range res.ContractCleanReasons {
			fmt.Fprintf(&b, "    - %s\n", item)
		}
	}
	if len(res.Judge.MissingRequiredTests) > 0 {
		fmt.Fprintf(&b, "  missing required tests:\n")
		for _, item := range res.Judge.MissingRequiredTests {
			fmt.Fprintf(&b, "    - %s\n", item)
		}
	}
	return b.String()
}
