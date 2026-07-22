// SPDX-License-Identifier: AGPL-3.0-only

package deviation

import (
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

func TestRecordIsDeterministicAndFailClosed(t *testing.T) {
	input := deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z")
	first, err := Record(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Record(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same exact event produced different receipts:\n%+v\n%+v", first, second)
	}
	tampered := first
	tampered.ObservedBehavior = "different behavior after receipt"
	if err := ValidateReceipt(tampered); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("tampered receipt must fail closed, got %v", err)
	}
	invalid := input
	invalid.Kind = "invented"
	if _, err := Record(invalid); err == nil || !strings.Contains(err.Error(), "unknown deviation kind") {
		t.Fatalf("unknown kind must be refused, got %v", err)
	}
}

func TestClusterCountsIndependentOccurrencesOnly(t *testing.T) {
	first := mustRecord(t, deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z"))
	second := mustRecord(t, deviationInput("task.two", "session.two", "change.two", "2026-07-22T13:00:00Z"))
	patterns, err := Cluster([]Receipt{second, first, first}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 {
		t.Fatalf("expected one pattern, got %d", len(patterns))
	}
	pattern := patterns[0]
	if pattern.IndependentOccurrenceCount != 2 || !pattern.CandidateEligible {
		t.Fatalf("duplicate retry changed recurrence: %+v", pattern)
	}
	if len(pattern.ReceiptIDs) != 2 || len(pattern.IndependenceKeys) != 2 {
		t.Fatalf("pattern did not preserve exact independent receipts: %+v", pattern)
	}
}

func TestClusterRefusesOneOccurrenceKeyBoundToDifferentEvents(t *testing.T) {
	firstInput := deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z")
	first := mustRecord(t, firstInput)
	secondInput := firstInput
	secondInput.Observed = "the same task and change claims a different deviation event"
	secondInput.SourceDigest = digest("different source")
	second := mustRecord(t, secondInput)
	if first.IndependenceKey != second.IndependenceKey || first.ID == second.ID {
		t.Fatal("fixture did not create conflicting events under one occurrence key")
	}
	if _, err := Cluster([]Receipt{first, second}, 2); err == nil || !strings.Contains(err.Error(), "binds multiple") {
		t.Fatalf("conflicting occurrence reuse must fail closed, got %v", err)
	}
}

func TestAnalyzeCreatesAdvisoryCandidateWithoutWeakeningArchitecture(t *testing.T) {
	binding := deviationBinding()
	existing := architecture.Claim{
		ID:                  "claim.existing.boundary",
		Statement:           architecture.ClaimStatement{Subject: "component.api", Predicate: "must_use_owner_path", Object: "component.store"},
		Scope:               architecture.ClaimScope{Repository: binding.RepositoryDomain, Components: []string{"component.api", "component.store"}},
		ArchitecturalPlane:  architecture.PlaneIntended,
		AssertionOrigin:     architecture.OriginDerived,
		EpistemicStatus:     architecture.StatusSupported,
		InferenceRule:       "fixture.rule.v1",
		SupportingEvidence:  []string{"evidence:existing"},
		Confidence:          1,
		HumanReviewRequired: true,
		PromotionStatus:     architecture.PromotionCandidate,
	}
	before := existing
	firstInput := deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z")
	secondInput := deviationInput("task.two", "session.two", "change.two", "2026-07-22T13:00:00Z")
	firstInput.RelatedClaims = []string{existing.ID}
	secondInput.RelatedClaims = []string{existing.ID}
	analysis, err := Analyze(binding, []Receipt{mustRecord(t, secondInput), mustRecord(t, firstInput)}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Candidates) != 1 {
		t.Fatalf("expected one repeated-deviation candidate, got %d", len(analysis.Candidates))
	}
	candidate := analysis.Candidates[0]
	if candidate.Claim.Confidence != 0 || !candidate.Claim.HumanReviewRequired || candidate.Claim.PromotionStatus != architecture.PromotionCandidate {
		t.Fatalf("candidate acquired authority: %+v", candidate.Claim)
	}
	if !containsString(candidate.Claim.ConflictsWith, existing.ID) || candidate.Claim.EpistemicStatus != architecture.StatusContested {
		t.Fatalf("candidate did not explicitly contest the related claim: %+v", candidate.Claim)
	}
	if !reflect.DeepEqual(existing, before) {
		t.Fatal("deviation analysis mutated the existing architectural claim")
	}
}

func TestAnalyzeIsOrderIndependentAndOneOccurrenceStaysLocalEvidence(t *testing.T) {
	binding := deviationBinding()
	first := mustRecord(t, deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z"))
	second := mustRecord(t, deviationInput("task.two", "session.two", "change.two", "2026-07-22T13:00:00Z"))
	left, err := Analyze(binding, []Receipt{first, second}, 2)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Analyze(binding, []Receipt{second, first}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatal("receipt order changed normalized deviation analysis")
	}
	digestLeft, _ := AnalysisDigest(left)
	digestRight, _ := AnalysisDigest(right)
	if digestLeft != digestRight {
		t.Fatal("receipt order changed exact analysis digest")
	}
	local, err := Analyze(binding, []Receipt{first}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(local.Patterns) != 1 || local.Patterns[0].CandidateEligible || len(local.Candidates) != 0 {
		t.Fatalf("one deviation must remain local evidence: %+v", local)
	}
}

func TestAnalysisReceiptIndexesAreMandatoryAndExact(t *testing.T) {
	binding := deviationBinding()
	first := mustRecord(t, deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z"))
	second := mustRecord(t, deviationInput("task.two", "session.two", "change.two", "2026-07-22T13:00:00Z"))
	analysis, err := Analyze(binding, []Receipt{first, second}, 2)
	if err != nil {
		t.Fatal(err)
	}
	delete(analysis.Receipt.CandidateIDsAndDigests, analysis.Candidates[0].ID)
	if err := ValidateAnalysis(analysis); err == nil || !strings.Contains(err.Error(), "semantic indexes") {
		t.Fatalf("missing candidate receipt index must fail, got %v", err)
	}
}

func TestPatternIdentityIgnoresOccurrenceMetadataButBindsScope(t *testing.T) {
	first := mustRecord(t, deviationInput("task.one", "session.one", "change.one", "2026-07-22T12:00:00Z"))
	secondInput := deviationInput("task.two", "session.two", "change.two", "2026-07-22T13:00:00Z")
	secondInput.AgentID = "another-agent"
	second := mustRecord(t, secondInput)
	patterns, err := Cluster([]Receipt{first, second}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 {
		t.Fatal("occurrence metadata fragmented one structured pattern")
	}
	otherInput := deviationInput("task.three", "session.three", "change.three", "2026-07-22T14:00:00Z")
	otherInput.Scope.Files = []string{"another.go"}
	other := mustRecord(t, otherInput)
	patterns, err = Cluster([]Receipt{first, second, other}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 2 {
		t.Fatalf("scope change must create a distinct pattern, got %d", len(patterns))
	}
}

func deviationInput(taskID, sessionID, changeLabel, recordedAt string) RecordInput {
	binding := deviationBinding()
	return RecordInput{
		Kind:    KindBypassedOwnerPath,
		Binding: binding,
		Scope: architecture.ClaimScope{
			Repository: binding.RepositoryDomain,
			Files:      []string{"api.go", "store.go"},
			Symbols:    []string{"api.Write", "store.Apply"},
			Components: []string{"component.api", "component.store"},
		},
		Shape:         Shape{Subject: "component.api", Predicate: "bypassed_owner_path", Object: "component.store"},
		Expected:      "mutate state through the governed store owner",
		Observed:      "implementation wrote state through a non-owner path",
		TaskID:        taskID,
		TaskSessionID: sessionID,
		AgentID:       "codex",
		ChangeDigest:  digest(changeLabel),
		SourceDigest:  digest("source:" + changeLabel),
		EvidenceRefs:  []string{"evidence:" + strings.ReplaceAll(changeLabel, ".", "_")},
		RecordedAt:    recordedAt,
		Timestamp:     "fixture",
	}
}

func deviationBinding() architecture.ClaimDocumentBinding {
	return architecture.ClaimDocumentBinding{
		RepositoryDomain:  "example/repo",
		Revision:          "abc123",
		RevisionStatus:    architecture.RevisionResolved,
		TreeDigestSHA256:  digest("tree"),
		GraphDigestSHA256: digest("graph"),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
}

func mustRecord(t *testing.T, input RecordInput) Receipt {
	t.Helper()
	receipt, err := Record(input)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func digest(value string) string { return sha256String(value) }

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
