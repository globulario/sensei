// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=cmd.awg.runtime_repair_report
// @awareness file_role=runtime_repair_validation_phase4

// Runtime proof lane — Phase 4: before/action/after runtime repair validation.
//
// `awg runtime-repair-report` classifies a runtime repair claim. A repair is
// valid_runtime_repair ONLY when all of these hold (per the spec):
//  1. a governing contract is identified
//  2. required runtime evidence lanes are present (in the AFTER snapshot)
//  3. evidence is fresh enough (stale evidence cannot VALIDATE a repair)
//  4. evidence comes from the declared owner/authority
//  5. no forbidden runtime action was used (this OVERRIDES otherwise-good evidence)
//  6. the after-state proves the issue resolved (cluster_converged)
//
// Anything short of that is named honestly (forbidden_runtime_action,
// repair_claim_unproven, runtime_evidence_missing/stale, state_owner_mismatch,
// scope_drift, still_not_converged) — never a false "repaired". Pure
// (fixture-tested); reuses the Phase-3 diagnosis engine for after-state.
//
// Note: distinct from `awg repair-report` (the governed POST-EDIT artifact);
// this is the RUNTIME repair validator.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Runtime repair verdict vocabulary.
const (
	rrValid         = "valid_runtime_repair"
	rrForbidden     = "forbidden_runtime_action"
	rrUnproven      = "repair_claim_unproven"
	rrOwnerMismatch = "state_owner_mismatch"
	rrScopeDrift    = "scope_drift"
	rrStillNotConv  = "still_not_converged"
	// runtime_evidence_missing / runtime_evidence_stale reuse the Phase-3 constants.
)

// Safe-action model. An action is not safe just because it "worked once": only
// an allow-listed action with satisfied preconditions can yield a valid repair,
// and a forbidden action overrides otherwise-good evidence.
var forbiddenRuntimeActions = map[string]bool{
	"bypass_quorum_gate":                         true,
	"write_desired_state_directly_to_storage":    true,
	"mutate_identity_from_non_owner":             true,
	"force_applied_hash_without_real_install":    true,
	"patch_etcd_manually_without_owner_contract": true,
	"claim_convergence_from_stale_cache":         true,
	"ignore_runtime_identity_mismatch":           true,
}

var allowedRuntimeActions = map[string]bool{
	"refresh_runtime_snapshot":                                  true,
	"rerun_reconcile":                                           true,
	"restart_service_when_policy_allows":                        true,
	"restore_missing_quorum_member":                             true,
	"compact_etcd_when_nospace_and_snapshot_safe":               true,
	"rebuild_route_from_owner_state":                            true,
	"re_dispatch_available_release_to_ready_unconverged_joiner": true,
}

type runtimeRepairReport struct {
	Subject       string `json:"subject"`
	Contract      string `json:"contract"`
	Action        string `json:"action"`
	ActionClass   string `json:"action_class"` // allowed | forbidden | unknown
	BeforeVerdict string `json:"before_verdict"`
	AfterVerdict  string `json:"after_verdict"`
	Verdict       string `json:"verdict"`
	Reason        string `json:"reason"`
}

func classifyActionSafety(action string) string {
	switch {
	case forbiddenRuntimeActions[action]:
		return "forbidden"
	case allowedRuntimeActions[action]:
		return "allowed"
	default:
		return "unknown"
	}
}

func snapshotSubject(s runtimeEvidenceSnapshot) string {
	if s.Subject.ID == "" {
		return ""
	}
	subj := s.Subject.Type + ":" + s.Subject.ID
	if s.Subject.Node != "" {
		subj += "@" + s.Subject.Node
	}
	return subj
}

func afterRequiredOwnerMissing(s runtimeEvidenceSnapshot) (string, bool) {
	required := s.VerdictInputs.RequiredLanes
	if len(required) == 0 {
		required = inferRequiredLanes(s.Subject.Type)
	}
	for _, name := range required {
		if lane, ok := s.Lanes[name]; ok && strings.TrimSpace(lane.Owner) == "" {
			return name, true
		}
	}
	return "", false
}

