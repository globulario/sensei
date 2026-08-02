// SPDX-License-Identifier: AGPL-3.0-only

package candidateapply

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/admissioncomposition"
)

func TestApplyAdmittedArtifactWithoutCommitting(t *testing.T) {
	fixture := newApplyFixture(t)
	req, receipt, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != DispositionApplied || receipt.PatchDigestSHA256 == "" {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if len(req.ModifyPaths) != 1 || req.ModifyPaths[0] != "a.txt" {
		t.Fatalf("unexpected request scope: %#v", req.ModifyPaths)
	}
	content, err := os.ReadFile(filepath.Join(fixture.input.TargetRoot, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new\n" {
		t.Fatalf("candidate content not applied: %q", content)
	}
	if got := runGit(t, fixture.input.TargetRoot, "rev-parse", "HEAD"); got != fixture.head {
		t.Fatalf("application created a commit: got %s want %s", got, fixture.head)
	}
}

func TestApplyRefusesDirtyTarget(t *testing.T) {
	fixture := newApplyFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.input.TargetRoot, "untracked.txt"), []byte("ambient"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z"); err == nil {
		t.Fatal("expected dirty target refusal")
	}
	content, err := os.ReadFile(filepath.Join(fixture.input.TargetRoot, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old\n" {
		t.Fatal("dirty-target refusal mutated the admitted file")
	}
}

func TestApplyRefusesWrongBaseRevision(t *testing.T) {
	fixture := newApplyFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.input.TargetRoot, "b.txt"), []byte("next"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, fixture.input.TargetRoot, "add", "b.txt")
	runGit(t, fixture.input.TargetRoot, "commit", "-qm", "next")
	if _, _, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z"); err == nil {
		t.Fatal("expected base revision refusal")
	}
}

func TestApplyRefusesNonAdmittedDecision(t *testing.T) {
	fixture := newApplyFixture(t)
	identity := *fixture.input.AdmissionRequest.AdmissionRequestIdentityDigestSHA256
	decision := canonicalDecisionFixture(t, fixture.head, fixture.input.AdmissionRequest.DerivedScope, identity, admission.DecisionRefused)
	decisionValue := decision.Decision
	decisionDigest := decision.DecisionDigestSHA256
	fixture.input.Decision = decision
	fixture.input.AdmissionReceipt.AdmissionDecision = &decisionValue
	fixture.input.AdmissionReceipt.AdmissionDecisionDigestSHA256 = &decisionDigest
	receiptDigest, err := admissioncomposition.ReceiptDigest(fixture.input.AdmissionReceipt)
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.AdmissionReceipt.ReceiptDigestSHA256 = receiptDigest
	if _, _, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z"); err == nil {
		t.Fatal("expected refused decision to block application")
	}
}

func TestApplyRefusesArtifactTampering(t *testing.T) {
	fixture := newApplyFixture(t)
	fixture.input.CandidateArtifact.Manifest[0].Content = []byte("tampered\n")
	if _, _, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z"); err == nil {
		t.Fatal("expected artifact tampering refusal")
	}
}

func TestApplyRefusesTargetContentDriftHiddenFromGitStatus(t *testing.T) {
	fixture := newApplyFixture(t)
	path := filepath.Join(fixture.input.TargetRoot, "a.txt")
	runGit(t, fixture.input.TargetRoot, "update-index", "--assume-unchanged", "a.txt")
	if err := os.WriteFile(path, []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status := runGit(t, fixture.input.TargetRoot, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("fixture did not hide target drift: %q", status)
	}
	if _, _, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z"); err == nil {
		t.Fatal("expected manifest-bound target content mismatch refusal")
	}
}

func TestAttachVerificationBindsDecisionAndPatch(t *testing.T) {
	fixture := newApplyFixture(t)
	_, receipt, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	verification := canonicalVerificationFixture(t, fixture.input.Decision, receipt.PatchDigestSHA256, admission.VerificationScopeCompliant)
	finalReceipt, err := AttachVerification(receipt, fixture.input.Decision, verification, "2026-08-02T00:01:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if finalReceipt.Disposition != DispositionVerificationRecorded || finalReceipt.AdmissionVerificationDigestSHA256 == nil {
		t.Fatalf("verification not attached: %#v", finalReceipt)
	}
	wrong := canonicalVerificationFixture(t, fixture.input.Decision, hex64("wrong-patch"), admission.VerificationScopeCompliant)
	if _, err := AttachVerification(receipt, fixture.input.Decision, wrong, "2026-08-02T00:01:00Z"); err == nil {
		t.Fatal("expected patch-binding refusal")
	}
}

func TestReceiptIdentityExcludesObservationTime(t *testing.T) {
	fixture := newApplyFixture(t)
	_, first, err := Apply(context.Background(), fixture.input, "2026-08-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, fixture.input.TargetRoot, "reset", "--hard", fixture.head)
	_, second, err := Apply(context.Background(), fixture.input, "2026-08-02T03:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if first.ReceiptDigestSHA256 != second.ReceiptDigestSHA256 {
		t.Fatal("observation time changed semantic application receipt identity")
	}
}
