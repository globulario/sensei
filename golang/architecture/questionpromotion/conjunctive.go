// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import (
	"context"
	"encoding/json"
	"fmt"
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
		return QuestionPromotionReceipt{}, err
	}
	// 1. Journal head is exactly one valid promotion_committed event.
	if len(chain) < 2 || chain[len(chain)-1].EventType != EventPromotionCommitted {
		return QuestionPromotionReceipt{}, fmt.Errorf("journal head is not promotion_committed")
	}
	committedEntry := chain[len(chain)-1]
	gvEntry := chain[len(chain)-2]
	if gvEntry.EventType != EventGraphVerified {
		return QuestionPromotionReceipt{}, fmt.Errorf("promotion_committed not preceded by graph_verified")
	}
	var cp commitPayload
	if err := json.Unmarshal(committedEntry.Payload, &cp); err != nil {
		return QuestionPromotionReceipt{}, err
	}

	// 3. The durable receipt validates and matches the commit event.
	rc, err := loadReceipt(filepath.Join(promotionDir, receiptFileName))
	if err != nil {
		return QuestionPromotionReceipt{}, fmt.Errorf("load receipt: %w", err)
	}
	if err := Validate(rc); err != nil {
		return QuestionPromotionReceipt{}, fmt.Errorf("receipt invalid: %w", err)
	}
	if rc.ReceiptDigestSHA256 != cp.ReceiptDigestSHA256 {
		return QuestionPromotionReceipt{}, fmt.Errorf("receipt digest does not match the commit event")
	}
	// 2. Receipt digest and committed causal identity recompute.
	if d, _ := Digest(rc); d != rc.ReceiptDigestSHA256 {
		return QuestionPromotionReceipt{}, fmt.Errorf("receipt digest does not recompute")
	}
	wantCommitted := committedCausalIdentity(lineageID, rc.ReceiptDigestSHA256, gvEntry.EntryDigestSHA256, gvEntry.ProducedAt)
	if wantCommitted != cp.CommittedCausalIdentitySHA256 || rc.CommittedCausalIdentitySHA256 != wantCommitted {
		return QuestionPromotionReceipt{}, fmt.Errorf("committed causal identity does not recompute")
	}

	// 7. Lineage id ties the frozen world together.
	if rc.PromotionLineageID != lineageID {
		return QuestionPromotionReceipt{}, fmt.Errorf("receipt lineage id drifted")
	}

	// 4. The governed source record exists with the exact mutation identity —
	// reconstructed from the receipt, not a proposal.
	bodyDigest, found, berr := governedmutation.RecordBodyDigest(repoRoot, rc.SourceDocument, rc.TopLevelKey, rc.CanonicalRecordID)
	if berr != nil {
		return QuestionPromotionReceipt{}, fmt.Errorf("read governed record: %w", berr)
	}
	if !found {
		return QuestionPromotionReceipt{}, fmt.Errorf("governed source record is absent")
	}
	if bodyDigest != rc.CanonicalMutationDigestSHA256 {
		return QuestionPromotionReceipt{}, fmt.Errorf("governed record mutation identity drifted")
	}

	// 5. The current persisted graph and marker independently verify.
	reloaded, verr := repograph.VerifyPersisted(ctx, repoRoot)
	if verr != nil {
		return QuestionPromotionReceipt{}, fmt.Errorf("graph reverify: %w", verr)
	}
	if reloaded.GraphSemanticDigestSHA256 != rc.GraphSemanticDigestSHA256 ||
		reloaded.CompiledGraphByteDigestSHA256 != rc.PersistedGraphByteDigestSHA256 {
		return QuestionPromotionReceipt{}, fmt.Errorf("persisted graph drifted from the receipt-bound world")
	}
	// 6. The governed node and complete promotion provenance chain are present.
	if perr := repograph.VerifyProvenance(ctx, repoRoot, buildProvenanceInput(rc, lineageID)); perr != nil {
		return QuestionPromotionReceipt{}, fmt.Errorf("provenance chain: %w", perr)
	}
	return rc, nil
}
