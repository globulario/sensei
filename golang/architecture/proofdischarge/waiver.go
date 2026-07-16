// SPDX-License-Identifier: Apache-2.0

package proofdischarge

import (
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// GovernanceException is Phase 5's minimal read model of the ontology class
// GovernanceException. No frozen JSON Schema file defines this record in this
// snapshot; this shape is proposed here (keyed by WaiverReceipt.PolicyID, the
// only foreign key available on the frozen WaiverReceipt) and MUST be reconciled
// with whatever schema a later phase freezes for it. A governance exception is
// the governance ceiling; a waiver is the concrete, narrower grant under it.
type GovernanceException struct {
	ID                     string   `json:"id" yaml:"id"`
	Status                 string   `json:"status" yaml:"status"`    // must be "governed" to authorize a waiver
	Dimension              string   `json:"dimension" yaml:"dimension"` // must equal "proof"
	AppliesToObligationIDs []string `json:"applies_to_obligation_ids,omitempty" yaml:"applies_to_obligation_ids,omitempty"`
	AppliesToSlotIDs       []string `json:"applies_to_slot_ids,omitempty" yaml:"applies_to_slot_ids,omitempty"`
	ExpiresAt              string   `json:"expires_at,omitempty" yaml:"expires_at,omitempty"`
}

const governanceExceptionGoverned = "governed"

// ResolveWaiver reports whether a governed exception justifies skipping a
// missing required slot. ALL conditions must hold, in order, short-circuiting on
// the first failure. A CLI flag, free-text note, environment variable, or
// unvalidated struct literal is never a waiver: only a typed WaiverReceipt that
// independently validates AND resolves through a governed GovernanceException at
// the exact slot granularity satisfies a slot.
func ResolveWaiver(ob ProofObligation, slot ProofSlotSpec, ctx Context) (waiverID string, ok bool) {
	now, err := time.Parse(time.RFC3339, strings.TrimSpace(ctx.ObservedAt))
	if err != nil {
		return "", false
	}
	for _, w := range ctx.Waivers {
		// 0. The waiver must independently be a valid, schema-conformant record.
		if closureprotocol.ValidateWaiverReceipt(w) != nil {
			continue
		}
		// 1. proof dimension, valid status.
		if w.Dimension != closureprotocol.DimensionProof || w.Status != closureprotocol.ReceiptValid {
			continue
		}
		// 2. Not expired relative to the deterministic now.
		exp, err := time.Parse(time.RFC3339, strings.TrimSpace(w.ExpiresAt))
		if err != nil || !exp.After(now) {
			continue
		}
		// 3. Scoped to exactly this slot (never obligation-wide blanket).
		if !containsExact(w.AppliesTo, slot.ID) {
			continue
		}
		// 4. Resolves to a governed governance exception keyed by PolicyID.
		ge, found := ctx.GovernanceExceptions[strings.TrimSpace(w.PolicyID)]
		if !found || ge.Status != governanceExceptionGoverned {
			continue
		}
		// 5. Exception is a proof exception.
		if ge.Dimension != string(closureprotocol.DimensionProof) {
			continue
		}
		// 6. Exception covers this slot (slot granularity) or, when it lists no
		//    slots, this obligation (obligation granularity ceiling).
		if !exceptionCoversSlot(ge, ob, slot) {
			continue
		}
		// 7. Exception not itself expired.
		if e := strings.TrimSpace(ge.ExpiresAt); e != "" {
			ee, err := time.Parse(time.RFC3339, e)
			if err != nil || !ee.After(now) {
				continue
			}
		}
		return w.WaiverID, true
	}
	return "", false
}

func exceptionCoversSlot(ge GovernanceException, ob ProofObligation, slot ProofSlotSpec) bool {
	if containsExact(ge.AppliesToSlotIDs, slot.ID) {
		return true
	}
	if len(ge.AppliesToSlotIDs) == 0 && containsExact(ge.AppliesToObligationIDs, ob.ID) {
		return true
	}
	return false
}

func containsExact(list []string, want string) bool {
	for _, v := range list {
		if strings.TrimSpace(v) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
}
