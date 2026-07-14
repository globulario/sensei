// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/questiongen"
)

type recordAnswerOptions struct {
	Dialogue             string
	QuestionID           string
	Statement            string
	StatementFile        string
	Classification       repeatFlag
	AuthorRole           string
	AuthorID             string
	RecordedAt           string
	GovernanceStatus     string
	Format               string
	Output               string
	ReportOutput         string
	Check                bool
	Condition            repeatFlag
	EvidenceRef          repeatFlag
	EvidencePointer      repeatFlag
	SelectedHypothesis   repeatFlag
	ReframedQuestion     string
	RequiresEvidence     bool
	MissingEvidenceNotes repeatFlag
	ScopeRepository      string
	ScopeDomain          string
	ScopeSourceSet       string
	ScopeFile            repeatFlag
	ScopeSymbol          repeatFlag
	ScopeComponent       repeatFlag
}

type adjudicateAnswerOptions struct {
	Dialogue                  string
	AnswerID                  string
	Status                    string
	AdjudicatedAt             string
	ReplacementQuestionText   string
	ReplacementQuestionStatus string
	SupersededByAnswer        string
	Format                    string
	Output                    string
	ReportOutput              string
	Check                     bool
}

func runRecordAnswer(args []string) int {
	fs := flag.NewFlagSet("sensei record-answer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := recordAnswerOptions{}
	fs.StringVar(&opts.Dialogue, "dialogue", "", "architecture_dialogue YAML document")
	fs.StringVar(&opts.QuestionID, "question", "", "open question ID to answer")
	fs.StringVar(&opts.Statement, "statement", "", "exact architect answer statement")
	fs.StringVar(&opts.StatementFile, "statement-file", "", "read exact architect answer statement from file")
	fs.Var(&opts.Classification, "classification", "explicit answer classification; may repeat")
	fs.StringVar(&opts.AuthorRole, "author-role", "", "author role token")
	fs.StringVar(&opts.AuthorID, "author-id", "", "optional author identifier")
	fs.StringVar(&opts.RecordedAt, "recorded-at", "", "explicit RFC3339 answer recording time")
	fs.StringVar(&opts.GovernanceStatus, "governance-status", architecture.AnswerGovernanceRecorded, "recorded | awaiting_evidence | awaiting_governance")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write updated dialogue instead of stdout")
	fs.StringVar(&opts.ReportOutput, "report-output", "", "write architecture_answer_recording report")
	fs.BoolVar(&opts.Check, "check", false, "compare --output and optional --report-output with fresh deterministic output")
	fs.Var(&opts.Condition, "condition", "answer condition; may repeat")
	fs.Var(&opts.EvidenceRef, "evidence-ref", "evidence:<id> reference; may repeat")
	fs.Var(&opts.EvidencePointer, "evidence-pointer", "free-form evidence pointer; may repeat")
	fs.Var(&opts.SelectedHypothesis, "select-hypothesis", "hypothesis ID selected for this question; may repeat")
	fs.StringVar(&opts.ReframedQuestion, "reframed-question", "", "replacement question text when classification is question_reframing")
	fs.BoolVar(&opts.RequiresEvidence, "requires-evidence", false, "mark recorded answer as requiring evidence")
	fs.Var(&opts.MissingEvidenceNotes, "missing-evidence", "missing evidence note; may repeat")
	fs.StringVar(&opts.ScopeRepository, "scope-repository", "", "explicit answer scope repository")
	fs.StringVar(&opts.ScopeDomain, "scope-domain", "", "explicit answer scope domain")
	fs.StringVar(&opts.ScopeSourceSet, "scope-source-set", "", "explicit answer scope source set")
	fs.Var(&opts.ScopeFile, "scope-file", "explicit scoped file; may repeat")
	fs.Var(&opts.ScopeSymbol, "scope-symbol", "explicit scoped symbol; may repeat")
	fs.Var(&opts.ScopeComponent, "scope-component", "explicit scoped component; may repeat")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei record-answer --dialogue <dialogue.yaml> --question <id> --statement <text> --classification <type> --author-role <role> --recorded-at <RFC3339> [flags]

Record an exact architect answer in an offline architecture_dialogue artifact.
The command does not classify, promote, query, or mutate the graph.

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
	if strings.TrimSpace(opts.Dialogue) == "" || strings.TrimSpace(opts.QuestionID) == "" || strings.TrimSpace(opts.AuthorRole) == "" || strings.TrimSpace(opts.RecordedAt) == "" {
		fmt.Fprintln(os.Stderr, "sensei record-answer: --dialogue, --question, --author-role, and --recorded-at are required")
		return 2
	}
	if (strings.TrimSpace(opts.Statement) == "") == (strings.TrimSpace(opts.StatementFile) == "") {
		fmt.Fprintln(os.Stderr, "sensei record-answer: exactly one of --statement or --statement-file is required")
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei record-answer: --check requires --output")
		return 2
	}
	if err := ensureCandidateOutputs(opts.Output, opts.ReportOutput); err != nil {
		fmt.Fprintf(os.Stderr, "sensei record-answer: %v\n", err)
		return 2
	}
	dialogueBytes, reportBytes, report, err := buildRecordAnswerOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei record-answer: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, dialogueBytes) {
			fmt.Fprintf(os.Stderr, "record-answer: STALE - %s differs from fresh dialogue\n", opts.Output)
			return 1
		}
		if opts.ReportOutput != "" && checkBytesDiffer(opts.ReportOutput, reportBytes) {
			fmt.Fprintf(os.Stderr, "record-answer: STALE - %s differs from fresh report\n", opts.ReportOutput)
			return 1
		}
		fmt.Fprintf(os.Stderr, "record-answer: fresh (%s answers %s)\n", report.AnswerID, report.QuestionID)
		return 0
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, dialogueBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei record-answer: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(dialogueBytes))
	}
	if opts.ReportOutput != "" {
		if err := writeOutput(opts.ReportOutput, reportBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei record-answer: write --report-output: %v\n", err)
			return 1
		}
	}
	return 0
}

