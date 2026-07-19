// SPDX-License-Identifier: Apache-2.0

package questiondisposition_test

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	qd "github.com/globulario/sensei/golang/architecture/questiondisposition"
)

func validReceipt() qd.QuestionDispositionReceipt {
	hex := "0000000000000000000000000000000000000000000000000000000000000000"
	return qd.QuestionDispositionReceipt{
		SchemaVersion:                        qd.SchemaVersion,
		Task:                                 closureprotocol.TaskBinding{ID: "task.1", SessionID: "session.1"},
		ResultBindingDigestSHA256:            hex,
		ResultTransitionReceiptDigestSHA256:  hex,
		ArchitectQuestionsBundleDigestSHA256: hex,
		QuestionID:                           "question.1",
		Disposition:                          qd.DispositionAnswered,
		Reusability:                          qd.ReusabilityReusableCandidate,
		Rationale:                            "because",
		AnswerID:                             "answer.1",
		AnswerBytesDigestSHA256:              hex,
		AnsweringActorBindingDigestSHA256:    hex,
		AuthorityGrantID:                     qd.GrantQuestionDisposition,
		AuthorityRoleID:                      "role.repository_repair_agent",
		Producer:                             qd.GeneratedBy,
		DisposedAt:                           "2026-07-16T00:00:00Z",
	}
}

func TestValidateRejectsPerDispositionViolations(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*qd.QuestionDispositionReceipt)
		wantErr bool
	}{
		{"answered-happy", func(r *qd.QuestionDispositionReceipt) {}, false},
		{"answered-missing-answer", func(r *qd.QuestionDispositionReceipt) { r.AnswerID = ""; r.AnswerBytesDigestSHA256 = "" }, true},
		{"answered-no-reusability", func(r *qd.QuestionDispositionReceipt) { r.Reusability = qd.ReusabilityNone }, true},
		{"answered-task-local-ok", func(r *qd.QuestionDispositionReceipt) { r.Reusability = qd.ReusabilityTaskLocal }, false},
		{"task-local-reusable-forbidden", func(r *qd.QuestionDispositionReceipt) {
			r.Disposition = qd.DispositionTaskLocal
			r.Reusability = qd.ReusabilityReusableCandidate
			r.AnswerID = ""
			r.AnswerBytesDigestSHA256 = ""
		}, true},
		{"dismissed-with-answer-forbidden", func(r *qd.QuestionDispositionReceipt) {
			r.Disposition = qd.DispositionDismissed
			r.Reusability = qd.ReusabilityNone
		}, true},
		{"deferred-clean-ok", func(r *qd.QuestionDispositionReceipt) {
			r.Disposition = qd.DispositionDeferred
			r.Reusability = qd.ReusabilityNone
			r.AnswerID = ""
			r.AnswerBytesDigestSHA256 = ""
		}, false},
		{"missing-authority", func(r *qd.QuestionDispositionReceipt) { r.AuthorityGrantID = "" }, true},
		{"bad-timestamp", func(r *qd.QuestionDispositionReceipt) { r.DisposedAt = "yesterday" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validReceipt()
			tc.mutate(&r)
			err := qd.Validate(r)
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
