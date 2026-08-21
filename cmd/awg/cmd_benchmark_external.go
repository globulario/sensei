// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/benchmark"
)

func runBenchmarkFreezeExternal(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-freeze", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var task, sourceRepo, oracle, outputDir, format, senseiRepo string
	var check bool
	fs.StringVar(&task, "task", "", "benchmark task manifest")
	fs.StringVar(&sourceRepo, "source-repo", "", "local git repository")
	fs.StringVar(&senseiRepo, "sensei-repo", ".", "Sensei checkout whose authority governs this run")
	fs.StringVar(&oracle, "oracle", "", "sealed oracle manifest")
	fs.StringVar(&outputDir, "output-dir", "", "frozen workspace output directory")
	fs.StringVar(&format, "format", "yaml", "output format: yaml|json")
	fs.BoolVar(&check, "check", false, "compare with existing workspace and write nothing")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-freeze --task <task.yaml> --source-repo <repo> --oracle <sealed-oracle.yaml> --output-dir <workspace>

Freezes a blind local benchmark workspace. No network access, source mutation,
oracle reveal, active graph import, or reconstruction is performed.

The receipt also binds the Sensei authority that governs the run (revision,
seed digest, authored-corpus digest) so a later replay can prove it ran under
the same authority. Verify that with:

  sensei benchmark-status --workspace <workspace> --sensei-repo <checkout>
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if task == "" || sourceRepo == "" || oracle == "" || outputDir == "" {
		fmt.Fprintln(os.Stderr, "sensei benchmark-freeze: --task, --source-repo, --oracle, and --output-dir are required")
		return 2
	}
	receipt, contamination, err := benchmark.Freeze(benchmark.FreezeOptions{TaskPath: task, SourceRepo: sourceRepo, OraclePath: oracle, OutputDir: outputDir, SenseiRepo: senseiRepo, Check: check})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-freeze: %v\n", err)
		return 1
	}
	return printBenchmarkFreeze(receipt, contamination, format)
}

func runBenchmarkReconstruct(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-reconstruct", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var workspace, questionCreatedAt, format string
	var check, requireNoFalseGreenInput bool
	fs.StringVar(&workspace, "workspace", "", "frozen benchmark workspace")
	fs.StringVar(&questionCreatedAt, "question-created-at", "", "RFC3339 timestamp for generated questions")
	fs.StringVar(&format, "format", "yaml", "output format: yaml|json")
	fs.BoolVar(&check, "check", false, "compare reconstruction receipt and write nothing")
	fs.BoolVar(&requireNoFalseGreenInput, "require-no-false-green-input", false, "reserved strict guard; reconstruction still fails on contamination")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-reconstruct --workspace <workspace> --question-created-at <RFC3339>

Writes deterministic reconstruction receipts for a frozen blind workspace.
No agent, test, network, or active graph operation is performed.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_ = requireNoFalseGreenInput
	if workspace == "" || questionCreatedAt == "" {
		fmt.Fprintln(os.Stderr, "sensei benchmark-reconstruct: --workspace and --question-created-at are required")
		return 2
	}
	receipt, err := benchmark.Reconstruct(workspace, questionCreatedAt, check)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-reconstruct: %v\n", err)
		return 1
	}
	return printBenchmarkObject(receipt, format)
}

func runBenchmarkEvaluateExternal(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-evaluate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var workspace, oracle, review, mapping, ledger, output, format string
	var check, requireDemonstrated bool
	fs.StringVar(&workspace, "workspace", "", "frozen benchmark workspace")
	fs.StringVar(&oracle, "oracle", "", "sealed oracle manifest")
	fs.StringVar(&review, "question-review", "", "question review YAML")
	fs.StringVar(&mapping, "oracle-mapping", "", "oracle mapping YAML")
	fs.StringVar(&ledger, "intervention-ledger", "", "optional intervention ledger YAML")
	fs.StringVar(&output, "output", "", "optional task report output path")
	fs.StringVar(&format, "format", "yaml", "output format: yaml|json")
	fs.BoolVar(&check, "check", false, "compare output report and write nothing")
	fs.BoolVar(&requireDemonstrated, "require-demonstrated", false, "exit non-zero unless outcome is demonstrated")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-evaluate --workspace <workspace> --oracle <sealed-oracle.yaml> --question-review <review.yaml> --oracle-mapping <mapping.yaml>

Reveals oracle receipts and writes a categorical benchmark report. The oracle is
comparative evidence, not automatic correctness authority.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if workspace == "" || oracle == "" || review == "" || mapping == "" {
		fmt.Fprintln(os.Stderr, "sensei benchmark-evaluate: --workspace, --oracle, --question-review, and --oracle-mapping are required")
		return 2
	}
	report, err := benchmark.Evaluate(benchmark.EvaluateOptions{Workspace: workspace, OraclePath: oracle, QuestionReviewPath: review, OracleMappingPath: mapping, InterventionLedgerPath: ledger, OutputPath: output, Check: check})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-evaluate: %v\n", err)
		return 1
	}
	code := printBenchmarkObject(report, format)
	if code != 0 {
		return code
	}
	if requireDemonstrated && report.Outcome != benchmark.OutcomeDemonstrated {
		return 1
	}
	return 0
}

