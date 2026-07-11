// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=cmd.awg.runtime_candidate
// @awareness file_role=runtime_memory_promotion_phase6
// @awareness enforces=meta.discovery_produces_candidates_not_facts

// Runtime proof lane — Phase 6: memory promotion (candidate, not fact).
//
// Closes the loop: a recurring runtime verdict (from cluster-diagnose /
// runtime-repair-report) is turned into a CANDIDATE governance artifact for
// review — never an auto-enforced rule. This is the runtime-lane enforcement of
// meta.discovery_produces_candidates_not_facts and the spec's law:
//
//	Memory may suggest.  AWG governs.  Runtime evidence proves.  CI/gate enforces.
//
// So this command SUGGESTS (emits status=candidate, requires_review=true) and
// NEVER writes the corpus or seed. Promotion is a separate, human/governed step
// (sensei propose / behavioral_promote_principle). buildRuntimeCandidate is pure
// and fixture-tested; the key invariant under test is that its output is always a
// candidate, never an active/enforced rule.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const runtimeCandidateSchema = "runtime-candidate/v1"

// candidate kinds AWG can be ASKED to govern (after review).
const (
	candFailureMode     = "failure_mode"
	candInvariant       = "invariant"
	candForbiddenAction = "forbidden_action"
	candRequiredLane    = "required_evidence_lane"
)

type runtimeCandidate struct {
	SchemaVersion  string `json:"schema_version" yaml:"schema_version"`
	Status         string `json:"status" yaml:"status"`                   // ALWAYS "candidate"
	RequiresReview bool   `json:"requires_review" yaml:"requires_review"` // ALWAYS true
	Kind           string `json:"kind" yaml:"kind"`
	ProposedID     string `json:"proposed_id" yaml:"proposed_id"`
	Title          string `json:"title" yaml:"title"`
	Subject        string `json:"subject,omitempty" yaml:"subject,omitempty"`
	SourceVerdict  string `json:"source_verdict" yaml:"source_verdict"`
	Rationale      string `json:"rationale" yaml:"rationale"`
	PromotionPath  string `json:"promotion_path" yaml:"promotion_path"`
}

type runtimeReportInput struct {
	Verdict  string `yaml:"verdict" json:"verdict"`
	Subject  string `yaml:"subject" json:"subject"`
	Action   string `yaml:"action" json:"action"`
	Contract string `yaml:"contract" json:"contract"`
}

