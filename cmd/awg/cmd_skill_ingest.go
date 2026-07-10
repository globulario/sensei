// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/awareness-graph/golang/skillimport"
)

func runSkillIngest(args []string) int {
	rootArg := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		rootArg = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("awg skill-ingest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out", filepath.Join("docs", "awareness", "candidates", "skills"), "directory to write generated candidate YAML")
	repo := fs.String("repo", "", "repository domain for provenance reporting")
	sourceSet := fs.String("source-set", "external/skills", "source set label for provenance reporting")
	includeDeprecated := fs.Bool("include-deprecated", false, "include skills/deprecated")
	dryRun := fs.Bool("dry-run", false, "validate and report without writing files")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg skill-ingest <skill-pack-root> [flags]

Generate review-only AWG ImplementationPattern candidates from external SKILL.md files.
Generated candidates are written under docs/awareness/candidates/skills by default and are not active graph authority.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if rootArg == "" && fs.NArg() == 1 {
		rootArg = fs.Arg(0)
	}
	if rootArg == "" || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "awg skill-ingest: exactly one <skill-pack-root> is required")
		fs.Usage()
		return 2
	}

	root := rootArg
	discovered, err := skillimport.DiscoverSkillsWithReport(root, *includeDeprecated)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg skill-ingest: discover: %v\n", err)
		return 1
	}
	if len(discovered.Skills) == 0 {
		fmt.Fprintf(os.Stderr, "awg skill-ingest: zero valid skills found under %s\n", root)
		if len(discovered.Invalid) > 0 {
			printInvalidSkills(discovered.Invalid)
		}
		return 1
	}

	opts := skillimport.ImportOptions{
		InputRoot:         root,
		OutputDir:         *outDir,
		Repo:              *repo,
		SourceSet:         *sourceSet,
		DefaultStatus:     "candidate",
		DefaultConfidence: "medium",
		IncludeDeprecated: *includeDeprecated,
	}
	candidates := skillimport.ExtractCandidates(discovered.Skills, opts)

	if *dryRun {
		wouldWrite := 0
		for _, candidate := range candidates {
			data, err := skillimport.RenderCandidate(candidate)
			if err != nil {
				fmt.Fprintf(os.Stderr, "awg skill-ingest: render %s: %v\n", candidate.ID, err)
				return 1
			}
			if err := skillimport.ValidateCandidateYAML(data); err != nil {
				fmt.Fprintf(os.Stderr, "awg skill-ingest: validate %s: %v\n", candidate.ID, err)
				return 1
			}
			path := filepath.Join(*outDir, skillimport.CandidateFileName(candidate))
			fmt.Printf("[dry-run] would write %s\n", path)
			wouldWrite++
		}
		printSkillIngestSummary(len(discovered.Skills), wouldWrite, len(discovered.Skipped), len(discovered.Invalid), *outDir)
		printInvalidSkills(discovered.Invalid)
		fmt.Println()
		fmt.Println("Candidates are review-only. Run awg promote after human review.")
		return 0
	}

	written, err := skillimport.WriteCandidates(candidates, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg skill-ingest: %v\n", err)
		return 1
	}

	printSkillIngestSummary(len(discovered.Skills), len(written.Paths), len(discovered.Skipped), len(discovered.Invalid), *outDir)
	for _, path := range written.Paths {
		fmt.Printf("wrote %s\n", path)
	}
	printInvalidSkills(discovered.Invalid)
	fmt.Println()
	fmt.Println("Candidates are review-only. Run awg promote after human review.")
	return 0
}

func printSkillIngestSummary(discovered, imported, skipped, invalid int, outDir string) {
	fmt.Printf("skill-ingest: discovered=%d imported=%d skipped=%d invalid=%d\n", discovered, imported, skipped, invalid)
	fmt.Printf("output directory: %s\n", outDir)
}

func printInvalidSkills(invalid []skillimport.FileIssue) {
	for _, issue := range invalid {
		fmt.Fprintf(os.Stderr, "invalid %s: %v\n", issue.Path, issue.Err)
	}
}
