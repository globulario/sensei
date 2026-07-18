// SPDX-License-Identifier: Apache-2.0

package completion

import (
	"context"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

const completionProjectionSchemaVersion = "completion.projection/v1"

// CompletionProjection is the ONE canonical, deterministic, read-only operational
// view of a task's completion status. Server, task-status, and task-briefing consume
// this single projection rather than each inventing its own mapping of the Phase-8
// owners. It is a presentation of the owner's reconstruction and end-to-end
// verification — it is NOT an owner, holds no mutation authority, and is never
// terminal truth. It preserves the owners' closed state/verdict vocabularies without
// collapsing them.
type CompletionProjection struct {
	SchemaVersion string                        `json:"schema_version" yaml:"schema_version"`
	Task          closureprotocol.TaskBinding   `json:"task" yaml:"task"`
	ResultBinding closureprotocol.ResultBinding `json:"result_binding,omitempty" yaml:"result_binding,omitempty"`
	// TerminalState is exactly what InspectTerminalState reconstructed.
	TerminalState TerminalState `json:"terminal_state" yaml:"terminal_state"`
	// ClosureVerdict is exactly what VerifyCompletionClosure produced.
	ClosureVerdict ClosureVerdict `json:"closure_verdict" yaml:"closure_verdict"`
	// AuthoritativeCompletion is derived ONLY from ClosureAuthoritativeCompletion.
	AuthoritativeCompletion bool `json:"authoritative_completion" yaml:"authoritative_completion"`
	// GovernedDriftAfterCompletion preserves the distinction between historical
	// authoritative completion and current governed drift.
	GovernedDriftAfterCompletion bool                    `json:"governed_drift_after_completion" yaml:"governed_drift_after_completion"`
	Components                   []ComponentVerification `json:"components,omitempty" yaml:"components,omitempty"`
	Detail                       string                  `json:"detail,omitempty" yaml:"detail,omitempty"`
	// Distinctions state which of the three claims this view shows and disclaims the
	// other two.
	Distinctions []string `json:"distinctions" yaml:"distinctions"`
	// NonAuthoritativeProjection is always true — explicit read-only semantics.
	NonAuthoritativeProjection bool     `json:"non_authoritative_projection" yaml:"non_authoritative_projection"`
	Bound                      []string `json:"bound" yaml:"bound"`
	DigestSHA256               string   `json:"digest_sha256,omitempty" yaml:"digest_sha256,omitempty"`
}

func projectionDistinctions() []string {
	return []string{
		"this projection shows the completion status of exactly ONE task's current result; it is not Phase-8 implementation closure and not repository-wide perfection",
		"authoritative_completion here means only that the durable event/receipt conjunction re-verified for THIS task; it asserts nothing about any other task or the repository",
		"the Phase-8 closure report is implementation evidence, never a task-completion fact, and is never used here as terminal authority",
	}
}

func projectionBound() []string {
	return []string{
		"a read-only, non-authoritative operational projection composed from the completion owner's read surfaces (InspectTerminalState, VerifyCompletionClosure)",
		"the durable completed event plus its matching receipt, reconstructed by the owner, is the sole terminal truth; building or rendering this projection mutates nothing",
	}
}

// BuildCompletionProjection composes the canonical read-only completion projection by
// calling the exported Phase-8 read owners. It re-derives no terminal truth, reads no
// raw ledger/receipt files, and never calls CompleteTask or RecoverProjections. It is
// deterministic: identical durable evidence yields a byte-identical projection.
func BuildCompletionProjection(ctx context.Context, req Request) (CompletionProjection, error) {
	closure, err := VerifyCompletionClosure(ctx, req)
	if err != nil {
		return CompletionProjection{}, err
	}
	p := CompletionProjection{
		SchemaVersion:                completionProjectionSchemaVersion,
		Task:                         closure.Terminal.Task,
		ResultBinding:                closure.Terminal.CurrentResultBinding,
		TerminalState:                closure.Terminal.State,
		ClosureVerdict:               closure.Verdict,
		AuthoritativeCompletion:      closure.Verdict == ClosureAuthoritativeCompletion,
		GovernedDriftAfterCompletion: closure.GovernedDriftAfterCompletion,
		Components:                   closure.Components,
		Detail:                       closure.Terminal.Detail,
		Distinctions:                 projectionDistinctions(),
		NonAuthoritativeProjection:   true,
		Bound:                        projectionBound(),
	}
	p.DigestSHA256 = ""
	if d, derr := closureprotocol.SemanticDigest(p); derr == nil {
		p.DigestSHA256 = d
	}
	return p, nil
}

// Summary is a single deterministic line for compact operational display. It is the
// single canonical text mapping — surfaces render this rather than re-deriving one.
func (p CompletionProjection) Summary() string {
	drift := ""
	if p.GovernedDriftAfterCompletion {
		drift = " (governed drift after completion)"
	}
	return fmt.Sprintf("completion: state=%s verdict=%s authoritative=%v%s [non-authoritative projection]",
		p.TerminalState, p.ClosureVerdict, p.AuthoritativeCompletion, drift)
}
