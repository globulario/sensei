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

// runCompleteTask is the thin invocation surface for terminal completion. It carries
// NO completion logic: readiness re-proof, authority resolution, terminal-history
// cardinality, receipt construction, and the append transaction all live in
// golang/architecture/completion.CompleteTask, the sole terminal-completion authority.
// This command only resolves inputs, delegates once, and reports the owner's closed
// outcome set. It cannot manufacture completion: it accepts no caller-supplied status,
// and every refusal is surfaced honestly while the owner writes nothing.
func runCompleteTask(args []string) int {
	fs := flag.NewFlagSet("sensei complete-task", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var repoRoot, taskDir, identityRoot, expectedHead, format string
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&identityRoot, "identity-root", "", "completion actor identity store (default: <repo>/.sensei/identity)")
	fs.StringVar(&expectedHead, "expected-head", "", "expected task-ledger head digest (optimistic-concurrency guard)")
	fs.StringVar(&format, "format", "text", "output format: text | json | yaml")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei complete-task --expected-head <digest> [--repo <dir>] [--task-dir <dir>] [flags]

Delegates to the Phase-8 terminal-completion owner (completion.CompleteTask): it
re-proves readiness, resolves completion authority, checks terminal-history
cardinality, and — only when the whole durable conjunction holds — appends the
frozen 'completed' event referencing a content-addressed completion receipt. A
replay of an already-completed, unchanged task is idempotent (exact_replay); any
refusal (not_ready, stale_expected_head, authority_refusal, ...) is reported and
leaves the ledger untouched. This surface adds no completion logic and no new
authority: it accepts no caller-supplied status and cannot manufacture completion.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	// Reject positional input outright rather than silently ignoring it: the surface
	// accepts no completion status or claim, only typed flags the owner consumes.
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "sensei complete-task: unexpected argument %q; this surface accepts no completion status or positional input\n", fs.Arg(0))
		return 2
	}
	if strings.TrimSpace(expectedHead) == "" {
		fmt.Fprintln(os.Stderr, "sensei complete-task: --expected-head is required")
		return 2
	}
	if format != "text" && format != "json" && format != "yaml" {
		fmt.Fprintln(os.Stderr, "sensei complete-task: --format must be text | json | yaml")
		return 2
	}

	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei complete-task:", err)
		return 2
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei complete-task:", err)
		return 2
	}
	if strings.TrimSpace(identityRoot) == "" {
		identityRoot = filepath.Join(absRepo, ".sensei", "identity")
	}

	res, err := completeTaskDelegate(context.Background(), completion.CompleteRequest{
		RepositoryRoot:                 absRepo,
		TaskDirectory:                  dir,
		IdentityRoot:                   identityRoot,
		ExpectedLedgerHeadDigestSHA256: strings.TrimSpace(expectedHead),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei complete-task: %v\n", err)
		return 1
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(res); encErr != nil {
			fmt.Fprintf(os.Stderr, "sensei complete-task: %v\n", encErr)
			return 1
		}
	case "yaml":
		data, mErr := yaml.Marshal(res)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "sensei complete-task: %v\n", mErr)
			return 1
		}
		fmt.Print(string(data))
	default:
		fmt.Print(renderCompleteTaskText(res))
	}

	// Exit status reflects the owner's outcome, never a caller assertion.
	return completeTaskExitCode(res.Outcome)
}

// completeTaskDelegate is the terminal-completion owner. It is a package var so tests can
// inject any of the closed outcomes (and an infrastructure error) to prove the full
// outcome→exit-code mapping without reconstructing the readiness world.
var completeTaskDelegate = completion.CompleteTask

// completeTaskExitCode maps the owner's closed outcome set to a process exit code: only
// the two success outcomes are 0; every refusal or failure is 1. It is the single place
// the surface classifies an outcome, and it invents no outcome of its own.
func completeTaskExitCode(o completion.Outcome) int {
	switch o {
	case completion.OutcomeCommitted, completion.OutcomeExactReplay:
		return 0
	default:
		return 1
	}
}

func renderCompleteTaskText(res completion.CompleteResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Completion outcome: %s\n", res.Outcome)
	if strings.TrimSpace(res.Detail) != "" {
		fmt.Fprintf(&b, "Detail: %s\n", res.Detail)
	}
	if res.Receipt != nil {
		fmt.Fprintf(&b, "Receipt path: %s\n", res.ReceiptPath)
		fmt.Fprintf(&b, "Receipt digest: %s\n", res.Receipt.ReceiptDigestSHA256)
		fmt.Fprintf(&b, "Causal identity: %s\n", res.Receipt.CausalIdentitySHA256)
	}
	if res.Outcome == completion.OutcomeNotReady && res.Assessment != nil {
		fmt.Fprintf(&b, "Readiness: not ready (assessment %s)\n", res.Assessment.DigestSHA256)
		for _, ob := range res.Assessment.Obligations {
			if ob.State == completion.EvidenceSatisfied {
				continue
			}
			fmt.Fprintf(&b, "  - %s: %s", ob.Obligation, ob.State)
			if strings.TrimSpace(ob.Detail) != "" {
				fmt.Fprintf(&b, " — %s", ob.Detail)
			}
			fmt.Fprintln(&b)
		}
	}
	return b.String()
}
