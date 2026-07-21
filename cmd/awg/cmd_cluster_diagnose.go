// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=cmd.awg.cluster_diagnose
// @awareness file_role=runtime_diagnosis_verdicts_phase3

// Runtime proof lane — Phase 3: typed cluster diagnosis from a normalized
// runtime-evidence/v1 snapshot.
//
// This is the first lane phase that produces a VERDICT, so it is where the
// freshness + authority rules bite (the schema phases only checked shape):
//   - missing required evidence => runtime_evidence_missing (cannot diagnose).
//   - a "green" verdict (cluster_converged) is produced ONLY from fresh evidence;
//     a would-be-converged subject on non-fresh evidence yields
//     runtime_evidence_stale, never a false green ("unknown must not produce green").
//   - a BLOCKED verdict (blocked_by_quorum, ...) is the diagnosis succeeding — it
//     may be drawn from stale evidence, but the confidence is labelled.
//
// diagnoseRuntime is pure (fixture-tested); it consumes only the normalized
// snapshot — never a platform RPC. Platform specifics live in the adapter
// (examples/globular-runtime-adapter), not here.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Diagnosis verdict vocabulary (subset of the spec; the first set).
const (
	vConverged          = "cluster_converged"
	vNotConverged       = "cluster_not_converged"
	vBlockedQuorum      = "blocked_by_quorum"
	vBlockedDependency  = "blocked_by_dependency"
	vBlockedAdmission   = "blocked_by_admission_state"
	vBlockedDesiredMiss = "blocked_by_desired_state_missing"
	vIdentityMismatch   = "runtime_identity_mismatch"
	vEvidenceMissing    = "runtime_evidence_missing"
	vEvidenceStale      = "runtime_evidence_stale"
)

type diagnosis struct {
	Subject      string   `json:"subject"`
	Verdict      string   `json:"verdict"`
	Confidence   string   `json:"confidence"` // high | low_stale_evidence
	Reason       string   `json:"reason"`
	MissingLanes []string `json:"missing_lanes,omitempty"`
	StaleLanes   []string `json:"stale_lanes,omitempty"`
}

// inferRequiredLanes is the default minimum to reach a convergence verdict when
// the snapshot does not declare its own required_lanes.
func inferRequiredLanes(subjectType string) []string {
	return []string{"desired_state", "observed_state"}
}

