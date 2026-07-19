// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

var evalNow = mustTime("2026-07-15T20:00:00Z")

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// testProfile is a static_test test-evidence profile.
func testProfile() Profile {
	return Profile{
		ProfileID:            "profile.test.closure",
		Owner:                "component.closure",
		LegalObservationPath: "test_runner.go_test",
		EvidenceKind:         closureprotocol.EvidenceTest,
		Freshness:            "per-result",
		Trust:                "high",
		GovernedTarget:       "authority.closure",
		Status:               closureprotocol.ReceiptValid,
	}
}

func testReceipt() Receipt {
	return Receipt{
		ReceiptID:    "receipt.test.closure",
		EvidenceKind: closureprotocol.EvidenceTest,
		ProfileID:    "profile.test.closure",
		ResultBinding: ResultBinding{
			BaseRevision:           "base123",
			PatchDigestSHA256:      "patch123",
			ResultTreeDigestSHA256: "tree123",
			GraphDigestSHA256:      "graph123",
		},
		Producer:            "ci.local",
		ObservationPath:     "go_test",
		ObservedAt:          "2026-07-15T19:05:00Z",
		ExpiresAt:           "2026-07-16T19:05:00Z",
		Status:              closureprotocol.ReceiptValid,
		Trust:               "high",
		PayloadDigestSHA256: "payload123",
	}
}

func expectedResult() ResultBinding {
	return ResultBinding{
		BaseRevision:           "base123",
		PatchDigestSHA256:      "patch123",
		ResultTreeDigestSHA256: "tree123",
		GraphDigestSHA256:      "graph123",
	}
}

// runtimeProfile is a runtime-evidence profile (owner-only, cluster targeted).
func runtimeProfile() Profile {
	return Profile{
		ProfileID:            "profile.runtime.cluster",
		Owner:                "component.cluster",
		LegalObservationPath: "owner_rpc",
		EvidenceKind:         closureprotocol.EvidenceRuntime,
		Freshness:            "24h",
		Trust:                "high",
		GovernedTarget:       "authority.cluster",
		RuntimeTargetKind:    "cluster",
		Status:               closureprotocol.ReceiptValid,
	}
}

func runtimeReceipt() Receipt {
	r := testReceipt()
	r.ReceiptID = "receipt.runtime.cluster"
	r.EvidenceKind = closureprotocol.EvidenceRuntime
	r.ProfileID = "profile.runtime.cluster"
	r.ObservationPath = "owner_rpc"
	r.RuntimeTarget = &RuntimeTarget{
		Platform:                "globular",
		DeploymentID:            "clusterA",
		ConfigurationGeneration: "gen-7",
	}
	return r
}

func hasReason(codes []string, want string) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

