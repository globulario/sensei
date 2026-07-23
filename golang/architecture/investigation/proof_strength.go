// SPDX-License-Identifier: AGPL-3.0-only

package investigation

type ProofStrength string

const (
	ProofAssertionOnly      ProofStrength = "P0_assertion_only"
	ProofStaticSource       ProofStrength = "P1_static_source_citation"
	ProofStructuralPath     ProofStrength = "P2_structural_path_demonstrated"
	ProofDeterministicTest  ProofStrength = "P3_deterministic_test_executed"
	ProofIntegrationRuntime ProofStrength = "P4_integration_runtime_observed"
	ProofProductionObserved ProofStrength = "P5_production_observation"
)

func IsValidProofStrength(ps ProofStrength) bool {
	switch ps {
	case ProofAssertionOnly, ProofStaticSource, ProofStructuralPath, ProofDeterministicTest, ProofIntegrationRuntime, ProofProductionObserved:
		return true
	default:
		return false
	}
}
