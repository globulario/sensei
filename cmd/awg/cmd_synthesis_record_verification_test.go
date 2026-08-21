// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/candidateapply"
)

// verificationFor builds the admission verification an operator would produce
// by running verify-admission against the applied worktree, and writes it
// where the command expects to read it.
func verificationFor(t *testing.T, f applyFixture, status string) string {
	t.Helper()
	var receipt candidateapply.Receipt
	readJSONFixture(t, filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json"), &receipt)

	decision, err := admission.LoadDecision(f.decisionPath)
	if err != nil {
		t.Fatal(err)
	}
	verification := admission.Verification{
		SchemaVersion:         admission.SchemaVersion,
		GeneratedBy:           admission.GeneratedBy,
		AdmissionID:           decision.AdmissionID,
		DecisionDigestSHA256:  decision.DecisionDigestSHA256,
		Status:                status,
		Binding:               decision.Binding,
		SessionID:             "session",
		IterationDigestSHA256: fixtureHex(t, "iteration"),
		PatchDigestSHA256:     receipt.PatchDigestSHA256,
		ScopeOnly:             true,
		CorrectnessCertified:  false,
	}
	path := filepath.Join(t.TempDir(), "verification.yaml")
	data, err := admission.MarshalCanonicalVerificationYAML(verification)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f applyFixture) record(t *testing.T, verificationPath string, extra ...string) int {
	t.Helper()
	args := append([]string{
		"--lineage", f.lineagePath,
		"--decision", f.decisionPath,
		"--verification", verificationPath,
	}, extra...)
	return runSynthesisRecordVerification(args)
}

// The ordinary flow Decision A describes: apply, verify, record. The record is
// a THIRD document -- the application receipt is not touched.
func TestRecordVerificationBindsToTheApplicationWithoutRewritingIt(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}
	receiptPath := filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json")
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}

	verificationPath := verificationFor(t, f, admission.VerificationScopeCompliant)
	if code := f.record(t, verificationPath); code != exitVerificationRecorded {
		t.Fatalf("record exit = %d, want %d", code, exitVerificationRecorded)
	}

	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("recording a verification rewrote the application receipt")
	}

	matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json"))
	if len(matches) != 1 {
		t.Fatalf("expected exactly one verification record, found %v", matches)
	}
	var record candidateapply.VerificationRecord
	readJSONFixture(t, matches[0], &record)
	if err := candidateapply.ValidateVerificationRecord(record); err != nil {
		t.Fatalf("the persisted record does not validate: %v", err)
	}
	if record.CandidateArtifactDigestSHA256 != f.artifact.CandidateArtifactDigestSHA256 {
		t.Error("the record does not name the candidate that was applied")
	}
}

// The proof-matrix row that was unreachable before this command existed: a
// scope violation, recorded against a real application, WITHOUT re-applying.
func TestAScopeViolationIsRecordedAndReportedWithoutReapplying(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}
	appliedBefore, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}

	verificationPath := verificationFor(t, f, admission.VerificationScopeViolated)
	if code := f.record(t, verificationPath); code != exitRecordedScopeViolated {
		t.Fatalf("record exit = %d, want %d (recorded, and not compliant)", code, exitRecordedScopeViolated)
	}

	// The record exists: bad news is still evidence.
	matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json"))
	if len(matches) != 1 {
		t.Fatalf("a scope violation was reported but not recorded: %v", matches)
	}
	// And nothing was applied a second time to produce it.
	appliedAfter, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(appliedBefore) != string(appliedAfter) {
		t.Fatal("recording a verification modified the applied worktree")
	}
}

// A verification of something else must not be recordable here, or the record
// would read as evidence about an application it never described.
func TestRecordRefusesAVerificationBoundElsewhere(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}

	// A well-formed, canonically marshalled verification that simply describes
	// a DIFFERENT patch. Tampering with the YAML text instead would only prove
	// the loader rejects garbage.
	decision, err := admission.LoadDecision(f.decisionPath)
	if err != nil {
		t.Fatal(err)
	}
	wrong := admission.Verification{
		SchemaVersion:         admission.SchemaVersion,
		GeneratedBy:           admission.GeneratedBy,
		AdmissionID:           decision.AdmissionID,
		DecisionDigestSHA256:  decision.DecisionDigestSHA256,
		Status:                admission.VerificationScopeCompliant,
		Binding:               decision.Binding,
		SessionID:             "session",
		IterationDigestSHA256: fixtureHex(t, "iteration"),
		PatchDigestSHA256:     fixtureHex(t, "some-other-patch"),
		ScopeOnly:             true,
		CorrectnessCertified:  false,
	}
	data, err := admission.MarshalCanonicalVerificationYAML(wrong)
	if err != nil {
		t.Fatal(err)
	}
	wrongPath := filepath.Join(t.TempDir(), "wrong.yaml")
	if err := os.WriteFile(wrongPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	// The exit code matters as much as the refusal: "not bound to this
	// application" and "could not read the file" are different facts, and a
	// test satisfied by any non-zero would pass if the fixture were merely
	// malformed.
	if code := f.record(t, wrongPath); code != exitVerificationNotBound {
		t.Fatalf("record exit = %d, want %d (not bound to this application)", code, exitVerificationNotBound)
	}
	if matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json")); len(matches) != 0 {
		t.Fatalf("a refused verification still wrote a record: %v", matches)
	}
}

