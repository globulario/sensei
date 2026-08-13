// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
)

// testCertifiedInterpretationCommand keeps O1 transition tests honest about
// the new authority boundary. It exercises the same public promotion
// constructor production uses rather than setting the package-private marker
// directly just because these tests live in package synthesis.
func testCertifiedInterpretationCommand(t *testing.T, state SessionState, interp Interpretation) RecordInterpretationCommand {
	t.Helper()
	truth := make([]interpretationclosure.TruthFinding, 0, len(interp.BindingInvariants))
	for _, claimID := range interp.BindingInvariants {
		truth = append(truth, interpretationclosure.UnknownTruth(
			claimID,
			"test",
			"fixture",
			"synthesis fixture exercises neutral contradiction-gate coverage",
			"test-fixture:synthesis-truth-challenge",
		))
	}
	proofs := make([]interpretationclosure.ProofObservation, 0, len(interp.RequiredProofObligations))
	for _, obligationID := range interp.RequiredProofObligations {
		proofs = append(proofs, interpretationclosure.ProofObservation{
			ObligationID:         obligationID,
			RequiredForAuthority: true,
			Status:               interpretationclosure.ProofSatisfied,
			EvidenceReferences:   []string{"test-fixture:synthesis-proof"},
		})
	}
	receipt, err := interpretationclosure.Certify(interpretationclosure.Input{
		InterpretationDigestSHA256: interp.InterpretationDigestSHA256,
		RepositoryRevision:         state.Session.BaseRevision,
		GraphAuthorityDigestSHA256: state.Session.GraphAuthorityDigestSHA256,
		ClosureDigestSHA256:        state.Session.ClosureDigestSHA256,
		TruthFindings:              truth,
		Completeness: interpretationclosure.CompletenessAssessment{
			Status:             interpretationclosure.CompletenessComplete,
			EvidenceReferences: []string{"test-fixture:synthesis-scope"},
		},
		Realization: interpretationclosure.RealizationAssessment{
			Status:             interpretationclosure.RealizationUnknown,
			EvidenceReferences: []string{"test-fixture:synthesis-no-realization-claim"},
		},
		ProofObservations: proofs,
	})
	if err != nil {
		t.Fatalf("interpretationclosure.Certify: %v", err)
	}
	command, err := NewRecordInterpretationCommand(state, interp, receipt)
	if err != nil {
		t.Fatalf("NewRecordInterpretationCommand: %v", err)
	}
	return command
}
