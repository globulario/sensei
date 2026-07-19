// SPDX-License-Identifier: Apache-2.0

package briefingfeedback

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildUnavailable_AllReasonsAreValidProjections(t *testing.T) {
	for _, reason := range []UnavailableReason{
		RepositoryContextAbsent, RepositoryContextDomainMismatch,
		CanonicalTaskScopeNotEstablished, FeedbackProjectionInternalUnavailable,
	} {
		t.Run(string(reason), func(t *testing.T) {
			p, err := BuildUnavailable(Scope{RepositoryIdentity: testDomain, RequestedDomain: testDomain, RequestedFiles: []string{testFile}}, reason)
			if err != nil {
				t.Fatalf("BuildUnavailable(%s) error: %v", reason, err)
			}
			if verr := ValidateProjection(p); verr != nil {
				t.Fatalf("unavailable projection invalid: %v", verr)
			}
			if p.Availability != FeedbackUnavailable {
				t.Fatalf("availability = %q, want feedback_unavailable", p.Availability)
			}
			if len(p.Findings) != 1 || p.Findings[0].Class != PromotionDiscoveryUnavailable ||
				p.Findings[0].Disposition != DispositionUnavailable || p.Findings[0].ReasonCode != string(reason) {
				t.Fatalf("finding malformed: %+v", p.Findings)
			}
			if len(p.Records) != 0 {
				t.Fatalf("unavailable projection must carry no records")
			}
			if !p.NonAuthoritativeProjection {
				t.Fatalf("must be non-authoritative")
			}
		})
	}
}

func TestBuildUnavailable_UnknownReasonFailsClosed(t *testing.T) {
	if _, err := BuildUnavailable(Scope{RepositoryIdentity: testDomain}, UnavailableReason("made_up")); err == nil {
		t.Fatal("unknown unavailable reason must fail closed")
	}
	if _, err := BuildUnavailable(Scope{RepositoryIdentity: testDomain}, ""); err == nil {
		t.Fatal("zero reason must fail closed")
	}
}

func TestBuildUnavailable_AbsentRepositoryContextBlanksIdentity(t *testing.T) {
	// No configured repository → empty identity is a valid unavailable carrier.
	p, err := BuildUnavailable(Scope{RequestedDomain: testDomain, RequestedFiles: []string{testFile}}, RepositoryContextAbsent)
	if err != nil {
		t.Fatal(err)
	}
	if p.RepositoryIdentity != "" || p.RequestedDomain != "" {
		t.Fatalf("absent context must blank unestablished identity: %+v", p)
	}
	if p.Findings[0].AffectedDomain != testDomain {
		t.Fatalf("requested domain must survive as finding provenance")
	}
}

func TestBuildUnavailable_DeterministicAndNoLeaks(t *testing.T) {
	scope := Scope{RepositoryIdentity: testDomain, RequestedDomain: testDomain, RequestedFiles: []string{testFile}}
	a, _ := BuildUnavailable(scope, RepositoryContextDomainMismatch)
	b, _ := BuildUnavailable(scope, RepositoryContextDomainMismatch)
	if a.DigestSHA256 != b.DigestSHA256 {
		t.Fatal("unavailable projection is not deterministic")
	}
	blob, _ := json.Marshal(a)
	if strings.Contains(string(blob), "/") && strings.Contains(string(blob), "\\") {
		t.Fatalf("unexpected path shape in projection")
	}
}

func TestBuildUnavailable_TaskHonoredOnlyWhenCoherent(t *testing.T) {
	// Exact coherent task binding is honored.
	ok, err := BuildUnavailable(Scope{
		RepositoryIdentity: testDomain, RequestedDomain: testDomain,
		Task: &TaskBinding{TaskID: "t", SessionID: "s", RepositoryDomain: testDomain},
	}, FeedbackProjectionInternalUnavailable)
	if err != nil {
		t.Fatal(err)
	}
	if ok.TaskID != "t" || ok.SessionID != "s" {
		t.Fatalf("coherent task binding must be honored: %+v", ok)
	}
	// A half/incoherent binding is never stamped (no fabricated identity).
	bad, err := BuildUnavailable(Scope{
		RepositoryIdentity: testDomain, RequestedDomain: testDomain,
		Task: &TaskBinding{TaskID: "t", SessionID: ""},
	}, CanonicalTaskScopeNotEstablished)
	if err != nil {
		t.Fatal(err)
	}
	if bad.TaskID != "" || bad.SessionID != "" {
		t.Fatalf("incoherent task binding must not be stamped: %+v", bad)
	}
}