func TestValidate(t *testing.T) {
	staleReceipt := testReceipt()
	staleReceipt.ExpiresAt = "2026-07-15T18:00:00Z" // before evalNow

	selfDeclaredProfile := testProfile()
	selfDeclaredProfile.Freshness = "self-declared"
	unobservedReceipt := testReceipt()
	unobservedReceipt.ObservedAt = ""
	unobservedReceipt.ExpiresAt = ""

	nonOwnerPath := testReceipt()
	nonOwnerPath.ObservationPath = "repository_edit"

	wrongRepo := testReceipt()
	wrongRepo.ResultBinding.BaseRevision = "otherbase"

	wrongTree := testReceipt()
	wrongTree.ResultBinding.ResultTreeDigestSHA256 = "othertree"

	wrongCluster := runtimeReceipt()
	wrongCluster.RuntimeTarget.DeploymentID = "clusterB"

	wrongGeneration := runtimeReceipt()
	wrongGeneration.RuntimeTarget.ConfigurationGeneration = "gen-99"

	noRuntime := runtimeReceipt()
	noRuntime.RuntimeTarget = nil

	profileMismatch := testReceipt()
	profileMismatch.ProfileID = "profile.other"

	malformed := testReceipt()
	malformed.PayloadDigestSHA256 = ""

	revoked := testReceipt()
	revoked.Status = closureprotocol.ReceiptRevoked

	cases := []struct {
		name    string
		req     ProofRequest
		receipt Receipt
		want    Status
		reason  string
	}{
		{
			name:    "valid static_test receipt",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: testReceipt(),
			want:    closureprotocol.ReceiptValid,
		},
		{
			name:    "receipt from wrong repository fails",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: wrongRepo,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonRepositoryMismatch,
		},
		{
			name:    "receipt against wrong result tree fails",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: wrongTree,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonResultTreeMismatch,
		},
		{
			name:    "runtime receipt from wrong cluster fails",
			req:     ProofRequest{Profile: runtimeProfile(), ExpectedResult: expectedResult(), RuntimeTarget: &RuntimeTarget{DeploymentID: "clusterA"}, Now: evalNow},
			receipt: wrongCluster,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonRuntimeTargetMismatch,
		},
		{
			name:    "runtime receipt from wrong generation fails",
			req:     ProofRequest{Profile: runtimeProfile(), ExpectedResult: expectedResult(), RuntimeTarget: &RuntimeTarget{ConfigurationGeneration: "gen-7"}, Now: evalNow},
			receipt: wrongGeneration,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonRuntimeTargetMismatch,
		},
		{
			name:    "runtime profile with missing runtime target fails",
			req:     ProofRequest{Profile: runtimeProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: noRuntime,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonRuntimeMissing,
		},
		{
			name:    "stale receipt blocks",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: staleReceipt,
			want:    closureprotocol.ReceiptStale,
			reason:  ReasonReceiptExpired,
		},
		{
			name:    "self-declared freshness without observed time blocks",
			req:     ProofRequest{Profile: selfDeclaredProfile, ExpectedResult: expectedResult(), Now: evalNow},
			receipt: unobservedReceipt,
			want:    closureprotocol.ReceiptUnknown,
			reason:  ReasonFreshnessUnobserved,
		},
		{
			name:    "non-owner-path receipt cannot satisfy owner-only profile",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: nonOwnerPath,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonOwnerPathViolation,
		},
		{
			name:    "profile id mismatch fails",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: profileMismatch,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonProfileMismatch,
		},
		{
			name:    "malformed receipt fails",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: malformed,
			want:    closureprotocol.ReceiptInvalid,
			reason:  ReasonReceiptMalformed,
		},
		{
			name:    "revoked receipt stays revoked",
			req:     ProofRequest{Profile: testProfile(), ExpectedResult: expectedResult(), Now: evalNow},
			receipt: revoked,
			want:    closureprotocol.ReceiptRevoked,
			reason:  ReasonReceiptRevoked,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Validate(tc.req, tc.receipt)
			if got.Status != tc.want {
				t.Fatalf("status = %q, want %q (reasons %v)", got.Status, tc.want, got.ReasonCodes)
			}
			if got.Status == closureprotocol.ReceiptValid && got.OK() != true {
				t.Fatalf("OK() should be true for valid")
			}
			if tc.reason != "" && !hasReason(got.ReasonCodes, tc.reason) {
				t.Fatalf("reason %q not in %v", tc.reason, got.ReasonCodes)
			}
			// Fail-closed law: nothing that is not literally valid may read as OK.
			if tc.want != closureprotocol.ReceiptValid && got.OK() {
				t.Fatalf("non-valid assessment reported OK()")
			}
		})
	}
}

func TestDurationFreshnessExpiryDerived(t *testing.T) {
	// Duration freshness with no explicit expiry: derive observed+window.
	p := testProfile()
	p.Freshness = "1h"
	r := testReceipt()
	r.ObservedAt = "2026-07-15T18:00:00Z" // +1h = 19:00, before evalNow 20:00 -> stale
	r.ExpiresAt = ""
	got := Validate(ProofRequest{Profile: p, ExpectedResult: expectedResult(), Now: evalNow}, r)
	if got.Status != closureprotocol.ReceiptStale {
		t.Fatalf("status = %q, want stale", got.Status)
	}
}

func TestDetectConflicts(t *testing.T) {
	a := testReceipt()
	a.ReceiptID = "receipt.a"
	b := testReceipt()
	b.ReceiptID = "receipt.b"
	b.PayloadDigestSHA256 = "payload999" // same subject, different observation

	conflicts := DetectConflicts([]Receipt{a, b})
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].Reason != ReasonOwnerPathConflicted {
		t.Fatalf("unexpected conflict reason %q", conflicts[0].Reason)
	}

	// Agreeing receipts (identical payload) do not conflict.
	c := testReceipt()
	c.ReceiptID = "receipt.c"
	if got := DetectConflicts([]Receipt{a, c}); len(got) != 0 {
		t.Fatalf("agreeing receipts should not conflict, got %d", len(got))
	}
}

func TestDeterministicDigest(t *testing.T) {
	r := testReceipt()
	r.Conflicts = []string{"z", "a", "m"}

	d1, err := Digest(r)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := Digest(r)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("digest not stable: %s != %s", d1, d2)
	}

	// Conflict ordering must not change the digest.
	reordered := testReceipt()
	reordered.Conflicts = []string{"a", "m", "z"}
	d3, err := Digest(reordered)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d3 {
		t.Fatalf("digest sensitive to conflict order: %s != %s", d1, d3)
	}
}
