// SPDX-License-Identifier: AGPL-3.0-only

package providerport

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func testCertifiedInterpretationCommand(t *testing.T, state synthesis.SessionState, interp synthesis.Interpretation) synthesis.RecordInterpretationCommand {
	t.Helper()
	truth := make([]interpretationclosure.TruthFinding, 0, len(interp.BindingInvariants))
	for _, claimID := range interp.BindingInvariants {
		truth = append(truth, interpretationclosure.UnknownTruth(claimID, "test", "fixture", "providerport fixture exercises neutral contradiction-gate coverage"))
	}
	proofs := make([]interpretationclosure.ProofObservation, 0, len(interp.RequiredProofObligations))
	for _, obligationID := range interp.RequiredProofObligations {
		proofs = append(proofs, interpretationclosure.ProofObservation{
			ObligationID:         obligationID,
			RequiredForAuthority: true,
			Status:               interpretationclosure.ProofSatisfied,
			EvidenceReferences:   []string{"test-fixture:providerport-proof"},
		})
	}
	receipt, err := interpretationclosure.Certify(interpretationclosure.Input{
		InterpretationDigestSHA256: interp.InterpretationDigestSHA256,
		RepositoryRevision:         state.Session.BaseRevision,
		GraphAuthorityDigestSHA256: state.Session.GraphAuthorityDigestSHA256,
		ClosureDigestSHA256:        state.Session.ClosureDigestSHA256,
		TruthFindings:              truth,
		Completeness:               interpretationclosure.CompletenessAssessment{Status: interpretationclosure.CompletenessComplete},
		Realization:                interpretationclosure.RealizationAssessment{Status: interpretationclosure.RealizationUnknown},
		ProofObservations:          proofs,
	})
	if err != nil {
		t.Fatalf("interpretationclosure.Certify: %v", err)
	}
	command, err := synthesis.NewRecordInterpretationCommand(state, interp, receipt)
	if err != nil {
		t.Fatalf("synthesis.NewRecordInterpretationCommand: %v", err)
	}
	return command
}
