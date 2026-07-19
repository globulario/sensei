// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/repograph"
)

// proveCommittedConjunction re-proves that reusable governed truth exists for a
// promotion, entirely from durable artifacts — the journal, the receipt, the
// governed source, and the persisted graph. ALL conditions must hold, and no
// single artifact is authority by itself. It reconstructs the record-present
// check from the receipt (no PromoteRequest needed), so it is reused by both the
// transaction (post-commit + replay) and any consumer (e.g. briefing) that must
// independently verify a discovered promotion. It returns the verified receipt.
func proveCommittedConjunction(ctx context.Context, repoRoot, lineageID, promotionDir string, j *Journal) (QuestionPromotionReceipt, error) {
	chain, err := j.Verify()
	if err != nil {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "journal_invalid", "", err)
	}
	// 1. Journal head is exactly one valid promotion_committed event.
	if len(chain) < 2 || chain[len(chain)-1].EventType != EventPromotionCommitted {
		return QuestionPromotionReceipt{}, vfail(VerifyIncomplete, "journal_head_not_committed", "journal head is not promotion_committed", nil)
	}
	committedEntry := chain[len(chain)-1]
	gvEntry := chain[len(chain)-2]
	if gvEntry.EventType != EventGraphVerified {
		return QuestionPromotionReceipt{}, vfail(VerifyIncomplete, "not_preceded_by_graph_verified", "promotion_committed not preceded by graph_verified", nil)
	}
	var cp commitPayload
	if err := json.Unmarshal(committedEntry.Payload, &cp); err != nil {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "commit_payload_invalid", "", err)
	}

	// 3. The durable receipt validates and matches the commit event.
	rc, err := loadReceipt(filepath.Join(promotionDir, receiptFileName))
	if err != nil {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "receipt_unreadable", "load receipt", err)
	}
	if err := Validate(rc); err != nil {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "receipt_invalid", "receipt invalid", err)
	}
	if rc.ReceiptDigestSHA256 != cp.ReceiptDigestSHA256 {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "receipt_digest_mismatch", "receipt digest does not match the commit event", nil)
	}
	// 2. Receipt digest and committed causal identity recompute.
	if d, _ := Digest(rc); d != rc.ReceiptDigestSHA256 {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "receipt_digest_recompute", "receipt digest does not recompute", nil)
	}
	wantCommitted := committedCausalIdentity(lineageID, rc.ReceiptDigestSHA256, gvEntry.EntryDigestSHA256, gvEntry.ProducedAt)
	if wantCommitted != cp.CommittedCausalIdentitySHA256 || rc.CommittedCausalIdentitySHA256 != wantCommitted {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "causal_identity_recompute", "committed causal identity does not recompute", nil)
	}

	// 7. Lineage id ties the frozen world together.
	if rc.PromotionLineageID != lineageID {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "lineage_drift", "receipt lineage id drifted", nil)
	}

	// 4. The governed source record exists with the exact mutation identity —
	// reconstructed from the receipt, not a proposal.
	bodyDigest, found, berr := governedmutation.RecordBodyDigest(repoRoot, rc.SourceDocument, rc.TopLevelKey, rc.CanonicalRecordID)
	if berr != nil {
		return QuestionPromotionReceipt{}, vfail(VerifyUnverifiable, "governed_record_unreadable", "read governed record", berr)
	}
	if !found {
		return QuestionPromotionReceipt{}, vfail(VerifyIncomplete, "governed_record_absent", "governed source record is absent", nil)
	}
	if bodyDigest != rc.CanonicalMutationDigestSHA256 {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "governed_mutation_drift", "governed record mutation identity drifted", nil)
	}

	// 5. The current persisted graph and marker independently verify.
	reloaded, gverr := repograph.VerifyPersisted(ctx, repoRoot)
	if gverr != nil {
		return QuestionPromotionReceipt{}, vfailFacility(VerifyUnverifiable, "graph_reverify_failed", "graph reverify", gverr)
	}
	if reloaded.GraphSemanticDigestSHA256 != rc.GraphSemanticDigestSHA256 ||
		reloaded.CompiledGraphByteDigestSHA256 != rc.PersistedGraphByteDigestSHA256 {
		return QuestionPromotionReceipt{}, vfail(VerifyStale, "graph_drifted", "persisted graph drifted from the receipt-bound world", nil)
	}
	// 6. The governed node and complete promotion provenance chain are present.
	if perr := repograph.VerifyProvenance(ctx, repoRoot, buildProvenanceInput(rc, lineageID)); perr != nil {
		return QuestionPromotionReceipt{}, vfail(VerifyIntegrityFailure, "provenance_chain_invalid", "provenance chain", perr)
	}
	return rc, nil
}
