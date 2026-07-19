// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"fmt"

	"github.com/globulario/sensei/golang/architecture/briefingfeedback"
)

// PromotedGovernedRecord is one committed governed promotion surfaced in a task
// briefing, carrying the exact provenance lineage back to its originating
// question, answer, and disposition. It is verified truth, not a task-local claim,
// and it asserts no certification or completion.
type PromotedGovernedRecord struct {
	GovernedNodeIRI                string `json:"governed_node_iri" yaml:"governed_node_iri"`
	Kind                           string `json:"kind" yaml:"kind"`
	CanonicalRecordID              string `json:"canonical_record_id" yaml:"canonical_record_id"`
	SourceDocument                 string `json:"source_document" yaml:"source_document"`
	PromotionLineageID             string `json:"promotion_lineage_id" yaml:"promotion_lineage_id"`
	ReceiptDigestSHA256            string `json:"receipt_digest_sha256" yaml:"receipt_digest_sha256"`
	QuestionID                     string `json:"question_id" yaml:"question_id"`
	AnswerID                       string `json:"answer_id" yaml:"answer_id"`
	DispositionReceiptDigestSHA256 string `json:"disposition_receipt_digest_sha256" yaml:"disposition_receipt_digest_sha256"`
	TaskID                         string `json:"task_id" yaml:"task_id"`
	SessionID                      string `json:"session_id" yaml:"session_id"`
}

// collectPromotedKnowledge is a thin adapter over the canonical briefingfeedback
// owner: the task briefing no longer discovers, verifies, or scope-filters
// promotions itself. It builds the owner's deterministic feedback projection for
// the task scope and maps the VERIFIED records into the briefing's compatibility
// shape and the typed findings into human-readable limitations. It never
// re-implements verification and never parses raw verification error text —
// limitations are derived from the projection's TYPED finding class + reason.
func collectPromotedKnowledge(repoRoot, file string, taskFiles map[string]bool, domain string) ([]PromotedGovernedRecord, []string) {
	files := make([]string, 0, len(taskFiles))
	for f := range taskFiles {
		files = append(files, f)
	}
	proj, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot:     repoRoot,
		RepositoryIdentity: domain,
		RequestedDomain:    domain,
		RequestedFiles:     []string{file},
		Task:               &briefingfeedback.TaskBinding{Files: files},
	})
	if err != nil {
		// Impossible internal projection state (digest/validation) — never a bare zero
		// value of promoted knowledge presented as truth.
		return nil, []string{"promoted-knowledge projection unavailable"}
	}
	out := make([]PromotedGovernedRecord, 0, len(proj.Records))
	for _, r := range proj.Records {
		out = append(out, PromotedGovernedRecord{
			GovernedNodeIRI:                r.GovernedNodeIRI,
			Kind:                           r.GovernedKind,
			CanonicalRecordID:              r.CanonicalRecordID,
			SourceDocument:                 r.SourceDocument,
			PromotionLineageID:             r.PromotionLineageID,
			ReceiptDigestSHA256:            r.PromotionReceiptDigestSHA256,
			QuestionID:                     r.QuestionID,
			AnswerID:                       r.AnswerID,
			DispositionReceiptDigestSHA256: r.DispositionReceiptDigestSHA256,
			TaskID:                         r.OriginatingTaskID,
			SessionID:                      r.OriginatingSessionID,
		})
	}
	var limitations []string
	for _, f := range proj.Findings {
		limitations = append(limitations, limitationFromFinding(f))
	}
	return out, limitations
}

// limitationFromFinding renders a TYPED feedback finding as a stable human-readable
// limitation string. It uses only the closed finding class + reason code + lineage
// provenance — never the underlying verification error text.
func limitationFromFinding(f briefingfeedback.Finding) string {
	switch f.Class {
	case briefingfeedback.PromotionDiscoveryUnavailable:
		return "promoted-knowledge discovery unavailable"
	case briefingfeedback.PromotionScopeIdentityInvalid:
		return "promoted-knowledge request invalid: " + f.ReasonCode
	default:
		return fmt.Sprintf("promoted knowledge %s excluded (%s): %s", shortLineage(f.LineageID), f.Class, f.ReasonCode)
	}
}

func shortLineage(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
