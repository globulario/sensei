// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/investigationsurface"
)

type candidateReviewReceipt struct {
	SchemaVersion       string `json:"schema_version" yaml:"schema_version"`
	ReviewedBy          string `json:"reviewed_by" yaml:"reviewed_by"`
	ReviewedAt          string `json:"reviewed_at" yaml:"reviewed_at"`
	ResultDigest        string `json:"result_digest_sha256" yaml:"result_digest_sha256"`
	CandidateID         string `json:"candidate_id" yaml:"candidate_id"`
	Disposition         string `json:"disposition" yaml:"disposition"`
	Rationale           string `json:"rationale" yaml:"rationale"`
	PromotionAuthorized bool   `json:"promotion_authorized" yaml:"promotion_authorized"`
}

func runCandidates(args []string) int {
	if len(args) == 0 {
		printCandidatesUsage()
		return 2
	}
	switch args[0] {
	case "list":
		return runCandidatesList(args[1:])
	case "show":
		return runCandidatesShow(args[1:])
	case "review":
		return runCandidatesReview(args[1:])
	case "help", "-h", "--help":
		printCandidatesUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "sensei candidates: unknown subcommand %q\n", args[0])
		printCandidatesUsage()
		return 2
	}
}
func printCandidatesUsage() {
	fmt.Fprint(os.Stderr, `Usage: sensei candidates <list|show|review> [flags]

Candidate review records a governed-review input artifact only. It never promotes
or mutates canonical awareness; accepted promotion still uses awareness_propose
and the existing authored-source path.
`)
}
func loadCandidateResult(path string) (investigationsurface.CandidateReport, error) {
	r, err := investigationsurface.LoadResult(path)
	if err != nil {
		return investigationsurface.CandidateReport{}, err
	}
	return investigationsurface.Candidates(r)
}
func runCandidatesList(args []string) int {
	fs := flag.NewFlagSet("sensei candidates list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("result", "", "investigator result artifact")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprintln(os.Stderr, "sensei candidates list: --result is required")
		return 2
	}
	report, err := loadCandidateResult(*path)
	if err != nil {
		return cliSurfaceError("candidates list", err)
	}
	if *asJSON {
		return emitSurfaceJSON(report)
	}
	fmt.Printf("RESULT %s\n", report.ResultDigest)
	fmt.Println("RANK  KIND  STATUS  CANDIDATE  CLAIM")
	for _, v := range report.Candidates {
		rank := 0
		if v.Ranking != nil {
			rank = v.Ranking.Rank
		}
		status := "unchallenged"
		if v.Challenge != nil {
			status = string(v.Challenge.Status)
		}
		fmt.Printf("%-5d %-14s %-22s %s  %s\n", rank, v.Candidate.OutputKind, status, v.Candidate.CandidateID, v.Claim.ID)
	}
	fmt.Printf("\n%d candidate(s)\n", len(report.Candidates))
	return 0
}
func runCandidatesShow(args []string) int {
	fs := flag.NewFlagSet("sensei candidates show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("result", "", "investigator result artifact")
	id := fs.String("id", "", "candidate id")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || *id == "" {
		fmt.Fprintln(os.Stderr, "sensei candidates show: --result and --id are required")
		return 2
	}
	r, err := investigationsurface.LoadResult(*path)
	if err != nil {
		return cliSurfaceError("candidates show", err)
	}
	view, digest, err := investigationsurface.FindCandidate(r, *id)
	if err != nil {
		return cliSurfaceError("candidates show", err)
	}
	if *asJSON {
		return emitSurfaceJSON(map[string]any{"schema_version": "investigation.surface.candidate.v1", "result_digest_sha256": digest, "candidate": view})
	}
	data, _ := json.MarshalIndent(view, "", "  ")
	fmt.Printf("result_digest: %s\n%s\n", digest, data)
	return 0
}
func runCandidatesReview(args []string) int {
	fs := flag.NewFlagSet("sensei candidates review", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("result", "", "investigator result artifact")
	id := fs.String("id", "", "candidate id")
	disposition := fs.String("disposition", "", "defer | needs_evidence | reject | accept_for_governed_proposal")
	reviewer := fs.String("reviewer", "", "reviewer identity")
	reviewedAt := fs.String("reviewed-at", "", "explicit RFC3339 review time")
	rationale := fs.String("rationale", "", "review rationale")
	out := fs.String("out", "-", "review receipt path or -")
	format := fs.String("format", "json", "json or yaml")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *path == "" || *id == "" || *disposition == "" || *reviewer == "" || *reviewedAt == "" || *rationale == "" {
		fmt.Fprintln(os.Stderr, "sensei candidates review: result, id, disposition, reviewer, reviewed-at, and rationale are required")
		return 2
	}
	allowed := []string{"accept_for_governed_proposal", "defer", "needs_evidence", "reject"}
	sort.Strings(allowed)
	ok := false
	for _, v := range allowed {
		if *disposition == v {
			ok = true
		}
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "sensei candidates review: disposition must be one of %s\n", strings.Join(allowed, ", "))
		return 2
	}
	if _, err := time.Parse(time.RFC3339, *reviewedAt); err != nil {
		return cliSurfaceError("candidates review", err)
	}
	r, err := investigationsurface.LoadResult(*path)
	if err != nil {
		return cliSurfaceError("candidates review", err)
	}
	_, digest, err := investigationsurface.FindCandidate(r, *id)
	if err != nil {
		return cliSurfaceError("candidates review", err)
	}
	receipt := candidateReviewReceipt{SchemaVersion: "investigation.candidate-review.v1", ReviewedBy: strings.TrimSpace(*reviewer), ReviewedAt: strings.TrimSpace(*reviewedAt), ResultDigest: digest, CandidateID: strings.TrimSpace(*id), Disposition: *disposition, Rationale: strings.TrimSpace(*rationale), PromotionAuthorized: false}
	if err := investigationsurface.WriteArtifact(*out, *format, receipt); err != nil {
		return cliSurfaceError("candidates review", err)
	}
	return 0
}
