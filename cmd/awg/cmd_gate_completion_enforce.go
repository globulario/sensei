// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/completion"
)

// runGateCompletionEnforce is Phase 9.4b: the completion gate's ENFORCE path. It
// resolves the domain's completion policy, consumes the same read-only typed envelope
// the advisory path uses, applies the pure decision (decideCompletionEnforcement), and
// exits 1 only on a block. It mutates nothing; identity source is the explicit
// --task-dir (no fallback). A malformed policy fails loudly (exit 2), never advisory.
func runGateCompletionEnforce(repoRoot, taskDir, domain, policyPath string, asJSON bool, sarifPath string) int {
	td := strings.TrimSpace(taskDir)
	if td == "" {
		fmt.Fprintln(os.Stderr, "sensei gate --completion --enforce: --task-dir is required (explicit task identity; no fallback)")
		return 2
	}
	absRepo, err := filepath.Abs(strings.TrimSpace(repoRoot))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei gate --completion --enforce:", err)
		return 2
	}
	absTask, err := filepath.Abs(td)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei gate --completion --enforce:", err)
		return 2
	}

	// Resolve the completion policy for this domain. A malformed/invalid/unreadable
	// policy is a loud configuration error — never silently treated as absent or advisory.
	state, perr := resolveCompletionPolicy(policyPath, absRepo, domain)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "sensei gate --completion --enforce: invalid completion policy: %v\n", perr)
		return 2
	}

	// Consume the typed availability envelope, read-only, exactly as the advisory path.
	env := completion.BuildCompletionProjectionEnvelope(context.Background(), completion.Request{RepositoryRoot: absRepo, TaskDirectory: absTask})
	pub := env.PublicationView()

	in := completionEnforceInput{
		PolicyState:      state,
		PublicationValid: completion.ValidateCompletionPublication(pub) == nil,
		Availability:     env.Availability,
		UnavailableClass: env.UnavailableClass,
	}
	if env.Projection != nil {
		in.Verdict = env.Projection.ClosureVerdict
	}
	decision := decideCompletionEnforcement(in)

	if sarifPath != "" {
		if werr := writeCompletionEnforceSARIF(sarifPath, absTask, domain, state, in, decision); werr != nil {
			// A SARIF write failure is advisory-only; it never changes the gate verdict.
			fmt.Fprintf(os.Stderr, "sensei gate --completion --enforce: warning: could not write SARIF %q: %v\n", sarifPath, werr)
		}
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if eerr := enc.Encode(completionEnforceReport(absTask, domain, state, in, decision)); eerr != nil {
			fmt.Fprintf(os.Stderr, "sensei gate --completion --enforce: %v\n", eerr)
			return 2
		}
	} else {
		fmt.Print(renderCompletionEnforceText(absTask, domain, state, in, decision))
	}

	return exitCodeForDecision(decision)
}

// exitCodeForDecision is the externally observable exit contract for an EVALUATED
// enforcement decision: a block exits 1; pass and the authorized degraded-runtime pass
// both exit 0. (Malformed policy / missing --task-dir exit 2 before a decision exists.)
func exitCodeForDecision(d completionDecision) int {
	if d.Result == decisionBlock {
		return 1
	}
	return 0
}

// completionEnforceResultJSON is the minimum typed enforcement result — a stable,
// machine-readable decision surface, distinct from (and additive to) the completion
// publication format.
type completionEnforceResultJSON struct {
	SchemaVersion    string `json:"schema_version"`
	Domain           string `json:"domain"`
	PolicyState      string `json:"policy_state"`
	Result           string `json:"result"`
	Reason           string `json:"reason"`
	Availability     string `json:"availability"`
	Verdict          string `json:"closure_verdict,omitempty"`
	UnavailableClass string `json:"unavailable_class,omitempty"`
	PublicationValid bool   `json:"publication_valid"`
}

func completionEnforceReport(taskDir, domain string, state completionPolicyState, in completionEnforceInput, d completionDecision) completionEnforceResultJSON {
	return completionEnforceResultJSON{
		SchemaVersion:    "completion.enforce_result/v1",
		Domain:           domain,
		PolicyState:      state.String(),
		Result:           d.Result.String(),
		Reason:           string(d.Reason),
		Availability:     string(in.Availability),
		Verdict:          string(in.Verdict),
		UnavailableClass: string(in.UnavailableClass),
		PublicationValid: in.PublicationValid,
	}
}

func renderCompletionEnforceText(taskDir, domain string, state completionPolicyState, in completionEnforceInput, d completionDecision) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Completion gate (enforce) — task %s\n", taskDir)
	fmt.Fprintf(&b, "Domain: %s\n", domain)
	fmt.Fprintf(&b, "Policy: %s\n", state)
	fmt.Fprintf(&b, "Availability: %s\n", in.Availability)
	if in.Availability == completion.CompletionAvailable {
		fmt.Fprintf(&b, "Verdict: %s\n", in.Verdict)
	} else {
		fmt.Fprintf(&b, "Unavailable class: %s\n", in.UnavailableClass)
	}
	fmt.Fprintf(&b, "Decision: %s (%s)\n", d.Result, d.Reason)
	switch d.Result {
	case decisionBlock:
		fmt.Fprintf(&b, "enforce: BLOCKED — %s.\n", d.Reason)
	case decisionDegradedPass:
		fmt.Fprintf(&b, "enforce: degraded pass — Sensei/owner runtime unavailable after a valid identity; fail-safe, does not block.\n")
	default:
		fmt.Fprintf(&b, "enforce: pass.\n")
	}
	return b.String()
}

// writeCompletionEnforceSARIF renders the enforce decision as a single SARIF result:
// error on a block, warning on a degraded pass, note on a pass. The stable reason code
// is preserved in structured properties.
func writeCompletionEnforceSARIF(path, taskDir, domain string, state completionPolicyState, in completionEnforceInput, d completionDecision) error {
	level := "note"
	switch d.Result {
	case decisionBlock:
		level = "error"
	case decisionDegradedPass:
		level = "warning"
	}
	rule := sarifRule{
		ID:                   "sensei.completion_gate.enforce",
		Name:                 "SenseiCompletionGateEnforce",
		ShortDescription:     sarifText{Text: "Enforced completion-gate decision for an opted-in domain."},
		DefaultConfiguration: sarifConfig{Level: level},
	}
	result := sarifResult{
		RuleID:  rule.ID,
		Level:   level,
		Message: sarifText{Text: fmt.Sprintf("completion gate (enforce): domain=%s policy=%s decision=%s (%s)", domain, state, d.Result, d.Reason)},
		Locations: []sarifLocation{{PhysicalLocation: sarifPhysical{
			ArtifactLocation: sarifArtifact{URI: filepath.ToSlash(taskDir)},
			Region:           sarifRegion{StartLine: 1},
		}}},
		Properties: map[string]interface{}{
			"domain":            domain,
			"policy_state":      state.String(),
			"result":            d.Result.String(),
			"reason":            string(d.Reason),
			"availability":      string(in.Availability),
			"closure_verdict":   string(in.Verdict),
			"unavailable_class": string(in.UnavailableClass),
		},
	}
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "sensei-completion-gate", InformationURI: "https://github.com/globulario/sensei", Rules: []sarifRule{rule}}},
			Results: []sarifResult{result},
		}},
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
