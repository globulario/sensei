// SPDX-License-Identifier: Apache-2.0

package resultpipeline

import (
	"bytes"
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

const canonRecordedAt = "2026-07-17T14:30:00Z"

func prepReq(t *testing.T) (PrepareTransitionRequest, string, string) {
	t.Helper()
	repo, taskDir, resultRev := e2eSeedClean(t)
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	return PrepareTransitionRequest{
		Build: BuildRequest{
			RepositoryRoot: repo, TaskDirectory: taskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain,
		},
		ExpectedLedgerHeadDigestSHA256: head,
		RecordedAt:                     canonRecordedAt,
	}, repo, taskDir
}

func TestPrepareTransitionCleanCandidate(t *testing.T) {
	req, repo, taskDir := prepReq(t)
	statusBefore := e2eGit(t, repo, "status", "--porcelain")
	headBefore, _ := admission.TaskLedgerHead(taskDir)

	c, err := PrepareTransition(context.Background(), req)
	if err != nil {
		t.Fatalf("PrepareTransition: %v", err)
	}
	if err := ValidateTransitionCandidate(c); err != nil {
		t.Fatalf("candidate invalid: %v", err)
	}
	if err := closureprotocol.ValidateResultTransitionReceipt(c.Receipt); err != nil {
		t.Fatalf("receipt invalid: %v", err)
	}
	// Receipt self-digest recomputes, bytes deterministic.
	wantDigest, _ := closureprotocol.ResultTransitionReceiptDigest(c.Receipt)
	if c.Receipt.ReceiptDigestSHA256 != wantDigest {
		t.Fatal("receipt self-digest does not recompute")
	}
	wantBytes, _ := closureprotocol.MarshalCanonicalResultTransitionReceipt(c.Receipt)
	if !bytes.Equal(wantBytes, c.ReceiptBytes) || c.ReceiptByteDigestSHA256 != sha256hex(c.ReceiptBytes) {
		t.Fatal("receipt bytes not deterministic")
	}
	if len(c.Receipt.OperationalArtifactReceipts) != 10 || len(c.Receipt.Derivations) != 10 || len(c.Receipt.GovernedKnowledgeImpacts) != 10 {
		t.Fatal("candidate not fully complete")
	}
	if c.Receipt.RecordedAt != canonRecordedAt || c.Receipt.Status != closureprotocol.ReceiptValid {
		t.Fatal("receipt time/status wrong")
	}

	// No side effects.
	if e2eGit(t, repo, "status", "--porcelain") != statusBefore {
		t.Fatal("repository changed")
	}
	if h, _ := admission.TaskLedgerHead(taskDir); h != headBefore {
		t.Fatal("ledger head moved (an append would change it)")
	}
}

func TestPrepareTransitionDeterministicBytes(t *testing.T) {
	req, _, _ := prepReq(t)
	a, err := PrepareTransition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PrepareTransition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.ReceiptBytes, b.ReceiptBytes) || a.Receipt.ReceiptDigestSHA256 != b.Receipt.ReceiptDigestSHA256 {
		t.Fatal("repeated preparation is not byte-identical")
	}
}

// The transition id names the logical transition; a different recording time keeps
// the same id but yields a different receipt digest.
func TestTransitionIDStableAcrossRecordedAt(t *testing.T) {
	req, _, _ := prepReq(t)
	a, err := PrepareTransition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	req.RecordedAt = "2026-07-18T09:00:00Z"
	b, err := PrepareTransition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if a.Receipt.TransitionID != b.Receipt.TransitionID {
		t.Fatal("transition id should be stable across recording time")
	}
	if a.Receipt.ReceiptDigestSHA256 == b.Receipt.ReceiptDigestSHA256 {
		t.Fatal("receipt digest should differ with recording time")
	}
}

func TestPrepareTransitionRejectsNonCanonicalTime(t *testing.T) {
	for _, bad := range []string{"2026-07-17T14:30:00+02:00", "2026-07-17 14:30:00", "not-a-time", "2026-07-17T14:30:00.5Z"} {
		req, _, _ := prepReq(t)
		req.RecordedAt = bad
		if _, err := PrepareTransition(context.Background(), req); err == nil {
			t.Fatalf("accepted non-canonical time %q", bad)
		}
	}
}

func TestPrepareTransitionRejectsWorktree(t *testing.T) {
	req, _, _ := prepReq(t)
	req.Build.ResultMode = resulttransition.ResultModeWorktree
	req.Build.ResultRevision = ""
	_, err := PrepareTransition(context.Background(), req)
	wantVCode(t, err, CodeTransitionRequiresCommittedResult)
}

// --- candidate validation adversarial matrix ---

func validCandidate(t *testing.T) TransitionCandidate {
	t.Helper()
	req, _, _ := prepReq(t)
	c, err := PrepareTransition(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCandidateRejectsForgedReceiptDigest(t *testing.T) {
	c := validCandidate(t)
	c.Receipt.ReceiptDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	// Receipt bytes still embed the real digest, so re-render will differ too; the
	// self-digest check fires first.
	wantVCode(t, ValidateTransitionCandidate(c), CodeTransitionReceiptDigestMismatch)
}

func TestCandidateRejectsAlteredBytes(t *testing.T) {
	c := validCandidate(t)
	c.ReceiptBytes = append(append([]byte(nil), c.ReceiptBytes...), ' ')
	wantVCode(t, ValidateTransitionCandidate(c), CodeTransitionReceiptBytesMismatch)
}

func TestCandidateRejectsWrongProducerSummary(t *testing.T) {
	c := validCandidate(t)
	c.Receipt.PipelineProducerVersions = append(c.Receipt.PipelineProducerVersions, closureprotocol.ProducerVersion{Producer: "sensei.impostor", Version: "v1"})
	reReceiptDigestAndBytes(t, &c)
	wantVCode(t, ValidateTransitionCandidate(c), CodeTransitionProducerSummaryMismatch)
}

func TestCandidateRejectsOmittedImpact(t *testing.T) {
	c := validCandidate(t)
	c.Receipt.GovernedKnowledgeImpacts = c.Receipt.GovernedKnowledgeImpacts[:9]
	reReceiptDigestAndBytes(t, &c)
	wantVCode(t, ValidateTransitionCandidate(c), CodeTransitionImpactMismatch)
}

func TestCandidateRejectsBuildDigestMismatch(t *testing.T) {
	c := validCandidate(t)
	c.BuildResultDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	wantVCode(t, ValidateTransitionCandidate(c), CodeBuildResultDigestMismatch)
}

// reReceiptDigestAndBytes re-seals a mutated receipt so a downstream semantic law
// (not the self-digest check) is the one under test.
func reReceiptDigestAndBytes(t *testing.T, c *TransitionCandidate) {
	t.Helper()
	c.Receipt.ReceiptDigestSHA256 = ""
	d, err := closureprotocol.ResultTransitionReceiptDigest(c.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	c.Receipt.ReceiptDigestSHA256 = d
	b, err := closureprotocol.MarshalCanonicalResultTransitionReceipt(c.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	c.ReceiptBytes = b
	c.ReceiptByteDigestSHA256 = sha256hex(b)
}
