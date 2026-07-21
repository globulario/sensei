// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
	"github.com/globulario/sensei/golang/architecture/probe"
	"gopkg.in/yaml.v3"
)

type planProbesOptions struct {
	Closure           string
	Claims            string
	Dialogue          string
	GraphNT           string
	MaintenanceReport string
	PlaneAssessment   string
	EvidenceState     string
	ExistingProbes    string
	Format            string
	Output            string
	ReportOutput      string
	Check             bool
	RequireComplete   bool
	Templates         repeatFlag
	ListTemplates     bool
}

type artifactFlags []probe.ArtifactInput

func (a *artifactFlags) String() string { return fmt.Sprint([]probe.ArtifactInput(*a)) }
func (a *artifactFlags) Set(v string) error {
	kind, path, ok := strings.Cut(strings.TrimSpace(v), "=")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(path) == "" {
		return fmt.Errorf("artifact must be kind=path")
	}
	*a = append(*a, probe.ArtifactInput{Kind: strings.TrimSpace(kind), Path: strings.TrimSpace(path)})
	return nil
}

type recordProbeResultOptions struct {
	Probes                  string
	ProbeID                 string
	ResultStatus            string
	ExecutedBy              string
	ObservedAt              string
	Output                  string
	Results                 string
	Claims                  string
	GraphNT                 string
	EvidenceState           string
	EvidenceStateOutput     string
	EvidenceStatus          string
	EvidenceFreshness       string
	ApprovalReceipt         string
	Artifacts               artifactFlags
	ObservationSource       string
	Notes                   repeatFlag
	ReplaceExistingEvidence bool
	Format                  string
	Check                   bool
	ReportOutput            string
}

func runPlanProbes(args []string) int {
	fs := flag.NewFlagSet("sensei plan-probes", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := planProbesOptions{}
	fs.StringVar(&opts.Closure, "closure", "", "architecture_closure_assessment YAML document")
	fs.StringVar(&opts.Claims, "claims", "", "maintained architecture_claims YAML document")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "architecture_dialogue YAML document")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "explicit compiled N-Triples graph snapshot")
	fs.StringVar(&opts.MaintenanceReport, "maintenance-report", "", "optional claim_truth_maintenance report")
	fs.StringVar(&opts.PlaneAssessment, "plane-assessment", "", "optional architectural_plane_assessment report")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "optional architecture_evidence_state YAML document")
	fs.StringVar(&opts.ExistingProbes, "existing-probes", "", "optional existing architecture_evidence_probes YAML document")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write probe document instead of stdout")
	fs.StringVar(&opts.ReportOutput, "report-output", "", "write architecture_probe_generation report")
	fs.BoolVar(&opts.Check, "check", false, "compare --output and optional --report-output with fresh deterministic output")
	fs.BoolVar(&opts.RequireComplete, "require-complete", false, "exit 1 when eligible evidence questions are unsupported or insufficient")
	fs.Var(&opts.Templates, "template", "template ID to enable; may repeat")
	fs.BoolVar(&opts.ListTemplates, "list-templates", false, "list deterministic probe templates and exit")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei plan-probes --closure <closure.yaml> --claims <claims.yaml> --dialogue <dialogue.yaml> --graph-nt <awareness.nt> [flags]

Plan non-authoritative EvidenceProbe artifacts from explicit offline inputs.
The command never executes probes, tests, shell commands, graph queries, or runtime observations.

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
	reg, err := probe.DefaultRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei plan-probes: %v\n", err)
		return 1
	}
	if opts.ListTemplates {
		data, err := renderProbeTemplates(reg.Descriptors(), opts.Format)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei plan-probes: %v\n", err)
			return 2
		}
		fmt.Print(string(data))
		return 0
	}
	if opts.Closure == "" || opts.Claims == "" || opts.Dialogue == "" || opts.GraphNT == "" {
		fmt.Fprintln(os.Stderr, "sensei plan-probes: --closure, --claims, --dialogue, and --graph-nt are required")
		return 2
	}
	if opts.Check && opts.Output == "" {
		fmt.Fprintln(os.Stderr, "sensei plan-probes: --check requires --output")
		return 2
	}
	if err := ensureCandidateOutputs(opts.Output, opts.ReportOutput); err != nil {
		fmt.Fprintf(os.Stderr, "sensei plan-probes: %v\n", err)
		return 2
	}
	probeBytes, reportBytes, result, err := buildPlanProbesOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei plan-probes: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, probeBytes) {
			fmt.Fprintf(os.Stderr, "plan-probes: STALE - %s differs from fresh probes\n", opts.Output)
			return 1
		}
		if opts.ReportOutput != "" && checkBytesDiffer(opts.ReportOutput, reportBytes) {
			fmt.Fprintf(os.Stderr, "plan-probes: STALE - %s differs from fresh report\n", opts.ReportOutput)
			return 1
		}
		return planProbesStrictExit(opts, result.Report)
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, probeBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei plan-probes: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(probeBytes))
	}
	if opts.ReportOutput != "" {
		if err := writeOutput(opts.ReportOutput, reportBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei plan-probes: write --report-output: %v\n", err)
			return 1
		}
	}
	return planProbesStrictExit(opts, result.Report)
}

