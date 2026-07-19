// SPDX-License-Identifier: AGPL-3.0-only

package questiondisposition_test

import (
	"context"
	"testing"

	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

// TestE2EAnsweredReusableRecordsAndProjects proves the happy path: an authorized
// answered+reusable_candidate disposition prepares, records exactly one event,
// reloads by digest, and projects the promote-reusable next action.
func TestE2EAnsweredReusableRecordsAndProjects(t *testing.T) {
	env := seedDisposable(t)

	cand, err := qd.Prepare(answeredReusable(env))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if cand.Receipt.AuthorityGrantID != qd.GrantQuestionDisposition {
		t.Fatalf("grant = %q, want %q", cand.Receipt.AuthorityGrantID, qd.GrantQuestionDisposition)
	}
	if cand.Receipt.DisposedAt == "" {
		t.Fatal("disposed_at not ledger-anchored")
	}

	res, err := qd.RecordDisposition(context.Background(), qd.RecordRequest{TaskDirectory: env.TaskDir, Candidate: cand})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if res.Outcome != qd.OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded", res.Outcome)
	}

	rec, err := qd.LoadRecordedDisposition(env.TaskDir, cand.Receipt.ReceiptDigestSHA256)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if rec.Receipt.QuestionID != env.QuestionID {
		t.Fatalf("reloaded question = %q, want %q", rec.Receipt.QuestionID, env.QuestionID)
	}

	proj, err := qd.ProjectQuestion(env.TaskDir, env.QuestionID)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if !proj.Disposed || proj.Contested {
		t.Fatalf("projection disposed=%v contested=%v, want disposed & not contested", proj.Disposed, proj.Contested)
	}
	if proj.NextAction != qd.NextPromoteReusable {
		t.Fatalf("next action = %s, want %s", proj.NextAction, qd.NextPromoteReusable)
	}
}
