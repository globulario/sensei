// SPDX-License-Identifier: AGPL-3.0-only

package questiongen

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
)

// evidenceCtx is a context whose single blocker asks for evidence about one
// derived observed claim, with the premise receipt the claim cites.
func evidenceCtx(t *testing.T, mutate func(*architecture.Claim, *architecture.ClaimFactReceipt)) Context {
	t.Helper()
	ctx := questionGenContext()
	fact := architecture.Fact{
		ID: "fact.abc123", Kind: "write", Subject: "doctor.Configured", Predicate: "writes",
		Object: "cluster/state", Extractor: "go_semantic_extractor", Confidence: 0.5,
		Evidence: architecture.Evidence{SourceFile: "internal/doctor/doctor.go", LineStart: 12, LineEnd: 12},
	}
	receipt := architecture.ClaimFactReceipt{
		Fact: fact,
		Provenance: architecture.Provenance{
			RepositoryDomain: "repo", RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			Revision: "0123456789abcdef", RevisionStatus: architecture.RevisionResolved,
			SourceDigest: strings.Repeat("c", 64), SourceDigestStatus: architecture.SourceDigestResolved,
			SourceKind: "source_file",
		},
	}
	claim := architecture.Claim{
		ID: "claim.self.evidenced", ArchitecturalPlane: architecture.PlaneObserved,
		AssertionOrigin: architecture.OriginDerived, PremiseFacts: []string{fact.ID},
		Statement: architecture.ClaimStatement{Subject: "cluster/state", Predicate: "has_observed_writer_set", Object: "doctor.Configured"},
	}
	if mutate != nil {
		mutate(&claim, &receipt)
	}
	ctx.Claims.Claims = []architecture.Claim{claim}
	ctx.Claims.FactReceipts = []architecture.ClaimFactReceipt{receipt}
	ctx.Closure.Blockers = []closure.Blocker{{
		ID: "blocker.evidence.abcdef012345", Dimension: closure.DimensionEvidence,
		Severity: architecture.QuestionPriorityHigh, Code: "closure.evidence.claim_unknown",
		Summary: "Claim has no evidence.", ClaimIDs: []string{claim.ID},
		Files: []string{"internal/doctor/doctor.go"}, RequiredNextAction: "add_evidence",
	}}
	return ctx
}

// #230 item 2: asking a human for evidence about a claim whose every premise is
// already source-backed has exactly one possible answer — read the source the
// extractor already read — and the unanswered question blocks closure.
func TestQuestionIsNotRaisedWhenTheClaimCarriesItsOwnEvidence(t *testing.T) {
	report, err := generateReport(t, evidenceCtx(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Generated) != 0 {
		t.Fatalf("a self-evidenced claim produced a question: %+v", report.Generated)
	}
	if len(report.Skipped) != 1 || report.Skipped[0].Disposition != DispositionSelfEvidenced {
		t.Fatalf("the skip was not recorded as self-evidenced: %+v", report.Skipped)
	}
	if !strings.Contains(report.Skipped[0].Detail, "source the extractor already read") {
		t.Fatalf("the detail does not say why nobody can answer it: %q", report.Skipped[0].Detail)
	}
}

// Narrow in four ways, because suppressing a real question is a worse failure
// than asking an awkward one. Each of these must still be asked.
func TestQuestionsThatAreStillAsked(t *testing.T) {
	cases := []struct {
		name string
		ctx  func(t *testing.T) Context
	}{
		{"an authored claim is somebody's assertion, answerable by them", func(t *testing.T) Context {
			return evidenceCtx(t, func(c *architecture.Claim, _ *architecture.ClaimFactReceipt) {
				c.AssertionOrigin = architecture.OriginAuthored
			})
		}},
		{"an intended claim is about what should be, which source cannot settle", func(t *testing.T) Context {
			return evidenceCtx(t, func(c *architecture.Claim, _ *architecture.ClaimFactReceipt) {
				c.ArchitecturalPlane = architecture.PlaneIntended
			})
		}},
		{"a premise whose digest is unresolved reaches past the source", func(t *testing.T) Context {
			return evidenceCtx(t, func(_ *architecture.Claim, r *architecture.ClaimFactReceipt) {
				r.Provenance.SourceDigestStatus = architecture.SourceDigestUnavailable
			})
		}},
		{"a premise that is not source-backed", func(t *testing.T) Context {
			return evidenceCtx(t, func(_ *architecture.Claim, r *architecture.ClaimFactReceipt) {
				r.Provenance.SourceKind = "git_history"
			})
		}},
		{"a claim with no premises at all", func(t *testing.T) Context {
			return evidenceCtx(t, func(c *architecture.Claim, _ *architecture.ClaimFactReceipt) {
				c.PremiseFacts = nil
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report, err := generateReport(t, tc.ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range report.Skipped {
				if s.Disposition == DispositionSelfEvidenced {
					t.Fatalf("this question was suppressed as self-evidenced: %+v", s)
				}
			}
		})
	}
}

// The suppression must never widen past questions that ask for a POINTER. An
// authority, contract or direction question asks a human to DECIDE, which no
// extraction answers.
func TestOnlyEvidenceBlockersCanBeSelfEvidenced(t *testing.T) {
	deciding := []string{
		"closure.authority.owner_missing", "closure.contract.crossing_without_contract",
		"closure.direction.desired_missing", "closure.scope.undefined",
	}
	for _, code := range deciding {
		if evidenceBlockerCode(code) {
			t.Errorf("%q asks for a decision, not a pointer", code)
		}
	}
	for _, code := range []string{"closure.evidence.claim_unknown", "closure.agent.required_test_unidentified"} {
		if !evidenceBlockerCode(code) {
			t.Errorf("%q asks for evidence and was not recognised", code)
		}
	}
}

func generateReport(t *testing.T, ctx Context) (Report, error) {
	t.Helper()
	res, err := Generate(ctx, nil)
	return res.Report, err
}
