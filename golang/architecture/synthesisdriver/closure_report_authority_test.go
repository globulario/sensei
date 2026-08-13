// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func TestClosureReportAuthorityBlocksIncompleteInterpretationSurface(t *testing.T) {
	report := testClosureReport([]string{"a.go", "b.go"})
	authority, err := NewClosureReportAuthority(ClosureReportAuthorityConfig{Report: report})
	if err != nil {
		t.Fatal(err)
	}
	request := testInterpretationAuthorityRequest(t, report, []string{"file:a.go"}, nil, nil)
	receipt, err := authority.Assess(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Authority != interpretationclosure.AuthorityAdvisory {
		t.Fatalf("authority=%q blockers=%v", receipt.Authority, receipt.Blockers)
	}
	if receipt.Completeness.Status != interpretationclosure.CompletenessIncomplete || len(receipt.Completeness.MissingSurface) != 1 || receipt.Completeness.MissingSurface[0] != "b.go" {
		t.Fatalf("completeness=%+v", receipt.Completeness)
	}
}

func TestClosureReportAuthorityRefutedCanonicalClaimBlocksButUnknownDoesNot(t *testing.T) {
	report := testClosureReport([]string{"a.go"})
	report.RelevantClaims = []closure.ClaimReceipt{{
		ID:                 "invariant.refuted",
		PropositionKey:     "prop.refuted",
		ArchitecturalPlane: "observed",
		EpistemicStatus:    architecture.StatusRefuted,
	}}
	authority, err := NewClosureReportAuthority(ClosureReportAuthorityConfig{Report: report})
	if err != nil {
		t.Fatal(err)
	}
	request := testInterpretationAuthorityRequest(t, report, []string{"file:a.go"}, []string{"invariant.refuted", "invariant.no-checker"}, nil)
	receipt, err := authority.Assess(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Authority != interpretationclosure.AuthorityAdvisory {
		t.Fatalf("refuted claim did not block: authority=%q blockers=%v", receipt.Authority, receipt.Blockers)
	}
	statuses := map[string]interpretationclosure.TruthStatus{}
	for _, finding := range receipt.TruthFindings {
		statuses[finding.ClaimID] = finding.Status
	}
	if statuses["invariant.refuted"] != interpretationclosure.TruthContradicted {
		t.Fatalf("refuted claim status=%q", statuses["invariant.refuted"])
	}
	if statuses["invariant.no-checker"] != interpretationclosure.TruthUnknown {
		t.Fatalf("unsupported claim status=%q, want unknown", statuses["invariant.no-checker"])
	}
}

func TestClosureReportAuthorityUnresolvedDeclaredProofCannotGovern(t *testing.T) {
	report := testClosureReport([]string{"a.go"})
	authority, err := NewClosureReportAuthority(ClosureReportAuthorityConfig{Report: report})
	if err != nil {
		t.Fatal(err)
	}
	request := testInterpretationAuthorityRequest(t, report, []string{"file:a.go"}, nil, []string{"proof.required"})
	receipt, err := authority.Assess(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Authority != interpretationclosure.AuthorityAdvisory || len(receipt.ProofObservations) != 1 || receipt.ProofObservations[0].Status != interpretationclosure.ProofUnresolved {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestClosureReportAuthorityRejectsClosureReportNotBoundToSession(t *testing.T) {
	report := testClosureReport([]string{"a.go"})
	authority, err := NewClosureReportAuthority(ClosureReportAuthorityConfig{Report: report})
	if err != nil {
		t.Fatal(err)
	}
	request := testInterpretationAuthorityRequest(t, report, []string{"file:a.go"}, nil, nil)
	request.Session.ClosureDigestSHA256 = strings.Repeat("f", 64)
	if _, err := authority.Assess(context.Background(), request); err == nil {
		t.Fatal("authority accepted a closure report not bound to the session")
	}
}

func testClosureReport(files []string) closure.Report {
	return closure.Report{
		SchemaVersion: closure.SchemaVersion,
		GeneratedBy:   closure.GeneratedBy,
		Verdict:       closure.VerdictClosed,
		ScopeReceipt: closure.ScopeReceipt{
			Files:               append([]string(nil), files...),
			RepresentedFiles:    []closure.FileRepresentationReceipt{},
			Symbols:             []string{},
			Components:          []string{},
			ClaimIDs:            []string{},
			PropositionKeys:     []string{},
			NodeIDs:             []string{},
			MissingFiles:        []string{},
			MissingSymbols:      []string{},
			MissingComponents:   []string{},
			MissingClaims:       []string{},
			MissingPropositions: []string{},
		},
		AuthorityBindings: []closure.AuthorityApplicabilityReceipt{},
		Dimensions:        []closure.DimensionAssessment{},
		Blockers:          []closure.Blocker{},
		Conditions:        []closure.Condition{},
		RelevantClaims:    []closure.ClaimReceipt{},
		RelevantNodes:     []closure.NodeReceipt{},
		Questions:         []closure.QuestionReceipt{},
		Limitations:       []architecture.Limitation{},
	}
}

func testInterpretationAuthorityRequest(t *testing.T, report closure.Report, references, invariants, proofs []string) InterpretationAuthorityRequest {
	t.Helper()
	closureDigest, err := closureprotocol.SemanticDigest(report)
	if err != nil {
		t.Fatal(err)
	}
	refs := make([]synthesis.SourceReference, 0, len(references))
	for _, reference := range references {
		refs = append(refs, synthesis.SourceReference{Reference: reference, SourceDigestSHA256: strings.Repeat("c", 64)})
	}
	interp := synthesis.NormalizeInterpretation(synthesis.Interpretation{
		SchemaVersion:            synthesis.InterpretationSchemaVersion,
		InterpretationID:         "interpretation.authority.test",
		SessionDigestSHA256:      strings.Repeat("a", 64),
		GeneratedBy:              synthesis.GeneratedBy,
		Objective:                "test interpretation closure",
		ApplicableIntent:         []string{},
		BindingInvariants:        append([]string(nil), invariants...),
		RelevantContracts:        []string{},
		AuthorityBoundaries:      []string{},
		KnownFailureModes:        []string{},
		ForbiddenFixes:           []string{},
		RequiredProofObligations: append([]string(nil), proofs...),
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{},
		SourceReferences:         refs,
		Limitations:              []synthesis.Limitation{},
	})
	interpDigest, err := synthesis.InterpretationDigest(interp)
	if err != nil {
		t.Fatal(err)
	}
	interp.InterpretationDigestSHA256 = interpDigest
	return InterpretationAuthorityRequest{
		RepositoryRoot: t.TempDir(),
		Session: synthesis.Session{
			SessionDigestSHA256:        interp.SessionDigestSHA256,
			BaseRevision:               "deadbeef",
			GraphAuthorityDigestSHA256: strings.Repeat("b", 64),
			ClosureDigestSHA256:        closureDigest,
		},
		Interpretation: interp,
	}
}
