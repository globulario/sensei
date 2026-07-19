// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/changebinding"
	consumer "github.com/globulario/sensei/golang/architecture/changebindingconsumer"
	cbp "github.com/globulario/sensei/golang/architecture/changebindingproducer"
)

// runGateCompletionComposed is Phase 9.4c Checkpoint 3: it consumes an authoritative
// change-to-task binding BEFORE the 9.4b completion evaluation. The current trusted subject
// is reconstructed from the GitHub event + read-only checkout + explicit task/completion
// inputs, INDEPENDENTLY of the binding publication. The completion evaluation runs only
// inside consumer.Compose's thunk, which is invoked only when the binding is accepted — so
// an invalid binding blocks before any completion interpretation and can never reach the
// 9.4b runtime-degradation lane.
func runGateCompletionComposed(a completionEnforceArgs, absRepo, absTask string, state completionPolicyState, evaluate func() (completionDecision, completionEnforceInput)) int {
	if a.bindingPath == "" || a.taskID == "" || a.sessionID == "" || a.completionDigest == "" {
		fmt.Fprintln(os.Stderr, "sensei gate --completion --enforce --require-binding: --binding, --task-id, --session-id, and --completion-digest are all required")
		return 2
	}

	// Binding is required only where the domain actually requires completion; a binding
	// problem must never activate enforcement for a not-required / unlisted domain.
	required := state == completionPolicyPresentRequired

	// Reconstruct the current subject from the trusted execution context (never the publication).
	ev, _ := cbp.ExtractGitHubEvent(os.Getenv, os.ReadFile)
	co, _ := cbp.ReadCheckout(absRepo)
	subject := consumer.CurrentSubject{
		RepositoryProvider:           ev.RepositoryProvider,
		RepositoryIdentity:           ev.RepositoryIdentity,
		ChangeProvider:               ev.ChangeProvider,
		ChangeID:                     ev.ChangeID,
		BaseSHA:                      ev.BaseSHA,
		HeadSHA:                      ev.HeadSHA,
		CheckoutRepositoryIdentity:   co.RepositoryIdentity,
		CheckoutHeadSHA:              co.HeadSHA,
		TaskDirectory:                a.taskDir,
		TaskID:                       a.taskID,
		TaskSessionID:                a.sessionID,
		CompletionResultDigestSHA256: a.completionDigest,
		ExpectedIssuer:               "sensei.ci",
		ExpectedTool:                 "sensei",
	}

	var bg consumer.BindingGate
	if !required {
		bg = consumer.BindingGate{Validity: consumer.BindingNotRequired}
	} else if bindings, direct := loadBindingForGate(a.bindingPath); direct != nil {
		bg = *direct
	} else {
		bg = consumer.Consume(true, bindings, subject, cbp.DefaultGitHubAuthority())
	}

	var lastIn completionEnforceInput
	final := consumer.Compose(bg, func() consumer.CompletionOutcome {
		d, in := evaluate()
		lastIn = in
		return consumer.CompletionOutcome{Result: d.Result.String(), Reason: string(d.Reason)}
	})

	audit := buildCombinedAudit(a.domain, state, subject, final, lastIn)
	if a.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(audit)
	} else {
		fmt.Print(renderCombinedAudit(audit))
	}
	return exitCodeForFinal(final)
}

// loadBindingForGate reads and strictly parses the binding artifact from the ONE documented
// path. A missing file yields no bindings (→ absent when required); a malformed/unsupported
// publication yields a direct typed binding-gate failure. It searches no alternative paths.
func loadBindingForGate(path string) ([]changebinding.ChangeTaskBinding, *consumer.BindingGate) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil // absent → the set validator blocks with binding_absent
	}
	b, v := changebinding.ParseBinding(data)
	switch v {
	case "":
		return []changebinding.ChangeTaskBinding{b}, nil
	case changebinding.BindingUnsupportedVersion:
		g := consumer.BindingGate{Validity: consumer.BindingGateUnsupportedVersion}
		return nil, &g
	default:
		g := consumer.BindingGate{Validity: consumer.BindingGateMalformed}
		return nil, &g
	}
}

// exitCodeForFinal maps the composed result to the external exit contract: pass and the
// authorized degraded-runtime pass → 0; any block (binding or completion) → 1.
func exitCodeForFinal(f consumer.FinalResult) int {
	if f.Result == "block" {
		return 1
	}
	return 0
}

// combinedEnforceAudit joins the evaluated subject, the binding result, the completion
// result, and the final enforcement result into one deterministic typed record. It carries
// no secrets/tokens/raw event payloads and never flattens the failures into one string.
type combinedEnforceAudit struct {
	SchemaVersion string `json:"schema_version"`
	Domain        string `json:"domain"`
	PolicyState   string `json:"policy_state"`

	// Evaluated subject.
	RepositoryIdentity           string `json:"repository_identity,omitempty"`
	ChangeID                     string `json:"change_id,omitempty"`
	BaseSHA                      string `json:"base_sha,omitempty"`
	HeadSHA                      string `json:"head_sha,omitempty"`
	TaskID                       string `json:"task_id,omitempty"`
	TaskSessionID                string `json:"task_session_id,omitempty"`
	CompletionResultDigestSHA256 string `json:"completion_result_digest_sha256,omitempty"`

	// Binding layer.
	BindingValidity string `json:"binding_validity"`
	BindingReason   string `json:"binding_reason"`

	// Completion layer (present only when the binding permitted completion to run).
	CompletionResult string `json:"completion_result,omitempty"`
	CompletionReason string `json:"completion_reason,omitempty"`
	CompletionRan    bool   `json:"completion_ran"`

	// Final enforcement result.
	FinalResult string `json:"final_result"`
	FinalReason string `json:"final_reason"`
	FinalStage  string `json:"final_stage"`
}

func buildCombinedAudit(domain string, state completionPolicyState, s consumer.CurrentSubject, f consumer.FinalResult, in completionEnforceInput) combinedEnforceAudit {
	a := combinedEnforceAudit{
		SchemaVersion:                "completion.change_task_enforcement/v1",
		Domain:                       domain,
		PolicyState:                  state.String(),
		RepositoryIdentity:           s.RepositoryIdentity,
		ChangeID:                     s.ChangeID,
		BaseSHA:                      s.BaseSHA,
		HeadSHA:                      s.HeadSHA,
		TaskID:                       s.TaskID,
		TaskSessionID:                s.TaskSessionID,
		CompletionResultDigestSHA256: s.CompletionResultDigestSHA256,
		BindingValidity:              string(f.Binding.Validity),
		BindingReason:                string(f.Binding.Validity),
		FinalResult:                  f.Result,
		FinalReason:                  f.Reason,
		FinalStage:                   f.Stage,
	}
	if f.Completion != nil {
		a.CompletionRan = true
		a.CompletionResult = f.Completion.Result
		a.CompletionReason = f.Completion.Reason
	}
	return a
}

func renderCombinedAudit(a combinedEnforceAudit) string {
	completionLine := "completion: (not evaluated — blocked at the binding stage)\n"
	if a.CompletionRan {
		completionLine = fmt.Sprintf("Completion: %s (%s)\n", a.CompletionResult, a.CompletionReason)
	}
	return fmt.Sprintf(
		"Completion gate (enforce + binding) — domain %s\nPolicy: %s\nBinding: %s\n%sFinal: %s (%s) [stage=%s]\n",
		a.Domain, a.PolicyState, a.BindingValidity, completionLine, a.FinalResult, a.FinalReason, a.FinalStage)
}
