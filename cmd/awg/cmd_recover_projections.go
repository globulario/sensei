// SPDX-License-Identifier: Apache-2.0

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

// recoverProjectionsDelegate is the derived-state recovery owner. It is a package var so
// tests can inject any of the closed recovery outcomes without seeding a durable world.
var recoverProjectionsDelegate = completion.RecoverProjections

// runRecoverProjections is the thin surface over completion.RecoverProjections. It
// carries NO recovery logic: it resolves inputs, delegates once, and reports the owner's
// closed outcome. Recovery is derived-state maintenance only — it appends no ledger
// event, rewrites no receipt, never normalizes contradiction, and never blesses residue;
// the surface adds none of that and can never manufacture a terminal fact.
func runRecoverProjections(args []string) int {
	fs := flag.NewFlagSet("sensei recover-projections", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var repoRoot, taskDir, format string
	fs.StringVar(&repoRoot, "repo", ".", "repository root")
	fs.StringVar(&taskDir, "task-dir", "", "task directory (default: the active task)")
	fs.StringVar(&format, "format", "text", "output format: text | json | yaml")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei recover-projections [--repo <dir>] [--task-dir <dir>] [flags]

Delegates to the Phase-8 recovery owner (completion.RecoverProjections): when a
completion is valid but its derived projections are stale or missing, it rebuilds
them from the durable conjunction. Derived-state maintenance ONLY — it appends no
ledger event, rewrites no receipt, resolves no completion authority, never
normalizes contradictory terminal history, and never blesses receipt-only residue;
retry through complete-task is the only path that may append a completion. Reports
the owner's closed outcome (projections_rebuilt, already_current,
nothing_to_recover, contradictory_terminal_history, broken_completion,
unsupported, input_invalid).
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
		fmt.Fprintf(os.Stderr, "sensei recover-projections: unexpected argument %q; this surface accepts no positional input\n", fs.Arg(0))
		return 2
	}
	if format != "text" && format != "json" && format != "yaml" {
		fmt.Fprintln(os.Stderr, "sensei recover-projections: --format must be text | json | yaml")
		return 2
	}

	dir, err := resolveTaskLedgerDir(repoRoot, taskDir, taskDir == "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei recover-projections:", err)
		return 2
	}
	absRepo, err := filepath.Abs(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sensei recover-projections:", err)
		return 2
	}

	res, err := recoverProjectionsDelegate(context.Background(), completion.Request{RepositoryRoot: absRepo, TaskDirectory: dir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei recover-projections: %v\n", err)
		return 1
	}

	switch format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(res); encErr != nil {
			fmt.Fprintf(os.Stderr, "sensei recover-projections: %v\n", encErr)
			return 1
		}
	case "yaml":
		data, mErr := yaml.Marshal(res)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "sensei recover-projections: %v\n", mErr)
			return 1
		}
		fmt.Print(string(data))
	default:
		fmt.Print(renderRecoverText(res))
	}
	return recoverExitCode(res.Outcome)
}

// recoverExitCode maps the owner's closed recovery outcome to an exit code: only the
// outcomes where the derived projections are current against a valid terminal fact
// (rebuilt or already-current) are 0; every other outcome — nothing to recover, a
// contradiction that must NOT be normalized, a broken completion, an unsupported ledger,
// or invalid input — is 1. It invents no outcome of its own.
func recoverExitCode(o completion.RecoverOutcome) int {
	switch o {
	case completion.RecoverProjectionsRebuilt, completion.RecoverAlreadyCurrent:
		return 0
	default:
		return 1
	}
}

func renderRecoverText(res completion.RecoverResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Recovery outcome: %s\n", res.Outcome)
	if strings.TrimSpace(res.Detail) != "" {
		fmt.Fprintf(&b, "Detail: %s\n", res.Detail)
	}
	fmt.Fprintf(&b, "Before: %s\n", res.Before.State)
	if res.After != nil {
		fmt.Fprintf(&b, "After: %s\n", res.After.State)
	}
	return b.String()
}
