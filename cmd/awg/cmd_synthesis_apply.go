// SPDX-License-Identifier: AGPL-3.0-only

// cmd_synthesis_apply.go is O5B's CLI surface: the last step of the governed
// synthesis chain that had no way to be reached from a command line.
//
// golang/architecture/candidateapply has existed and been tested since PR #138,
// and nothing could invoke it. That is not a cosmetic gap. It meant the only
// way to get an admitted candidate into a working tree was for a human to copy
// file contents out of a sealed JSON artifact by hand -- discarding, at the
// last step, every digest binding the preceding four owners spent their whole
// design establishing. A chain that is digest-bound from interpretation to
// admission and then ends in a copy-paste is not digest-bound.
//
// What this command does NOT do is as load-bearing as what it does. It never
// commits, pushes, opens a pull request, approves, merges, or promotes
// knowledge (issue #149 hard law 7). It writes files into one dedicated
// worktree and records receipts. Everything after that stays a human decision,
// and every existing refusal in candidateapply -- dirty target, wrong base,
// unadmitted decision, wrong artifact, unsupported operation -- is surfaced
// rather than worked around.
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
	"github.com/globulario/sensei/golang/architecture/candidateapply"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
)

// Exit-code contract, continuing synthesis-run's and synthesis-admit's. The
// three refusal classes are separated because they call for different actions:
// an unadmitted decision means go back to admission, a refused target means fix
// the worktree, and a failed verification means the applied result is not what
// was admitted and must be investigated rather than retried.
const (
	exitCandidateApplied      = 0
	exitAdmissionNotAdmitting = 3
	exitTargetRefused         = 4
	exitVerificationFailed    = 5
	exitAlreadyConsumed       = 6
	exitAppliedWithoutReceipt = 7
)

