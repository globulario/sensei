// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/maintenance"
)

type maintainClaimsOptions struct {
	Claims            string
	Previous          string
	Dialogue          string
	EvidenceState     string
	Repo              string
	GraphDigest       string
	GraphDigestStatus string
	EvaluatedAt       string
	Format            string
	Output            string
	ReportOutput      string
	Check             bool
}

func runMaintainClaims(args []string) int {
	fs := flag.NewFlagSet("sensei maintain-claims", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := maintainClaimsOptions{}
	fs.StringVar(&opts.Claims, "claims", "", "current architecture_claims YAML document")
	fs.StringVar(&opts.Previous, "previous", "", "previous architecture_claims YAML document")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "optional architecture_dialogue YAML document for report visibility")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "optional architecture_evidence_state YAML document")
	fs.StringVar(&opts.Repo, "repo", ".", "repository root to verify source receipts")
	fs.StringVar(&opts.GraphDigest, "graph-digest", "", "explicit verified graph digest for observed binding")
	fs.StringVar(&opts.GraphDigestStatus, "graph-digest-status", architecture.GraphDigestNotRequested, "graph digest status: resolved | unavailable | not_requested")
	fs.StringVar(&opts.EvaluatedAt, "evaluated-at", "", "explicit RFC3339 evaluation time")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write maintained architecture_claims document instead of stdout")
	fs.StringVar(&opts.ReportOutput, "report-output", "", "write claim_truth_maintenance report")
	fs.BoolVar(&opts.Check, "check", false, "compare --output and optional --report-output with fresh deterministic maintenance")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei maintain-claims --claims <current-claims.yaml> --repo <checkout> [flags]

Recalculate non-authoritative ArchitectureClaim candidate status from explicit
source receipts, graph binding, evidence state, dependencies, and history.
The command is offline: it does not query or mutate the live graph.

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
		fmt.Fprintln(os.Stderr, "sensei maintain-claims: --claims is required")
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei maintain-claims: --check requires --output")
		return 2
	}
	if err := validateGraphDigestFlags(opts.GraphDigest, opts.GraphDigestStatus); err != nil {
		fmt.Fprintf(os.Stderr, "sensei maintain-claims: %v\n", err)
		return 2
	}
	root, err := filepath.Abs(opts.Repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei maintain-claims: resolve repo: %v\n", err)
		return 1
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei maintain-claims: --repo must be an existing directory: %s\n", root)
		return 2
	}
	for _, out := range []string{opts.Output, opts.ReportOutput} {
		if out != "" && !inferClaimsOutputPathAllowed(root, out) {
			fmt.Fprintln(os.Stderr, "sensei maintain-claims: outputs under docs/awareness or docs/intent must be inside a candidates directory")
			return 2
		}
	}
	claimBytes, reportBytes, result, err := buildMaintainClaimsOutput(root, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei maintain-claims: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, claimBytes) {
			fmt.Fprintf(os.Stderr, "maintain-claims: STALE - %s differs from fresh maintenance\n", opts.Output)
			return 1
		}
		if opts.ReportOutput != "" && checkBytesDiffer(opts.ReportOutput, reportBytes) {
			fmt.Fprintf(os.Stderr, "maintain-claims: STALE - %s differs from fresh report\n", opts.ReportOutput)
			return 1
		}
		fmt.Fprintf(os.Stderr, "maintain-claims: fresh (%d claim(s), %d retired)\n", len(result.Document.Claims), len(result.Report.RetiredClaims))
		return 0
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, claimBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei maintain-claims: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(claimBytes))
	}
	if opts.ReportOutput != "" {
		if err := writeOutput(opts.ReportOutput, reportBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei maintain-claims: write --report-output: %v\n", err)
			return 1
		}
	}
	return 0
}

func buildMaintainClaimsOutput(root string, opts maintainClaimsOptions) ([]byte, []byte, maintenance.Result, error) {
	current, err := architecture.LoadClaimDocument(opts.Claims)
	if err != nil {
		return nil, nil, maintenance.Result{}, err
	}
	var previous *architecture.ClaimDocument
	if opts.Previous != "" {
		prev, err := architecture.LoadClaimDocument(opts.Previous)
		if err != nil {
			return nil, nil, maintenance.Result{}, err
		}
		previous = &prev
	}
	var dialogue *architecture.DialogueDocument
	if opts.Dialogue != "" {
		doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
		if err != nil {
			return nil, nil, maintenance.Result{}, err
		}
		dialogue = &doc
	}
	var evidence *maintenance.EvidenceStateDocument
	if opts.EvidenceState != "" {
		doc, err := maintenance.LoadEvidenceStateDocument(opts.EvidenceState)
		if err != nil {
			return nil, nil, maintenance.Result{}, err
		}
		evidence = &doc
	}
	revision, revisionStatus, _ := architecture.ResolveRevision(root, true)
	observed := architecture.ClaimDocumentBinding{
		RepositoryDomain:  current.Binding.RepositoryDomain,
		Revision:          revision,
		RevisionStatus:    revisionStatus,
		GraphDigestSHA256: strings.TrimSpace(opts.GraphDigest),
		GraphDigestStatus: strings.TrimSpace(opts.GraphDigestStatus),
	}
	result, err := maintenance.Evaluate(maintenance.Context{
		RepositoryRoot:  root,
		Current:         current,
		Previous:        previous,
		Dialogue:        dialogue,
		Evidence:        evidence,
		ObservedBinding: observed,
		EvaluatedAt:     opts.EvaluatedAt,
	})
	if err != nil {
		return nil, nil, maintenance.Result{}, err
	}
	claimBytes, err := renderInferClaimsDocument(result.Document, opts.Format)
	if err != nil {
		return nil, nil, maintenance.Result{}, err
	}
	reportBytes, err := renderMaintenanceReport(result.Report, opts.Format)
	if err != nil {
		return nil, nil, maintenance.Result{}, err
	}
	return claimBytes, reportBytes, result, nil
}

func renderMaintenanceReport(report maintenance.Report, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return maintenance.MarshalCanonicalReportYAML(report)
	case "json":
		return maintenance.MarshalCanonicalReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func checkBytesDiffer(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	return !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want))
}

func writeOutput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func renderJSONEnvelope(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
