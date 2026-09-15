// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/completion"
	"gopkg.in/yaml.v3"
)

// runAbandonTask is the thin invocation surface for governed abandonment. Like
// complete-task it carries NO transition logic: authority resolution, terminal
// cardinality, receipt construction, the append transaction and the active-pointer
// retirement all live in completion.AbandonTask.
//
// It sits in this family rather than in a new command group because it answers the
// same question complete-task and inspect-terminal answer -- how does this task
// end -- and a separate group would invite a second, divergent notion of terminal.
func runAbandonTask(args []string) int {
	fs := flag.NewFlagSet("sensei abandon-task", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var repoRoot, taskDir, identityRoot, expectedHead, reason, format string
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&identityRoot, "identity-root", "", "abandoning actor identity store (default: <repo>/.sensei/identity)")
	fs.StringVar(&expectedHead, "expected-head", "", "expected task-ledger head digest (optimistic-concurrency guard)")
	fs.StringVar(&reason, "reason", "", "why this task is being abandoned (required)")
	fs.StringVar(&format, "format", "text", "output format: text | json | yaml")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei abandon-task --expected-head <digest> --reason <text> [--repo <dir>] [--task-dir <dir>] [flags]

Delegates to completion.AbandonTask: it records that a task stopped WITHOUT
producing a result, then retires the active-task pointer through its owner.

Abandonment is for a task that cannot satisfy the completion conjunction — work
that landed outside the governed session, or was dropped. It writes the
'abandoned' terminal the closure vocabulary already defines, and it never
attributes a result to the session: the receipt has no result-binding field to
put one in.

The durable record is written BEFORE the pointer is cleared, so an interruption
between the two leaves a resumable residue rather than an untracked task. Rerun
to finish; the transition is idempotent.

A task that already completed or was revoked is REFUSED, not overwritten.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "sensei abandon-task: unexpected argument %q; this surface accepts no terminal status or positional input\n", fs.Arg(0))
		return 2
	}
	if strings.TrimSpace(expectedHead) == "" {
		fmt.Fprintln(os.Stderr, "sensei abandon-task: --expected-head is required")
		return 2
	}
	// The reason is refused here as well as in the owner. A caller that forgot it
	// should learn so before a lock is taken, and the owner still refuses an empty
	// one because this surface is not the only caller.
	if strings.TrimSpace(reason) == "" {
		fmt.Fprintln(os.Stderr, "sensei abandon-task: --reason is required; a terminal state with no stated cause is not a record")
		return 2
	}
	if format != "text" && format != "json" && format != "yaml" {
		fmt.Fprintln(os.Stderr, "sensei abandon-task: --format must be text | json | yaml")
		return 2
	}

	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei abandon-task:", err)
		return 2
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei abandon-task:", err)
		return 2
	}
	if strings.TrimSpace(identityRoot) == "" {
		identityRoot = filepath.Join(absRepo, ".sensei", "identity")
	}

	res, err := abandonTaskDelegate(context.Background(), completion.AbandonRequest{
		RepositoryRoot:                 absRepo,
		TaskDirectory:                  dir,
		IdentityRoot:                   identityRoot,
		ExpectedLedgerHeadDigestSHA256: strings.TrimSpace(expectedHead),
		Reason:                         strings.TrimSpace(reason),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei abandon-task: %v\n", err)
		return 1
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(res); encErr != nil {
			fmt.Fprintf(os.Stderr, "sensei abandon-task: %v\n", encErr)
			return 1
		}
	case "yaml":
		data, mErr := yaml.Marshal(res)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "sensei abandon-task: %v\n", mErr)
			return 1
		}
		fmt.Print(string(data))
	default:
		fmt.Print(renderAbandonTaskText(res))
	}
	return completeTaskExitCode(res.Outcome)
}

// abandonTaskDelegate is the owner. A package var for the same reason
// completeTaskDelegate is one: the outcome-to-exit-code mapping is provable
// without reconstructing a governed world.
var abandonTaskDelegate = completion.AbandonTask

func renderAbandonTaskText(res completion.AbandonResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Abandonment outcome: %s\n", res.Outcome)
	if strings.TrimSpace(res.Detail) != "" {
		fmt.Fprintf(&b, "Detail: %s\n", res.Detail)
	}
	if res.Receipt != nil {
		fmt.Fprintf(&b, "Terminal status: %s\n", res.Receipt.TerminalStatus)
		fmt.Fprintf(&b, "Reason: %s\n", res.Receipt.Reason)
		fmt.Fprintf(&b, "Result produced: none — this session attests no result\n")
		fmt.Fprintf(&b, "Receipt path: %s\n", res.ReceiptPath)
		fmt.Fprintf(&b, "Receipt digest: %s\n", res.Receipt.ReceiptDigestSHA256)
	}
	// Stated either way. "Cleared" and "was already clear" are different facts and
	// a reader resuming an interrupted run needs to know which one happened.
	if res.ActivePointerCleared {
		fmt.Fprintln(&b, "Active pointer: retired by this call")
	} else {
		fmt.Fprintln(&b, "Active pointer: not retired by this call (already clear, or naming another task)")
	}
	return b.String()
}
