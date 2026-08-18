// SPDX-License-Identifier: AGPL-3.0-only

// cmd_synthesis_admit.go closes the gap `sensei synthesis-run`'s own usage
// text names: it persists an admission lineage bundle, and until now "no CLI
// command reads that bundle yet", so the operator was told to "review the
// bundle directly before authoring an admission request by hand". Hand-
// authoring the one document that binds a sealed candidate to an admission
// decision is exactly where a governed chain stops being governed -- a typo in
// a digest, or a scope that quietly says something other than what the
// candidate actually changed, and the receipt still looks complete.
//
// This command DERIVES that request instead, through the existing O5A owner
// (golang/architecture/admissioncomposition), which validates the full
// O1/O2/O3/O4 lineage and derives the mutation scope from the sealed manifests
// rather than from anyone's description of them.
//
// It composes only existing owners and invents no authority. It does NOT
// evaluate admission (`sensei admit-change` does, and remains a separate,
// deliberate step), does not apply, commit, push, or merge, and never mutates
// the candidate or its receipts. Its entire output is a request document and,
// when the candidate is not eligible, a receipt saying so and why.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/tasksession"
)

// Exit-code contract. As with synthesis-run, every distinct outcome gets its
// own code so a caller does not have to parse text to tell "there is an
// admission request to evaluate" apart from "this candidate can never be
// admitted by the current vocabulary" apart from "the inputs did not resolve".
// 0 is reserved for the single outcome that has a next step.
const (
	exitAdmissionRequestComposed = 0
	// 1 and 2 deliberately match synthesis-run's exitResolutionFailure /
	// exitInvalidInvocation so the two commands script identically.
	exitUnsupportedOperationRefused = 3
	exitCandidateChangesNothing     = 4
)

