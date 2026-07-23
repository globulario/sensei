// SPDX-License-Identifier: AGPL-3.0-only

package controlstateproto

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func fieldNumbersByName(fields protoreflect.FieldDescriptors) map[string]int {
	out := make(map[string]int, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		out[string(f.Name())] = int(f.Number())
	}
	return out
}

func checkFieldNumbers(t *testing.T, message string, got map[string]int, want map[string]int) {
	t.Helper()
	for field, wantNum := range want {
		if got[field] != wantNum {
			t.Errorf("%s: field %q number = %d, want %d", message, field, got[field], wantNum)
		}
	}
}

// Mirrors briefing_feedback_adapter_test.go's TestBriefingResponse_FeedbackIsAdditiveFieldSeven:
// the four control-panel read-model messages must never renumber, retype, or remove an
// existing field — additions land as new fields with new numbers.
func TestControlPanelMessages_FieldNumbersAreFrozen(t *testing.T) {
	checkFieldNumbers(t, "ArchitectureControlSnapshot",
		fieldNumbersByName((&awarenesspb.ArchitectureControlSnapshot{}).ProtoReflect().Descriptor().Fields()),
		map[string]int{
			"meta": 1, "registry_digest": 2, "graph_authority": 3, "counts_by_class": 4,
			"assessment_coverage_counts": 5, "closure_counts": 6, "lifecycle_unknown_count": 7,
			"attention_counts_by_severity": 8, "top_attention": 9, "open_question_count": 10,
			"contradiction_count": 11, "missing_evidence_count": 12, "missing_test_count": 13,
			"missing_enforcement_count": 14, "coverage": 15, "active_task": 16,
			"completion": 17, "feedback_context": 18,
		})

	checkFieldNumbers(t, "ArchitectureArtifactIndex",
		fieldNumbersByName((&awarenesspb.ArchitectureArtifactIndex{}).ProtoReflect().Descriptor().Fields()),
		map[string]int{
			"meta": 1, "registry_digest": 2, "page": 3, "next_cursor": 4, "truncated": 5,
		})

	checkFieldNumbers(t, "ArchitectureArtifactState",
		fieldNumbersByName((&awarenesspb.ArchitectureArtifactState{}).ProtoReflect().Descriptor().Fields()),
		map[string]int{
			"meta": 1, "identity": 2, "canonical_class": 3, "assessment_coverage": 4,
			"closure": 5, "closure_reason": 6, "lifecycle": 7, "dimensions": 8,
			"attention": 9, "questions": 10, "evidence": 11, "feedback": 12,
			"next_action_owner": 13,
		})

	checkFieldNumbers(t, "OntologyNavigationDescriptor",
		fieldNumbersByName((&awarenesspb.OntologyNavigationDescriptor{}).ProtoReflect().Descriptor().Fields()),
		map[string]int{
			"meta": 1, "registry_digest": 2, "families": 3, "unknown_class_fallback": 4,
		})
}