func runBenchmarkStatusExternal(args []string) int {
	fs := flag.NewFlagSet("sensei benchmark-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var workspace, report, format, senseiRepo string
	fs.StringVar(&workspace, "workspace", "", "frozen benchmark workspace")
	fs.StringVar(&report, "report", "", "optional task report YAML")
	// Defaults to the current checkout, matching benchmark-freeze. The gate is
	// opt-OUT (--sensei-repo="") rather than opt-in: the canonical workflow in
	// the bundled sensei-benchmark skill invokes benchmark-status without this
	// flag, so an opt-in default left the machine-consumer hole open on exactly
	// the path evaluators are told to follow.
	fs.StringVar(&senseiRepo, "sensei-repo", ".", "Sensei checkout to verify the frozen authority against (\"\" disables the replay gate)")
	fs.StringVar(&format, "format", "text", "output format: text|yaml|json")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei benchmark-status --workspace <workspace> [--report <task-report.yaml>]

Prints compact benchmark state. No score is computed.

Also compares the authority frozen with the run against the authority observed
now and reports whether a replay is comparable, exiting 3 when it is not.
Defaults to the current checkout; pass --sensei-repo="" to disable that gate.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if workspace == "" {
		fmt.Fprintln(os.Stderr, "sensei benchmark-status: --workspace is required")
		return 2
	}
	// The replay gate is computed BEFORE the output format is chosen, and its
	// verdict gates every format identically. It previously guarded only the
	// text branch, which inverted the point: --format json is precisely what a
	// harness consumes, so the one caller the fail-closed exit exists to stop
	// was the one caller that never saw it.
	var drift benchmark.AuthorityDrift
	gated := senseiRepo != ""
	if gated {
		var err error
		drift, err = benchmark.VerifyWorkspaceAuthority(workspace, senseiRepo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei benchmark-status: %v\n", err)
			return 1
		}
	}
	// A replay that cannot be certified comparable is not a benchmark result.
	// Exit non-zero so a harness cannot scrape the numbers and silently treat
	// them as the original run's.
	gateExit := func(code int) int {
		if gated && !drift.Comparable {
			return 3
		}
		return code
	}

	if format == "text" {
		text, err := benchmark.Status(workspace, report)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei benchmark-status: %v\n", err)
			return 1
		}
		fmt.Print(text)
		if gated {
			fmt.Printf("Replay authority: %s\n", drift.Verdict)
			fmt.Printf("Replay comparable: %s\n", yesNoText(drift.Comparable))
			for _, r := range drift.Reasons {
				if r.Detail != "" {
					fmt.Printf("  - %s: %s\n", r.Code, r.Detail)
					continue
				}
				fmt.Printf("  - %s\n", r.Code)
			}
		}
		return gateExit(0)
	}
	freeze, contamination, err := benchmark.LoadWorkspaceFreeze(workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei benchmark-status: %v\n", err)
		return 1
	}
	out := map[string]interface{}{"freeze": freeze, "contamination": contamination}
	if gated {
		out["replay_authority"] = drift
	}
	if code := printBenchmarkObject(out, format); code != 0 {
		return code
	}
	return gateExit(0)
}

func printBenchmarkFreeze(receipt benchmark.FreezeReceipt, contamination benchmark.ContaminationReport, format string) int {
	return printBenchmarkObject(map[string]interface{}{"freeze": receipt, "contamination": contamination}, format)
}

func printBenchmarkObject(v interface{}, format string) int {
	switch format {
	case "yaml":
		// JSON-shaped output is acceptable YAML and stays deterministic because
		// encoding/json sorts map keys.
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark: render: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
		return 0
	case "json":
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "benchmark: render: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
		return 0
	default:
		fmt.Fprintf(os.Stderr, "benchmark: unsupported format %q\n", format)
		return 2
	}
}

// yesNoText renders a replay-comparability decision. It is spelled out rather
// than printed as a bare bool so a reader scanning status output cannot mistake
// "false" for a missing field.
func yesNoText(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
