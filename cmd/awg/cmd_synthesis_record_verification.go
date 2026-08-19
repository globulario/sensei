// SPDX-License-Identifier: AGPL-3.0-only

// cmd_synthesis_record_verification.go is the second post-application step
// Decision A requires.
//
// The problem it solves is temporal, not cosmetic. `synthesis-apply
// --verification` asks for a verification of the applied result at the moment
// of applying -- before that result exists. The verification can only be
// produced afterwards, by which time the candidate is consumed and the
// application receipt is closed. The two facts therefore need a third
// document: an immutable record saying which already-recorded application a
// given admission verification describes.
//
// This command NEVER applies files, never re-applies, never requires the
// candidate to be unconsumed, never mutates the application receipt, and never
// judges the applied result. It links two documents that already exist and
// reports the status the admission owner produced.
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
	"github.com/globulario/sensei/golang/architecture/candidateapply"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

// Exit-code contract, continuing the synthesis family's. The distinction that
// matters most here is between "this could not be recorded" and "this was
// recorded and it says the applied result violated scope": the second is a
// successful recording of bad news, and collapsing it into a generic failure
// would send an operator looking for a broken tool instead of a broken change.
const (
	exitVerificationRecorded   = 0
	exitVerificationNotBound   = 3
	exitRecordedScopeViolated  = 4
	exitRecordConflict         = 5
	exitApplicationNotRecorded = 6
)

func runSynthesisRecordVerification(args []string) int {
	fs := flag.NewFlagSet("sensei synthesis-record-verification", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	lineagePath := fs.String("lineage", "", "path to the <candidate-digest>.lineage.json written by 'sensei synthesis-run' (required)")
	decisionPath := fs.String("decision", "", "the admission decision the application was made under (required)")
	verificationPath := fs.String("verification", "", "admission verification YAML produced by 'sensei verify-admission' against the applied worktree (required)")
	format := fs.String("format", "text", "output format: text | json")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei synthesis-record-verification --lineage <path>.lineage.json --decision <decision.yaml> --verification <verification.yaml> [flags]

Records the immutable binding between an application that already happened and
an admission verification produced afterwards.

  sensei synthesis-apply              -> immutable application receipt
  sensei verify-admission             -> immutable admission.Verification
  sensei synthesis-record-verification-> immutable binding between the two

This command NEVER applies, re-applies, commits, pushes, opens a pull request,
approves, merges, or promotes knowledge. It does not judge the applied result:
it carries the admission owner's status verbatim.

The application receipt is NOT rewritten. Several later verifications of one
application are several immutable records, keyed by verification digest, never
a mutation of one historical document.

Outcomes:
  0  verification recorded, and it reports the applied result scope compliant
  1  inputs did not resolve
  2  invalid invocation
  3  the verification is not bound to this application
  4  verification recorded, and it reports the applied result NOT compliant
  5  a different record already exists under this identity
  6  no application receipt: nothing to record a verification against

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitInvalidInvocation
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: unexpected argument %q\n", fs.Arg(0))
		return exitInvalidInvocation
	}
	for name, value := range map[string]string{"--lineage": *lineagePath, "--decision": *decisionPath, "--verification": *verificationPath} {
		if strings.TrimSpace(value) == "" {
			fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: %s is required\n", name)
			return exitInvalidInvocation
		}
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-record-verification: --format must be \"text\" or \"json\"")
		return exitInvalidInvocation
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The store root is the lineage file's own directory, for the same reason
	// synthesis-admit uses it: the directory the bundle is physically in is
	// where its sibling documents were sealed, while a path recorded inside a
	// document is a string a previous process wrote.
	lineage, err := loadSynthesisRunLineage(*lineagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: %v\n", err)
		return exitResolutionFailure
	}
	storeDir := filepath.Dir(*lineagePath)
	store, err := runnercomposition.NewFSCandidateArtifactStore(storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: open candidate store %s: %v\n", storeDir, err)
		return exitResolutionFailure
	}

	// The application must already have been recorded. This is the ONLY
	// acceptable prerequisite: requiring the candidate to still be
	// unconsumed would be requiring the application not to have happened.
	receiptPath := filepath.Join(storeDir, lineage.CandidateArtifactDigestSHA256+".o5b-receipt.json")
	var receipt candidateapply.Receipt
	if err := readJSONDocument(receiptPath, &receipt); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: %v\n", err)
		fmt.Fprintln(os.Stderr, "  there is no application receipt to record a verification against; run 'sensei synthesis-apply' first")
		return exitApplicationNotRecorded
	}

	decision, err := admission.LoadDecision(*decisionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: load decision: %v\n", err)
		return exitResolutionFailure
	}
	verification, err := admission.LoadVerification(*verificationPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: load verification: %v\n", err)
		return exitResolutionFailure
	}

	record, err := candidateapply.RecordVerification(receipt, decision, verification, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: %v\n", err)
		return exitVerificationNotBound
	}

	// Keyed by the VERIFICATION, so a later verification of the same
	// application is an additional record rather than a replacement of the
	// earlier one. Re-recording identical evidence is a no-op; different bytes
	// under one identity is a conflict, never an overwrite.
	name := lineage.CandidateArtifactDigestSHA256 + "." + record.AdmissionVerificationDigestSHA256[:12] + ".o5b-verification-record.json"
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: marshal record: %v\n", err)
		return exitInternalDefect
	}
	recordPath := filepath.Join(storeDir, name)
	if existing, rerr := os.ReadFile(recordPath); rerr == nil {
		if string(existing) != string(data) {
			fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: %s already records a different result for this verification\n", recordPath)
			fmt.Fprintln(os.Stderr, "  an existing record is never overwritten: two different statements cannot both be what was observed")
			return exitRecordConflict
		}
	} else if err := store.PutAuxiliaryFile(ctx, name, data); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: persist record: %v\n", err)
		return exitResolutionFailure
	}

	report := synthesisRecordVerificationReport{
		Disposition:                       "verification-recorded",
		ApplicationReceiptDigestSHA256:    record.ApplicationReceiptDigestSHA256,
		CandidateArtifactDigestSHA256:     record.CandidateArtifactDigestSHA256,
		PatchDigestSHA256:                 record.PatchDigestSHA256,
		AdmissionVerificationDigestSHA256: record.AdmissionVerificationDigestSHA256,
		AdmissionVerificationStatus:       record.AdmissionVerificationStatus,
		RecordDigestSHA256:                record.RecordDigestSHA256,
		RecordPath:                        recordPath,
	}
	printSynthesisRecordVerificationReport(report, *format)

	// scope_compliant is the only status saying the applied result stayed
	// inside what was admitted. Anything else is reported as non-success --
	// and the default is "not compliant unless it says so", so a status this
	// build has never seen cannot pass by being unrecognized.
	if record.AdmissionVerificationStatus != admission.VerificationScopeCompliant {
		return exitRecordedScopeViolated
	}
	return exitVerificationRecorded
}

