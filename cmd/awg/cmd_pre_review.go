// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/architecture/prereview"
)

// runPreReview generates an architectural pre-review report for a proposed
// change. In this milestone it produces an advisory-coverage report: the diff is
// bound and the applicable governed rules are resolved locally from
// docs/awareness, without a running server. Governed (task-backed) coverage
// arrives in a later milestone.
func runPreReview(args []string) int {
	fs := flag.NewFlagSet("sensei pre-review", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository root")
	base := fs.String("base", "origin/main", "base revision to diff against")
	head := fs.String("head", "HEAD", "head revision")
	taskDir := fs.String("task-dir", "", "governed task dir (governed coverage; later milestone)")
	active := fs.Bool("active", false, "use the active governed task (later milestone)")
	format := fs.String("format", "markdown", "output format: text|markdown|yaml|json")
	output := fs.String("output", "", "write the report to this file instead of stdout")
	sarif := fs.String("sarif", "", "also write SARIF findings for file-anchored concerns to this path")
	maxItems := fs.Int("max-reviewer-items", 0, "max reviewer-attention items in markdown (0 = default)")
	includeNeural := fs.Bool("include-neural", false, "include neural candidates (later milestone)")
	strict := fs.Bool("strict", false, "exit non-zero on an uncollectable graph or a blocking finding")
	purpose := fs.String("purpose", "", "one-line description of the change's intent")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	if *taskDir != "" || *active {
		fmt.Fprintln(os.Stderr, "pre-review: governed (task-backed) coverage is a later milestone; producing an advisory report")
	}
	if *includeNeural {
		fmt.Fprintln(os.Stderr, "pre-review: neural candidates are a later milestone; none included")
	}

	report, err := prereview.GenerateAdvisory(context.Background(), gitDiffSource{}, localGraphSource{repoRoot: *repo}, prereview.GenerateRequest{
		RepoRoot: *repo,
		Base:     *base,
		Head:     *head,
		Purpose:  *purpose,
		Strict:   *strict,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pre-review:", err)
		return 1
	}

	rendered, err := renderPreReview(report, *format, *maxItems)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pre-review:", err)
		return 1
	}
	if *output != "" {
		if err := os.WriteFile(*output, rendered, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "pre-review: write output:", err)
			return 1
		}
	} else {
		os.Stdout.Write(rendered)
	}

	if *sarif != "" {
		data, err := preReviewSARIF(report)
		if err != nil {
			fmt.Fprintln(os.Stderr, "pre-review: sarif:", err)
			return 1
		}
		if err := os.WriteFile(*sarif, data, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "pre-review: write sarif:", err)
			return 1
		}
	}

	if *strict && preReviewHasBlocker(report) {
		return 3
	}
	return 0
}

func renderPreReview(report prereview.PreReviewReport, format string, maxItems int) ([]byte, error) {
	switch format {
	case "json":
		return prereview.RenderJSON(report)
	case "yaml":
		return prereview.RenderYAML(report)
	case "markdown", "text", "":
		return prereview.RenderMarkdown(report, prereview.RenderOptions{MaxReviewerItems: maxItems})
	default:
		return nil, fmt.Errorf("unknown format %q", format)
	}
}

// preReviewHasBlocker reports whether the report's disposition demands action
// before human review can proceed.
func preReviewHasBlocker(r prereview.PreReviewReport) bool {
	switch r.Disposition {
	case prereview.DispositionReadyForHumanReview,
		prereview.DispositionCertified,
		prereview.DispositionTerminallyClosed:
		return false
	default:
		return true
	}
}

// preReviewSARIF renders file-anchored reviewer concerns as SARIF, reusing the
// shared SARIF types. Concerns without a related file are omitted (SARIF is for
// file-level findings).
func preReviewSARIF(r prereview.PreReviewReport) ([]byte, error) {
	rules := make([]sarifRule, 0)
	results := make([]sarifResult, 0)
	seenRule := map[string]bool{}
	for _, a := range r.ReviewerAttention {
		if len(a.RelatedFiles) == 0 {
			continue
		}
		level := sarifLevel(a)
		if !seenRule[a.ID] {
			rules = append(rules, sarifRule{
				ID:                   a.ID,
				Name:                 string(a.Category),
				ShortDescription:     sarifText{Text: a.Question},
				DefaultConfiguration: sarifConfig{Level: level},
			})
			seenRule[a.ID] = true
		}
		locs := make([]sarifLocation, 0, len(a.RelatedFiles))
		for _, f := range a.RelatedFiles {
			locs = append(locs, sarifLocation{PhysicalLocation: sarifPhysical{
				ArtifactLocation: sarifArtifact{URI: f},
				Region:           sarifRegion{StartLine: 1},
			}})
		}
		results = append(results, sarifResult{
			RuleID:    a.ID,
			Level:     level,
			Message:   sarifText{Text: a.Question},
			Locations: locs,
		})
	}
	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "sensei-pre-review", InformationURI: "https://github.com/globulario/sensei", Rules: rules}},
			Results: results,
		}},
	}
	return json.MarshalIndent(log, "", "  ")
}

func sarifLevel(a prereview.ReviewerAttentionItem) string {
	if a.Blocking {
		return "error"
	}
	if a.Severity == prereview.SeverityHigh || a.Severity == prereview.SeverityCritical {
		return "warning"
	}
	return "note"
}
