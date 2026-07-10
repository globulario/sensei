// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const defaultBenchmarkRetryBudget = 1

var (
	benchmarkRetryContaminatedModes = map[string]bool{
		"wrapper_stream_abort":                true,
		"wrapper_timeout":                     true,
		"wrapper_finalization_failure":        true,
		"model_tool_use_incomplete_terminal":  true,
		"model_tool_use_incomplete_resumable": true,
	}
	benchmarkRetryProtocolModes = map[string]bool{
		"protocol_violation": true,
	}
	benchmarkRetryNonRetryable = map[string]bool{
		"out_of_scope_edit":                       true,
		"contract_scope_too_broad":                true,
		"contract_scope_too_narrow_shared_helper": true,
		"verification_schema_brittleness":         true,
		"verification_impossible":                 true,
		"required_path_review_only":               true,
	}
)

type benchmarkRetryOutput struct {
	Mode                      string             `json:"mode"`
	RetryPlan                 benchmarkRetryPlan `json:"retry_plan"`
	RetryResultClassification string             `json:"retry_result_classification,omitempty"`
}

type benchmarkRetryPlan struct {
	RetryAllowed                   bool     `json:"retry_allowed"`
	RetryKind                      string   `json:"retry_kind"`
	RetryReason                    string   `json:"retry_reason"`
	MaxAttempts                    int      `json:"max_attempts"`
	AttemptIndex                   int      `json:"attempt_index"`
	OriginalPrimaryFailureMode     string   `json:"original_primary_failure_mode,omitempty"`
	RetryPromptAdjustment          string   `json:"retry_prompt_adjustment"`
	PreserveContract               bool     `json:"preserve_contract,omitempty"`
	PreserveAWGContext             bool     `json:"preserve_awg_context,omitempty"`
	RequireAuthoritativeRepairPlan bool     `json:"require_authoritative_repair_plan,omitempty"`
	RequireCleanRestart            bool     `json:"require_clean_restart"`
	RequiredPreflight              []string `json:"required_preflight,omitempty"`
	ForbiddenChanges               []string `json:"forbidden_changes,omitempty"`
	PromotionAllowedAfterRetry     bool     `json:"promotion_allowed_after_retry,omitempty"`
	LearningAllowedAfterRetry      bool     `json:"learning_allowed_after_retry,omitempty"`
	TerminalIfRepeated             bool     `json:"terminal_if_repeated,omitempty"`
	RecommendedNextStep            string   `json:"recommended_next_step"`
}