type synthesisRecordVerificationReport struct {
	Disposition                       string `json:"disposition"`
	ApplicationReceiptDigestSHA256    string `json:"application_receipt_digest_sha256"`
	CandidateArtifactDigestSHA256     string `json:"candidate_artifact_digest_sha256"`
	PatchDigestSHA256                 string `json:"patch_digest_sha256"`
	AdmissionVerificationDigestSHA256 string `json:"admission_verification_digest_sha256"`
	AdmissionVerificationStatus       string `json:"admission_verification_status"`
	RecordDigestSHA256                string `json:"record_digest_sha256"`
	RecordPath                        string `json:"record_path"`
}

func printSynthesisRecordVerificationReport(r synthesisRecordVerificationReport, format string) {
	if format == "json" {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-record-verification: marshal report: %v\n", err)
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}
	fmt.Fprintf(os.Stdout, "disposition:        %s\n", r.Disposition)
	fmt.Fprintf(os.Stdout, "application:        %s\n", r.ApplicationReceiptDigestSHA256)
	fmt.Fprintf(os.Stdout, "candidate:          %s\n", r.CandidateArtifactDigestSHA256)
	fmt.Fprintf(os.Stdout, "verification:       %s\n", r.AdmissionVerificationDigestSHA256)
	fmt.Fprintf(os.Stdout, "verification status: %s\n", r.AdmissionVerificationStatus)
	fmt.Fprintf(os.Stdout, "record:             %s\n", r.RecordPath)
}