func TestBuildInvalid_ReasonsAndFailClosed(t *testing.T) {
	for _, reason := range []InvalidReason{RequestedFileNoncanonical, RequestedDomainNoncanonical} {
		p, err := BuildInvalid(Scope{RepositoryIdentity: testDomain, RequestedDomain: testDomain}, reason)
		if err != nil {
			t.Fatalf("BuildInvalid(%s): %v", reason, err)
		}
		if verr := ValidateProjection(p); verr != nil {
			t.Fatalf("invalid projection not canonical: %v", verr)
		}
		if p.Availability != FeedbackInvalid || len(p.Findings) != 1 ||
			p.Findings[0].Class != PromotionScopeIdentityInvalid || p.Findings[0].Disposition != DispositionExcluded ||
			p.Findings[0].ReasonCode != string(reason) {
			t.Fatalf("finding malformed: %+v", p.Findings)
		}
	}
	if _, err := BuildInvalid(Scope{RepositoryIdentity: testDomain}, "made_up"); err == nil {
		t.Fatal("unknown invalid reason must fail closed")
	}
}

func TestBuildInvalid_SerializesNoRawUnsafeIdentity(t *testing.T) {
	// A caller may pass unsafe raw values in scope; the carrier must drop/blank them.
	p, err := BuildInvalid(Scope{RepositoryIdentity: testDomain, RequestedDomain: "pad ded", RequestedFiles: []string{"../escape"}}, RequestedFileNoncanonical)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.RequestedFiles) != 0 || p.RequestedDomain != "" {
		t.Fatalf("unsafe raw identity leaked: files=%v domain=%q", p.RequestedFiles, p.RequestedDomain)
	}
}

// The unavailable identity matrix: only repository_context_absent may blank the identity.
func TestValidateProjection_UnavailableIdentityMatrix(t *testing.T) {
	// Positive: the exact repository_context_absent carrier (blank identity) is valid.
	absent, err := BuildUnavailable(Scope{RequestedDomain: testDomain}, RepositoryContextAbsent)
	if err != nil {
		t.Fatalf("repository_context_absent carrier must be valid: %v", err)
	}
	if absent.RepositoryIdentity != "" {
		t.Fatalf("absent carrier should blank identity")
	}

	// Positive: every other unavailable reason with a canonical non-empty identity is valid.
	for _, reason := range []UnavailableReason{RepositoryContextDomainMismatch, CanonicalTaskScopeNotEstablished, FeedbackProjectionInternalUnavailable} {
		p, err := BuildUnavailable(Scope{RepositoryIdentity: testDomain, RequestedDomain: testDomain}, reason)
		if err != nil {
			t.Fatalf("%s with identity must be valid: %v", reason, err)
		}
		if p.RepositoryIdentity != testDomain {
			t.Fatalf("%s must keep the established identity", reason)
		}
	}

	// Negative: an unavailable projection with a blank identity but a NON-absent reason is
	// rejected (a manually assembled projection cannot erase established identity).
	bad := absent
	bad.Findings = append([]Finding(nil), absent.Findings...)
	bad.Findings[0].ReasonCode = string(RepositoryContextDomainMismatch)
	bad.DigestSHA256 = ""
	dig, _ := ComputeDigest(bad)
	bad.DigestSHA256 = dig
	if ValidateProjection(bad) == nil {
		t.Fatal("blank-identity unavailable with a non-absent reason must be rejected")
	}

	// Negative: an unavailable projection with a blank identity carrying a verified record is rejected.
	bad2 := absent
	bad2.Records = []VerifiedRecord{{PromotionLineageID: "x", GovernedNodeIRI: "aw:x", VerificationClass: PromotionVerified, ProvenanceInterpretation: provenanceInterpretation, EffectiveFileScope: []string{"a/b.go"}, CanonicalRecordID: "r", PromotionReceiptDigestSHA256: "d", QuestionID: "q", AnswerID: "a", DispositionReceiptDigestSHA256: "dd", OriginatingTaskID: "t", OriginatingSessionID: "s"}}
	bad2.DigestSHA256 = ""
	dig2, _ := ComputeDigest(bad2)
	bad2.DigestSHA256 = dig2
	if ValidateProjection(bad2) == nil {
		t.Fatal("blank-identity unavailable with a record must be rejected")
	}
}