func runRecordProbeResult(args []string) int {
	fs := flag.NewFlagSet("sensei record-probe-result", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := recordProbeResultOptions{}
	fs.StringVar(&opts.Probes, "probes", "", "architecture_evidence_probes YAML document")
	fs.StringVar(&opts.ProbeID, "probe", "", "probe ID")
	fs.StringVar(&opts.ResultStatus, "result-status", "", "completed | inconclusive | unavailable | failed | rejected")
	fs.StringVar(&opts.ExecutedBy, "executed-by", "", "external executor identifier")
	fs.StringVar(&opts.ObservedAt, "observed-at", "", "explicit RFC3339 observation time")
	fs.StringVar(&opts.Output, "output", "", "write architecture_probe_results YAML")
	fs.StringVar(&opts.Results, "results", "", "optional existing architecture_probe_results YAML")
	fs.StringVar(&opts.Claims, "claims", "", "maintained architecture_claims YAML document for evidence-state binding")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "explicit graph snapshot for evidence-state binding")
	fs.StringVar(&opts.EvidenceState, "evidence-state", "", "optional existing architecture_evidence_state YAML document")
	fs.StringVar(&opts.EvidenceStateOutput, "evidence-state-output", "", "write updated architecture_evidence_state YAML")
	fs.StringVar(&opts.EvidenceStatus, "evidence-status", "", "pass | fail | warning | stale | unknown")
	fs.StringVar(&opts.EvidenceFreshness, "evidence-freshness", "", "current | stale | unknown | historical")
	fs.StringVar(&opts.ApprovalReceipt, "approval-receipt", "", "opaque approval receipt")
	fs.Var(&opts.Artifacts, "artifact", "artifact receipt kind=path; may repeat")
	fs.StringVar(&opts.ObservationSource, "observation-source", "", "explicit observation source")
	fs.Var(&opts.Notes, "note", "result note; may repeat")
	fs.BoolVar(&opts.ReplaceExistingEvidence, "replace-existing-evidence", false, "replace an existing evidence-state record")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.BoolVar(&opts.Check, "check", false, "compare outputs with fresh deterministic recording")
	fs.StringVar(&opts.ReportOutput, "report-output", "", "write architecture_probe_result_recording report")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if opts.Probes == "" || opts.ProbeID == "" || opts.ResultStatus == "" || opts.ObservedAt == "" || opts.ExecutedBy == "" || opts.Output == "" {
		fmt.Fprintln(os.Stderr, "sensei record-probe-result: --probes, --probe, --result-status, --executed-by, --observed-at, and --output are required")
		return 2
	}
	if opts.EvidenceStateOutput != "" && (opts.Claims == "" || opts.GraphNT == "") {
		fmt.Fprintln(os.Stderr, "sensei record-probe-result: --evidence-state-output requires --claims and --graph-nt")
		return 2
	}
	if err := ensureCandidateOutputs(opts.Output, opts.ReportOutput, opts.EvidenceStateOutput); err != nil {
		fmt.Fprintf(os.Stderr, "sensei record-probe-result: %v\n", err)
		return 2
	}
	resultBytes, reportBytes, evidenceBytes, err := buildRecordProbeResultOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei record-probe-result: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, resultBytes) {
			fmt.Fprintf(os.Stderr, "record-probe-result: STALE - %s differs from fresh results\n", opts.Output)
			return 1
		}
		if opts.ReportOutput != "" && checkBytesDiffer(opts.ReportOutput, reportBytes) {
			fmt.Fprintf(os.Stderr, "record-probe-result: STALE - %s differs from fresh report\n", opts.ReportOutput)
			return 1
		}
		if opts.EvidenceStateOutput != "" && len(evidenceBytes) > 0 && checkBytesDiffer(opts.EvidenceStateOutput, evidenceBytes) {
			fmt.Fprintf(os.Stderr, "record-probe-result: STALE - %s differs from fresh evidence state\n", opts.EvidenceStateOutput)
			return 1
		}
		return 0
	}
	if err := writeOutput(opts.Output, resultBytes); err != nil {
		fmt.Fprintf(os.Stderr, "sensei record-probe-result: write --output: %v\n", err)
		return 1
	}
	if opts.ReportOutput != "" {
		if err := writeOutput(opts.ReportOutput, reportBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei record-probe-result: write --report-output: %v\n", err)
			return 1
		}
	}
	if opts.EvidenceStateOutput != "" && len(evidenceBytes) > 0 {
		if err := writeOutput(opts.EvidenceStateOutput, evidenceBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei record-probe-result: write --evidence-state-output: %v\n", err)
			return 1
		}
	}
	return 0
}

