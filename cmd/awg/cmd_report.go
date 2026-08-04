// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/senseireport"
)

// runReport implements `sensei report` (docs/design/sensei-md-report.md):
// generate SENSEI.md and SENSEI.report.json from this repository's real,
// on-disk Sensei state, or (--check) verify the committed pair still
// matches a fresh rebuild without writing anything.
//
// SENSEI.report.json deliberately lives at the repo root next to SENSEI.md,
// NOT under .sensei/report.json as docs/design/sensei-md-report.md
// originally proposed: .sensei/ is fully gitignored in this repository (and
// in practice accumulates tens of GB of local task-governance state under
// .sensei/tasks and .sensei/project), so a file that must be committed and
// versioned can never live there without a fragile, repo-hygiene-risking
// .gitignore carve-out. A root-level sibling to SENSEI.md is trivially
// committable and keeps .gitignore untouched.
func runReport(args []string) int {
	fs := flag.NewFlagSet("sensei report", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repoFlag := fs.String("repo", "", "repository root (default: auto-detect from cwd)")
	checkOnly := fs.Bool("check", false, "verify only; write nothing, exit 1 if missing, stale, schema-obsolete, or hand-modified")
	stdoutOnly := fs.Bool("stdout", false, "render Markdown to stdout only; write nothing")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei report [flags]

Generate SENSEI.md and SENSEI.report.json: a human-readable and a
machine-readable summary of what Sensei currently knows about this
repository's architecture and active governed task.

Reproduction is always exactly:
    sensei report
    sensei report --check

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	repoRoot, err := resolveProjectRoot(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei report: %v\n", err)
		return 1
	}

	report, err := senseireport.Build(repoRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei report: %v\n", err)
		return 1
	}
	if errs := senseireport.Validate(report); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "sensei report: %s\n", e.Error())
		}
		return 1
	}

	mdPath := filepath.Join(repoRoot, "SENSEI.md")
	jsonPath := filepath.Join(repoRoot, "SENSEI.report.json")

	if *checkOnly {
		return checkReport(report, mdPath, jsonPath)
	}

	markdown := senseireport.RenderMarkdown(report)

	if *stdoutOnly {
		os.Stdout.Write(markdown)
		return 0
	}

	jsonData, err := marshalReportJSON(report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei report: encode: %v\n", err)
		return 1
	}

	if err := os.WriteFile(mdPath, markdown, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei report: write %s: %v\n", mdPath, err)
		return 1
	}
	if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei report: write %s: %v\n", jsonPath, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "sensei report: wrote %s and %s\n", mdPath, jsonPath)
	return 0
}

// marshalReportJSON is the one JSON encoding used both when writing
// SENSEI.report.json and when --check re-derives it for comparison, so the
// two paths can never silently drift into different formatting.
func marshalReportJSON(r senseireport.Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// checkReport verifies a committed SENSEI.md + SENSEI.report.json pair
// against a fresh rebuild, writing nothing.
//
// fresh.Identity.EvaluatedCommit/EvaluatedCommitStatus are deliberately
// overwritten with the on-disk report's own values before rendering and
// byte-comparing (see Identity's doc comment in types.go: evaluated_commit
// is informational provenance ONLY, never freshness-authoritative).
// Without this, --check would fail on every single invocation immediately
// following the commit that records the report: committing SENSEI.md/
// SENSEI.report.json necessarily advances HEAD past the commit the report
// itself recorded, so a raw byte-for-byte comparison would spuriously see
// "evaluated_commit changed" as a content difference forever. Real
// freshness is decided entirely by the content-digest comparison below;
// the rest of this function only detects genuine drift -- hand edits, a
// stale disposition, changed findings/candidates -- elsewhere in the
// document.
func checkReport(report senseireport.Report, mdPath, jsonPath string) int {
	diskJSON, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: %s does not exist (run `sensei report`)\n", jsonPath)
			return 1
		}
		fmt.Fprintf(os.Stderr, "sensei report --check: %v\n", err)
		return 1
	}
	diskMarkdown, err := os.ReadFile(mdPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: %s does not exist (run `sensei report`)\n", mdPath)
			return 1
		}
		fmt.Fprintf(os.Stderr, "sensei report --check: %v\n", err)
		return 1
	}

	var onDisk senseireport.Report
	if err := json.Unmarshal(diskJSON, &onDisk); err != nil {
		fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: %s is not valid JSON: %v\n", jsonPath, err)
		return 1
	}
	if onDisk.SchemaVersion != senseireport.SchemaVersion {
		fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: %s has schema_version %q, this binary produces %q (schema-obsolete; run `sensei report`)\n",
			jsonPath, onDisk.SchemaVersion, senseireport.SchemaVersion)
		return 1
	}

	if report.Identity.EvaluatedContentDigestSHA256 != onDisk.Identity.EvaluatedContentDigestSHA256 {
		fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: STALE — repository content digest changed since %s was generated (run `sensei report`)\n", jsonPath)
		return 1
	}

	fresh := report
	fresh.Identity.EvaluatedCommit = onDisk.Identity.EvaluatedCommit
	fresh.Identity.EvaluatedCommitStatus = onDisk.Identity.EvaluatedCommitStatus

	freshJSON, err := marshalReportJSON(fresh)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei report --check: internal error re-encoding freshly built report: %v\n", err)
		return 1
	}
	freshMarkdown := senseireport.RenderMarkdown(fresh)

	if !bytes.Equal(diskJSON, freshJSON) {
		fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: %s does not match a fresh rebuild (hand-edited, or generated by a different version; run `sensei report`)\n", jsonPath)
		return 1
	}
	if !bytes.Equal(diskMarkdown, freshMarkdown) {
		fmt.Fprintf(os.Stderr, "sensei report --check: FAIL: %s does not match a fresh rebuild (hand-edited, or generated by a different version; run `sensei report`)\n", mdPath)
		return 1
	}

	fmt.Fprintln(os.Stderr, "sensei report --check: OK")
	return 0
}
