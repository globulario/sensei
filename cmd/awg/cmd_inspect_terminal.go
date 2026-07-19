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

// inspectTerminalDelegate is the read-only terminal-state reconstruction owner. It is a
// package var so tests can inject any of the closed terminal states without seeding a
// durable world.
var inspectTerminalDelegate = completion.InspectTerminalState

// runInspectTerminal is the thin read surface over completion.InspectTerminalState. It
// carries NO reconstruction logic and NEVER mutates: it resolves inputs, delegates once,
// and reports the owner's honest terminal-state reconstruction verbatim. It accepts no
// caller-supplied status and reinterprets nothing — a broken or contradictory state is
// surfaced as itself, not re-mapped into a pass/fail verdict.
func runInspectTerminal(args []string) int {
	fs := flag.NewFlagSet("sensei inspect-terminal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var repoRoot, taskDir, format string
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&format, "format", "text", "output format: text | json | yaml")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei inspect-terminal [--repo <dir>] [--task-dir <dir>] [flags]

Delegates to the Phase-8 terminal-state reconstruction owner
(completion.InspectTerminalState): it reconstructs the honest terminal state of a
task from durable owners alone — not_completed, committed, receipt_without_event,
event_without_valid_receipt, contradictory_terminal_history,
wrong_task_or_result_binding, integrity_failure, projection_stale_or_missing, or
unsupported. Read-only: it establishes no completion, blesses no residue, and
normalizes no contradiction. This surface reports the reconstruction verbatim.
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
		fmt.Fprintf(os.Stderr, "sensei inspect-terminal: unexpected argument %q; this surface accepts no positional input\n", fs.Arg(0))
		return 2
	}
	if format != "text" && format != "json" && format != "yaml" {
		fmt.Fprintln(os.Stderr, "sensei inspect-terminal: --format must be text | json | yaml")
		return 2
	}

	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei inspect-terminal:", err)
		return 2
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei inspect-terminal:", err)
		return 2
	}

	res, err := inspectTerminalDelegate(context.Background(), completion.Request{RepositoryRoot: absRepo, TaskDirectory: dir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei inspect-terminal: %v\n", err)
		return 1
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(res); encErr != nil {
			fmt.Fprintf(os.Stderr, "sensei inspect-terminal: %v\n", encErr)
			return 1
		}
	case "yaml":
		data, mErr := yaml.Marshal(res)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "sensei inspect-terminal: %v\n", mErr)
			return 1
		}
		fmt.Print(string(data))
	default:
		fmt.Print(renderTerminalStateText(res))
	}
	// Read-only: a successful reconstruction of ANY honest state exits 0. The surface
	// does not re-map the reconstructed state into a pass/fail verdict.
	return 0
}

func renderTerminalStateText(a completion.TerminalStateAssessment) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Terminal state: %s\n", a.State)
	if strings.TrimSpace(a.Detail) != "" {
		fmt.Fprintf(&b, "Detail: %s\n", a.Detail)
	}
	fmt.Fprintf(&b, "Completed events: %d  Revoked events: %d\n", a.CompletedCount, a.RevokedCount)
	if a.Committed != nil {
		fmt.Fprintf(&b, "Committed receipt: %s\n", a.Committed.ReceiptDigestSHA256)
		fmt.Fprintf(&b, "Governed drift after completion: %v\n", a.Committed.GovernedDriftAfterCompletion)
	}
	fmt.Fprintf(&b, "Assessment digest: %s\n", a.DigestSHA256)
	return b.String()
}
