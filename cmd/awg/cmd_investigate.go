// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/factextract"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"github.com/globulario/sensei/golang/architecture/investigationsurface"
	"github.com/globulario/sensei/golang/architecture/investigator"
)

type repeatedString []string

func (s *repeatedString) String() string { return strings.Join(*s, ",") }
func (s *repeatedString) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*s = append(*s, p)
		}
	}
	return nil
}

func runInvestigate(args []string) int {
	if len(args) == 0 {
		printInvestigateUsage()
		return 2
	}
	switch args[0] {
	case "how":
		return runInvestigateHow(args[1:])
	case "why":
		return runInvestigateWhy(args[1:])
	case "architecture":
		return runInvestigateArchitecture(args[1:])
	case "blast-radius":
		return runInvestigateBlastRadius(args[1:])
	case "challenge":
		return runInvestigateChallenge(args[1:])
	case "validate":
		return runInvestigateValidate(args[1:])
	case "help", "-h", "--help":
		printInvestigateUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "sensei investigate: unknown subcommand %q\n", args[0])
		printInvestigateUsage()
		return 2
	}
}

func printInvestigateUsage() {
	fmt.Fprint(os.Stderr, `Usage: sensei investigate <subcommand> [flags]

Subcommands:
  how           extract a receipt-bound deterministic HOW document
  why           investigate declared WHY providers against an exact HOW artifact
  architecture  compose advisory candidates and challenge receipts
  blast-radius  show the grounded scope of one candidate
  challenge     show challenge, counterexamples, and evidence requests
  validate      validate a Phase 10 artifact and print its exact digest
`)
}