func runSynthesisAdmit(args []string) int {
	fs := flag.NewFlagSet("sensei synthesis-admit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	repoFlag := fs.String("repo", ".", "repository checkout the candidate's base revision is read from")
	lineagePath := fs.String("lineage", "", "path to the <candidate-digest>.lineage.json written by 'sensei synthesis-run' (required)")
	templatePath := fs.String("admission-template", "", "admission request template YAML (default: the task's current-generation admission request)")
	taskFlag := fs.String("task", "", "task directory used to resolve the default template (default: the active task)")
	format := fs.String("format", "text", "output format: text | json")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei synthesis-admit --lineage <candidate-digest>.lineage.json [flags]

Composes the O5 admission request for a candidate a completed
'sensei synthesis-run' sealed, through golang/architecture/admissioncomposition.

The mutation scope is DERIVED by diffing the candidate's sealed manifest
against the repository's own tree at the candidate's base revision -- it is
never taken from the template, from the task's declared scope, or from any
description of what the candidate was supposed to change.

This command NEVER evaluates admission, applies, commits, pushes, or merges.
It writes a request for 'sensei admit-change' to decide, and nothing else.

Outcomes:
  0  admission request composed  -> run 'sensei admit-change --request <path>'
  1  inputs did not resolve
  2  invalid invocation
  3  candidate performs an operation admission does not support (add, delete,
     mode/type change, symlink mutation); a refusal receipt is written
  4  candidate changes nothing at its base revision; there is nothing to admit

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitInvalidInvocation
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: unexpected argument %q\n", fs.Arg(0))
		return exitInvalidInvocation
	}
	if strings.TrimSpace(*lineagePath) == "" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-admit: --lineage is required")
		return exitInvalidInvocation
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-admit: --format must be \"text\" or \"json\"")
		return exitInvalidInvocation
	}

	absRepo, err := filepath.Abs(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: resolve --repo: %v\n", err)
		return exitResolutionFailure
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// --- step 1: read the lineage bundle and open the store it lives in ---
	//
	// The store root is the lineage file's own directory, not the
	// CandidateArtifactPath the bundle records. That path is a human-facing
	// string written by a previous process; the directory the bundle is
	// physically in is where synthesis-run's own store sealed both the
	// candidate and this bundle, through one directory descriptor. Trusting
	// the recorded string instead would let a bundle name a candidate
	// somewhere else entirely.
	lineage, err := loadSynthesisRunLineage(*lineagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: %v\n", err)
		return exitResolutionFailure
	}
	storeDir := filepath.Dir(*lineagePath)
	store, err := runnercomposition.NewFSCandidateArtifactStore(storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: open candidate store %s: %v\n", storeDir, err)
		return exitResolutionFailure
	}
	artifact, err := store.Get(ctx, lineage.CandidateArtifactDigestSHA256)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: read sealed candidate %s: %v\n", lineage.CandidateArtifactDigestSHA256, err)
		return exitResolutionFailure
	}

	// --- step 2: resolve the admission request template ---
	template, templateSource, err := resolveAdmissionTemplate(absRepo, *templatePath, *taskFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 3: read the base manifest from git, at the candidate's own
	// base revision ---
	//
	// Computed fresh rather than carried in the bundle, deliberately: the
	// question O5A has to answer is what this candidate changes RELATIVE TO
	// THE REPOSITORY, and only the repository can answer that. A frozen copy
	// would still verify against itself long after the branch it described
	// had moved on. If the revision is not in this checkout, or its tree no
	// longer digests to the candidate's recorded input, composition refuses
	// rather than deriving a scope against the wrong base.
	_, baseManifest, baseDigest, cleanup, err := runnercomposition.ExtractSnapshot(ctx, absRepo, artifact.BaseRevision)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: read base revision %s from %s: %v\n", artifact.BaseRevision, absRepo, err)
		return exitResolutionFailure
	}
	defer func() { _ = cleanup() }()
	if baseDigest != artifact.InputCandidateDigestSHA256 {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: base-revision drift: %s in %s digests to %s, but the candidate was generated against %s -- this checkout is not the tree the candidate was built from\n",
			artifact.BaseRevision, absRepo, baseDigest, artifact.InputCandidateDigestSHA256)
		return exitResolutionFailure
	}

	// --- step 4: compose, through the O5A owner ---
	request, concrete, err := admissioncomposition.ComposeRequest(admissioncomposition.ComposeInput{
		SynthesisReceipt:  lineage.SynthesisReceipt,
		RunnerReceipt:     lineage.RunnerReceipt,
		EvaluationReceipt: lineage.EvaluationReceipt,
		CandidateArtifact: artifact,
		BaseManifest:      baseManifest,
		AdmissionTemplate: template,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: compose admission request: %v\n", err)
		return exitResolutionFailure
	}

	requestName := lineage.CandidateArtifactDigestSHA256 + ".o5a-request.json"
	requestBytes, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: marshal composed request: %v\n", err)
		return exitInternalDefect
	}
	if err := store.PutAuxiliaryFile(ctx, requestName, requestBytes); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: write composed request: %v\n", err)
		return exitResolutionFailure
	}
	report := synthesisAdmitReport{
		CandidateArtifactDigestSHA256: request.CandidateArtifactDigestSHA256,
		RepositoryDomain:              request.RepositoryDomain,
		BaseRevision:                  request.BaseRevision,
		AdmissionTemplateSource:       templateSource,
		ComposedRequestPath:           filepath.Join(storeDir, requestName),
		RequestDigestSHA256:           request.RequestDigestSHA256,
		AdmissionEligible:             request.AdmissionEligible,
		ModifiedFiles:                 scopePaths(request),
	}

	// --- step 5: the three terminal states, kept distinct ---
	//
	// "Refused because admission has no vocabulary for this operation" and
	// "there is nothing here to admit" are different facts about a candidate,
	// and neither is a failure of this command. Collapsing them into one
	// non-zero exit would tell an operator to go looking for a defect in the
	// second case, where the honest answer is that the run produced no change.
	switch {
	case len(request.UnsupportedOperations) > 0:
		receipt, rerr := admissioncomposition.ComposeUnsupportedReceipt(request, time.Now().UTC().Format(time.RFC3339))
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-admit: compose refusal receipt: %v\n", rerr)
			return exitInternalDefect
		}
		receiptName := lineage.CandidateArtifactDigestSHA256 + ".o5a-receipt.json"
		receiptBytes, merr := json.MarshalIndent(receipt, "", "  ")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-admit: marshal refusal receipt: %v\n", merr)
			return exitInternalDefect
		}
		if werr := store.PutAuxiliaryFile(ctx, receiptName, receiptBytes); werr != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-admit: write refusal receipt: %v\n", werr)
			return exitResolutionFailure
		}
		report.Disposition = string(receipt.Disposition)
		report.Detail = receipt.Detail
		report.ReceiptPath = filepath.Join(storeDir, receiptName)
		report.UnsupportedOperations = request.UnsupportedOperations
		printSynthesisAdmitReport(report, *format)
		return exitUnsupportedOperationRefused

	case concrete == nil:
		report.Disposition = "candidate-changes-nothing"
		report.Detail = "the candidate's sealed manifest is identical to the repository tree at its base revision; there is no mutation to admit"
		printSynthesisAdmitReport(report, *format)
		return exitCandidateChangesNothing
	}

	admissionName := lineage.CandidateArtifactDigestSHA256 + ".admission-request.yaml"
	admissionBytes, err := admission.MarshalCanonicalRequestYAML(*concrete)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: marshal admission request: %v\n", err)
		return exitInternalDefect
	}
	if err := store.PutAuxiliaryFile(ctx, admissionName, admissionBytes); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-admit: write admission request: %v\n", err)
		return exitResolutionFailure
	}
	report.Disposition = "admission-request-composed"
	report.AdmissionRequestPath = filepath.Join(storeDir, admissionName)
	if request.AdmissionRequestIdentityDigestSHA256 != nil {
		report.AdmissionRequestIdentityDigestSHA256 = *request.AdmissionRequestIdentityDigestSHA256
	}
	printSynthesisAdmitReport(report, *format)
	return exitAdmissionRequestComposed
}