func runAdjudicateAnswer(args []string) int {
	fs := flag.NewFlagSet("sensei adjudicate-answer", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	opts := adjudicateAnswerOptions{}
	fs.StringVar(&opts.Dialogue, "dialogue", "", "architecture_dialogue YAML document")
	fs.StringVar(&opts.AnswerID, "answer", "", "architect answer ID to adjudicate")
	fs.StringVar(&opts.Status, "status", "", "accepted_for_question | rejected | awaiting_evidence | awaiting_governance | superseded")
	fs.StringVar(&opts.AdjudicatedAt, "adjudicated-at", "", "explicit RFC3339 time for replacement question creation")
	fs.StringVar(&opts.ReplacementQuestionText, "replacement-question", "", "replacement question text for accepted question_reframing answers")
	fs.StringVar(&opts.ReplacementQuestionStatus, "replacement-question-status", "", "optional replacement question status")
	fs.StringVar(&opts.SupersededByAnswer, "superseded-by-answer", "", "superseding answer ID for --status superseded")
	fs.StringVar(&opts.Format, "format", "yaml", "output format: yaml | json")
	fs.StringVar(&opts.Output, "output", "", "write updated dialogue instead of stdout")
	fs.StringVar(&opts.ReportOutput, "report-output", "", "write architecture_answer_adjudication report")
	fs.BoolVar(&opts.Check, "check", false, "compare --output and optional --report-output with fresh deterministic output")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei adjudicate-answer --dialogue <dialogue.yaml> --answer <id> --status <status> [flags]

Apply an explicit governance decision to a recorded architect answer.
The command does not promote claims, create evidence, query, or mutate the graph.

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
	if strings.TrimSpace(opts.Dialogue) == "" || strings.TrimSpace(opts.AnswerID) == "" || strings.TrimSpace(opts.Status) == "" {
		fmt.Fprintln(os.Stderr, "sensei adjudicate-answer: --dialogue, --answer, and --status are required")
		return 2
	}
	if opts.Check && strings.TrimSpace(opts.Output) == "" {
		fmt.Fprintln(os.Stderr, "sensei adjudicate-answer: --check requires --output")
		return 2
	}
	if err := ensureCandidateOutputs(opts.Output, opts.ReportOutput); err != nil {
		fmt.Fprintf(os.Stderr, "sensei adjudicate-answer: %v\n", err)
		return 2
	}
	dialogueBytes, reportBytes, report, err := buildAdjudicateAnswerOutput(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei adjudicate-answer: %v\n", err)
		return 1
	}
	if opts.Check {
		if checkBytesDiffer(opts.Output, dialogueBytes) {
			fmt.Fprintf(os.Stderr, "adjudicate-answer: STALE - %s differs from fresh dialogue\n", opts.Output)
			return 1
		}
		if opts.ReportOutput != "" && checkBytesDiffer(opts.ReportOutput, reportBytes) {
			fmt.Fprintf(os.Stderr, "adjudicate-answer: STALE - %s differs from fresh report\n", opts.ReportOutput)
			return 1
		}
		fmt.Fprintf(os.Stderr, "adjudicate-answer: fresh (%s -> %s)\n", report.AnswerID, report.GovernanceStatus)
		return 0
	}
	if opts.Output != "" {
		if err := writeOutput(opts.Output, dialogueBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei adjudicate-answer: write --output: %v\n", err)
			return 1
		}
	} else {
		fmt.Print(string(dialogueBytes))
	}
	if opts.ReportOutput != "" {
		if err := writeOutput(opts.ReportOutput, reportBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei adjudicate-answer: write --report-output: %v\n", err)
			return 1
		}
	}
	return 0
}

func buildRecordAnswerOutput(opts recordAnswerOptions) ([]byte, []byte, questiongen.AnswerRecordingReport, error) {
	doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
	if err != nil {
		return nil, nil, questiongen.AnswerRecordingReport{}, err
	}
	statement := opts.Statement
	if opts.StatementFile != "" {
		data, err := os.ReadFile(opts.StatementFile)
		if err != nil {
			return nil, nil, questiongen.AnswerRecordingReport{}, err
		}
		statement = string(data)
	}
	updated, report, err := questiongen.RecordAnswer(doc, questiongen.RecordAnswerOptions{
		QuestionID: opts.QuestionID, Statement: statement, Classifications: opts.Classification,
		AuthorRole: opts.AuthorRole, AuthorID: opts.AuthorID, RecordedAt: opts.RecordedAt,
		GovernanceStatus: opts.GovernanceStatus,
		Scope: architecture.ClaimScope{
			Repository: strings.TrimSpace(opts.ScopeRepository), Repo: strings.TrimSpace(opts.ScopeRepository),
			Domain: strings.TrimSpace(opts.ScopeDomain), SourceSet: strings.TrimSpace(opts.ScopeSourceSet),
			Files: opts.ScopeFile, Symbols: opts.ScopeSymbol, Components: opts.ScopeComponent,
		},
		Conditions: opts.Condition, EvidenceRefs: opts.EvidenceRef, EvidencePointers: opts.EvidencePointer,
		SelectedHypotheses: selections(opts.QuestionID, opts.SelectedHypothesis),
		ReframedQuestion:   opts.ReframedQuestion, RequiresEvidence: opts.RequiresEvidence, MissingEvidenceNotes: opts.MissingEvidenceNotes,
	})
	if err != nil {
		return nil, nil, questiongen.AnswerRecordingReport{}, err
	}
	dialogueBytes, err := renderDialogueDocument(updated, opts.Format)
	if err != nil {
		return nil, nil, questiongen.AnswerRecordingReport{}, err
	}
	reportBytes, err := renderAnswerRecordingReport(report, opts.Format)
	if err != nil {
		return nil, nil, questiongen.AnswerRecordingReport{}, err
	}
	return dialogueBytes, reportBytes, report, nil
}

func buildAdjudicateAnswerOutput(opts adjudicateAnswerOptions) ([]byte, []byte, questiongen.AnswerAdjudicationReport, error) {
	doc, err := architecture.LoadDialogueDocument(opts.Dialogue)
	if err != nil {
		return nil, nil, questiongen.AnswerAdjudicationReport{}, err
	}
	updated, report, err := questiongen.AdjudicateAnswer(doc, questiongen.AdjudicateAnswerOptions{
		AnswerID: opts.AnswerID, Status: opts.Status, AdjudicatedAt: opts.AdjudicatedAt,
		ReplacementQuestionText: opts.ReplacementQuestionText, ReplacementQuestionStatus: opts.ReplacementQuestionStatus,
		SupersededByAnswer: opts.SupersededByAnswer,
	})
	if err != nil {
		return nil, nil, questiongen.AnswerAdjudicationReport{}, err
	}
	dialogueBytes, err := renderDialogueDocument(updated, opts.Format)
	if err != nil {
		return nil, nil, questiongen.AnswerAdjudicationReport{}, err
	}
	reportBytes, err := renderAnswerAdjudicationReport(report, opts.Format)
	if err != nil {
		return nil, nil, questiongen.AnswerAdjudicationReport{}, err
	}
	return dialogueBytes, reportBytes, report, nil
}

func renderAnswerRecordingReport(report questiongen.AnswerRecordingReport, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return questiongen.MarshalAnswerRecordingReportYAML(report)
	case "json":
		return questiongen.MarshalAnswerRecordingReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func renderAnswerAdjudicationReport(report questiongen.AnswerAdjudicationReport, format string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "yaml", "yml":
		return questiongen.MarshalAnswerAdjudicationReportYAML(report)
	case "json":
		return questiongen.MarshalAnswerAdjudicationReportJSON(report)
	default:
		return nil, fmt.Errorf("--format must be yaml or json")
	}
}

func selections(questionID string, ids []string) []architecture.HypothesisSelection {
	var out []architecture.HypothesisSelection
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, architecture.HypothesisSelection{QuestionID: strings.TrimSpace(questionID), HypothesisID: id})
	}
	return out
}

func ensureCandidateOutputs(outputs ...string) error {
	root, err := filepath.Abs(".")
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}
	for _, out := range outputs {
		if out != "" && !inferClaimsOutputPathAllowed(root, out) {
			return fmt.Errorf("outputs under docs/awareness or docs/intent must be inside a candidates directory")
		}
	}
	return nil
}