func runSynthesisApply(args []string) int {
	fs := flag.NewFlagSet("sensei synthesis-apply", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	repoFlag := fs.String("repo", ".", "repository checkout the candidate's base revision is read from")
	lineagePath := fs.String("lineage", "", "path to the <candidate-digest>.lineage.json written by 'sensei synthesis-run' (required)")
	decisionPath := fs.String("decision", "", "admission decision YAML produced by 'sensei admit-change --output' (required)")
	targetRoot := fs.String("target", "", "dedicated, clean Git worktree checked out at the admitted base revision (required)")
	taskFlag := fs.String("task", "", "task directory the candidate was generated under (default: the active task); used to refuse task and closure drift")
	verificationPath := fs.String("verification", "", "HISTORICAL: attach an already-produced admission verification to this application. A verification OF the applied result cannot exist yet at this point; use 'sensei synthesis-record-verification' after applying instead")
	format := fs.String("format", "text", "output format: text | json")

	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei synthesis-apply --lineage <path>.lineage.json --decision <decision.yaml> --target <worktree> [flags]

Materializes ONLY the exact sealed candidate an admission decision admitted,
into one dedicated Git worktree that is clean and already checked out at the
admitted base revision.

This command NEVER commits, pushes, opens a pull request, approves, merges, or
promotes knowledge. It writes files and records receipts; every step after that
is a human decision.

It refuses rather than adapts:
  - a decision that does not admit mutation;
  - a target that is dirty, or whose HEAD is not the admitted base;
  - a candidate that is not the one the decision admitted;
  - any operation outside modify (add, delete, mode change, symlink).

On any mid-apply failure the worktree is rolled back from the immutable base
manifest, so a partial application is never left behind.

Pipeline:
  sensei synthesis-run     -> sealed candidate + lineage bundle
  sensei synthesis-admit   -> derived admission request
  sensei admit-change      -> admission decision        (separate, deliberate)
  sensei synthesis-apply   -> this command
  sensei verify-admission  -> admission verification of the applied result
  sensei synthesis-record-verification -> immutable link between the two

--verification is the HISTORICAL attachment path and rewrites this receipt. It
cannot express the ordinary case, because a verification of the applied result
does not exist until after the application. Record it as a separate immutable
document with 'sensei synthesis-record-verification' instead.

Outcomes:
  0  candidate applied to the target worktree
  1  inputs did not resolve
  2  invalid invocation
  3  the decision does not authorize mutation of this candidate
  4  the target worktree was refused (dirty, wrong base, or not a worktree)
  5  the recorded verification did not pass
  6  this candidate was already applied; its receipt is the consumption record
  7  the candidate WAS applied but its receipt could not be persisted; the
     worktree is modified and unaudited — reconcile before doing anything else

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitInvalidInvocation
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: unexpected argument %q\n", fs.Arg(0))
		return exitInvalidInvocation
	}
	// Checked in a fixed order, not by ranging a map: two runs of the same
	// wrong invocation must not report different missing flags.
	for _, required := range []struct{ name, value string }{
		{"--lineage", *lineagePath},
		{"--decision", *decisionPath},
		{"--target", *targetRoot},
	} {
		if strings.TrimSpace(required.value) == "" {
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: %s is required\n", required.name)
			return exitInvalidInvocation
		}
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintln(os.Stderr, "sensei synthesis-apply: --format must be \"text\" or \"json\"")
		return exitInvalidInvocation
	}

	absRepo, err := filepath.Abs(*repoFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: resolve --repo: %v\n", err)
		return exitResolutionFailure
	}
	absTarget, err := filepath.Abs(*targetRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: resolve --target: %v\n", err)
		return exitResolutionFailure
	}
	// A DEDICATED worktree means one that is not the checkout the task and the
	// base manifest come from. candidateapply enforces clean-and-at-base, but
	// it never receives the source path, so it cannot enforce this command's
	// documented separation: `--repo . --target .` passes every check it makes
	// and mutates the source checkout directly. Symlinks are resolved first so
	// two spellings of one directory cannot slip past the comparison.
	if same, serr := sameDirectory(absRepo, absTarget); serr != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: compare --repo and --target: %v\n", serr)
		return exitResolutionFailure
	} else if same {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: --target must be a dedicated worktree, not the source checkout (%s)\n", absRepo)
		fmt.Fprintln(os.Stderr, "  applying into the source would mutate the checkout the task and base manifest are read from.")
		return exitTargetRefused
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// --- step 1: the candidate and its store, addressed the same way
	// synthesis-admit addresses them: by the bundle's own directory, never by
	// a path a previous process recorded inside it. ---
	lineage, err := loadSynthesisRunLineage(*lineagePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: %v\n", err)
		return exitResolutionFailure
	}
	storeDir := filepath.Dir(*lineagePath)
	store, err := runnercomposition.NewFSCandidateArtifactStore(storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: open candidate store %s: %v\n", storeDir, err)
		return exitResolutionFailure
	}
	artifact, err := store.Get(ctx, lineage.CandidateArtifactDigestSHA256)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: read sealed candidate %s: %v\n", lineage.CandidateArtifactDigestSHA256, err)
		return exitResolutionFailure
	}

	// --- step 2: the O5A documents synthesis-admit composed ---
	var o5aRequest admissioncomposition.Request
	o5aRequestPath := filepath.Join(storeDir, lineage.CandidateArtifactDigestSHA256+".o5a-request.json")
	if err := readJSONDocument(o5aRequestPath, &o5aRequest); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: %v\n  run 'sensei synthesis-admit --lineage %s' first\n", err, *lineagePath)
		return exitResolutionFailure
	}
	concrete, err := admission.LoadRequest(filepath.Join(storeDir, lineage.CandidateArtifactDigestSHA256+".admission-request.yaml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: load the derived admission request: %v\n", err)
		return exitResolutionFailure
	}
	decision, err := admission.LoadDecision(*decisionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: load admission decision: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 3: refuse a bundle whose task or closure state has moved ---
	//
	// Hard law 10 (#149). A candidate generated under one closure state and
	// applied under another is not the same proposal, and every digest in the
	// bundle would still verify.
	if err := verifyTaskBindingUnchanged(absRepo, *taskFlag, lineage.TaskBinding, artifact.SessionDigestSHA256); err != nil {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: %v\n", err)
		return exitResolutionFailure
	}

	// --- step 4: refuse a candidate that has already been applied ---
	//
	// Issue #149's proof matrix names "previously-consumed application
	// refusal", and without this there is nothing to enforce it. A second run
	// against a reset worktree would apply the same candidate again and
	// PutAuxiliaryFile would overwrite the first receipt -- erasing the
	// evidence that the first application ever happened. Two applications
	// would then be indistinguishable from one, which is precisely the
	// property an audit of a governed mutation exists to provide.
	//
	// The receipt IS the consumption record, so removing it is the explicit
	// human act that permits a re-apply. That is deliberately a decision
	// somebody has to make, not a flag.
	consumedPath := filepath.Join(storeDir, lineage.CandidateArtifactDigestSHA256+".o5b-receipt.json")
	if prior, perr := os.Stat(consumedPath); perr == nil && prior.Mode().IsRegular() {
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: this candidate was already applied; its receipt is at %s\n", consumedPath)
		fmt.Fprintln(os.Stderr, "  applying it again would overwrite that receipt, making two applications look like one.")
		fmt.Fprintln(os.Stderr, "  if a re-apply is genuinely intended, remove the receipt first -- deliberately.")
		return exitAlreadyConsumed
	}
	// Claim consumption ATOMICALLY, before mutating anything.
	//
	// The Stat above is a courtesy that produces a good message; it is not the
	// gate. Two processes starting concurrently for the same candidate and
	// different clean targets both pass a stat check, both apply, and the
	// replace-capable auxiliary write lets the second overwrite the first
	// receipt -- recreating exactly the two-applications-looking-like-one
	// condition this gate exists to prevent. O_CREATE|O_EXCL makes the claim a
	// single indivisible operation, so precisely one process proceeds.
	claimPath := filepath.Join(storeDir, lineage.CandidateArtifactDigestSHA256+".o5b-claim")
	claim, cerr := os.OpenFile(claimPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if cerr != nil {
		if os.IsExist(cerr) {
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: another application of this candidate holds the claim at %s\n", claimPath)
			fmt.Fprintln(os.Stderr, "  if no other process is running, that claim is from an interrupted apply: inspect the target worktree,")
			fmt.Fprintln(os.Stderr, "  then remove the claim deliberately once you know whether the candidate was applied.")
			return exitAlreadyConsumed
		}
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: claim consumption: %v\n", cerr)
		return exitResolutionFailure
	}
	_ = claim.Close()
	// Released on every path that did NOT mutate the target. Once the apply
	// succeeds the receipt becomes the durable record and the claim is removed
	// with it; an interrupted run deliberately leaves the claim behind, because
	// a stale claim is a question a human should answer, not one this command
	// should answer by guessing.
	claimReleased := false
	releaseClaim := func() {
		if !claimReleased {
			_ = os.Remove(claimPath)
			claimReleased = true
		}
	}

	// --- step 5: bind the decision to the composed request ---
	//
	// This is the join that makes application authorized rather than merely
	// requested, and it is done through the O5A owner rather than by comparing
	// fields here: ComposeDecisionReceipt refuses a decision whose identity
	// digest or scope does not match the request that was composed, which is
	// exactly the substitution an attacker -- or an operator with two candidate
	// directories open -- would otherwise make by accident.
	o5aReceipt, err := admissioncomposition.ComposeDecisionReceipt(o5aRequest, concrete, decision, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		releaseClaim()
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: this decision does not authorize this candidate: %v\n", err)
		return exitAdmissionNotAdmitting
	}

	// Whether the decision ADMITS is checked here, explicitly, rather than
	// left to surface as a generic failure out of candidateapply.
	//
	// ComposeDecisionReceipt above validates that the decision is *bound to
	// this composition*; it deliberately does not judge the outcome, so a
	// refusal sails through it and is caught several layers down. A caller
	// then sees a generic exit 1 for the most ordinary outcome in the whole
	// pipeline -- "admission said no" -- and cannot tell it from a missing
	// file. Reading the two capability fields directly is also stronger than
	// matching the library's error text, which would silently reclassify if
	// that wording ever changed.
	if reason := admissionDoesNotAuthorize(decision); reason != "" {
		releaseClaim()
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: %s\n  nothing was applied; this is admission's answer, not a defect to retry\n", reason)
		return exitAdmissionNotAdmitting
	}

	// --- step 6: the base manifest, re-read from git at the candidate's own
	// base revision. Same reasoning as synthesis-admit: what a candidate
	// changes relative to the repository is a question only the repository can
	// answer, and candidateapply's rollback is bound to these exact bytes. ---
	_, baseManifest, baseDigest, cleanup, err := runnercomposition.ExtractSnapshot(ctx, absRepo, artifact.BaseRevision)
	if err != nil {
		releaseClaim()
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: read base revision %s from %s: %v\n", artifact.BaseRevision, absRepo, err)
		return exitResolutionFailure
	}
	defer func() { _ = cleanup() }()
	if baseDigest != artifact.InputCandidateDigestSHA256 {
		releaseClaim()
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: base-revision drift: %s in %s digests to %s, but the candidate was generated against %s\n",
			artifact.BaseRevision, absRepo, baseDigest, artifact.InputCandidateDigestSHA256)
		return exitResolutionFailure
	}

	// --- step 7: apply, through O5B ---
	applyReq, applyReceipt, err := candidateapply.Apply(ctx, candidateapply.ApplyInput{
		AdmissionRequest:  o5aRequest,
		AdmissionReceipt:  o5aReceipt,
		Decision:          decision,
		CandidateArtifact: artifact,
		BaseManifest:      baseManifest,
		TargetRoot:        absTarget,
	}, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		// candidateapply rolls the worktree back from the immutable base
		// manifest on every mid-apply failure, so nothing was consumed and the
		// claim is released.
		releaseClaim()
		// candidateapply's own refusals are already specific; the only value
		// added here is telling the caller which KIND of problem it is, so a
		// script does not have to match on message text.
		code := exitResolutionFailure
		if isTargetRefusal(err) {
			code = exitTargetRefused
		}
		fmt.Fprintf(os.Stderr, "sensei synthesis-apply: %v\n", err)
		return code
	}

	report := synthesisApplyReport{
		Disposition:                   string(applyReceipt.Disposition),
		Detail:                        applyReceipt.Detail,
		CandidateArtifactDigestSHA256: applyReceipt.CandidateArtifactDigestSHA256,
		BaseRevision:                  applyReq.BaseRevision,
		TargetRoot:                    absTarget,
		AppliedPaths:                  applyReceipt.AppliedPaths,
		PatchDigestSHA256:             applyReceipt.PatchDigestSHA256,
		RequestDigestSHA256:           applyReceipt.RequestDigestSHA256,
	}

	// --- step 8: an optional, already-produced verification ---
	//
	// This command does not verify; it RECORDS a verification the admission
	// owner produced. Generating and judging its own verification would make
	// the applier the referee of its own application.
	exitCode := exitCandidateApplied
	if strings.TrimSpace(*verificationPath) != "" {
		verification, verr := admission.LoadVerification(*verificationPath)
		if verr != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: applied, but the verification could not be read: %v\n", verr)
			_ = persistApplyDocuments(ctx, store, storeDir, lineage.CandidateArtifactDigestSHA256, applyReq, applyReceipt, &report)
			printSynthesisApplyReport(report, *format)
			return exitVerificationFailed
		}
		attached, aerr := candidateapply.AttachVerification(applyReceipt, decision, verification, time.Now().UTC().Format(time.RFC3339))
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: applied, but the verification is not bound to this application: %v\n", aerr)
			_ = persistApplyDocuments(ctx, store, storeDir, lineage.CandidateArtifactDigestSHA256, applyReq, applyReceipt, &report)
			printSynthesisApplyReport(report, *format)
			return exitVerificationFailed
		}
		applyReceipt = attached
		report.Disposition = string(attached.Disposition)
		report.Detail = attached.Detail
		if attached.AdmissionVerificationStatus != nil {
			report.AdmissionVerificationStatus = *attached.AdmissionVerificationStatus
			// scope_compliant is the only status that says the applied result
			// stayed inside what was admitted. Anything else -- today that is
			// scope_violated -- is reported as a failure rather than folded
			// into success, and the default is deliberately "not compliant
			// unless it says so", so a status this build has never seen cannot
			// pass by being unrecognized.
			if *attached.AdmissionVerificationStatus != admission.VerificationScopeCompliant {
				exitCode = exitVerificationFailed
			}
		}
	}

	if !persistApplyDocuments(ctx, store, storeDir, lineage.CandidateArtifactDigestSHA256, applyReq, applyReceipt, &report) {
		report.Detail = "the candidate was applied but its receipt could not be persisted; the target worktree is modified and unaudited"
		printSynthesisApplyReport(report, *format)
		fmt.Fprintln(os.Stderr, "sensei synthesis-apply: the consumption record is MISSING while the worktree is modified.")
		fmt.Fprintf(os.Stderr, "  the claim at %s is deliberately left in place so a second apply cannot proceed silently.\n", claimPath)
		return exitAppliedWithoutReceipt
	}
	// The receipt is now the durable consumption record, so the claim has done
	// its job and is removed with it.
	releaseClaim()
	printSynthesisApplyReport(report, *format)
	return exitCode
}

