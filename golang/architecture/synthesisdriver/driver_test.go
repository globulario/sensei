// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
)

func finishResultTestState(t *testing.T) synthesis.SessionState {
	t.Helper()
	_, revision := createO7Repository(t)
	const repositoryDomain = "github.com/example/o7-finish-result"
	identity := createO7Identity(repositoryDomain, revision)
	identityDigest, err := workspacecontract.IdentityDigest(identity)
	if err != nil {
		t.Fatal(err)
	}
	session := createO7Session(t, repositoryDomain, revision, identityDigest, 0, 0)
	state, err := synthesis.NewSessionState(session)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// TestFinishResult_UsesSealedButUncarriedDigestWhenCandidateIsNil is the
// direct regression test for a live review finding: runnercomposition.Run's
// DispositionDigestMismatch path seals a candidate artifact and stamps
// RunnerReceipt.CandidateArtifactDigestSHA256 BEFORE discovering the
// mismatch, but the driver's PhaseAttempting case only has the trace/
// handoff at that point -- never a full runnercomposition.CandidateArtifact
// object -- so `candidate` (the local var finishResult's own candidate
// parameter derives from) stays nil for a run's first, non-verified
// attempt. Without the sealedButUncarriedCandidateDigestSHA256 parameter,
// the resulting Receipt.CandidateArtifactDigestSHA256 would incorrectly
// stay nil too, even though the artifact really is durably sealed.
func TestFinishResult_UsesSealedButUncarriedDigestWhenCandidateIsNil(t *testing.T) {
	state := finishResultTestState(t)
	const sealedDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	result, err := finishResult(state, nil, nil, nil, strPtr(sealedDigest), Trace{}, 1, DispositionRunnerStopped, "O3 ended with digest-mismatch", "2026-08-02T00:00:00Z", func() time.Time {
		return time.Date(2026, 8, 2, 0, 1, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("finishResult: %v", err)
	}
	if result.Receipt.CandidateArtifactDigestSHA256 == nil || *result.Receipt.CandidateArtifactDigestSHA256 != sealedDigest {
		t.Fatalf("Receipt.CandidateArtifactDigestSHA256 = %v, want %q", result.Receipt.CandidateArtifactDigestSHA256, sealedDigest)
	}
	// Result.Candidate (the full object) is correctly left nil -- the
	// digest override must never fabricate a fake CandidateArtifact.
	if result.Candidate != nil {
		t.Fatalf("Result.Candidate = %+v, want nil (only the digest is known, never fabricate the full object)", result.Candidate)
	}
}

// TestFinishResult_CandidateTakesPrecedenceOverUncarriedDigest covers the
// companion case: when candidate (the full object) is non-nil, its own
// digest is used, regardless of what sealedButUncarriedCandidateDigestSHA256
// says -- the override parameter is a fallback for when candidate is nil,
// never a competing source of truth.
func TestFinishResult_CandidateTakesPrecedenceOverUncarriedDigest(t *testing.T) {
	state := finishResultTestState(t)
	const realDigest = "1111111111111111111111111111111111111111111111111111111111111111"
	const staleOverrideDigest = "2222222222222222222222222222222222222222222222222222222222222222"
	candidate := &runnercomposition.CandidateArtifact{CandidateArtifactDigestSHA256: realDigest}
	result, err := finishResult(state, nil, nil, candidate, strPtr(staleOverrideDigest), Trace{}, 1, DispositionRunnerStopped, "O3 ended", "2026-08-02T00:00:00Z", func() time.Time {
		return time.Date(2026, 8, 2, 0, 1, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatalf("finishResult: %v", err)
	}
	if result.Receipt.CandidateArtifactDigestSHA256 == nil || *result.Receipt.CandidateArtifactDigestSHA256 != realDigest {
		t.Fatalf("Receipt.CandidateArtifactDigestSHA256 = %v, want %q (candidate's own digest, not the override)", result.Receipt.CandidateArtifactDigestSHA256, realDigest)
	}
}

func strPtr(s string) *string { return &s }