func runInvestigateHow(args []string) int {
	fs := flag.NewFlagSet("sensei investigate how", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("repo", ".", "repository root")
	domain := fs.String("domain", "", "repository domain (default: derive from repository)")
	revision := fs.String("revision", "", "exact revision (default: git HEAD)")
	revisionStatus := fs.String("revision-status", "", "revision status (default: resolved when revision exists)")
	tree := fs.String("tree-digest", "", "exact repository tree SHA-256 for non-revision worlds")
	graph := fs.String("graph-digest", "", "exact graph SHA-256 when available")
	graphStatus := fs.String("graph-status", "", "graph status (default: resolved when digest exists, otherwise not_requested)")
	captured := fs.String("captured-at", "", "explicit RFC3339 capture time")
	out := fs.String("out", "-", "output artifact path or -")
	format := fs.String("format", "json", "json or yaml")
	summary := fs.Bool("summary", false, "print a compact human summary instead of the full artifact")
	var limits repeatedString
	fs.Var(&limits, "resource-limit", "resource limit key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*captured) == "" {
		fmt.Fprintln(os.Stderr, "sensei investigate how: --captured-at is required")
		return 2
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return cliSurfaceError("investigate how", err)
	}
	binding := resolveSurfaceBinding(abs, *domain, *revision, *revisionStatus, *tree, *graph, *graphStatus)
	doc, err := investigationsurface.RunHow(investigationsurface.HowRequest{Root: abs, CapturedAt: *captured, Repository: binding, ResourceLimits: parseKeyValues(limits)})
	if err != nil {
		return cliSurfaceError("investigate how", err)
	}
	if *summary {
		return printDocumentSummary(doc)
	}
	if err := investigationsurface.WriteArtifact(*out, *format, doc); err != nil {
		return cliSurfaceError("investigate how", err)
	}
	return 0
}

func runInvestigateWhy(args []string) int {
	fs := flag.NewFlagSet("sensei investigate why", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("repo", ".", "repository root")
	howPath := fs.String("how", "", "exact HOW JSON/YAML artifact")
	captured := fs.String("captured-at", "", "explicit RFC3339 capture time")
	queryID := fs.String("query-id", "phase10.why", "stable query id")
	start := fs.String("history-start", "", "explicit history range start")
	end := fs.String("history-end", "", "explicit history range end")
	out := fs.String("out", "-", "output artifact path or -")
	format := fs.String("format", "json", "json or yaml")
	summary := fs.Bool("summary", false, "print a compact human summary")
	var observations, evidence, providers repeatedString
	fs.Var(&observations, "observation", "target HOW observation id (repeatable)")
	fs.Var(&evidence, "evidence-id", "target HOW evidence id (repeatable)")
	fs.Var(&providers, "provider", "provider id (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *howPath == "" || *captured == "" || *start == "" || *end == "" {
		fmt.Fprintln(os.Stderr, "sensei investigate why: --how, --captured-at, --history-start, and --history-end are required")
		return 2
	}
	how, err := investigationsurface.LoadDocument(*howPath)
	if err != nil {
		return cliSurfaceError("investigate why", err)
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		return cliSurfaceError("investigate why", err)
	}
	doc, err := investigationsurface.RunWhy(context.Background(), investigationsurface.WhyRequest{Root: abs, CapturedAt: *captured, How: how, QueryID: *queryID, ObservationIDs: observations, EvidenceIDs: evidence, HistoryStart: *start, HistoryEnd: *end, ProviderIDs: providers})
	if err != nil {
		return cliSurfaceError("investigate why", err)
	}
	if *summary {
		return printDocumentSummary(doc)
	}
	if err := investigationsurface.WriteArtifact(*out, *format, doc); err != nil {
		return cliSurfaceError("investigate why", err)
	}
	return 0
}

func runInvestigateArchitecture(args []string) int {
	fs := flag.NewFlagSet("sensei investigate architecture", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	howPath := fs.String("how", "", "HOW artifact")
	whyPath := fs.String("why", "", "WHY artifact")
	groundingPath := fs.String("grounding", "", "optional grounding snapshot; default derives from HOW and WHY")
	graph := fs.String("graph-digest", "", "exact graph SHA-256")
	claims := fs.String("claims-digest", "", "exact current claims SHA-256")
	closure := fs.String("closure-digest", "", "exact closure-state SHA-256")
	questions := fs.String("questions-digest", "", "exact existing-questions SHA-256")
	review := fs.String("review-digest", "", "exact review-history SHA-256")
	timestamp := fs.String("timestamp-source", "", "explicit RFC3339 deterministic timestamp source")
	out := fs.String("out", "-", "output artifact path or -")
	format := fs.String("format", "json", "json or yaml")
	summary := fs.Bool("summary", false, "print compact human summary")
	var limits repeatedString
	fs.Var(&limits, "resource-limit", "resource limit key=value (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *howPath == "" || *whyPath == "" || *graph == "" || *claims == "" || *closure == "" || *questions == "" || *review == "" || *timestamp == "" {
		fmt.Fprintln(os.Stderr, "sensei investigate architecture: HOW, WHY, five exact digests, and --timestamp-source are required")
		return 2
	}
	how, err := investigationsurface.LoadDocument(*howPath)
	if err != nil {
		return cliSurfaceError("investigate architecture", err)
	}
	why, err := investigationsurface.LoadDocument(*whyPath)
	if err != nil {
		return cliSurfaceError("investigate architecture", err)
	}
	grounding := investigationsurface.GroundingFromDocuments(how, why)
	if *groundingPath != "" {
		grounding, err = investigationsurface.LoadGrounding(*groundingPath)
		if err != nil {
			return cliSurfaceError("investigate architecture", err)
		}
	}
	limitsMap := parseKeyValues(limits)
	if len(limitsMap) == 0 {
		limitsMap = map[string]string{"surface": "bounded"}
	}
	result, err := investigationsurface.RunArchitecture(investigationsurface.ArchitectureRequest{How: how, Why: why, Grounding: grounding, Digests: investigator.InputDigests{GraphDigestSHA256: *graph, CurrentClaimsDigestSHA256: *claims, ClosureStateDigestSHA256: *closure, ExistingQuestionsDigestSHA256: *questions, ReviewHistoryDigestSHA256: *review}, Options: investigator.ComposeOptions{TimestampSource: *timestamp, ResourceLimits: limitsMap}})
	if err != nil {
		return cliSurfaceError("investigate architecture", err)
	}
	if *summary {
		report, err := investigationsurface.Candidates(result)
		if err != nil {
			return cliSurfaceError("investigate architecture", err)
		}
		fmt.Printf("architecture investigation: %d candidate(s), %d challenge(s), %d evidence request(s)\nresult_digest: %s\n", len(report.Candidates), len(result.Challenges), len(result.EvidenceRequests), report.ResultDigest)
		return 0
	}
	if err := investigationsurface.WriteArtifact(*out, *format, result); err != nil {
		return cliSurfaceError("investigate architecture", err)
	}
	return 0
}

func runInvestigateBlastRadius(args []string) int { return runResultView(args, "blast-radius") }
func runInvestigateChallenge(args []string) int   { return runResultView(args, "challenge") }
func runResultView(args []string, mode string) int {
	fs := flag.NewFlagSet("sensei investigate "+mode, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	resultPath := fs.String("result", "", "investigator result artifact")
	candidate := fs.String("candidate", "", "candidate id")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *resultPath == "" || *candidate == "" {
		fmt.Fprintf(os.Stderr, "sensei investigate %s: --result and --candidate are required\n", mode)
		return 2
	}
	result, err := investigationsurface.LoadResult(*resultPath)
	if err != nil {
		return cliSurfaceError("investigate "+mode, err)
	}
	var value any
	if mode == "blast-radius" {
		value, err = investigationsurface.BlastRadius(result, *candidate)
	} else {
		value, err = investigationsurface.Challenge(result, *candidate)
	}
	if err != nil {
		return cliSurfaceError("investigate "+mode, err)
	}
	if *asJSON {
		return emitSurfaceJSON(value)
	}
	data, _ := json.MarshalIndent(value, "", "  ")
	fmt.Println(string(data))
	return 0
}

func runInvestigateValidate(args []string) int {
	fs := flag.NewFlagSet("sensei investigate validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	artifact := fs.String("artifact", "", "artifact path")
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *artifact == "" {
		fmt.Fprintln(os.Stderr, "sensei investigate validate: --artifact is required")
		return 2
	}
	report := investigationsurface.ValidateArtifact(*artifact)
	if *asJSON {
		return emitSurfaceJSON(report)
	}
	fmt.Printf("artifact: %s\nkind: %s\nvalid: %t\n", report.Path, report.ArtifactKind, report.Valid)
	if report.DigestSHA256 != "" {
		fmt.Printf("digest: %s\n", report.DigestSHA256)
	}
	if report.Error != "" {
		fmt.Printf("error: %s\n", report.Error)
	}
	if !report.Valid {
		return 1
	}
	return 0
}

func resolveSurfaceBinding(root, domain, revision, revisionStatus, tree, graph, graphStatus string) architecture.ClaimDocumentBinding {
	if strings.TrimSpace(domain) == "" {
		domain = factextract.ResolveRepositoryIdentity(root).Domain
	}
	if strings.TrimSpace(revision) == "" {
		if out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output(); err == nil {
			revision = strings.TrimSpace(string(out))
		}
	}
	if revisionStatus == "" {
		if revision != "" {
			revisionStatus = architecture.RevisionResolved
		} else {
			revisionStatus = architecture.RevisionNotGit
		}
	}
	if graphStatus == "" {
		if graph != "" {
			graphStatus = architecture.GraphDigestResolved
		} else {
			graphStatus = architecture.GraphDigestNotRequested
		}
	}
	return architecture.ClaimDocumentBinding{RepositoryDomain: strings.TrimSpace(domain), Revision: strings.TrimSpace(revision), RevisionStatus: strings.TrimSpace(revisionStatus), TreeDigestSHA256: strings.TrimSpace(tree), GraphDigestSHA256: strings.TrimSpace(graph), GraphDigestStatus: strings.TrimSpace(graphStatus)}
}
func parseKeyValues(values []string) map[string]string {
	out := map[string]string{}
	for _, v := range values {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" {
			out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return out
}
func printDocumentSummary(doc investigation.Document) int {
	summary, err := investigationsurface.SummarizeDocument(doc)
	if err != nil {
		return cliSurfaceError("investigate", err)
	}
	fmt.Printf("mode: %s\nrepository: %s\ndigest: %s\nobservations: %d\nevidence: %d\ncoverage: %d\ncandidates: %d\nquestions: %d\nlimitations: %d\n", summary.Mode, summary.Repository.RepositoryDomain, summary.DocumentDigest, summary.ObservationCount, summary.EvidenceCount, summary.CoverageCount, summary.CandidateClaimCount, summary.QuestionCount, summary.LimitationCount)
	return 0
}
func cliSurfaceError(command string, err error) int {
	fmt.Fprintf(os.Stderr, "sensei %s: %v\n", command, err)
	return 1
}
func emitSurfaceJSON(value any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		return 1
	}
	return 0
}

var _ = time.RFC3339