// loadSynthesisRunLineage reads and shape-checks the bundle synthesis-run
// wrote. It only rejects what it can judge alone -- the deep O1/O2/O3/O4
// digest chain is admissioncomposition's to verify, and re-implementing a
// weaker copy of it here would be a second, divergent authority on the same
// question.
func loadSynthesisRunLineage(path string) (synthesisRunLineage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return synthesisRunLineage{}, fmt.Errorf("read lineage bundle: %w", err)
	}
	var lineage synthesisRunLineage
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lineage); err != nil {
		return synthesisRunLineage{}, fmt.Errorf("decode lineage bundle %s: %w", path, err)
	}
	if lineage.SchemaVersion != synthesisRunLineageSchemaVersion {
		return synthesisRunLineage{}, fmt.Errorf("lineage bundle %s declares schema %q, want %q", path, lineage.SchemaVersion, synthesisRunLineageSchemaVersion)
	}
	if strings.TrimSpace(lineage.CandidateArtifactDigestSHA256) == "" {
		return synthesisRunLineage{}, fmt.Errorf("lineage bundle %s names no candidate artifact digest", path)
	}
	return lineage, nil
}

// resolveAdmissionTemplate returns the admission request the composed scope is
// grafted onto, and a human-readable statement of where it came from -- so a
// receipt trail never has to guess which of two plausible files was actually
// used.
//
// The default is the task's CURRENT generation request, not the prepare-time
// one, for the same reason synthesis-run resolves the current generation's
// decision: `sensei advance-task` recomputes both, and only the current one
// describes the convergence iteration a decision would be evaluated against.
func resolveAdmissionTemplate(absRepo, explicit, taskFlag string) (admission.Request, string, error) {
	path := strings.TrimSpace(explicit)
	source := "--admission-template"
	if path == "" {
		taskDir := strings.TrimSpace(taskFlag)
		if taskDir == "" {
			ptr, err := tasksession.LoadActivePointer(absRepo)
			if err != nil {
				return admission.Request{}, "", fmt.Errorf("no --admission-template and no active task to take one from; run 'sensei prepare-change' or pass --admission-template: %w", err)
			}
			taskDir = filepath.Dir(ptr.SessionPath)
		} else if !filepath.IsAbs(taskDir) {
			taskDir = filepath.Join(absRepo, taskDir)
		}
		resolved, err := tasksession.ResolveCurrentAdmissionRequestPath(taskDir)
		if err != nil {
			return admission.Request{}, "", fmt.Errorf("resolve the task's current admission request: %w", err)
		}
		path = resolved
		source = "task current generation"
	}
	req, err := admission.LoadRequest(path)
	if err != nil {
		return admission.Request{}, "", fmt.Errorf("load admission template %s: %w", path, err)
	}
	return req, source + " (" + path + ")", nil
}

