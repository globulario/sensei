// SPDX-License-Identifier: Apache-2.0

package resultrecording

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// Closed artifact-key vocabulary for a result_transition_recorded event. Every
// key appears exactly once; unknown or missing keys fail strict validation. Stage
// keys derive from closureprotocol.ResultPipelineStages — never from filenames,
// and never a second stage-order list.
const (
	KeyReceipt      = "result_transition_receipt"
	KeyImpactReport = "governed_knowledge_impact_report"
	KeySession      = "session"
	KeyTaskControl  = "task_control"
	KeyStatus       = "status"

	stageKeyPrefix = "result_stage."
)

func stageKey(stage closureprotocol.ResultPipelineStage) string {
	return stageKeyPrefix + string(stage)
}

// expectedArtifactKeys returns the exact closed set an event must carry: receipt,
// impact report, one key per canonical stage, and the three projections.
func expectedArtifactKeys() map[string]bool {
	keys := map[string]bool{
		KeyReceipt:      true,
		KeyImpactReport: true,
		KeySession:      true,
		KeyTaskControl:  true,
		KeyStatus:       true,
	}
	for _, stage := range closureprotocol.ResultPipelineStages {
		keys[stageKey(stage)] = true
	}
	return keys
}

// validateArtifactKeySet requires the artifact map to be exactly the closed set.
func validateArtifactKeySet(artifacts map[string]closureprotocol.LedgerPayloadRef) error {
	want := expectedArtifactKeys()
	for k := range artifacts {
		if !want[k] {
			return recErr(CodeArtifactContractInvalid, "unrecognized artifact key %q", k)
		}
	}
	for k := range want {
		if _, ok := artifacts[k]; !ok {
			return recErr(CodeArtifactContractInvalid, "missing artifact key %q", k)
		}
	}
	return nil
}
