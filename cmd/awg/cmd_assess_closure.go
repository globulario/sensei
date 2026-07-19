// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
)

type assessClosureOptions struct {
	Request           string
	Claims            string
	MaintenanceReport string
	PlaneAssessment   string
	Dialogue          string
	EvidenceState     string
	GraphNT           string
	Repo              string
	GraphDigest       string
	GraphDigestStatus string
	Format            string
	Output            string
	Check             bool
	RequireClosed     bool
}

func runAssessClosure(args []string) int {
	fs := flag.NewFlagSet("sensei assess-closure", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := assessClosureOptions{}
	fs.StringVar(&opts.Request, "request", "", "architecture_closure_request YAML document")
	fs.StringVar(&opts.Claims, "claims", "", "maintained architecture_claims YAML document")
	fs.StringVar(&opts.MaintenanceReport, "maintenance-report", "", "claim_truth_maintenance report")
	fs.StringVar(&opts.PlaneAssessment, "plane-assessment", "", "architectural_plane_assessment report")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "architecture_dialogue YAML document")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "architecture_evidence_state YAML document")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "explicit compiled N-Triples graph snapshot")
	fs.StringVar(&opts.Repo, "repo", ".", "repository checkout to verify")
	fs.StringVar(&opts.GraphDigest, "graph-digest", "", "explicit SHA-256 digest for --graph-nt")
	fs.StringVar(&opts.GraphDigestStatus, "graph-digest-status", architecture.GraphDigestNotRequested, "graph digest status: resolved | unavailable | not_requested")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write report instead of stdout")
	fs.BoolVar(&opts.Check, "check", false, "compare --output with fresh deterministic closure assessment")
	fs.BoolVar(&opts.RequireClosed, "require-closed", false, "exit 1 unless verdict is closed")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei assess-closure --request <closure-request.yaml> --claims <maintained-claims.yaml> [flags]

Evaluate a bounded architectural closure request against explicit offline
artifacts. The command never queries or mutates the live graph.

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
	if strings.TrimSpace(opts.Request) == "" {
		fmt.Fprintln(os.Stderr, "sensei assess-closure: --request is required")
		return 2
	}
	if strings.TrimSpace(opts.Claims) == "" {
		fmt.Fprintln(os.Stderr, "sensei assess-closure: --claims is required")
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei assess-closure: --check requires --output")
		return 2
	}
	if err := validateGraphDigestFlags(opts.GraphDigest, opts.GraphDigestStatus); err != nil {
		fmt.Fprintf(os.Stderr, "sensei assess-closure: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei assess-closure: resolve cwd: %v\n", err)
		return 1
	}
	if opts.Output != "" && !inferClaimsOutputPathAllowed(root, opts.Output) {
		fmt.Fprintln(os.Stderr, "sensei assess-closure: outputs under docs/awareness or docs/intent must be inside a candidates directory")
		return 2
	}
	rendered, report, err := buildAssessClosureOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei assess-closure: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, rendered) {
			fmt.Fprintf(os.Stderr, "assess-closure: STALE - %s differs from fresh assessment\n", opts.Output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "assess-closure: fresh (%s)\n", report.Verdict)
		return assessClosureStrictExit(opts, report)
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, rendered); err != nil {
			fmt.Fprintf(os.Stderr, "sensei assess-closure: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(rendered))
	}
	return assessClosureStrictExit(opts, report)
}

func buildAssessClosureOutput(opts assessClosureOptions) ([]byte, closure.Report, error) {
	req, err := closure.LoadRequest(opts.Request)
	if err != nil {
		return nil, closure.Report{}, err
	}
	claims, err := architecture.LoadClaimDocument(opts.Claims)
	if err != nil {
		return nil, closure.Report{}, err
	}
	missing := map[string]bool{}
	var maint *maintenance.Report
	if opts.MaintenanceReport == "" {
		missing["maintenance"] = true
	} else {
		doc, err := maintenance.LoadReport(opts.MaintenanceReport)
		if err != nil {
			return nil, closure.Report{}, err
		}
		maint = &doc
	}
	var planeReport *plane.Report
	if opts.PlaneAssessment == "" {
		missing["plane"] = true
	} else {
		doc, err := plane.LoadReport(opts.PlaneAssessment)
		if err != nil {
			return nil, closure.Report{}, err
		}
		planeReport = &doc
	}
	var dialogue *architecture.DialogueDocument
	if opts.Dialogue == "" {
		missing["dialogue"] = true
	} else {
		doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
		if err != nil {
			return nil, closure.Report{}, err
		}
		dialogue = &doc
	}
	var evidence *maintenance.EvidenceStateDocument
	if opts.EvidenceState == "" {
		missing["evidence"] = true
	} else {
		doc, err := maintenance.LoadEvidenceStateDocument(opts.EvidenceState)
		if err != nil {
			return nil, closure.Report{}, err
		}
		evidence = &doc
	}
	graphReceipt, err := graphsnapshot.Verify(opts.GraphNT, opts.GraphDigest, opts.GraphDigestStatus)
	if err != nil {
		return nil, closure.Report{}, err
	}
	graph := closure.GraphIndex{Nodes: map[string]closure.Node{}, NodesByID: map[string]string{}, FilesByPath: map[string]string{}, SymbolsByID: map[string]string{}}
	if opts.GraphNT == "" {
		missing["graph"] = true
	} else {
		graph, err = closure.LoadGraphIndex(opts.GraphNT)
		if err != nil {
			return nil, closure.Report{}, err
		}
	}
	repoRoot, err := filepath.Abs(opts.Repo)
	if err != nil {
		return nil, closure.Report{}, err
	}
	if st, err := os.Stat(repoRoot); err != nil || !st.IsDir() {
		missing["repo"] = true
	}
	revision, revStatus, _ := architecture.ResolveRevision(repoRoot, true)
	report, err := closure.Evaluate(closure.Context{
		Request: req, Claims: claims, Maintenance: maint, Plane: planeReport, Dialogue: dialogue, Evidence: evidence,
		Graph: graph, GraphReceipt: graphReceipt, RepositoryRoot: repoRoot, RepositoryRev: revision, RepositoryStatus: revStatus,
		MissingInputs: missing,
	})
	if err != nil {
		return nil, closure.Report{}, err
	}
	rendered, err := renderClosureAssessmentReport(report, opts.Format)
	if err != nil {
		return nil, closure.Report{}, err
	}
	return rendered, report, nil
}

func renderClosureAssessmentReport(report closure.Report, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return closure.MarshalCanonicalReportYAML(report)
	case "json":
		return closure.MarshalCanonicalReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func assessClosureStrictExit(opts assessClosureOptions, report closure.Report) int {
	if !opts.RequireClosed {
		return 0
	}
	if report.Verdict != closure.VerdictClosed {
		fmt.Fprintf(os.Stderr, "assess-closure: verdict is %s\n", report.Verdict)
		return 1
	}
	return 0
}
