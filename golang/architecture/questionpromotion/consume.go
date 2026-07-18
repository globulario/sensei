// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"context"
	"os"
	"path/filepath"
	"sort"
)

// VerifiedPromotion is a committed promotion re-proven from durable artifacts,
// with its exact provenance lineage exposed for a consumer (e.g. a task
// briefing). It carries no authority of its own beyond the verified receipt.
type VerifiedPromotion struct {
	PromotionLineageID string
	Receipt            QuestionPromotionReceipt
	GovernedNodeIRI    string
	ReceiptNodeIRI     string
}

// VerifyCommittedPromotion independently re-proves the COMPLETE promotion
// conjunction for one lineage from durable artifacts alone — the journal, the
// receipt, the governed source record, and the persisted graph + provenance
// chain — without any PromoteRequest. A stale, tampered, incomplete, or
// non-committed promotion returns a typed error and never yields a verified
// result. This is the single reuse boundary: consumers must call it rather than
// re-implement receipt/journal/source/graph/provenance/authority validation.
func VerifyCommittedPromotion(ctx context.Context, repoRoot, lineageID string) (VerifiedPromotion, error) {
	promotionDir := filepath.Join(repoRoot, ".sensei", "project", "promotions", lineageID)
	rc, err := proveCommittedConjunction(ctx, repoRoot, lineageID, promotionDir, OpenJournal(promotionDir))
	if err != nil {
		return VerifiedPromotion{}, err
	}
	return VerifiedPromotion{
		PromotionLineageID: lineageID,
		Receipt:            rc,
		GovernedNodeIRI:    rc.GovernedNodeIRI,
		ReceiptNodeIRI:     ReceiptNodeIRI(rc),
	}, nil
}

// DiscoverCommittedPromotions lists the promotion-attempt lineage ids present in
// the repository promotion index (.sensei/project/promotions/<lineage>/).
// Discovery is NON-AUTHORITATIVE: each id must be re-proven with
// VerifyCommittedPromotion before it may be trusted or consumed.
func DiscoverCommittedPromotions(repoRoot string) ([]string, error) {
	base := filepath.Join(repoRoot, ".sensei", "project", "promotions")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