func runBenchmarkRetry(args []string) int {
	fs := flag.NewFlagSet("awg benchmark-retry", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mode := fs.String("mode", "", "retry controller mode: c | d")
	recordPath := fs.String("record", "", "benchmark run record (.yaml or .json)")
	eventPath := fs.String("event", "", "learning event (.yaml or .json), required for mode d")
	retryEventPath := fs.String("retry-event", "", "optional retry attempt event for result classification")
	retryBudget := fs.Int("retry-budget", defaultBenchmarkRetryBudget, "maximum retry attempts for this failure family")
	format := fs.String("format", "text", "output format: text | json")
	asJSON := fs.Bool("json", false, "output as JSON (deprecated: same as --format json)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg benchmark-retry [flags]

Build a reusable retry plan for benchmark runs from the recorded outcome and,
for contract-first flows, the emitted learning event.

This moves the retry-controller logic out of shell/Python harness code and into
a repository-native command other benchmark workflows can call directly.

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
	if strings.TrimSpace(*mode) == "" {
		fmt.Fprintln(os.Stderr, "awg benchmark-retry: --mode is required")
		return 2
	}
	if strings.TrimSpace(*recordPath) == "" {
		fmt.Fprintln(os.Stderr, "awg benchmark-retry: --record is required")
		return 2
	}
	modeVal := strings.ToLower(strings.TrimSpace(*mode))
	record, err := loadBenchmarkRetryDoc(*recordPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg benchmark-retry: load record: %v\n", err)
		return 1
	}

	out := benchmarkRetryOutput{Mode: modeVal}
	switch modeVal {
	case "c":
		out.RetryPlan = benchmarkModeCRetryPlan(record, *retryBudget)
	case "d":
		if strings.TrimSpace(*eventPath) == "" {
			fmt.Fprintln(os.Stderr, "awg benchmark-retry: --event is required for mode d")
			return 2
		}
		eventDoc, err := loadBenchmarkRetryDoc(*eventPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg benchmark-retry: load event: %v\n", err)
			return 1
		}
		event := benchmarkRetryUnwrapEvent(eventDoc)
		out.RetryPlan = benchmarkModeDRetryPlan(record, event, *retryBudget)
		if strings.TrimSpace(*retryEventPath) != "" {
			retryEventDoc, err := loadBenchmarkRetryDoc(*retryEventPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "awg benchmark-retry: load retry event: %v\n", err)
				return 1
			}
			out.RetryResultClassification = benchmarkClassifyRetryResult(event, benchmarkRetryUnwrapEvent(retryEventDoc), out.RetryPlan)
		}
	default:
		fmt.Fprintf(os.Stderr, "awg benchmark-retry: unsupported mode %q (want c or d)\n", *mode)
		return 2
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	default:
		fmt.Print(renderBenchmarkRetryText(out))
	}
	return 0
}

func loadBenchmarkRetryDoc(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &out); err != nil {
			return nil, err
		}
	default:
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func benchmarkRetryUnwrapEvent(doc map[string]any) map[string]any {
	if ev, ok := doc["learning_event"].(map[string]any); ok {
		return ev
	}
	return doc
}

func benchmarkModeCRetryPlan(record map[string]any, retryBudget int) benchmarkRetryPlan {
	attemptIndex := benchmarkInt(record["attempt_index"])
	nextAttemptIndex := attemptIndex + 1
	exhausted := nextAttemptIndex > retryBudget
	modeCStatus := benchmarkString(record["mode_c_status"])
	awgFocus := strings.TrimSpace(benchmarkString(record["awg_focus_summary"]))
	testResult := benchmarkString(record["test_result"])
	score := benchmarkInt(record["score"])
	testPass := benchmarkInt(record["score_test_pass"])

	result := benchmarkRetryPlan{
		RetryAllowed:                   false,
		RetryKind:                      "retry_not_allowed",
		RetryReason:                    "",
		MaxAttempts:                    retryBudget,
		AttemptIndex:                   nextAttemptIndex,
		RetryPromptAdjustment:          "none",
		PreserveAWGContext:             true,
		RequireAuthoritativeRepairPlan: false,
		RequireCleanRestart:            true,
		RecommendedNextStep:            "",
	}
	if exhausted {
		result.RetryKind = "retry_budget_exhausted"
		result.RetryReason = "Retry budget is exhausted for this Mode C run."
		result.RecommendedNextStep = "Stop retrying and report the prior AWG-guided failure."
		return result
	}
	if modeCStatus == "INVALID" {
		result.RetryReason = "Mode C structural bootstrap/validation failed; retrying without fixing the AWG context is unsafe."
		result.RecommendedNextStep = "Stop and repair the AWG structural context pipeline before rerunning Mode C."
		return result
	}
	if awgFocus == "" {
		result.RetryReason = "No actionable AWG focus was produced from the previous run."
		result.RecommendedNextStep = "Stop and inspect the prior run manually; there is no AWG direction to apply safely."
		return result
	}
	if testResult == "pass" && testPass >= 40 && score >= 70 {
		result.RetryReason = "The Mode C run already passed; do not auto-retry just to optimize score."
		result.RecommendedNextStep = "Keep the passing result and inspect AWG diagnostics manually if needed."
		return result
	}
	result.RetryAllowed = true
	result.RetryKind = "awg_focus_retry"
	result.RetryReason = "The run failed and AWG produced actionable focus that can guide a bounded retry."
	result.RetryPromptAdjustment = "awg_focus"
	result.RequireAuthoritativeRepairPlan = true
	result.RequiredPreflight = withAuthoritativeRepairPlanPreflight([]string{
		"read the previous AWG focus before patching",
		"use AWG context to narrow candidate files before broadening edits",
		"re-run the issue-driving tests after the focused patch",
	})
	result.ForbiddenChanges = []string{
		"do not ignore AWG focus silently",
		"do not broaden beyond issue-derived files without code evidence",
	}
	result.RecommendedNextStep = "Retry from a clean checkout and follow the previous AWG focus before exploring broader code paths."
	return result
}

func benchmarkModeDRetryPlan(record, event map[string]any, retryBudget int) benchmarkRetryPlan {
	diagnosis := benchmarkMap(event["diagnosis"])
	decision := benchmarkMap(event["decision"])
	primary := benchmarkString(diagnosis["primary_failure_mode"])
	attemptIndex := benchmarkInt(record["attempt_index"])
	nextAttemptIndex := attemptIndex + 1
	exhausted := nextAttemptIndex > retryBudget
	patchExists := benchmarkFileHasContent(benchmarkString(record["patch_file"]))
	contaminated := benchmarkTruthy(diagnosis["contaminated"])
	currentAction := benchmarkString(decision["action"])

	result := benchmarkRetryPlan{
		RetryAllowed:                   false,
		RetryKind:                      "retry_not_allowed",
		RetryReason:                    "",
		MaxAttempts:                    retryBudget,
		AttemptIndex:                   nextAttemptIndex,
		OriginalPrimaryFailureMode:     benchmarkCoalesceString(record["original_primary_failure_mode"], primary),
		RetryPromptAdjustment:          "none",
		PreserveContract:               true,
		RequireAuthoritativeRepairPlan: false,
		RequireCleanRestart:            true,
		PromotionAllowedAfterRetry:     false,
		LearningAllowedAfterRetry:      false,
		TerminalIfRepeated:             true,
		RecommendedNextStep:            "",
	}

	if exhausted && (currentAction == "auto_retry" || benchmarkTruthy(decision["retry_allowed"])) {
		result.RetryKind = "retry_budget_exhausted"
		result.RetryReason = "Retry budget is exhausted for this failure family."
		result.RetryPromptAdjustment = "report_only"
		result.RecommendedNextStep = "Stop retrying and report the repeated failure with the prior attempt lineage."
		return result
	}
	if benchmarkRetryContaminatedModes[primary] && currentAction == "auto_retry" {
		result.RetryAllowed = true
		result.RetryKind = "contaminated_execution_retry"
		result.RetryReason = "Execution contamination is retriable because no trustworthy project outcome was established."
		result.RetryPromptAdjustment = "minimal"
		result.RequireAuthoritativeRepairPlan = true
		result.RequiredPreflight = withAuthoritativeRepairPlanPreflight([]string{
			"reuse the same frozen contract and scope interpretation",
			"emit required Mode D artifacts completely before stopping",
		})
		result.ForbiddenChanges = []string{
			"do not change contract",
			"do not reinterpret scope",
			"do not promote learning from contaminated attempt",
		}
		result.RecommendedNextStep = "Rerun the same task from a clean checkout with the same contract and minimal prompt adjustment focused on completing artifact emission."
		return result
	}
	if (benchmarkRetryProtocolModes[primary] || benchmarkProtocolMissingOrInvalid(record)) && benchmarkTruthy(decision["retry_allowed"]) {
		result.RetryAllowed = true
		result.RetryKind = "protocol_preflight_retry"
		result.RetryReason = "Protocol failure can be retried only with hard preflight and, if needed, a clean restart."
		result.RetryPromptAdjustment = "hard_preflight"
		result.PreserveContract = !benchmarkProtocolMissingOrInvalid(record)
		result.RequireCleanRestart = patchExists
		result.RequireAuthoritativeRepairPlan = true
		result.RequiredPreflight = withAuthoritativeRepairPlanPreflight([]string{
			"produce contract_block.json before patch",
			"name governing contract",
			"name allowed scope",
			"name required verification paths",
			"declare confidence",
			"patch only after preflight passes",
		})
		result.ForbiddenChanges = []string{
			"do not promote learning from protocol-failed attempt",
			"do not accept a patch before valid contract_block.json exists",
		}
		if patchExists {
			result.RecommendedNextStep = "Restart from a clean checkout and enforce the hard contract preflight before any patching."
		} else {
			result.RecommendedNextStep = "Retry with a hard preflight that requires contract_block.json before any patching."
		}
		return result
	}
	if primary == "required_path_unavailable" {
		safeArtifactRerun := !contaminated && !patchExists
		result.RetryAllowed = safeArtifactRerun
		if safeArtifactRerun {
			result.RetryKind = "artifact_recollection_retry"
			result.RetryPromptAdjustment = "artifact_only"
			result.RequireAuthoritativeRepairPlan = true
			result.RequiredPreflight = withAuthoritativeRepairPlanPreflight([]string{"collect missing verification artifacts without changing the patch"})
			result.RecommendedNextStep = "Rerun safe artifact collection only."
		} else {
			result.RetryKind = "retry_not_allowed"
			result.RecommendedNextStep = "Stop and report that verification artifacts are unavailable."
		}
		result.RetryReason = "Required-path artifacts are unavailable."
		return result
	}
	if benchmarkRetryNonRetryable[primary] || !benchmarkTruthy(decision["retry_allowed"]) {
		result.RetryReason = "This failure class is not safe for automatic retry."
		result.RecommendedNextStep = benchmarkCoalesceString(decision["recommended_next_step"], "Do not retry automatically.")
		return result
	}
	result.RetryReason = "No safe retry plan is defined for this failure."
	result.RecommendedNextStep = benchmarkCoalesceString(decision["recommended_next_step"], "Stop and report.")
	return result
}

func benchmarkClassifyRetryResult(originalEvent, retryEvent map[string]any, retryPlan benchmarkRetryPlan) string {
	if !retryPlan.RetryAllowed && retryPlan.RetryKind == "retry_budget_exhausted" {
		return "retry_budget_exhausted"
	}
	if !retryPlan.RetryAllowed {
		return "retry_not_allowed"
	}
	origDiag := benchmarkMap(originalEvent["diagnosis"])
	retryDiag := benchmarkMap(retryEvent["diagnosis"])
	retryDecision := benchmarkMap(retryEvent["decision"])
	origPrimary := benchmarkString(origDiag["primary_failure_mode"])
	retryPrimary := benchmarkString(retryDiag["primary_failure_mode"])
	if benchmarkTruthy(retryDiag["contaminated"]) {
		return "retry_contaminated_again"
	}
	if retryPrimary == "clean_contract_repair" && benchmarkTruthy(retryDecision["promotion_allowed"]) {
		if benchmarkRetryProtocolModes[origPrimary] || retryPlan.RetryKind == "protocol_preflight_retry" {
			return "retry_protocol_fixed_and_clean"
		}
		return "retry_recovered"
	}
	if benchmarkRetryProtocolModes[origPrimary] && retryPrimary != origPrimary {
		return "retry_protocol_fixed_but_repair_failed"
	}
	if retryPrimary == origPrimary {
		return "retry_same_failure"
	}
	return "retry_worse"
}

func benchmarkProtocolMissingOrInvalid(record map[string]any) bool {
	status := benchmarkString(record["contract_block_status"])
	reasons := benchmarkStringSet(record["contract_clean_reasons"])
	return status == "missing" || status == "invalid" || reasons["contract_block_missing"] || reasons["contract_block_invalid"]
}

func benchmarkString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func benchmarkCoalesceString(v any, fallback string) string {
	s := benchmarkString(v)
	if s == "" || s == "<nil>" {
		return fallback
	}
	return s
}

func benchmarkTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	default:
		return false
	}
}

func benchmarkInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		var out int
		_, _ = fmt.Sscanf(strings.TrimSpace(t), "%d", &out)
		return out
	default:
		return 0
	}
}

func benchmarkMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func benchmarkStringSet(v any) map[string]bool {
	out := map[string]bool{}
	items, ok := v.([]any)
	if ok {
		for _, item := range items {
			s := benchmarkString(item)
			if s != "" && s != "<nil>" {
				out[s] = true
			}
		}
		return out
	}
	if items, ok := v.([]string); ok {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item != "" {
				out[item] = true
			}
		}
	}
	return out
}

func benchmarkFileHasContent(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func renderBenchmarkRetryText(out benchmarkRetryOutput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mode: %s\n", out.Mode)
	fmt.Fprintf(&b, "Retry allowed: %t\n", out.RetryPlan.RetryAllowed)
	fmt.Fprintf(&b, "Retry kind: %s\n", out.RetryPlan.RetryKind)
	fmt.Fprintf(&b, "Reason: %s\n", out.RetryPlan.RetryReason)
	fmt.Fprintf(&b, "Next attempt: %d/%d\n", out.RetryPlan.AttemptIndex, out.RetryPlan.MaxAttempts)
	if out.RetryPlan.RetryPromptAdjustment != "" {
		fmt.Fprintf(&b, "Prompt adjustment: %s\n", out.RetryPlan.RetryPromptAdjustment)
	}
	if out.RetryPlan.RecommendedNextStep != "" {
		fmt.Fprintf(&b, "Recommended next step: %s\n", out.RetryPlan.RecommendedNextStep)
	}
	if out.RetryPlan.RequireAuthoritativeRepairPlan {
		fmt.Fprintf(&b, "Authoritative repair plan: required\n")
	}
	if len(out.RetryPlan.RequiredPreflight) > 0 {
		fmt.Fprintf(&b, "\nRequired preflight:\n")
		for _, item := range out.RetryPlan.RequiredPreflight {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if len(out.RetryPlan.ForbiddenChanges) > 0 {
		fmt.Fprintf(&b, "\nForbidden changes:\n")
		for _, item := range out.RetryPlan.ForbiddenChanges {
			fmt.Fprintf(&b, "  - %s\n", item)
		}
	}
	if out.RetryResultClassification != "" {
		fmt.Fprintf(&b, "\nRetry result classification: %s\n", out.RetryResultClassification)
	}
	return b.String()
}

func withAuthoritativeRepairPlanPreflight(items []string) []string {
	return append([]string{
		"run awg benchmark-brief and require its authoritative repair plan output",
		"do not patch until awg repair-plan proves current graph authority for the scoped task/files",
	}, items...)
}
