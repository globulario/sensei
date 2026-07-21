// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
)

type assessPlanesOptions struct {
	Claims            string
	MaintenanceReport string
	GraphNT           string
	EvidenceState     string
	Dialogue          string
	GraphDigest       string
	GraphDigestStatus string
	Format            string
	Output            string
	Check             bool
	RequireJustified  bool
}

func runAssessPlanes(args []string) int {
	fs := flag.NewFlagSet("sensei assess-planes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := assessPlanesOptions{}
	fs.StringVar(&opts.Claims, "claims", "", "maintained architecture_claims YAML document")
	fs.StringVar(&opts.MaintenanceReport, "maintenance-report", "", "optional claim_truth_maintenance report")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "explicit compiled N-Triples graph snapshot")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "optional architecture_evidence_state YAML document")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "optional architecture_dialogue YAML document for non-probative context")
	fs.StringVar(&opts.GraphDigest, "graph-digest", "", "explicit SHA-256 digest for --graph-nt")
	fs.StringVar(&opts.GraphDigestStatus, "graph-digest-status", architecture.GraphDigestNotRequested, "graph digest status: resolved | unavailable | not_requested")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write report instead of stdout")
	fs.BoolVar(&opts.Check, "check", false, "compare --output with fresh deterministic plane assessment")
	fs.BoolVar(&opts.RequireJustified, "require-justified", false, "exit 1 when any claim is not plane-justified")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei assess-planes --claims <maintained-claims.yaml> [flags]

Assess whether non-authoritative ArchitectureClaim candidates have a valid
basis for their declared architectural plane. The command is offline and
read-only: it never queries or mutates the live graph.

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
	if strings.TrimSpace(opts.Claims) == "" {
		fmt.Fprintln(os.Stderr, "sensei assess-planes: --claims is required")
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei assess-planes: --check requires --output")
		return 2
	}
	if err := validateGraphDigestFlags(opts.GraphDigest, opts.GraphDigestStatus); err != nil {
		fmt.Fprintf(os.Stderr, "sensei assess-planes: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei assess-planes: resolve cwd: %v\n", err)
		return 1
	}
	if opts.Output != "" && !inferClaimsOutputPathAllowed(root, opts.Output) {
		fmt.Fprintln(os.Stderr, "sensei assess-planes: outputs under docs/awareness or docs/intent must be inside a candidates directory")
		return 2
	}
	rendered, report, err := buildAssessPlanesOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei assess-planes: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, rendered) {
			fmt.Fprintf(os.Stderr, "assess-planes: STALE - %s differs from fresh assessment\n", opts.Output)
			return 1
		}
		fmt.Fprintf(os.Stderr, "assess-planes: fresh (%d claim(s))\n", len(report.ClaimAssessments))
		return assessPlanesStrictExit(opts, report)
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, rendered); err != nil {
			fmt.Fprintf(os.Stderr, "sensei assess-planes: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(rendered))
	}
	return assessPlanesStrictExit(opts, report)
}

func buildAssessPlanesOutput(opts assessPlanesOptions) ([]byte, plane.Report, error) {
	claims, err := architecture.LoadClaimDocument(opts.Claims)
	if err != nil {
		return nil, plane.Report{}, err
	}
	graphDigest, verified, digestReasons, err := plane.VerifyGraphSnapshot(opts.GraphNT, opts.GraphDigest, opts.GraphDigestStatus)
	if err != nil {
		return nil, plane.Report{}, err
	}
	if opts.GraphDigestStatus == architecture.GraphDigestResolved && !verified {
		return nil, plane.Report{}, fmt.Errorf("%s", digestReasons[0].Detail)
	}
	graph := plane.GraphIndex{Nodes: map[string]plane.GovernedNode{}}
	if strings.TrimSpace(opts.GraphNT) != "" {
		graph, err = plane.LoadGraphIndex(opts.GraphNT)
		if err != nil {
			return nil, plane.Report{}, err
		}
	}
	var evidence *maintenance.EvidenceStateDocument
	if opts.EvidenceState != "" {
		doc, err := maintenance.LoadEvidenceStateDocument(opts.EvidenceState)
		if err != nil {
			return nil, plane.Report{}, err
		}
		evidence = &doc
	}
	var dialogue *architecture.DialogueDocument
	if opts.Dialogue != "" {
		doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
		if err != nil {
			return nil, plane.Report{}, err
		}
		dialogue = &doc
	}
	var maintReport *maintenance.Report
	if opts.MaintenanceReport != "" {
		doc, err := maintenance.LoadReport(opts.MaintenanceReport)
		if err != nil {
			return nil, plane.Report{}, err
		}
		maintReport = &doc
	}
	if graphDigest == "" {
		graphDigest = strings.TrimSpace(opts.GraphDigest)
	}
	report, err := plane.Assess(plane.Context{
		Claims:              claims,
		Maintenance:         maintReport,
		Graph:               graph,
		Evidence:            evidence,
		Dialogue:            dialogue,
		GraphSnapshotPath:   opts.GraphNT,
		GraphDigest:         graphDigest,
		GraphDigestStatus:   opts.GraphDigestStatus,
		GraphDigestVerified: verified,
	})
	if err != nil {
		return nil, plane.Report{}, err
	}
	rendered, err := renderPlaneAssessmentReport(report, opts.Format)
	if err != nil {
		return nil, plane.Report{}, err
	}
	return rendered, report, nil
}

func renderPlaneAssessmentReport(report plane.Report, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return plane.MarshalCanonicalReportYAML(report)
	case "json":
		return plane.MarshalCanonicalReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func assessPlanesStrictExit(opts assessPlanesOptions, report plane.Report) int {
	if !opts.RequireJustified {
		return 0
	}
	for _, a := range report.ClaimAssessments {
		if a.PlaneState != plane.StateJustified {
			fmt.Fprintf(os.Stderr, "assess-planes: %s is %s\n", a.ClaimID, a.PlaneState)
			return 1
		}
	}
	return 0
}
