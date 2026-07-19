// SPDX-License-Identifier: AGPL-3.0-only

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
