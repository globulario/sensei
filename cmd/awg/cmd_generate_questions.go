// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/questiongen"
	"gopkg.in/yaml.v3"
)

type repeatFlag []string

func (r *repeatFlag) String() string { return strings.Join(*r, ",") }
func (r *repeatFlag) Set(v string) error {
	*r = append(*r, strings.TrimSpace(v))
	return nil
}

type generateQuestionsOptions struct {
	Closure         string
	Claims          string
	GraphNT         string
	Dialogue        string
	CreatedAt       string
	Format          string
	Output          string
	ReportOutput    string
	Check           bool
	RequireComplete bool
	Templates       repeatFlag
	ListTemplates   bool
}

func runGenerateQuestions(args []string) int {
	fs := flag.NewFlagSet("sensei generate-questions", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := generateQuestionsOptions{}
	fs.StringVar(&opts.Closure, "closure", "", "architecture_closure_assessment YAML document")
	fs.StringVar(&opts.Claims, "claims", "", "maintained architecture_claims YAML document")
	fs.StringVar(&opts.GraphNT, "graph-nt", "", "explicit compiled N-Triples graph snapshot")
	fs.StringVar(&opts.Dialogue, "dialogue", "", "optional existing architecture_dialogue YAML document")
	fs.StringVar(&opts.CreatedAt, "created-at", "", "explicit RFC3339 creation time for generated questions")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write merged dialogue instead of stdout")
	fs.StringVar(&opts.ReportOutput, "report-output", "", "write architecture_question_generation report")
	fs.BoolVar(&opts.Check, "check", false, "compare --output and optional --report-output with fresh deterministic output")
	fs.BoolVar(&opts.RequireComplete, "require-complete", false, "exit 1 when any blocker is unsupported or insufficiently grounded")
	fs.Var(&opts.Templates, "template", "template ID to enable; may repeat")
	fs.BoolVar(&opts.ListTemplates, "list-templates", false, "list deterministic question templates and exit")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei generate-questions --closure <closure.yaml> --claims <claims.yaml> --graph-nt <awareness.nt> --created-at <RFC3339> [flags]

Generate deterministic OpenQuestion artifacts from an offline closure report.
The command never queries or mutates the live graph.

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
	registry, err := questiongen.DefaultRegistry()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei generate-questions: %v\n", err)
		return 1
	}
	if opts.ListTemplates {
		data, err := yaml.Marshal(map[string]interface{}{"question_templates": registry.Descriptors()})
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei generate-questions: %v\n", err)
			return 1
		}
		fmt.Print(string(data))
		return 0
	}
	if strings.TrimSpace(opts.Closure) == "" || strings.TrimSpace(opts.Claims) == "" || strings.TrimSpace(opts.GraphNT) == "" || strings.TrimSpace(opts.CreatedAt) == "" {
		fmt.Fprintln(os.Stderr, "sensei generate-questions: --closure, --claims, --graph-nt, and --created-at are required")
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei generate-questions: --check requires --output")
		return 2
	}
	root, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei generate-questions: resolve cwd: %v\n", err)
		return 1
	}
	for _, out := range []string{opts.Output, opts.ReportOutput} {
		if out != "" && !inferClaimsOutputPathAllowed(root, out) {
			fmt.Fprintln(os.Stderr, "sensei generate-questions: outputs under docs/awareness or docs/intent must be inside a candidates directory")
			return 2
		}
	}
	dialogueBytes, reportBytes, result, err := buildGenerateQuestionsOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei generate-questions: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, dialogueBytes) {
			fmt.Fprintf(os.Stderr, "generate-questions: STALE - %s differs from fresh dialogue\n", opts.Output)
			return 1
		}
		if opts.ReportOutput != "" && checkBytesDiffer(opts.ReportOutput, reportBytes) {
			fmt.Fprintf(os.Stderr, "generate-questions: STALE - %s differs from fresh report\n", opts.ReportOutput)
			return 1
		}
		return generateQuestionsStrictExit(opts, result.Report)
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, dialogueBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei generate-questions: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(dialogueBytes))
	}
	if opts.ReportOutput != "" {
		if err := writeOutput(opts.ReportOutput, reportBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei generate-questions: write --report-output: %v\n", err)
			return 1
		}
	}
	return generateQuestionsStrictExit(opts, result.Report)
}

func buildGenerateQuestionsOutput(opts generateQuestionsOptions) ([]byte, []byte, questiongen.Result, error) {
	registry, err := questiongen.DefaultRegistry()
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	registry, err = registry.SelectIDs(opts.Templates)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	closureBytes, err := os.ReadFile(opts.Closure)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	closureReport, err := closure.LoadReport(opts.Closure)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	claims, err := architecture.LoadClaimDocument(opts.Claims)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	graph, err := closure.LoadGraphIndex(opts.GraphNT)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	var existing *architecture.DialogueDocument
	if opts.Dialogue != "" {
		doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
		if err != nil {
			return nil, nil, questiongen.Result{}, err
		}
		existing = &doc
	}
	result, err := questiongen.Generate(questiongen.Context{
		Closure: closureReport, Claims: claims, Graph: graph, Existing: existing,
		CreatedAt: opts.CreatedAt, ClosureAssessmentDigestSHA256: questiongen.StableDigest(closureBytes),
	}, registry)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	dialogueBytes, err := renderDialogueDocument(result.Dialogue, opts.Format)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	reportBytes, err := renderQuestionGenerationReport(result.Report, opts.Format)
	if err != nil {
		return nil, nil, questiongen.Result{}, err
	}
	return dialogueBytes, reportBytes, result, nil
}

func renderDialogueDocument(doc architecture.DialogueDocument, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return architecture.MarshalCanonicalDialogueDocumentYAML(doc)
	case "json":
		return architecture.MarshalCanonicalDialogueDocument(doc)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderQuestionGenerationReport(report questiongen.Report, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return questiongen.MarshalCanonicalReportYAML(report)
	case "json":
		return questiongen.MarshalCanonicalReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func generateQuestionsStrictExit(opts generateQuestionsOptions, report questiongen.Report) int {
	if !opts.RequireComplete {
		return 0
	}
	for _, item := range report.Skipped {
		if item.Disposition == questiongen.DispositionInsufficientGrounding || item.Disposition == questiongen.DispositionUnsupportedTemplate {
			fmt.Fprintf(os.Stderr, "generate-questions: %s for %s\n", item.Disposition, item.BlockerID)
			return 1
		}
	}
	return 0
}