func buildPlanProbesOutput(opts planProbesOptions) ([]byte, []byte, probe.GenerationResult, error) {
	reg, err := probe.DefaultRegistry()
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	reg, err = reg.SelectIDs(opts.Templates)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	closureBytes, err := os.ReadFile(opts.Closure)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	dialogueBytes, err := os.ReadFile(opts.Dialogue)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	claimBytes, err := os.ReadFile(opts.Claims)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	closureReport, err := closure.LoadReport(opts.Closure)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	claims, err := architecture.LoadClaimDocument(opts.Claims)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	dialogue, err := architecture.LoadDialogueDocument(opts.Dialogue)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	graph, err := probe.LoadGraphIndex(opts.GraphNT)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	var maint *maintenance.Report
	if opts.MaintenanceReport != "" {
		m, err := maintenance.LoadReport(opts.MaintenanceReport)
		if err != nil {
			return nil, nil, probe.GenerationResult{}, err
		}
		maint = &m
	}
	var planeReport *plane.Report
	if opts.PlaneAssessment != "" {
		p, err := plane.LoadReport(opts.PlaneAssessment)
		if err != nil {
			return nil, nil, probe.GenerationResult{}, err
		}
		planeReport = &p
	}
	var ev *maintenance.EvidenceStateDocument
	if opts.EvidenceState != "" {
		e, err := maintenance.LoadEvidenceStateDocument(opts.EvidenceState)
		if err != nil {
			return nil, nil, probe.GenerationResult{}, err
		}
		ev = &e
	}
	var existing *probe.ProbeDocument
	ctx := &probe.ValidationContext{Dialogue: dialogue, Claims: claims, Graph: graph}
	if opts.ExistingProbes != "" {
		doc, err := probe.LoadDocument(opts.ExistingProbes, ctx)
		if err != nil {
			return nil, nil, probe.GenerationResult{}, err
		}
		existing = &doc
	}
	result, err := probe.Generate(probe.Context{
		Closure: closureReport, Claims: claims, Dialogue: dialogue, Maintenance: maint, Plane: planeReport, Evidence: ev, Graph: graph, Existing: existing,
		SourceClosureDigest: probe.Digest(closureBytes), SourceDialogueDigest: probe.Digest(dialogueBytes), SourceClaimsDigest: probe.Digest(claimBytes),
	}, reg)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	probeBytes, err := renderProbeDocument(result.Document, opts.Format, ctx)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	reportBytes, err := renderProbeGenerationReport(result.Report, opts.Format)
	if err != nil {
		return nil, nil, probe.GenerationResult{}, err
	}
	return probeBytes, reportBytes, result, nil
}

