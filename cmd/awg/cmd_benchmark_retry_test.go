// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestBenchmarkModeCRetryPlan_AllowsFocusedRetry(t *testing.T) {
	plan := benchmarkModeCRetryPlan(map[string]any{
		"attempt_index":     0,
		"mode_c_status":     "OK",
		"awg_focus_summary": "narrow to parser path first",
		"test_result":       "fail",
		"score":             55,
		"score_test_pass":   20,
	}, 1)
	if !plan.RetryAllowed {
		t.Fatalf("retry should be allowed: %+v", plan)
	}
	if plan.RetryKind != "awg_focus_retry" {
		t.Fatalf("retry kind=%q", plan.RetryKind)
	}
	if plan.RetryPromptAdjustment != "awg_focus" {
		t.Fatalf("prompt adjustment=%q", plan.RetryPromptAdjustment)
	}
	if !plan.RequireAuthoritativeRepairPlan {
		t.Fatalf("authoritative repair plan must be required: %+v", plan)
	}
	if len(plan.RequiredPreflight) == 0 || plan.RequiredPreflight[0] != "run sensei benchmark-brief and require its authoritative repair plan output" {
		t.Fatalf("required preflight missing governed repair spine: %+v", plan.RequiredPreflight)
	}
}

func TestBenchmarkModeCRetryPlan_DoesNotRetryPassingRun(t *testing.T) {
	plan := benchmarkModeCRetryPlan(map[string]any{
		"attempt_index":     0,
		"mode_c_status":     "OK",
		"awg_focus_summary": "could optimize further",
		"test_result":       "pass",
		"score":             80,
		"score_test_pass":   40,
	}, 1)
	if plan.RetryAllowed {
		t.Fatalf("passing run must not auto-retry: %+v", plan)
	}
	if plan.RetryKind != "retry_not_allowed" {
		t.Fatalf("retry kind=%q", plan.RetryKind)
	}
}

func TestBenchmarkModeDRetryPlan_ContaminatedExecutionRetry(t *testing.T) {
	plan := benchmarkModeDRetryPlan(
		map[string]any{"attempt_index": 0},
		map[string]any{
			"diagnosis": map[string]any{
				"primary_failure_mode": "wrapper_stream_abort",
				"contaminated":         true,
			},
			"decision": map[string]any{
				"action":        "auto_retry",
				"retry_allowed": true,
			},
		},
		1,
	)
	if !plan.RetryAllowed {
		t.Fatalf("contaminated execution should retry: %+v", plan)
	}
	if plan.RetryKind != "contaminated_execution_retry" {
		t.Fatalf("retry kind=%q", plan.RetryKind)
	}
	if !plan.PreserveContract {
		t.Fatalf("contaminated retry must preserve contract: %+v", plan)
	}
	if !plan.RequireAuthoritativeRepairPlan {
		t.Fatalf("authoritative repair plan must be required: %+v", plan)
	}
}

func TestBenchmarkModeDRetryPlan_ProtocolRetryRequiresHardPreflight(t *testing.T) {
	plan := benchmarkModeDRetryPlan(
		map[string]any{
			"attempt_index":          0,
			"contract_block_status":  "missing",
			"patch_file":             "",
			"contract_clean_reasons": []any{"contract_block_missing"},
		},
		map[string]any{
			"diagnosis": map[string]any{
				"primary_failure_mode": "protocol_violation",
			},
			"decision": map[string]any{
				"retry_allowed": true,
			},
		},
		1,
	)
	if !plan.RetryAllowed {
		t.Fatalf("protocol retry should be allowed: %+v", plan)
	}
	if plan.RetryKind != "protocol_preflight_retry" {
		t.Fatalf("retry kind=%q", plan.RetryKind)
	}
	if plan.RetryPromptAdjustment != "hard_preflight" {
		t.Fatalf("prompt adjustment=%q", plan.RetryPromptAdjustment)
	}
	if plan.PreserveContract {
		t.Fatalf("missing/invalid contract block should clear preserve_contract")
	}
	if !plan.RequireAuthoritativeRepairPlan {
		t.Fatalf("authoritative repair plan must be required: %+v", plan)
	}
}

func TestBenchmarkClassifyRetryResult_ProtocolFixedAndClean(t *testing.T) {
	classification := benchmarkClassifyRetryResult(
		map[string]any{
			"diagnosis": map[string]any{
				"primary_failure_mode": "protocol_violation",
			},
		},
		map[string]any{
			"diagnosis": map[string]any{
				"primary_failure_mode": "clean_contract_repair",
			},
			"decision": map[string]any{
				"promotion_allowed": true,
			},
		},
		benchmarkRetryPlan{RetryAllowed: true, RetryKind: "protocol_preflight_retry"},
	)
	if classification != "retry_protocol_fixed_and_clean" {
		t.Fatalf("classification=%q", classification)
	}
}
