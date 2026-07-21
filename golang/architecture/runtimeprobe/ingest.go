// SPDX-License-Identifier: AGPL-3.0-only

package runtimeprobe

import (
	"sort"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// IngestOutcome is the CLOSED replay/conflict vocabulary, mirroring the questiondisposition ledger.
type IngestOutcome string

const (
	// OutcomeRecorded: a new, non-conflicting evidence receipt.
	OutcomeRecorded IngestOutcome = "recorded"
	// OutcomeReplayed: an exact prior receipt (same content digest) — idempotent, nothing new happens.
	OutcomeReplayed IngestOutcome = "replayed"
	// OutcomeContested: same owner-path subject, DIFFERENT payload — both preserved, never first-row-wins.
	OutcomeContested IngestOutcome = "contested"
)

// IngestResult is the deterministic outcome of offering one receipt against the existing set.
type IngestResult struct {
	Outcome       IngestOutcome `json:"outcome" yaml:"outcome"`
	ReceiptDigest string        `json:"receipt_digest" yaml:"receipt_digest"`
	// ConflictingReceiptIDs are the prior receipts this one contests (sorted, deduped) — the conflict
	// is preserved, never resolved by arrival order.
	ConflictingReceiptIDs []string `json:"conflicting_receipt_ids,omitempty" yaml:"conflicting_receipt_ids,omitempty"`
}

// Retained reports whether the receipt is retained in the evidence set. ONLY an exact replay is inert;
// a CONTESTED receipt is retained (flagged, alongside the receipts it contests) and is NEVER silently
// excluded from the assessment input — the contradiction must remain visible to the owner.
func (r IngestResult) Retained() bool {
	return r.Outcome != OutcomeReplayed
}

// Ingest deterministically classifies an incoming receipt against the existing set: exact
// content-digest match → replayed (no new event); same owner-path subject with a different payload →
// contested (both preserved); otherwise recorded. It writes nothing — it is a pure classification.
func Ingest(existing []closureprotocol.EvidenceReceipt, incoming closureprotocol.EvidenceReceipt) (IngestResult, error) {
	dig, err := receiptDigest(incoming)
	if err != nil {
		return IngestResult{}, err
	}
	sorted := append([]closureprotocol.EvidenceReceipt(nil), existing...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ReceiptID < sorted[j].ReceiptID })

	var conflicts []string
	for _, e := range sorted {
		ed, derr := receiptDigest(e)
		if derr != nil {
			return IngestResult{}, derr
		}
		if ed == dig {
			return IngestResult{Outcome: OutcomeReplayed, ReceiptDigest: dig}, nil
		}
		if sameSubject(e, incoming) && e.PayloadDigestSHA256 != incoming.PayloadDigestSHA256 {
			conflicts = append(conflicts, e.ReceiptID)
		}
	}
	if len(conflicts) > 0 {
		return IngestResult{Outcome: OutcomeContested, ReceiptDigest: dig, ConflictingReceiptIDs: sortedUnique(conflicts)}, nil
	}
	return IngestResult{Outcome: OutcomeRecorded, ReceiptDigest: dig}, nil
}

// receiptDigest is the self-excluding content digest of a receipt (its declared conflicts list is
// cleared so the identity is the observation content, not the accumulated conflict annotations).
func receiptDigest(r closureprotocol.EvidenceReceipt) (string, error) {
	c := r
	c.Conflicts = nil
	return closureprotocol.SemanticDigest(c)
}

// sameSubject reports whether two receipts observe the same owner-path evidence subject: same
// evidence obligation (ProfileID) and same owner read-path.
func sameSubject(a, b closureprotocol.EvidenceReceipt) bool {
	return a.ProfileID == b.ProfileID && a.ObservationPath == b.ObservationPath
}
