// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
	"github.com/globulario/sensei/golang/architecture/closure"
)

// TaskBriefing exposes the EXACT canonical feedback projection, bound to the exact task
// identity established by control, with the compatibility surfaces mechanically derived from
// it (§2/§3 review repair).
func TestTaskBriefingExposesCanonicalProjection(t *testing.T) {
	repo, graph := testRepo(t)
	_, err := Prepare(PrepareOptions{
		RepoRoot: repo, RepositoryDomain: "github.com/example/project", Description: "modify gin",
		Mode: admission.ModeModify, TaskClass: "modify_gin", RiskClass: closure.RiskArchitectureSensitive,
		DirectionRequirement: closure.DirectionPreserve, Files: []FileOperation{{Path: "gin.go", Operation: admission.OperationModify}},
		GraphNT: graph, SetActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	brief, err := BuildTaskBriefing(repo, "", "gin.go", true)
	if err != nil {
		t.Fatal(err)
	}
	// The projection is present and is itself a valid canonical projection.
	if brief.FeedbackProjection == nil {
		t.Fatal("task briefing did not expose the feedback projection")
	}
	if verr := briefingfeedback.ValidateProjection(*brief.FeedbackProjection); verr != nil {
		t.Fatalf("exposed projection is not canonical: %v", verr)
	}
	// Bound to the EXACT canonical task identity (task id + non-empty session id).
	if brief.FeedbackProjection.TaskID != brief.TaskID || brief.TaskID == "" {
		t.Fatalf("projection task id %q != briefing task id %q", brief.FeedbackProjection.TaskID, brief.TaskID)
	}
	if brief.FeedbackProjection.SessionID == "" {
		t.Fatal("projection is not session-bound")
	}
	// Availability is a closed, distinguishable member (not the empty zero value).
	switch brief.FeedbackProjection.Availability {
	case briefingfeedback.FeedbackAvailable, briefingfeedback.FeedbackEmpty, briefingfeedback.FeedbackDegraded,
		briefingfeedback.FeedbackUnavailable, briefingfeedback.FeedbackInvalid:
	default:
		t.Fatalf("availability %q is off-vocabulary", brief.FeedbackProjection.Availability)
	}
	// Compatibility records are mechanically derived from the projection's records.
	if len(brief.PromotedGovernedKnowledge) != len(brief.FeedbackProjection.Records) {
		t.Fatalf("compatibility records (%d) do not match projection records (%d)",
			len(brief.PromotedGovernedKnowledge), len(brief.FeedbackProjection.Records))
	}
	for i, r := range brief.FeedbackProjection.Records {
		if brief.PromotedGovernedKnowledge[i].PromotionLineageID != r.PromotionLineageID {
			t.Fatalf("compatibility record %d lineage mismatch", i)
		}
	}
}

// The compatibility records and limitations equal the projection's records and typed findings
// (mechanical derivation, no raw error text), and the availability is distinguishable across
// scenarios — exercised at the adapter over a seeded committed promotion.
func TestPromotedKnowledgeDerivedFromProjection(t *testing.T) {
	file := "golang/server/reload.go"
	repo := seedCommittedPromotion(t, []string{file})

	// Available: one relevant verified promotion whose ORIGINATING task differs from the
	// consuming task — proving cross-task reuse.
	got := collectPromotedKnowledge(repo, file, map[string]bool{file: true}, cDomain, "task.consumer.distinct", "session.consumer.distinct")
	if got.Projection.Availability != briefingfeedback.FeedbackAvailable || len(got.Records) != 1 {
		t.Fatalf("cross-task verified knowledge must remain reusable: %q recs=%d", got.Projection.Availability, len(got.Records))
	}
	if got.Records[0].TaskID == "task.consumer.distinct" {
		t.Fatal("record must carry the ORIGINATING task, not the consuming task")
	}
	if len(got.Records) != len(got.Projection.Records) {
		t.Fatalf("compatibility records must equal projection records")
	}

	// Empty: an in-repo but out-of-scope file yields feedback_empty with no debris.
	empty := collectPromotedKnowledge(repo, "cmd/other/main.go", map[string]bool{"cmd/other/main.go": true}, cDomain, "task.consumer", "session.consumer")
	if empty.Projection.Availability != briefingfeedback.FeedbackEmpty || len(empty.Records) != 0 || len(empty.Limitations) != 0 {
		t.Fatalf("out-of-scope must be feedback_empty with no debris: %q", empty.Projection.Availability)
	}

	// Every limitation is mechanically derived from a typed finding (one per finding).
	if len(got.Limitations) != len(got.Projection.Findings) {
		t.Fatalf("limitations (%d) must equal typed findings (%d)", len(got.Limitations), len(got.Projection.Findings))
	}
}