// There is nothing to record against an application that never happened, and
// the command must say that rather than inventing one.
func TestRecordRefusesWhenNoApplicationWasRecorded(t *testing.T) {
	f := newApplyFixture(t)
	// Deliberately no apply.
	path := filepath.Join(t.TempDir(), "verification.yaml")
	if err := os.WriteFile(path, []byte("architecture_admission_verification: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := f.record(t, path); code != exitApplicationNotRecorded {
		t.Fatalf("record exit = %d, want %d", code, exitApplicationNotRecorded)
	}
}

// TestRepeatedRecordingIsIdempotentAcrossAClockTick is the regression for a
// flake that only appeared under load: the two recordings in
// TestRepeatedRecordingNeitherDuplicatesNorOverwrites pass only while both land
// inside the same wall-clock second.
//
// The record embeds ObservedAt (time.Now at second granularity), but
// VerificationRecordDigest deliberately EXCLUDES ObservedAt, precisely so that
// "recording the same verification against the same application at a different
// moment is the same fact". The conflict check compared raw JSON bytes, which
// do carry ObservedAt — so it re-admitted the clock the digest had excluded,
// and identical evidence recorded one second later was reported as
// exitRecordConflict: "two different statements cannot both be what was
// observed", about evidence that was in fact the same statement.
//
// Rewriting the stored record's ObservedAt reproduces the straddled tick
// deterministically, with no sleep and no clock injection.
func TestRepeatedRecordingIsIdempotentAcrossAClockTick(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}
	compliant := verificationFor(t, f, admission.VerificationScopeCompliant)
	if code := f.record(t, compliant); code != exitVerificationRecorded {
		t.Fatalf("first record exit = %d", code)
	}

	matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json"))
	if len(matches) != 1 {
		t.Fatalf("first record produced %d records, want 1", len(matches))
	}
	stored, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]interface{}
	if err := json.Unmarshal(stored, &record); err != nil {
		t.Fatal(err)
	}
	observed, _ := record["observed_at"].(string)
	if observed == "" {
		t.Fatal("stored record carries no observed_at; this test no longer reproduces the tick")
	}
	// Move only the clock. Every digest in the record stays valid, because
	// ObservedAt is not part of the record's identity.
	record["observed_at"] = "1999-12-31T23:59:59Z"
	rewritten, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matches[0], rewritten, 0o644); err != nil {
		t.Fatal(err)
	}

	if code := f.record(t, compliant); code != exitVerificationRecorded {
		t.Fatalf("re-recording identical evidence across a clock tick exit = %d, want %d (a no-op); "+
			"the conflict check must compare the record digest, not raw bytes", code, exitVerificationRecorded)
	}
	if matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json")); len(matches) != 1 {
		t.Fatalf("re-recording across a clock tick produced %d records, want 1", len(matches))
	}
}

// TestRecordRefusesAStoredRecordWhoseDigestDoesNotDescribeIt keeps the
// idempotence check from trusting a self-declared digest.
//
// record_digest_sha256 is a value some earlier process WROTE into the file; it
// is not a fact about the bytes on disk now. A record edited to say something
// else — a different verification status, a different binding — while keeping
// its original digest would be accepted as an idempotent re-record, and the
// command would report success while leaving conflicting proof in place. The
// package's own rule is that a declared digest must equal the computed one.
func TestRecordRefusesAStoredRecordWhoseDigestDoesNotDescribeIt(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}
	compliant := verificationFor(t, f, admission.VerificationScopeCompliant)
	if code := f.record(t, compliant); code != exitVerificationRecorded {
		t.Fatalf("first record exit = %d", code)
	}

	matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json"))
	if len(matches) != 1 {
		t.Fatalf("first record produced %d records, want 1", len(matches))
	}
	stored, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]interface{}
	if err := json.Unmarshal(stored, &record); err != nil {
		t.Fatal(err)
	}
	// Change what the record SAYS while leaving its declared digest intact.
	// Unlike observed_at, this field is inside the digest, so the stored
	// digest no longer describes the stored content.
	record["admission_verification_status"] = "tampered_status"
	tampered, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(matches[0], tampered, 0o644); err != nil {
		t.Fatal(err)
	}

	if code := f.record(t, compliant); code != exitRecordConflict {
		t.Fatalf("recording over a record whose digest does not describe it exit = %d, want %d; "+
			"a self-declared digest must be recomputed before it is trusted", code, exitRecordConflict)
	}
}