// buildRuntimeCandidate maps a runtime verdict to a proposed governance candidate.
// Returns ok=false for healthy verdicts (nothing to govern). The result is ALWAYS
// a candidate requiring review — it is never an active/enforced rule.
func buildRuntimeCandidate(in runtimeReportInput) (runtimeCandidate, bool) {
	c := runtimeCandidate{
		SchemaVersion:  runtimeCandidateSchema,
		Status:         "candidate", // invariant: never "active"/"enforced"
		RequiresReview: true,        // invariant: AWG governs; memory only suggests
		Subject:        in.Subject,
		SourceVerdict:  in.Verdict,
		PromotionPath:  "human/governed review, then `sensei propose` or behavioral_promote_principle — never auto-enforced",
	}
	switch in.Verdict {
	case vConverged, rrValid:
		return runtimeCandidate{}, false // healthy — nothing to govern

	case rrForbidden:
		c.Kind = candForbiddenAction
		c.ProposedID = "runtime.forbidden_action_attempted"
		c.Title = "A forbidden runtime action was attempted"
		c.Rationale = fmt.Sprintf("action %q was classified forbidden on %s; recurrence warrants a governed forbidden_action so the gate refuses it everywhere", in.Action, in.Subject)

	case vIdentityMismatch:
		c.Kind = candInvariant
		c.ProposedID = "runtime.identity_must_match_desired"
		c.Title = "Running runtime identity must match desired"
		c.Rationale = "a runtime_identity_mismatch recurred; propose an invariant that the running build/hash must equal the desired one"

	case rrUnproven:
		c.Kind = candFailureMode
		c.ProposedID = "runtime.repair_claimed_without_respected_contract"
		c.Title = "A runtime repair was claimed without a respected governing contract"
		c.Rationale = "repair_claim_unproven recurred; propose a failure_mode + the missing governing contract"

	case vEvidenceMissing:
		c.Kind = candRequiredLane
		c.ProposedID = "runtime.required_evidence_lane_absent"
		c.Title = "A required runtime evidence lane was absent"
		c.Rationale = "runtime_evidence_missing recurred; propose requiring/providing the absent lane in the adapter manifest"

	case vEvidenceStale:
		c.Kind = candFailureMode
		c.ProposedID = "runtime.evidence_stale_blocks_repair"
		c.Title = "Stale runtime evidence blocked a repair verdict"
		c.Rationale = "runtime_evidence_stale recurred; propose a failure_mode / freshness contract for the lane"

	case rrOwnerMismatch:
		c.Kind = candFailureMode
		c.ProposedID = "runtime.non_owner_evidence_used_for_state"
		c.Title = "Non-owner evidence was used for an authority-sensitive verdict"
		c.Rationale = "state_owner_mismatch recurred; propose a failure_mode binding the lane to its owner authority"

	case rrScopeDrift:
		c.Kind = candFailureMode
		c.ProposedID = "runtime.repair_evidence_scope_drift"
		c.Title = "Repair evidence drifted to a different subject"
		c.Rationale = "scope_drift recurred; propose a failure_mode that after-evidence must concern the repaired subject"

	case vBlockedQuorum, vBlockedDependency, vBlockedAdmission, vBlockedDesiredMiss, rrStillNotConv, vNotConverged:
		c.Kind = candFailureMode
		c.ProposedID = "runtime.recurring_block." + sanitizeVerdict(in.Verdict)
		c.Title = "A recurring runtime block pattern (" + in.Verdict + ")"
		c.Rationale = fmt.Sprintf("verdict %s recurred on %s; propose a failure_mode capturing the block pattern + the safe action and forbidden fixes", in.Verdict, in.Subject)

	default:
		c.Kind = candFailureMode
		c.ProposedID = "runtime.unclassified_verdict." + sanitizeVerdict(in.Verdict)
		c.Title = "An unclassified runtime verdict recurred"
		c.Rationale = "verdict " + in.Verdict + " has no candidate mapping yet; propose review to classify it"
	}
	return c, true
}

func sanitizeVerdict(v string) string {
	if v == "" {
		return "empty"
	}
	return v
}

func runRuntimeCandidate(args []string) int {
	fs := flag.NewFlagSet("sensei runtime-candidate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	report := fs.String("report", "", "path to a runtime report (cluster-diagnose / runtime-repair-report output) (required)")
	out := fs.String("out", "", "write the candidate here (default: stdout)")
	asJSON := fs.Bool("json", false, "emit JSON (default: YAML)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei runtime-candidate --report <file> [--out <file>] [--json]

Turns a recurring runtime verdict into a CANDIDATE governance artifact for review.
It SUGGESTS only: status is always "candidate", requires_review is always true,
and it NEVER writes the corpus or seed. Promotion is a separate governed step.
Healthy verdicts (cluster_converged / valid_runtime_repair) yield no candidate.
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *report == "" {
		fmt.Fprintln(os.Stderr, "sensei runtime-candidate: --report <file> is required")
		return 2
	}
	raw, err := os.ReadFile(*report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei runtime-candidate: read %s: %v\n", *report, err)
		return 1
	}
	var in runtimeReportInput
	if err := yaml.Unmarshal(raw, &in); err != nil {
		fmt.Fprintf(os.Stderr, "sensei runtime-candidate: parse %s: %v\n", *report, err)
		return 1
	}

	cand, ok := buildRuntimeCandidate(in)
	if !ok {
		fmt.Printf("sensei runtime-candidate: verdict %q is healthy — no governance candidate needed\n", in.Verdict)
		return 0
	}

	var rendered []byte
	if *asJSON {
		rendered, _ = json.MarshalIndent(cand, "", "  ")
		rendered = append(rendered, '\n')
	} else {
		rendered, _ = yaml.Marshal(cand)
	}
	if *out != "" {
		if err := os.WriteFile(*out, rendered, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "sensei runtime-candidate: write %s: %v\n", *out, err)
			return 1
		}
		fmt.Printf("sensei runtime-candidate: wrote candidate %s (status=candidate, requires_review) to %s\n", cand.ProposedID, *out)
	} else {
		fmt.Print(string(rendered))
	}
	return 0
}
