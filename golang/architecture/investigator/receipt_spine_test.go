// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func TestValidateAcceptsCompleteReceiptSpine(t *testing.T) {
	res, snap := validReceiptResult(t, false)
	if err := Validate(res, snap); err != nil {
		t.Fatalf("complete receipt must validate: %v", err)
	}
}

func TestValidateRefusesReceiptSemanticSpineMutations(t *testing.T) {
	base, snap := validReceiptResult(t, false)

	tests := []struct {
		name   string
		want   string
		mutate func(*Result)
	}{
		{
			name: "schema mismatch",
			want: "receipt schema version",
			mutate: func(res *Result) {
				res.Receipt.SchemaVersion = "other.schema.v1"
			},
		},
		{
			name: "generator identity mismatch",
			want: "receipt generated-by identity",
			mutate: func(res *Result) {
				res.Receipt.GeneratedBy = "other.generator"
			},
		},
		{
			name: "generator version mismatch",
			want: "receipt generator version",
			mutate: func(res *Result) {
				res.Receipt.GeneratorVersion = "generator.v2"
			},
		},
		{
			name: "ruleset version mismatch",
			want: "receipt ruleset version",
			mutate: func(res *Result) {
				res.Receipt.RulesetVersion = "ruleset.v2"
			},
		},
		{
			name: "candidate index absent",
			want: "receipt candidate semantic index is required",
			mutate: func(res *Result) {
				res.Receipt.CandidateIDsAndDigests = nil
			},
		},
		{
			name: "candidate index extra entry",
			want: "receipt candidate semantic index has unexpected",
			mutate: func(res *Result) {
				res.Receipt.CandidateIDsAndDigests["candidate_extra"] = SHA256String("extra")
			},
		},
		{
			name: "challenge index absent",
			want: "receipt challenge semantic index is required",
			mutate: func(res *Result) {
				res.Receipt.ChallengeIDsAndDigests = nil
			},
		},
		{
			name: "counterexample index absent",
			want: "receipt counterexample semantic index is required",
			mutate: func(res *Result) {
				res.Receipt.CounterexampleIDsAndDigests = nil
			},
		},
		{
			name: "evidence request index absent",
			want: "receipt evidence request semantic index is required",
			mutate: func(res *Result) {
				res.Receipt.EvidenceRequestIDsAndDigests = nil
			},
		},
		{
			name: "ranking digest mismatch",
			want: "receipt ranking digest",
			mutate: func(res *Result) {
				res.Receipt.RankingDigestSHA256 = SHA256String("wrong ranking")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := cloneReceiptResult(base)
			tt.mutate(&res)
			err := Validate(res, snap)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected refusal containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestValidateRefusesMissingAndStaleCandidateSemanticEntries(t *testing.T) {
	base, snap := validReceiptResult(t, true)
	candidateID := base.Candidates[0].CandidateID

	t.Run("missing", func(t *testing.T) {
		res := cloneReceiptResult(base)
		delete(res.Receipt.CandidateIDsAndDigests, candidateID)
		err := Validate(res, snap)
		if err == nil || !strings.Contains(err.Error(), "receipt candidate semantic index is missing") {
			t.Fatalf("missing candidate digest must be refused, got %v", err)
		}
	})

	t.Run("claim changed", func(t *testing.T) {
		res := cloneReceiptResult(base)
		res.Document.CandidateClaims[0].Statement.Object = "strictly_positive"
		err := Validate(res, snap)
		if err == nil || !strings.Contains(err.Error(), "receipt candidate semantic digest") {
			t.Fatalf("stale candidate digest must be refused, got %v", err)
		}
	})
}

func TestCandidateIDUsesExtendedDigestPrefix(t *testing.T) {
	binding := Binding{
		Repository: architecture.ClaimDocumentBinding{
			RepositoryDomain: "example/repo",
			Revision:         "abc",
		},
		GraphDigestSHA256:            SHA256String("graph"),
		EvidenceSnapshotDigestSHA256: SHA256String("evidence"),
	}
	id, digest, err := ComputeCandidateID("v1", binding, "proposition", architecture.ClaimScope{}, KindInvariant, "generator.v1")
	if err != nil {
		t.Fatal(err)
	}
	prefix := "candidate_invariant_"
	if !strings.HasPrefix(id, prefix) {
		t.Fatalf("unexpected candidate ID prefix: %s", id)
	}
	if got := len(strings.TrimPrefix(id, prefix)); got != candidateIDDigestPrefixLength {
		t.Fatalf("candidate ID digest prefix length = %d, want %d", got, candidateIDDigestPrefixLength)
	}
	if !strings.HasSuffix(id, digest[:candidateIDDigestPrefixLength]) {
		t.Fatalf("candidate ID %q does not carry the expected digest prefix", id)
	}
}

func validReceiptResult(t *testing.T, withCandidate bool) (Result, GroundingSnapshot) {
	t.Helper()

	digest := func(label string) string { return SHA256String(label) }
	repository := architecture.ClaimDocumentBinding{
		RepositoryDomain:  "example/repo",
		Revision:          "abc123",
		RevisionStatus:    architecture.RevisionResolved,
		GraphDigestSHA256: digest("graph"),
		GraphDigestStatus: architecture.GraphDigestResolved,
	}
	snap := GroundingSnapshot{}
	groundingDigest, err := GroundingSnapshotDigest(snap)
	if err != nil {
		t.Fatal(err)
	}

	binding := Binding{
		Repository:                    repository,
		HowDocumentDigestSHA256:       digest("how"),
		WhyDocumentDigestSHA256:       digest("why"),
		GraphDigestSHA256:             repository.GraphDigestSHA256,
		CurrentClaimsDigestSHA256:     digest("claims"),
		ClosureStateDigestSHA256:      digest("closure"),
		ExistingQuestionsDigestSHA256: digest("questions"),
		ReviewHistoryDigestSHA256:     digest("review"),
		EvidenceSnapshotDigestSHA256:  digest("evidence snapshot"),
		GroundingSnapshotDigestSHA256: groundingDigest,
		GeneratorVersion:              "generator.v1",
		RulesetVersion:                "ruleset.v1",
	}

	res := Result{
		SchemaVersion: "investigator.result.v1",
		GeneratedBy:   "sensei.investigator.test",
		Binding:       binding,
		Document: investigation.Document{
			SchemaVersion: "investigation.schema.v1",
			GeneratedBy:   "sensei.investigation.test",
			Mode:          investigation.ModeWhy,
			Binding: investigation.Binding{
				Repository:                   repository,
				EvidenceSnapshotDigestSHA256: binding.EvidenceSnapshotDigestSHA256,
				Why: investigation.WhyBinding{
					HowDocumentDigestSHA256: binding.HowDocumentDigestSHA256,
				},
			},
		},
		Candidates:       []CandidateEnvelope{},
		Challenges:       []ChallengeReceipt{},
		EvidenceRequests: []EvidenceRequest{},
		Rankings:         []RankingRecord{},
		Counterexamples:  []CounterexampleRecord{},
	}

	if withCandidate {
		claim := architecture.Claim{
			ID:    "claim.test_candidate",
			Label: "Test candidate",
			Statement: architecture.ClaimStatement{
				Subject:   "account",
				Predicate: "must_remain",
				Object:    "non_negative",
			},
			Scope: architecture.ClaimScope{
				Repository: repository.RepositoryDomain,
			},
			ArchitecturalPlane:  architecture.PlaneIntended,
			AssertionOrigin:     architecture.OriginAuthored,
			EpistemicStatus:     architecture.StatusUnknown,
			SupportingEvidence:  []string{"evidence:test"},
			HumanReviewRequired: true,
			PromotionStatus:     architecture.PromotionCandidate,
		}
		candidateID, _, candidateErr := ComputeCandidateID(
			res.SchemaVersion,
			binding,
			"account must remain non-negative",
			claim.Scope,
			KindInvariant,
			binding.GeneratorVersion,
		)
		if candidateErr != nil {
			t.Fatal(candidateErr)
		}
		res.Document.CandidateClaims = []architecture.Claim{claim}
		res.Candidates = []CandidateEnvelope{{
			CandidateID:             candidateID,
			ClaimID:                 claim.ID,
			OutputKind:              KindInvariant,
			FalsificationConditions: []string{"a negative balance is observed"},
		}}
	}

	semantic, err := ComputeReceiptSemanticDigests(res)
	if err != nil {
		t.Fatal(err)
	}
	res.Receipt = RunReceipt{
		SchemaVersion:                 res.SchemaVersion,
		GeneratedBy:                   res.GeneratedBy,
		InputBinding:                  binding,
		GroundingSnapshotDigestSHA256: groundingDigest,
		HowDocumentDigestSHA256:       binding.HowDocumentDigestSHA256,
		WhyDocumentDigestSHA256:       binding.WhyDocumentDigestSHA256,
		GraphDigestSHA256:             repository.GraphDigestSHA256,
		CurrentClaimsDigestSHA256:     binding.CurrentClaimsDigestSHA256,
		ClosureStateDigestSHA256:      binding.ClosureStateDigestSHA256,
		ExistingQuestionsDigestSHA256: binding.ExistingQuestionsDigestSHA256,
		ReviewHistoryDigestSHA256:     binding.ReviewHistoryDigestSHA256,
		GeneratorVersion:              binding.GeneratorVersion,
		RulesetVersion:                binding.RulesetVersion,
		CandidateIDsAndDigests:        semantic.CandidateIDsAndDigests,
		ChallengeIDsAndDigests:        semantic.ChallengeIDsAndDigests,
		CounterexampleIDsAndDigests:   semantic.CounterexampleIDsAndDigests,
		EvidenceRequestIDsAndDigests:  semantic.EvidenceRequestIDsAndDigests,
		RankingDigestSHA256:           semantic.RankingDigestSHA256,
		TimestampSource:               "caller_provided",
		NondeterminismDeclaration:     "none",
	}
	res.Receipt.ExactResultDigestSHA256, err = ResultDigest(res)
	if err != nil {
		t.Fatal(err)
	}
	return res, snap
}

func cloneReceiptResult(in Result) Result {
	out := in
	out.Document.CandidateClaims = append([]architecture.Claim(nil), in.Document.CandidateClaims...)
	out.Candidates = append([]CandidateEnvelope(nil), in.Candidates...)
	out.Receipt.CandidateIDsAndDigests = cloneStringMap(in.Receipt.CandidateIDsAndDigests)
	out.Receipt.ChallengeIDsAndDigests = cloneStringMap(in.Receipt.ChallengeIDsAndDigests)
	out.Receipt.CounterexampleIDsAndDigests = cloneStringMap(in.Receipt.CounterexampleIDsAndDigests)
	out.Receipt.EvidenceRequestIDsAndDigests = cloneStringMap(in.Receipt.EvidenceRequestIDsAndDigests)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
