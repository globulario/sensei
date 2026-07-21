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

// promotedKnowledgeResult carries the exact canonical feedback projection alongside the
// mechanically-derived compatibility surfaces. The projection is canonical (and explicitly
// non-authoritative); Records and Limitations are pure projections of it, so old consumers keep
// the two legacy surfaces while new consumers read the typed projection (availability, typed
// findings) without parsing prose.
type promotedKnowledgeResult struct {
	Projection  briefingfeedback.Projection
	Records     []PromotedGovernedRecord
	Limitations []string
}

// collectPromotedKnowledge is a thin adapter over the canonical briefingfeedback owner. It
// binds the EXACT canonical task identity supplied by task-session control (task id, session
// id, repository domain, verified task file set) into the owner request — it never infers
// identity from the task directory, active-task proximity, requested file, or working
// directory. The owner discovers, independently verifies, and scope-filters; this adapter only
// maps the one canonical projection into the briefing's compatibility shapes. Compatibility
// records come from the projection's VERIFIED records; compatibility limitations come from its
// TYPED findings — never from raw verification error text.
func collectPromotedKnowledge(repoRoot, file string, taskFiles map[string]bool, domain, taskID, sessionID string) promotedKnowledgeResult {
	files := make([]string, 0, len(taskFiles))
	for f := range taskFiles {
		files = append(files, f)
	}
	proj, err := briefingfeedback.Build(context.Background(), briefingfeedback.Request{
		RepositoryRoot:     repoRoot,
		RepositoryIdentity: domain,
		RequestedDomain:    domain,
		RequestedFiles:     []string{file},
		Task:               &briefingfeedback.TaskBinding{TaskID: taskID, SessionID: sessionID, RepositoryDomain: domain, Files: files},
	})
	if err != nil {
		// Impossible internal projection state (digest/validation) — never a bare zero value
		// of promoted knowledge presented as truth.
		return promotedKnowledgeResult{Limitations: []string{"promoted-knowledge projection unavailable"}}
	}
	res := promotedKnowledgeResult{Projection: proj}
	for _, r := range proj.Records {
		res.Records = append(res.Records, PromotedGovernedRecord{
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
	for _, f := range proj.Findings {
		res.Limitations = append(res.Limitations, limitationFromFinding(f))
	}
	return res
}

// limitationFromFinding renders a TYPED feedback finding as a stable human-readable limitation
// string. It uses only the closed finding class + reason code + lineage provenance — never the
// underlying verification error text.
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
