// SPDX-License-Identifier: AGPL-3.0-only

package questionpromotion

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/repograph"
)

// verifyConjunctiveAuthority re-proves that reusable truth exists — ALL conditions
// must hold, and no single artifact is authority by itself. It is called after a
// commit append and on committed replay, always re-proving from disk.
func verifyConjunctiveAuthority(ctx context.Context, req PromoteRequest, prepared QuestionPromotionReceipt, lineageID, promotionDir string, j *Journal) error {
	chain, err := j.Verify()
	if err != nil {
		return err
	}
	// 1. Journal head is exactly one valid promotion_committed event.
	if len(chain) == 0 || chain[len(chain)-1].EventType != EventPromotionCommitted {
		return fmt.Errorf("journal head is not promotion_committed")
	}
	committedEntry := chain[len(chain)-1]
	gvEntry := chain[len(chain)-2]
	var cp commitPayload
	if err := json.Unmarshal(committedEntry.Payload, &cp); err != nil {
		return err
	}

	// 3. The durable receipt validates and matches that event.
	rc, err := loadReceipt(filepath.Join(promotionDir, receiptFileName))
	if err != nil {
		return fmt.Errorf("load receipt: %w", err)
	}
	if err := Validate(rc); err != nil {
		return fmt.Errorf("receipt invalid: %w", err)
	}
	if rc.ReceiptDigestSHA256 != cp.ReceiptDigestSHA256 {
		return fmt.Errorf("receipt digest does not match the commit event")
	}
	// 2. Receipt digest and committed causal identity recompute.
	if d, _ := Digest(rc); d != rc.ReceiptDigestSHA256 {
		return fmt.Errorf("receipt digest does not recompute")
	}
	wantCommitted := committedCausalIdentity(lineageID, rc.ReceiptDigestSHA256, gvEntry.EntryDigestSHA256, gvEntry.ProducedAt)
	if wantCommitted != cp.CommittedCausalIdentitySHA256 || rc.CommittedCausalIdentitySHA256 != wantCommitted {
		return fmt.Errorf("committed causal identity does not recompute")
	}

	// 7. All frozen bindings still match (lineage id ties them together).
	if rc.PromotionLineageID != lineageID {
		return fmt.Errorf("receipt lineage id drifted")
	}

	// 4. The governed source record exists with the exact mutation identity.
	greq := governedmutation.Request{RepositoryRoot: req.RepositoryRoot, Proposal: req.Proposal}
	plan, perr := governedmutation.Plan(greq)
	if perr != nil {
		return fmt.Errorf("re-plan governed record: %w", perr)
	}
	if plan.Disposition != governedmutation.DispositionReplay {
		return fmt.Errorf("governed source record is absent")
	}
	if plan.MutationDigestSHA256 != rc.CanonicalMutationDigestSHA256 {
		return fmt.Errorf("governed record mutation identity drifted")
	}

	// 5. The current persisted graph and marker independently verify.
	reloaded, verr := repograph.VerifyPersisted(ctx, req.RepositoryRoot)
	if verr != nil {
		return fmt.Errorf("graph reverify: %w", verr)
	}
	if reloaded.GraphSemanticDigestSHA256 != rc.GraphSemanticDigestSHA256 ||
		reloaded.CompiledGraphByteDigestSHA256 != rc.PersistedGraphByteDigestSHA256 {
		return fmt.Errorf("persisted graph drifted from the receipt-bound world")
	}
	// 6. The governed node and complete promotion provenance chain are present.
	if perr := repograph.VerifyProvenance(ctx, req.RepositoryRoot, buildProvenanceInput(prepared, lineageID)); perr != nil {
		return fmt.Errorf("provenance chain: %w", perr)
	}
	return nil
}