func buildRecordProbeResultOutput(opts recordProbeResultOptions) ([]byte, []byte, []byte, error) {
	probeDocBytes, err := os.ReadFile(opts.Probes)
	if err != nil {
		return nil, nil, nil, err
	}
	probes, err := probe.UnmarshalDocumentYAML(probeDocBytes, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	var existing *probe.ResultDocument
	if opts.Results != "" {
		doc, err := probe.LoadResultDocument(opts.Results, probes)
		if err != nil {
			return nil, nil, nil, err
		}
		existing = &doc
	}
	var claims *architecture.ClaimDocument
	if opts.Claims != "" {
		doc, err := architecture.LoadClaimDocument(opts.Claims)
		if err != nil {
			return nil, nil, nil, err
		}
		claims = &doc
	}
	var graph *probe.GraphIndex
	if opts.GraphNT != "" {
		idx, err := probe.LoadGraphIndex(opts.GraphNT)
		if err != nil {
			return nil, nil, nil, err
		}
		graph = &idx
	}
	var ev *maintenance.EvidenceStateDocument
	if opts.EvidenceState != "" {
		doc, err := maintenance.LoadEvidenceStateDocument(opts.EvidenceState)
		if err != nil {
			return nil, nil, nil, err
		}
		ev = &doc
	}
	if opts.EvidenceStateOutput != "" && ev == nil && claims != nil {
		empty := maintenance.EvidenceStateDocument{SchemaVersion: "1", GeneratedBy: probe.ResultBy, Binding: probes.Binding, Evidence: []maintenance.EvidenceState{}}
		ev = &empty
	}
	result, err := probe.Record(probe.RecordContext{Probes: probes, Existing: existing, Claims: claims, Graph: graph, EvidenceState: ev, ProbeDocumentDigest: probe.Digest(probeDocBytes)}, probe.RecordOptions{
		ProbeID: opts.ProbeID, ResultStatus: opts.ResultStatus, ExecutedBy: opts.ExecutedBy, ObservedAt: opts.ObservedAt, ApprovalReceipt: opts.ApprovalReceipt,
		EvidenceStatus: opts.EvidenceStatus, EvidenceFreshness: opts.EvidenceFreshness, ObservationSource: opts.ObservationSource,
		Artifacts: opts.Artifacts, Notes: opts.Notes, ReplaceExistingEvidence: opts.ReplaceExistingEvidence,
	})
	if err != nil {
		return nil, nil, nil, err
	}
	resultBytes, err := renderProbeResultDocument(result.Document, probes, opts.Format)
	if err != nil {
		return nil, nil, nil, err
	}
	reportBytes, err := renderProbeRecordingReport(result.Report, opts.Format)
	if err != nil {
		return nil, nil, nil, err
	}
	var evBytes []byte
	if result.EvidenceState != nil {
		evBytes, err = maintenance.MarshalCanonicalEvidenceStateYAML(*result.EvidenceState)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return resultBytes, reportBytes, evBytes, nil
}

func renderProbeDocument(doc probe.ProbeDocument, format string, ctx *probe.ValidationContext) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return probe.MarshalCanonicalDocumentYAML(doc, ctx)
	case "json":
		return probe.MarshalCanonicalDocumentJSON(doc, ctx)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderProbeGenerationReport(report probe.GenerationReport, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return probe.MarshalGenerationReportYAML(report)
	case "json":
		return probe.MarshalGenerationReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderProbeResultDocument(doc probe.ResultDocument, probes probe.ProbeDocument, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return probe.MarshalResultDocumentYAML(doc, probes)
	case "json":
		return probe.MarshalResultDocumentJSON(doc, probes)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderProbeRecordingReport(report probe.RecordingReport, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return probe.MarshalRecordingReportYAML(report)
	case "json":
		return probe.MarshalRecordingReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderProbeTemplates(desc []probe.TemplateDescriptor, format string) ([]byte, error) {
	return renderYAMLOrJSON(map[string]any{"probe_templates": desc}, format)
}

func renderYAMLOrJSON(v any, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return yaml.Marshal(v)
	case "json":
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func planProbesStrictExit(opts planProbesOptions, report probe.GenerationReport) int {
	if !opts.RequireComplete {
		return 0
	}
	for _, item := range report.Items {
		if item.Disposition == probe.DispositionInsufficientGrounding || item.Disposition == probe.DispositionUnsupportedTemplate || item.Disposition == probe.DispositionUnavailable {
			fmt.Fprintf(os.Stderr, "plan-probes: %s for %s\n", item.Disposition, item.QuestionID)
			return 1
		}
	}
	return 0
}
