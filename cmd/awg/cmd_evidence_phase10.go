// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/investigationsurface"
)

func runPhase10Evidence(args []string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	switch args[0] {
	case "snapshot":
		return runEvidenceSnapshot(args[1:]), true
	case "import":
		return runEvidenceImport(args[1:]), true
	case "coverage":
		return runEvidenceCoverage(args[1:]), true
	default:
		return 0, false
	}
}
func runEvidenceSnapshot(args []string) int {
	fs := flag.NewFlagSet("sensei evidence snapshot", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	source := fs.String("source", "", "file or directory to freeze")
	captured := fs.String("captured-at", "", "explicit RFC3339 capture time")
	out := fs.String("out", "-", "snapshot path or -")
	format := fs.String("format", "json", "json or yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *source == "" || *captured == "" {
		fmt.Fprintln(os.Stderr, "sensei evidence snapshot: --source and --captured-at are required")
		return 2
	}
	snapshot, err := investigationsurface.CaptureEvidence(*source, *captured)
	if err != nil {
		return cliSurfaceError("evidence snapshot", err)
	}
	if err := investigationsurface.WriteArtifact(*out, *format, snapshot); err != nil {
		return cliSurfaceError("evidence snapshot", err)
	}
	return 0
}
func runEvidenceImport(args []string) int {
	fs := flag.NewFlagSet("sensei evidence import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	snapshotPath := fs.String("snapshot", "", "evidence snapshot artifact")
	repo := fs.String("repo", ".", "repository root")
	asJSON := fs.Bool("json", false, "emit receipt JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *snapshotPath == "" {
		fmt.Fprintln(os.Stderr, "sensei evidence import: --snapshot is required")
		return 2
	}
	var snapshot investigationsurface.EvidenceSnapshot
	if err := investigationsurface.ReadArtifact(*snapshotPath, &snapshot); err != nil {
		return cliSurfaceError("evidence import", err)
	}
	receipt, err := investigationsurface.ImportEvidence(snapshot, *repo)
	if err != nil {
		return cliSurfaceError("evidence import", err)
	}
	if *asJSON {
		return emitSurfaceJSON(receipt)
	}
	fmt.Printf("imported snapshot: %s\nfiles: %d\nmanifest: %s\n", receipt.SnapshotDigestSHA256, len(receipt.ImportedPaths), receipt.ManifestPath)
	return 0
}
func runEvidenceCoverage(args []string) int {
	fs := flag.NewFlagSet("sensei evidence coverage", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	artifact := fs.String("artifact", "", "HOW or WHY investigation artifact")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *artifact == "" {
		fmt.Fprintln(os.Stderr, "sensei evidence coverage: --artifact is required")
		return 2
	}
	doc, err := investigationsurface.LoadDocument(*artifact)
	if err != nil {
		return cliSurfaceError("evidence coverage", err)
	}
	report, err := investigationsurface.Coverage(doc)
	if err != nil {
		return cliSurfaceError("evidence coverage", err)
	}
	if *asJSON {
		return emitSurfaceJSON(report)
	}
	fmt.Printf("mode: %s\ndigest: %s\nevidence: %d\n", report.Mode, report.DocumentDigest, report.EvidenceCount)
	fmt.Println("STATUS  CATEGORY  PROVIDER  RESULTS")
	for _, entry := range report.Entries {
		fmt.Printf("%-22s %-22s %-30s %d\n", entry.Status, entry.Category, entry.ProviderID, len(entry.ResultEvidenceIDs))
	}
	return 0
}
