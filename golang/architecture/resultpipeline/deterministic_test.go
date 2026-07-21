// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

func cleanReq(t *testing.T) (DeterministicBuildRequest, string) {
	t.Helper()
	repo, taskDir, resultRev := e2eSeedClean(t)
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	return DeterministicBuildRequest{
		BuildRequest: BuildRequest{
			RepositoryRoot: repo, TaskDirectory: taskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain,
		},
		ExpectedLedgerHeadDigestSHA256: head,
	}, head
}

func TestBuildDeterministicallyAcceptsClean(t *testing.T) {
	req, head := cleanReq(t)
	db, err := BuildDeterministically(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildDeterministically: %v", err)
	}
	if db.LedgerHeadDigestSHA256 != head {
		t.Fatal("ledger head not carried")
	}
	if !isHex64(db.BuildResultDigestSHA256) {
		t.Fatalf("build result digest not a digest: %q", db.BuildResultDigestSHA256)
	}
	if err := ValidateBuildResult(db.Result); err != nil {
		t.Fatalf("returned result invalid: %v", err)
	}
	// The returned copy is independent: mutating it does not change its digest source.
	again, err := BuildResultDigest(db.Result)
	if err != nil || again != db.BuildResultDigestSHA256 {
		t.Fatalf("digest not stable on returned copy: %v", err)
	}
}

func TestBuildDeterministicallyInvokesBuildTwice(t *testing.T) {
	req, head := cleanReq(t)
	real, err := Build(context.Background(), req.BuildRequest)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	deps := preparationDeps{
		build:      func(context.Context, BuildRequest) (BuildResult, error) { calls++; return real, nil },
		ledgerHead: func(string) (string, error) { return head, nil },
	}
	if _, err := buildDeterministically(context.Background(), req, deps); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("Build invoked %d times, want exactly 2", calls)
	}
}

func TestCompareBuildResultsDetectsMismatch(t *testing.T) {
	req, _ := cleanReq(t)
	a, err := Build(context.Background(), req.BuildRequest)
	if err != nil {
		t.Fatal(err)
	}
	// A second result that is still valid but differs in a field the gate does not
	// constrain (pipeline policy id), so the difference surfaces at comparison.
	b := a
	b.PipelinePolicyID = "sensei.resultpipeline.other/v1"
	err = CompareBuildResults(a, b)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != CodeNonDeterministicResult {
		t.Fatalf("want non_deterministic_result, got %v", err)
	}
	if !strings.Contains(ve.Detail, "pipeline policy") {
		t.Fatalf("mismatch surface not named: %s", ve.Detail)
	}
	// Identical results compare equal.
	if err := CompareBuildResults(a, a); err != nil {
		t.Fatalf("identical results should compare equal: %v", err)
	}
}

func TestBuildDeterministicallyRejectsWorktree(t *testing.T) {
	req, _ := cleanReq(t)
	req.BuildRequest.ResultMode = resulttransition.ResultModeWorktree
	req.BuildRequest.ResultRevision = ""
	_, err := BuildDeterministically(context.Background(), req)
	wantVCode(t, err, CodeTransitionRequiresCommittedResult)
}

func TestBuildDeterministicallyRejectsMalformedHead(t *testing.T) {
	req, _ := cleanReq(t)
	req.ExpectedLedgerHeadDigestSHA256 = "not-a-digest"
	_, err := BuildDeterministically(context.Background(), req)
	wantVCode(t, err, CodeExpectedLedgerHeadInvalid)
}

func TestBuildDeterministicallyRejectsWrongHeadUpFront(t *testing.T) {
	req, _ := cleanReq(t)
	req.ExpectedLedgerHeadDigestSHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	_, err := BuildDeterministically(context.Background(), req)
	wantVCode(t, err, CodeLedgerHeadMismatch)
}

func TestBuildDeterministicallyRejectsHeadChangeAfterFirst(t *testing.T) {
	req, head := cleanReq(t)
	real, err := Build(context.Background(), req.BuildRequest)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	deps := preparationDeps{
		build: func(context.Context, BuildRequest) (BuildResult, error) { return real, nil },
		ledgerHead: func(string) (string, error) {
			n++
			if n == 1 {
				return head, nil // pre-build check passes
			}
			return "2222222222222222222222222222222222222222222222222222222222222222", nil
		},
	}
	_, err = buildDeterministically(context.Background(), req, deps)
	wantVCode(t, err, CodeLedgerChangedDuringPreparation)
}

func TestBuildResultDigestValidatesFirst(t *testing.T) {
	req, _ := cleanReq(t)
	res, err := Build(context.Background(), req.BuildRequest)
	if err != nil {
		t.Fatal(err)
	}
	res.ResultBindingDigestSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	if _, err := BuildResultDigest(res); err == nil {
		t.Fatal("expected BuildResultDigest to validate first and reject a forged result")
	}
}

func wantVCode(t *testing.T, err error, code string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError %s, got %T: %v", code, err, err)
	}
	if ve.Code != code {
		t.Fatalf("expected %s, got %s (%s)", code, ve.Code, ve.Detail)
	}
}