func factBool(f map[string]interface{}, key string) (val, present bool) {
	if f == nil {
		return false, false
	}
	v, ok := f[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func factString(f map[string]interface{}, key string) string {
	if f == nil {
		return ""
	}
	if v, ok := f[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func laneFresh(l runtimeEvidenceLane) bool { return l.Freshness == "fresh" }

func hasBlockingFinding(l runtimeEvidenceLane) (laneFinding, bool) {
	for _, f := range l.Findings {
		if f.Severity == "blocking" || f.Severity == "critical" {
			return f, true
		}
	}
	return laneFinding{}, false
}

// diagnoseRuntime is the pure verdict engine.
func diagnoseRuntime(s runtimeEvidenceSnapshot) diagnosis {
	subj := s.Subject.Type + ":" + s.Subject.ID
	if s.Subject.Node != "" {
		subj += "@" + s.Subject.Node
	}
	d := diagnosis{Subject: subj}

	required := s.VerdictInputs.RequiredLanes
	if len(required) == 0 {
		required = inferRequiredLanes(s.Subject.Type)
	}

	// 1. Required evidence must be present. Absent / status unavailable = missing.
	var missing []string
	for _, name := range required {
		lane, ok := s.Lanes[name]
		if !ok || lane.Status == "missing" || lane.Status == "unavailable" || lane.Freshness == "unavailable" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		d.Verdict, d.Confidence, d.MissingLanes = vEvidenceMissing, "high", missing
		d.Reason = "cannot diagnose: required evidence lane(s) absent/unavailable: " + strings.Join(missing, ", ")
		return d
	}

	// Freshness of required lanes (for green-gating + confidence).
	var stale []string
	for _, name := range required {
		if !laneFresh(s.Lanes[name]) {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	d.StaleLanes = stale
	confidence := "high"
	if len(stale) > 0 {
		confidence = "low_stale_evidence"
	}
	d.Confidence = confidence

	des := s.Lanes["desired_state"]
	obs := s.Lanes["observed_state"]
	ident, hasIdent := s.Lanes["runtime_identity"]
	quorum, hasQuorum := s.Lanes["quorum"]
	diag, hasDiag := s.Lanes["diagnosis"]

	set := func(v, reason string) diagnosis { d.Verdict, d.Reason = v, reason; return d }

	// 2. Explicit quorum block (owner/diagnostic fact, or a quorum finding).
	if hasQuorum {
		if met, present := factBool(quorum.Facts, "quorum_met"); present && !met {
			return set(vBlockedQuorum, fmt.Sprintf("quorum not satisfied (%v/%v %s members)",
				quorum.Facts["available_members"], quorum.Facts["required_members"], factString(quorum.Facts, "subsystem")))
		}
	}
	if hasDiag {
		if f, ok := hasBlockingFinding(diag); ok {
			low := strings.ToLower(f.ID + " " + f.Summary)
			switch {
			case strings.Contains(low, "quorum"):
				return set(vBlockedQuorum, "blocking diagnosis: "+f.Summary)
			case strings.Contains(low, "depend"):
				return set(vBlockedDependency, "blocking diagnosis: "+f.Summary)
			case strings.Contains(low, "admiss") || strings.Contains(low, "admit"):
				return set(vBlockedAdmission, "blocking diagnosis: "+f.Summary)
			default:
				return set(vNotConverged, "blocking diagnosis: "+f.Summary)
			}
		}
	}

	// 3. Desired present?
	desiredPresent := factString(des.Facts, "desired_build_id") != "" ||
		factString(des.Facts, "desired") == "present"
	if !desiredPresent && factString(des.Facts, "desired") == "absent" {
		return set(vBlockedDesiredMiss, "no desired state for the subject (nothing is supposed to run)")
	}

	// 4. Runtime identity mismatch (what runs != what is desired).
	if hasIdent && desiredPresent {
		want := factString(des.Facts, "desired_build_id")
		got := factString(ident.Facts, "installed_build_id")
		if got == "" {
			got = factString(ident.Facts, "running_build_id")
		}
		if want != "" && got != "" && want != got {
			return set(vIdentityMismatch, fmt.Sprintf("runtime identity mismatch: running %q, desired %q", got, want))
		}
	}

	// 5. Installed/running vs desired.
	installed, _ := factBool(obs.Facts, "installed")
	running, runningPresent := factBool(obs.Facts, "running")
	if desiredPresent && !installed {
		return set(vNotConverged, "desired but not installed, with no blocking diagnosis (release dispatch/convergence gap)")
	}
	if desiredPresent && installed && (running || !runningPresent) {
		// Candidate converged — but green requires fresh evidence.
		if len(stale) > 0 {
			return set(vEvidenceStale,
				"subject appears installed+running but required evidence is not fresh ("+strings.Join(stale, ", ")+
					") — cannot confirm convergence from stale/unknown evidence")
		}
		return set(vConverged, "desired, installed, running, identity consistent, evidence fresh")
	}

	return set(vNotConverged, "subject is not in a converged state and no specific block was identified")
}

// exitCodeForDiagnosis: a produced diagnosis (converged/blocked/mismatch) is a
// successful diagnosis (0); insufficient/stale-blocked evidence is non-zero.
func exitCodeForDiagnosis(verdict string) int {
	switch verdict {
	case vEvidenceMissing, vEvidenceStale:
		return 1
	default:
		return 0
	}
}

func runClusterDiagnose(args []string) int {
	fs := flag.NewFlagSet("sensei cluster-diagnose", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	evidence := fs.String("runtime-evidence", "", "path to a runtime-evidence/v1 snapshot (yaml or json) (required)")
	asJSON := fs.Bool("json", false, "machine-readable verdict")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei cluster-diagnose --runtime-evidence <snapshot.yaml|json> [--json]

Produces a typed runtime verdict from a normalized snapshot. Exit 0 for a
produced diagnosis (converged / blocked_by_* / runtime_identity_mismatch /
cluster_not_converged); exit 1 for runtime_evidence_missing or
runtime_evidence_stale (AWG will not emit a green verdict from non-fresh
evidence). Consumes normalized evidence only — never a platform RPC.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *evidence == "" {
		fmt.Fprintln(os.Stderr, "sensei cluster-diagnose: --runtime-evidence <file> is required")
		return 2
	}
	raw, err := os.ReadFile(*evidence)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei cluster-diagnose: read %s: %v\n", *evidence, err)
		return 1
	}
	var snap runtimeEvidenceSnapshot
	if err := yaml.Unmarshal(raw, &snap); err != nil { // yaml.Unmarshal also parses JSON
		fmt.Fprintf(os.Stderr, "sensei cluster-diagnose: parse %s: %v\n", *evidence, err)
		return 1
	}
	// Refuse to diagnose a structurally invalid snapshot.
	if vf := validateRuntimeSnapshot(snap); hasErrors(vf) {
		fmt.Fprintf(os.Stderr, "sensei cluster-diagnose: %s is not a valid runtime-evidence/v1 snapshot (run `sensei runtime-snapshot validate`)\n", *evidence)
		return 1
	}

	d := diagnoseRuntime(snap)
	if *asJSON {
		fmt.Printf("{\"subject\":%q,\"verdict\":%q,\"confidence\":%q,\"reason\":%q}\n", d.Subject, d.Verdict, d.Confidence, d.Reason)
	} else {
		fmt.Printf("cluster-diagnose: %s\n", d.Subject)
		fmt.Printf("  VERDICT:    %s\n", d.Verdict)
		fmt.Printf("  confidence: %s\n", d.Confidence)
		fmt.Printf("  reason:     %s\n", d.Reason)
		if len(d.MissingLanes) > 0 {
			fmt.Printf("  missing:    %s\n", strings.Join(d.MissingLanes, ", "))
		}
		if len(d.StaleLanes) > 0 {
			fmt.Printf("  stale:      %s\n", strings.Join(d.StaleLanes, ", "))
		}
	}
	return exitCodeForDiagnosis(d.Verdict)
}
