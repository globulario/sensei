// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture/questionpromotion"
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

// collectPromotedKnowledge discovers committed governed promotions, independently
// re-proves each through the questionpromotion verification boundary (it does NOT
// re-implement receipt/journal/source/graph/provenance validation), and includes
// only verified promotions whose governed scope intersects the task scope. A
// stale/tampered/incomplete/non-committed promotion is reported as a typed
// integrity limitation and excluded from binding context. Output is deterministic
// (sorted), so an unchanged repository/task world yields byte-identical content.
func collectPromotedKnowledge(repoRoot, file string, taskFiles map[string]bool, domain string) ([]PromotedGovernedRecord, []string) {
	lineages, err := questionpromotion.DiscoverCommittedPromotions(repoRoot)
	if err != nil {
		return nil, []string{"promoted-knowledge discovery unavailable: " + err.Error()}
	}
	var out []PromotedGovernedRecord
	var findings []string
	for _, lineage := range lineages {
		vp, verr := questionpromotion.VerifyCommittedPromotion(context.Background(), repoRoot, lineage)
		if verr != nil {
			findings = append(findings, fmt.Sprintf("promoted knowledge %s excluded (integrity): %v", shortLineage(lineage), verr))
			continue
		}
		if !promotionInScope(vp.Receipt, file, taskFiles, domain) {
			continue
		}
		rc := vp.Receipt
		out = append(out, PromotedGovernedRecord{
			GovernedNodeIRI: vp.GovernedNodeIRI, Kind: rc.GovernedTargetKind,
			CanonicalRecordID: rc.CanonicalRecordID, SourceDocument: rc.SourceDocument,
			PromotionLineageID: vp.PromotionLineageID, ReceiptDigestSHA256: rc.ReceiptDigestSHA256,
			QuestionID: rc.QuestionID, AnswerID: rc.AnswerID,
			DispositionReceiptDigestSHA256: rc.QuestionDispositionReceiptDigestSHA256,
			TaskID:                         rc.Task.ID, SessionID: rc.Task.SessionID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GovernedNodeIRI < out[j].GovernedNodeIRI })
	sort.Strings(findings)
	return out, findings
}

// promotionInScope requires the promotion's governed scope to intersect the task
// scope: the effective-scope domain must match (an unscoped-domain promotion
// applies to any), and at least one effective-scope file must be the briefing file
// or a task-scoped file. A promotion with no declared effective scope is not
// selected into binding context.
func promotionInScope(rc questionpromotion.QuestionPromotionReceipt, file string, taskFiles map[string]bool, domain string) bool {
	if rc.EffectiveScopeDomain != "" && domain != "" && rc.EffectiveScopeDomain != domain {
		return false
	}
	if len(rc.EffectiveScopeFiles) == 0 {
		return false
	}
	for _, f := range rc.EffectiveScopeFiles {
		if f == file || taskFiles[f] {
			return true
		}
	}
	return false
}

func shortLineage(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
}
