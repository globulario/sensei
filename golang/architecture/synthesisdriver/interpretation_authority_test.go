// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
)

// testGoverningInterpretationAuthority is intentionally a test-only evidence
// owner. It lets O7 lifecycle tests exercise stages after interpretation
// promotion without pretending that their synthetic repositories contain a
// real task closure report. Dedicated interpretation-closure tests cover the
// blocking policy itself.
func testGoverningInterpretationAuthority() InterpretationAuthority {
	return InterpretationAuthorityFunc(func(_ context.Context, request InterpretationAuthorityRequest) (interpretationclosure.Receipt, error) {
		truth := make([]interpretationclosure.TruthFinding, 0, len(request.Interpretation.BindingInvariants))
		for _, claimID := range request.Interpretation.BindingInvariants {
			truth = append(truth, interpretationclosure.UnknownTruth(
				claimID,
				"test",
				"synthetic_fixture",
				"test fixture explicitly exercises the neutral Gate-1 path",
				"test-fixture:synthetic-truth-challenge",
			))
		}
		proofs := make([]interpretationclosure.ProofObservation, 0, len(request.Interpretation.RequiredProofObligations))
		for _, obligationID := range request.Interpretation.RequiredProofObligations {
			proofs = append(proofs, interpretationclosure.ProofObservation{
				ObligationID:         obligationID,
				RequiredForAuthority: true,
				Status:               interpretationclosure.ProofSatisfied,
				EvidenceReferences:   []string{"test-fixture:synthetic-proof-discharge"},
			})
		}
		return interpretationclosure.Certify(interpretationclosure.Input{
			InterpretationDigestSHA256: request.Interpretation.InterpretationDigestSHA256,
			RepositoryRevision:         request.Session.BaseRevision,
			GraphAuthorityDigestSHA256: request.Session.GraphAuthorityDigestSHA256,
			ClosureDigestSHA256:        request.Session.ClosureDigestSHA256,
			TruthFindings:              truth,
			Completeness: interpretationclosure.CompletenessAssessment{
				Status:             interpretationclosure.CompletenessComplete,
				EvidenceReferences: []string{"test-fixture:synthetic-scope-complete"},
			},
			Realization: interpretationclosure.RealizationAssessment{
				Status:             interpretationclosure.RealizationUnknown,
				EvidenceReferences: []string{"test-fixture:no-realization-claim"},
			},
			ProofObservations: proofs,
		})
	})
}
