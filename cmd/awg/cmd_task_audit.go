// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// runTaskAudit promotes the read-only task inventory into the CLI.
//
// It reports; it never repairs. A malformed task directory is preserved and
// named, never deleted, cleared, superseded, or reconstructed — reconstructing a
// session would manufacture a record of work nobody did. What to do about a
// malformed task is an operator's decision, and this command exists to inform it.
//
// The exit code reflects whether the audit could be PERFORMED, not what it found.
// A repository with malformed tasks is a repository this command described
// successfully; making findings fail the command would make the honest answer
// indistinguishable from a broken tool, and would tempt callers to stop running it.
func runTaskAudit(args []string) int {
	fs := flag.NewFlagSet("sensei task-audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", ".", "repository checkout")
	format := fs.String("format", "text", "output format: text|yaml|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei task-audit [--repo <dir>] [--format text|yaml|json]

Inventories every task session directory and classifies it as valid or
invalid_or_unreadable. This command is READ-ONLY: it never deletes, clears,
supersedes, or reconstructs a task, so a malformed directory is preserved for an
operator to decide about.

Exit code reports whether the audit ran, not what it found.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}

	report, err := tasksession.AuditTasks(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-audit: %v\n", err)
		return 1
	}

	switch *format {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{"architecture_task_audit": report}); err != nil {
			fmt.Fprintf(os.Stderr, "sensei task-audit: %v\n", err)
			return 2
		}
	case "yaml":
		out, merr := yaml.Marshal(map[string]tasksession.TaskAuditReport{"architecture_task_audit": report})
		if merr != nil {
			fmt.Fprintf(os.Stderr, "sensei task-audit: %v\n", merr)
			return 2
		}
		os.Stdout.Write(out)
	default:
		printTaskAuditText(report)
	}
	return 0
}

func printTaskAuditText(r tasksession.TaskAuditReport) {
	if !r.Present {
		// Absence is answered, not treated as a fault.
		fmt.Printf("task sessions: none (%s does not exist)\n", r.TasksDir)
		return
	}
	fmt.Printf("task sessions in %s\n", r.TasksDir)
	fmt.Printf("  valid:   %d\n", r.ValidCount)
	fmt.Printf("  invalid: %d\n", r.InvalidCount)
	if r.ActiveDetail != "" {
		fmt.Printf("  active:  %s\n", r.ActiveDetail)
	}
	for _, e := range r.Entries {
		marker := " "
		if e.Active {
			marker = "*"
		}
		fmt.Printf("%s %-22s %s\n", marker, e.Disposition, e.Dir)
		if e.TaskID != "" {
			fmt.Printf("    task_id: %s  status: %s\n", e.TaskID, e.Status)
		}
		if e.Detail != "" {
			fmt.Printf("    detail:  %s\n", e.Detail)
		}
	}
	if r.InvalidCount > 0 {
		fmt.Printf("\n%d malformed task directory/directories preserved; this command does not repair them.\n", r.InvalidCount)
	}
}