// classifyRuntimeRepair is the pure before/action/after validator.
func classifyRuntimeRepair(before, after runtimeEvidenceSnapshot, action, contract string) runtimeRepairReport {
	beforeDiag := diagnoseRuntime(before)
	afterDiag := diagnoseRuntime(after)
	rep := runtimeRepairReport{
		Subject:       snapshotSubject(after),
		Contract:      contract,
		Action:        action,
		ActionClass:   classifyActionSafety(action),
		BeforeVerdict: beforeDiag.Verdict,
		AfterVerdict:  afterDiag.Verdict,
	}
	set := func(v, reason string) runtimeRepairReport { rep.Verdict, rep.Reason = v, reason; return rep }

	// scope: the after evidence must concern the SAME subject the repair claimed.
	bs, as := snapshotSubject(before), snapshotSubject(after)
	if bs != "" && as != "" && bs != as {
		return set(rrScopeDrift, fmt.Sprintf("before subject %q != after subject %q — repair evidence drifted to a different subject", bs, as))
	}

	// 5 (early): a forbidden action overrides otherwise-good evidence.
	if rep.ActionClass == "forbidden" {
		return set(rrForbidden, fmt.Sprintf("action %q is forbidden — it cannot produce a valid repair regardless of after-state", action))
	}
	// 1: a governing contract must be identified.
	if strings.TrimSpace(contract) == "" {
		return set(rrUnproven, "no governing contract identified — a repair without a respected contract is unproven")
	}
	// 5: the action must be a recognized safe action with preconditions.
	if rep.ActionClass != "allowed" {
		return set(rrUnproven, fmt.Sprintf("action %q is not in the safe-action allowlist — cannot certify it as a valid repair", action))
	}
	// 2 & 3: after evidence present + fresh (the Phase-3 engine already gates these).
	switch afterDiag.Verdict {
	case vEvidenceMissing:
		return set(vEvidenceMissing, "after-state evidence missing: "+afterDiag.Reason)
	case vEvidenceStale:
		return set(vEvidenceStale, "after-state evidence not fresh — stale evidence cannot validate a repair")
	}
	// 4: required after lanes must carry an owner authority anchor.
	if lane, missing := afterRequiredOwnerMissing(after); missing {
		return set(rrOwnerMismatch, fmt.Sprintf("after lane %q has no owner authority anchor — cannot validate the repair from non-owner evidence", lane))
	}
	// 6: after-state must prove resolution.
	if afterDiag.Verdict == vConverged {
		return set(rrValid, "contract identified, safe action, fresh owner evidence, after-state converged")
	}
	return set(rrStillNotConv, "after-state is not converged ("+afterDiag.Verdict+"): "+afterDiag.Reason)
}

func exitCodeForRepair(verdict string) int {
	if verdict == rrValid {
		return 0
	}
	return 1
}

func runRuntimeRepairReport(args []string) int {
	fs := flag.NewFlagSet("awg runtime-repair-report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	beforePath := fs.String("before", "", "runtime-evidence/v1 snapshot BEFORE the action (required)")
	afterPath := fs.String("after", "", "runtime-evidence/v1 snapshot AFTER the action (required)")
	action := fs.String("action", "", "the runtime action taken/proposed (required)")
	contract := fs.String("contract", "", "the governing contract id (required for a valid repair)")
	asJSON := fs.Bool("json", false, "machine-readable verdict")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg runtime-repair-report --before <b> --after <a> --action <name> [--contract <id>] [--json]

Validates a runtime repair claim (before/action/after). Exit 0 ONLY for
valid_runtime_repair; non-zero for every other verdict. Consumes normalized
snapshots only — never a platform RPC. (Distinct from "awg repair-report", the
governed post-edit artifact.)
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *beforePath == "" || *afterPath == "" || *action == "" {
		fmt.Fprintln(os.Stderr, "awg runtime-repair-report: --before, --after, and --action are required")
		return 2
	}
	before, err := loadSnapshot(*beforePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg runtime-repair-report: before: %v\n", err)
		return 1
	}
	after, err := loadSnapshot(*afterPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg runtime-repair-report: after: %v\n", err)
		return 1
	}
	rep := classifyRuntimeRepair(before, after, *action, *contract)
	if *asJSON {
		b, _ := json.Marshal(rep) // real JSON so `awg runtime-gate --report` can consume it
		fmt.Println(string(b))
	} else {
		fmt.Printf("runtime-repair-report: %s\n", rep.Subject)
		fmt.Printf("  contract:  %s\n", rep.Contract)
		fmt.Printf("  action:    %s (%s)\n", rep.Action, rep.ActionClass)
		fmt.Printf("  before:    %s\n", rep.BeforeVerdict)
		fmt.Printf("  after:     %s\n", rep.AfterVerdict)
		fmt.Printf("  VERDICT:   %s\n", rep.Verdict)
		fmt.Printf("  reason:    %s\n", rep.Reason)
	}
	return exitCodeForRepair(rep.Verdict)
}

// loadSnapshot reads + parses + structurally validates a snapshot file.
func loadSnapshot(path string) (runtimeEvidenceSnapshot, error) {
	var s runtimeEvidenceSnapshot
	raw, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("parse %s: %w", path, err)
	}
	if vf := validateRuntimeSnapshot(s); hasErrors(vf) {
		return s, fmt.Errorf("%s is not a valid runtime-evidence/v1 snapshot (run `awg runtime-snapshot validate`)", path)
	}
	return s, nil
}