// admissionDoesNotAuthorize returns the reason a decision may not be applied,
// or "" when it may. Both capability and outcome are checked: a decision can
// be "admitted" overall while refusing mutation specifically, and applying on
// the strength of the headline verdict alone would ignore the narrower
// statement that was the whole point of recording it separately.
func admissionDoesNotAuthorize(d admission.Decision) string {
	switch d.Decision {
	case admission.DecisionAdmitted, admission.DecisionAdmittedWithConditions:
	default:
		return fmt.Sprintf("admission decision is %q; it does not authorize applying anything", d.Decision)
	}
	switch d.MutationCapability {
	case admission.CapabilityAdmitted, admission.CapabilityAdmittedWithConditions:
	default:
		return fmt.Sprintf("admission mutation capability is %q; this decision permits inspection but not mutation", d.MutationCapability)
	}
	return ""
}

// isTargetRefusal separates "your worktree is not usable" from every other
// failure. It matches on candidateapply's own refusal wording, which is a
// coupling worth naming: if that wording changes, this degrades to reporting a
// generic resolution failure -- less specific, never wrong.
func isTargetRefusal(err error) bool {
	msg := err.Error()
	for _, marker := range []string{
		"target root must be a real directory",
		"target is not a usable Git worktree",
		"does not match admitted base",
		"target worktree is not clean",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// persistApplyDocuments writes the O5B request and receipt, and reports whether
// the RECEIPT landed.
//
// The receipt is the consumption record, so failing to persist it is not a
// cosmetic problem: the worktree is modified, and a later invocation finds no
// receipt and will happily apply the same candidate again. Returning ordinary
// success there would hand the caller a green exit for a mutation nothing
// recorded, which is the precise condition the consumption gate exists to
// prevent.
func persistApplyDocuments(ctx context.Context, store runnercomposition.CandidateArtifactStore, storeDir, digest string,
	req candidateapply.Request, receipt candidateapply.Receipt, report *synthesisApplyReport) (receiptPersisted bool) {
	write := func(suffix string, doc any) (string, bool) {
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: marshal %s: %v\n", suffix, err)
			return "", false
		}
		name := digest + suffix
		if err := store.PutAuxiliaryFile(ctx, name, data); err != nil {
			// The application already happened; this does not un-apply
			// anything, and pretending otherwise would be the lie.
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: applied, but could not persist %s: %v\n", name, err)
			return "", false
		}
		return filepath.Join(storeDir, name), true
	}
	report.RequestPath, _ = write(".o5b-request.json", req)
	report.ReceiptPath, receiptPersisted = write(".o5b-receipt.json", receipt)
	return receiptPersisted
}

func readJSONDocument(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

type synthesisApplyReport struct {
	Disposition                   string   `json:"disposition"`
	Detail                        string   `json:"detail,omitempty"`
	CandidateArtifactDigestSHA256 string   `json:"candidate_artifact_digest_sha256"`
	BaseRevision                  string   `json:"base_revision"`
	TargetRoot                    string   `json:"target_root"`
	AppliedPaths                  []string `json:"applied_paths"`
	PatchDigestSHA256             string   `json:"patch_digest_sha256"`
	RequestDigestSHA256           string   `json:"request_digest_sha256"`
	AdmissionVerificationStatus   string   `json:"admission_verification_status,omitempty"`
	RequestPath                   string   `json:"request_path,omitempty"`
	ReceiptPath                   string   `json:"receipt_path,omitempty"`
}

func printSynthesisApplyReport(r synthesisApplyReport, format string) {
	if format == "json" {
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei synthesis-apply: marshal report: %v\n", err)
			return
		}
		fmt.Fprintln(os.Stdout, string(data))
		return
	}
	fmt.Fprintf(os.Stdout, "disposition:      %s\n", r.Disposition)
	if r.Detail != "" {
		fmt.Fprintf(os.Stdout, "detail:           %s\n", r.Detail)
	}
	fmt.Fprintf(os.Stdout, "candidate:        %s\n", r.CandidateArtifactDigestSHA256)
	fmt.Fprintf(os.Stdout, "base revision:    %s\n", r.BaseRevision)
	fmt.Fprintf(os.Stdout, "target worktree:  %s\n", r.TargetRoot)
	fmt.Fprintf(os.Stdout, "patch digest:     %s\n", r.PatchDigestSHA256)
	fmt.Fprintf(os.Stdout, "applied:          %d file(s)\n", len(r.AppliedPaths))
	for _, p := range r.AppliedPaths {
		fmt.Fprintf(os.Stdout, "  modify %s\n", p)
	}
	if r.AdmissionVerificationStatus != "" {
		fmt.Fprintf(os.Stdout, "verification:     %s\n", r.AdmissionVerificationStatus)
	}
	if r.ReceiptPath != "" {
		fmt.Fprintf(os.Stdout, "receipt:          %s\n", r.ReceiptPath)
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "next: review the worktree. Nothing was committed, pushed, approved, or merged,")
	fmt.Fprintln(os.Stdout, "      and this command will not do any of those.")
}

// sameDirectory reports whether two paths name the same directory after
// symlink resolution, so two spellings of one worktree cannot pass a string
// comparison.
func sameDirectory(a, b string) (bool, error) {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false, err
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false, err
	}
	return filepath.Clean(ra) == filepath.Clean(rb), nil
}