type synthesisAdmitReport struct {
	Disposition                          string                                      `json:"disposition"`
	Detail                               string                                      `json:"detail,omitempty"`
	CandidateArtifactDigestSHA256        string                                      `json:"candidate_artifact_digest_sha256"`
	RepositoryDomain                     string                                      `json:"repository_domain"`
	BaseRevision                         string                                      `json:"base_revision"`
	AdmissionTemplateSource              string                                      `json:"admission_template_source"`
	ComposedRequestPath                  string                                      `json:"composed_request_path"`
	RequestDigestSHA256                  string                                      `json:"request_digest_sha256"`
	AdmissionEligible                    bool                                        `json:"admission_eligible"`
	AdmissionRequestPath                 string                                      `json:"admission_request_path,omitempty"`
	AdmissionRequestIdentityDigestSHA256 string                                      `json:"admission_request_identity_digest_sha256,omitempty"`
	ReceiptPath                          string                                      `json:"receipt_path,omitempty"`
	ModifiedFiles                        []string                                    `json:"modified_files"`
	UnsupportedOperations                []admissioncomposition.UnsupportedOperation `json:"unsupported_operations,omitempty"`
}

func scopePaths(req admissioncomposition.Request) []string {
	paths := make([]string, 0, len(req.DerivedScope.Files))
	for _, f := range req.DerivedScope.Files {
		paths = append(paths, f.Path)
	}
	return paths
}

func printSynthesisAdmitReport(r synthesisAdmitReport, format string) {
	if format == "json" {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-admit: marshal report: %v\n", err)
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}
	fmt.Fprintf(os.Stdout, "disposition:        %s\n", r.Disposition)
	if r.Detail != "" {
		fmt.Fprintf(os.Stdout, "detail:             %s\n", r.Detail)
	}
	fmt.Fprintf(os.Stdout, "candidate:          %s\n", r.CandidateArtifactDigestSHA256)
	fmt.Fprintf(os.Stdout, "repository domain:  %s\n", r.RepositoryDomain)
	fmt.Fprintf(os.Stdout, "base revision:      %s\n", r.BaseRevision)
	fmt.Fprintf(os.Stdout, "admission template: %s\n", r.AdmissionTemplateSource)
	fmt.Fprintf(os.Stdout, "composed request:   %s\n", r.ComposedRequestPath)
	fmt.Fprintf(os.Stdout, "request digest:     %s\n", r.RequestDigestSHA256)
	fmt.Fprintf(os.Stdout, "derived scope:      %d modified file(s)\n", len(r.ModifiedFiles))
	for _, p := range r.ModifiedFiles {
		fmt.Fprintf(os.Stdout, "  modify %s\n", p)
	}
	for _, u := range r.UnsupportedOperations {
		fmt.Fprintf(os.Stdout, "  UNSUPPORTED %s %s -- %s\n", u.Operation, u.Path, u.Detail)
	}
	if r.ReceiptPath != "" {
		fmt.Fprintf(os.Stdout, "refusal receipt:    %s\n", r.ReceiptPath)
	}
	fmt.Fprintln(os.Stdout)
	switch r.Disposition {
	case "admission-request-composed":
		fmt.Fprintf(os.Stdout, "next: sensei admit-change --request %s --bundle <convergence dir> --graph-nt <graph.nt> --repo <checkout>\n", r.AdmissionRequestPath)
		fmt.Fprintln(os.Stdout, "      (admission is permission to attempt, not proof of correctness; this command decided nothing)")
	case "candidate-changes-nothing":
		fmt.Fprintln(os.Stdout, "next: nothing to admit -- re-run synthesis, or accept that this run produced no mutation")
	default:
		fmt.Fprintln(os.Stdout, "next: this candidate cannot be admitted by the current operation vocabulary; it is not a defect to retry")
	}
}