// Idempotence at the CLI: re-recording identical evidence is a no-op, and a
// DIFFERENT verification of the same application is an additional record
// rather than a replacement.
func TestRepeatedRecordingNeitherDuplicatesNorOverwrites(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}

	compliant := verificationFor(t, f, admission.VerificationScopeCompliant)
	if code := f.record(t, compliant); code != exitVerificationRecorded {
		t.Fatalf("first record exit = %d", code)
	}
	if code := f.record(t, compliant); code != exitVerificationRecorded {
		t.Fatalf("re-recording identical evidence exit = %d, want it to be a no-op", code)
	}
	matches, _ := filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json"))
	if len(matches) != 1 {
		t.Fatalf("re-recording the same verification produced %d records", len(matches))
	}

	violated := verificationFor(t, f, admission.VerificationScopeViolated)
	if code := f.record(t, violated); code != exitRecordedScopeViolated {
		t.Fatalf("second, different verification exit = %d", code)
	}
	matches, _ = filepath.Glob(filepath.Join(f.storeDir, "*.o5b-verification-record.json"))
	if len(matches) != 2 {
		t.Fatalf("a later verification did not become its own record: %v", matches)
	}
}

// Hard law 7 for this command: it links documents, and touches nothing else.
func TestRecordVerificationMovesNoBranchAndAppliesNothing(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}
	head := strings.TrimSpace(gitIn(t, f.targetDir, "rev-parse", "HEAD"))
	status := gitIn(t, f.targetDir, "status", "--porcelain")

	if code := f.record(t, verificationFor(t, f, admission.VerificationScopeCompliant)); code != exitVerificationRecorded {
		t.Fatalf("record exit = %d", code)
	}
	if after := strings.TrimSpace(gitIn(t, f.targetDir, "rev-parse", "HEAD")); after != head {
		t.Fatalf("recording moved HEAD from %s to %s", head, after)
	}
	if after := gitIn(t, f.targetDir, "status", "--porcelain"); after != status {
		t.Fatalf("recording changed the worktree: %q -> %q", status, after)
	}
}

func TestRecordVerificationJSONReport(t *testing.T) {
	f := newApplyFixture(t)
	if code := f.apply(t); code != exitCandidateApplied {
		t.Fatalf("apply exit = %d", code)
	}
	out := captureStdout(t, func() {
		if code := f.record(t, verificationFor(t, f, admission.VerificationScopeCompliant), "--format", "json"); code != exitVerificationRecorded {
			t.Fatalf("record exit = %d", code)
		}
	})
	var report synthesisRecordVerificationReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("report is not JSON: %v\n%s", err, out)
	}
	if report.AdmissionVerificationStatus != admission.VerificationScopeCompliant {
		t.Errorf("status = %q", report.AdmissionVerificationStatus)
	}
	if report.RecordDigestSHA256 == "" || report.ApplicationReceiptDigestSHA256 == "" {
		t.Error("the report does not bind the record to the application")
	}
}

// THE negative control for the apply -> record boundary.
//
// Emission of the consumption record is broken AFTER materialization has
// already succeeded, and the requirement is precise: the command must report
// "applied but not recorded" as its own terminal state. It must NOT claim
// completion, and it must NOT try to repair the missing record by applying a
// second time -- which is the nasty failure mode durability logic invites,
// where a recording problem quietly becomes an extra mutation.
//
// The store directory is made unwritable so the receipt cannot be persisted,
// which is as close to "the machine failed at exactly that step" as a test can
// get without stubbing the writer.
func TestApplyReportsAppliedButUnrecordedRatherThanReapplying(t *testing.T) {
	f := newApplyFixture(t)

	// Break the receipt write ONLY, leaving the store otherwise usable, so the
	// failure lands after materialization rather than before it: a directory
	// occupying the receipt's exact filename cannot be written as a file.
	receiptPath := filepath.Join(f.storeDir, f.artifact.CandidateArtifactDigestSHA256+".o5b-receipt.json")
	if err := os.Mkdir(receiptPath, 0o755); err != nil {
		t.Fatal(err)
	}

	code := f.apply(t)
	if code != exitAppliedWithoutReceipt {
		t.Fatalf("exit = %d, want %d (applied, but the consumption record is missing)", code, exitAppliedWithoutReceipt)
	}

	// The application really did happen -- that is what makes the missing
	// record dangerous rather than harmless.
	applied, err := os.ReadFile(filepath.Join(f.targetDir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(applied) != "new\n" {
		t.Fatalf("the candidate was not applied at all: a.txt = %q", applied)
	}

	// No receipt file was written, so the state is honestly incomplete rather
	// than falsely complete.
	if info, err := os.Stat(receiptPath); err == nil && !info.IsDir() {
		t.Fatal("a receipt exists despite the command reporting it could not be persisted")
	}

	// And the claim is deliberately retained, so a retry cannot silently
	// mutate the worktree a second time to regenerate the record.
	gitIn(t, f.targetDir, "checkout", "--", ".")
	if code := f.apply(t); code == exitCandidateApplied {
		t.Fatal("a retry after a recording failure applied the candidate a SECOND time")
	}

	// Nothing to record a verification against either: the flow refuses to
	// continue from an application it cannot name.
	if code := f.record(t, filepath.Join(t.TempDir(), "absent.yaml")); code != exitApplicationNotRecorded {
		t.Fatalf("record exit = %d, want %d", code, exitApplicationNotRecorded)
	}
}
