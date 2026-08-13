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
		return interpretationclosure.Certify(interpretationclosure.Input{
			InterpretationDigestSHA256: request.Interpretation.InterpretationDigestSHA256,
			RepositoryRevision:         request.Session.BaseRevision,
			GraphAuthorityDigestSHA256: request.Session.GraphAuthorityDigestSHA256,
			TruthFindings:              []interpretationclosure.TruthFinding{},
			Completeness: interpretationclosure.CompletenessAssessment{
				Status:             interpretationclosure.CompletenessComplete,
				EvidenceReferences: []string{"test-fixture:synthetic-scope-complete"},
			},
			Realization: interpretationclosure.RealizationAssessment{
				Status:             interpretationclosure.RealizationUnknown,
				EvidenceReferences: []string{"test-fixture:no-realization-claim"},
			},
			ProofObservations: []interpretationclosure.ProofObservation{},
		})
	})
}
