// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"gopkg.in/yaml.v3"
)

func runTaskLedger(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, "Usage: sensei task-ledger <verify|status|rebuild-projections|import-legacy> [flags]\n")
		return 2
	}
	switch args[0] {
	case "verify":
		return runTaskLedgerVerify(args[1:])
	case "status":
		return runTaskLedgerStatus(args[1:])
	case "rebuild-projections":
		return runTaskLedgerRebuild(args[1:])
	case "import-legacy":
		return runTaskLedgerImportLegacy(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "sensei task-ledger: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runTaskLedgerVerify(args []string) int {
	var repoRoot, taskDir, format string
	var active bool
	fs := flag.NewFlagSet("sensei task-ledger verify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&repoRoot, "repo", ".", "repository checkout")
	fs.StringVar(&taskDir, "task-dir", "", "task directory; defaults to active task")
	fs.BoolVar(&active, "active", false, "resolve .sensei/tasks/active.yaml")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	taskDir, err := resolveTaskLedgerDir(repoRoot, taskDir, active)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger verify: %v\n", err)
		return 1
	}
	report, err := ledger.VerifyTaskLedger(taskDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger verify: %v\n", err)
		return 1
	}
	if err := printTaskLedgerReport(report, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger verify: %v\n", err)
		return 2
	}
	if !report.Valid {
		return 1
	}
	if report.ProjectionState == "projection_drift" {
		return 3
	}
	return 0
}

func runTaskLedgerStatus(args []string) int {
	return runTaskLedgerVerify(args)
}

func runTaskLedgerRebuild(args []string) int {
	var repoRoot, taskDir, format string
	var active bool
	fs := flag.NewFlagSet("sensei task-ledger rebuild-projections", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&repoRoot, "repo", ".", "repository checkout")
	fs.StringVar(&taskDir, "task-dir", "", "task directory; defaults to active task")
	fs.BoolVar(&active, "active", false, "resolve .sensei/tasks/active.yaml")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	taskDir, err := resolveTaskLedgerDir(repoRoot, taskDir, active)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger rebuild-projections: %v\n", err)
		return 1
	}
	set, err := ledger.RebuildProjections(taskDir, func(eventType closureprotocol.LedgerEventType, mediaType string, data []byte) error {
		return ledger.ValidateTaskEventPayload(eventType, data)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger rebuild-projections: %v\n", err)
		return 1
	}
	if err := printProjectionSet(set, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger rebuild-projections: %v\n", err)
		return 2
	}
	return 0
}

func runTaskLedgerImportLegacy(args []string) int {
	var taskDir, format, producerID, producedAt string
	fs := flag.NewFlagSet("sensei task-ledger import-legacy", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&taskDir, "task-dir", "", "legacy task directory")
	fs.StringVar(&producerID, "producer-id", "sensei task-ledger import-legacy", "producer identifier")
	fs.StringVar(&producedAt, "produced-at", "", "RFC3339 timestamp")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if strings.TrimSpace(taskDir) == "" {
		fmt.Fprint(os.Stderr, "sensei task-ledger import-legacy: --task-dir is required\n")
		return 2
	}
	opts := ledger.ImportOptions{ProducerID: producerID}
	if strings.TrimSpace(producedAt) != "" {
		ts, err := time.Parse(time.RFC3339, producedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei task-ledger import-legacy: invalid --produced-at: %v\n", err)
			return 2
		}
		opts.ProducedAt = ts
	}
	res, err := ledger.ImportLegacyTask(taskDir, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger import-legacy: %v\n", err)
		return 1
	}
	if err := printValue(res, format); err != nil {
		fmt.Fprintf(os.Stderr, "sensei task-ledger import-legacy: %v\n", err)
		return 2
	}
	return 0
}

func resolveTaskLedgerDir(repoRoot, taskDir string, active bool) (string, error) {
	if strings.TrimSpace(taskDir) != "" {
		return filepath.Abs(taskDir)
	}
	if !active {
		return "", fmt.Errorf("task directory is required unless --active is set")
	}
	ptr, err := tasksession.LoadActivePointer(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Abs(filepath.Join(repoRoot, filepath.Dir(filepath.FromSlash(ptr.SessionPath))))
}

func printTaskLedgerReport(report ledger.VerificationReport, format string) error {
	return printValue(report, format)
}

func printProjectionSet(set ledger.ProjectionSet, format string) error {
	paths := make([]string, 0, len(set.Files))
	for path := range set.Files {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return printValue(struct {
		Files []string `json:"files" yaml:"files"`
	}{Files: paths}, format)
}

func printValue(v any, format string) error {
	switch strings.TrimSpace(format) {
	case "", "text":
		data, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "yaml":
		data, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "json":
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func sortStrings(values []string) {
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			if values[j] < values[i] {
				values[i], values[j] = values[j], values[i]
			}
		}
	}
}
