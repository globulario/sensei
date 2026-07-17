// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// advanceResultOutput is the stable machine-readable render of the orchestration
// owner's result. It mirrors tasksession.AdvanceResult verbatim and adds nothing:
// the CLI translates operational inputs and renders the owner's typed result — it
// computes no phase, authority, digest, proof, or projection of its own.
// correctness_certified is ALWAYS false; this slice never certifies.
type advanceResultOutput struct {
	Outcome                       string   `json:"outcome" yaml:"outcome"`
	TransitionRecorded            bool     `json:"transition_recorded" yaml:"transition_recorded"`
	TransitionDisposition         string   `json:"transition_disposition,omitempty" yaml:"transition_disposition,omitempty"`
	TransitionID                  string   `json:"transition_id,omitempty" yaml:"transition_id,omitempty"`
	TransitionEntryDigestSHA256   string   `json:"transition_entry_digest_sha256,omitempty" yaml:"transition_entry_digest_sha256,omitempty"`
	CurrentLedgerHeadDigestSHA256 string   `json:"current_ledger_head_digest_sha256,omitempty" yaml:"current_ledger_head_digest_sha256,omitempty"`
	LedgerSequence                int      `json:"ledger_sequence,omitempty" yaml:"ledger_sequence,omitempty"`
	TaskPhase                     string   `json:"task_phase,omitempty" yaml:"task_phase,omitempty"`
	OperationalStatus             string   `json:"operational_status,omitempty" yaml:"operational_status,omitempty"`
	NextAction                    string   `json:"next_action,omitempty" yaml:"next_action,omitempty"`
	NextActionSummary             string   `json:"next_action_summary,omitempty" yaml:"next_action_summary,omitempty"`
	WaitingReasons                []string `json:"waiting_reasons,omitempty" yaml:"waiting_reasons,omitempty"`
	CurrentStateAvailable         bool     `json:"current_state_available" yaml:"current_state_available"`
	CurrentStateDetail            string   `json:"current_state_detail,omitempty" yaml:"current_state_detail,omitempty"`
	RefusalCode                   string   `json:"refusal_code,omitempty" yaml:"refusal_code,omitempty"`
	RefusalDetail                 string   `json:"refusal_detail,omitempty" yaml:"refusal_detail,omitempty"`
	PostCommitEntryDigestSHA256   string   `json:"post_commit_entry_digest_sha256,omitempty" yaml:"post_commit_entry_digest_sha256,omitempty"`
	PostCommitRecoveryAction      string   `json:"post_commit_recovery_action,omitempty" yaml:"post_commit_recovery_action,omitempty"`
	CorrectnessCertified          bool     `json:"correctness_certified" yaml:"correctness_certified"`
}

// runAdvanceTask advances a task by one legal action through the single
// orchestration owner tasksession.AdvanceResultTransition and renders its result.
// It accepts only operational inputs — repository root, task directory, domain,
// committed result revision — never a caller-supplied phase, status, authority
// verdict, admission success, digest, proof result, or certification claim.
func runAdvanceResult(args []string) int {
	var repoRoot, taskDir, domain, resultRevision, format string
	fs := flag.NewFlagSet("sensei advance-result", flag.ContinueOnError)
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&domain, "domain", "", "repository domain (default: resolved from the admitted base)")
	fs.StringVar(&resultRevision, "result-revision", "", "committed result revision to record at scope_verified")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "advance-result:", err)
		return 2
	}

	res, err := tasksession.AdvanceResultTransition(context.Background(), tasksession.AdvanceResultRequest{
		RepositoryRoot: repoRoot, TaskDirectory: dir, RepositoryDomain: domain,
		ResultRevision: resultRevision,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "advance-result:", err)
		return 2
	}

	out := advanceResultOutput{
		Outcome:                       string(res.Outcome),
		TransitionRecorded:            res.TransitionRecorded,
		TransitionDisposition:         string(res.TransitionDisposition),
		TransitionID:                  res.TransitionID,
		TransitionEntryDigestSHA256:   res.TransitionEntryDigestSHA256,
		CurrentLedgerHeadDigestSHA256: res.CurrentLedgerHeadSHA256,
		LedgerSequence:                res.LedgerSequence,
		TaskPhase:                     string(res.TaskPhase),
		OperationalStatus:             res.OperationalStatus,
		NextAction:                    res.NextAction.Action,
		NextActionSummary:             res.NextAction.Summary,
		WaitingReasons:                res.WaitingReasons,
		CurrentStateAvailable:         res.CurrentStateAvailable,
		CurrentStateDetail:            res.CurrentStateDetail,
		RefusalCode:                   res.RefusalCode,
		RefusalDetail:                 res.RefusalDetail,
		PostCommitEntryDigestSHA256:   res.PostCommitEntryDigestSHA256,
		PostCommitRecoveryAction:      res.PostCommitRecoveryAction,
		CorrectnessCertified:          false,
	}

	if strings.TrimSpace(format) == "" || format == "text" {
		renderAdvanceHuman(out)
	} else if err := printValue(out, format); err != nil {
		fmt.Fprintln(os.Stderr, "advance-result:", err)
		return 1
	}
	return advanceExitCode(res.Outcome)
}

// renderAdvanceHuman prints a concise human summary leading with the load-bearing
// next action, never a flat blob.
func renderAdvanceHuman(o advanceResultOutput) {
	fmt.Printf("outcome:  %s\n", o.Outcome)
	if o.TransitionRecorded {
		fmt.Printf("transition: %s (%s)\n", short(o.TransitionEntryDigestSHA256), o.TransitionDisposition)
		fmt.Printf("head:     %s  seq %d\n", short(o.CurrentLedgerHeadDigestSHA256), o.LedgerSequence)
	}
	if o.TaskPhase != "" {
		fmt.Printf("phase:    %s / %s\n", o.TaskPhase, o.OperationalStatus)
	}
	if o.NextActionSummary != "" {
		fmt.Printf("next:     %s\n", o.NextActionSummary)
	}
	for _, r := range o.WaitingReasons {
		fmt.Printf("waiting:  %s\n", r)
	}
	if o.RefusalCode != "" {
		fmt.Printf("refusal:  %s — %s\n", o.RefusalCode, o.RefusalDetail)
	}
	if o.PostCommitRecoveryAction != "" {
		fmt.Printf("recovery: %s (committed entry %s)\n", o.PostCommitRecoveryAction, short(o.PostCommitEntryDigestSHA256))
	}
	if !o.CurrentStateAvailable && o.CurrentStateDetail != "" {
		fmt.Printf("current:  %s\n", o.CurrentStateDetail)
	}
	fmt.Printf("correctness_certified: %v\n", o.CorrectnessCertified)
}

func short(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// advanceExitCode maps an outcome to a stable process exit code: 0 for recorded
// or waiting (a legal state), 3 for refused/stale (no legal advance now / must
// re-derive from the current head), 1 for post-commit incomplete (the entry is
// durable; retry the exact same command).
func advanceExitCode(o tasksession.AdvanceOutcome) int {
	switch o {
	case tasksession.OutcomeRecorded, tasksession.OutcomeWaiting:
		return 0
	case tasksession.OutcomeRefused, tasksession.OutcomeStale:
		return 3
	case tasksession.OutcomePostCommitIncomplete:
		return 1
	default:
		return 1
	}
}
