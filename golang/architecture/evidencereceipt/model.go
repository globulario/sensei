// SPDX-License-Identifier: Apache-2.0

// Package evidencereceipt implements the evidence layer of the architectural
// closure protocol (Phase 4). It provides typed evidence receipts (reusing the
// frozen closureprotocol primitives), evidence-profile validation, a validator
// that computes the effective status of a receipt against a proof request, and
// coverage-profile evaluation that folds receipt statuses into certification
// lanes.
//
// Core law: missing, stale, conflicting, or non-owner-path evidence must yield
// UNKNOWN / STALE / CONFLICTED / INVALID — never a PASS. The *validator*
// (not the producer of a receipt) decides whether a receipt is current and
// valid for a given proof request.
package evidencereceipt

import (
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Typed reuse of the frozen closure-protocol primitives. Evidence receipts are
// not reinvented here; this package layers validation semantics on top of the
// shared schema-backed types.
type (
	Receipt       = closureprotocol.EvidenceReceipt
	Profile       = closureprotocol.EvidenceProfile
	ResultBinding = closureprotocol.ResultBinding
	RuntimeTarget = closureprotocol.RuntimeTarget
	Status        = closureprotocol.ReceiptStatus
	LaneStatus    = closureprotocol.DimensionStatus
)

// Reason codes are stable, machine-readable explanations attached to an
// assessment. They mirror the closure-fixture vocabulary
// (e.g. evidence.receipt.expired, evidence.owner_path.conflicted).
const (
	ReasonReceiptMalformed      = "evidence.receipt.malformed"
	ReasonProfileMismatch       = "evidence.profile.mismatch"
	ReasonOwnerPathViolation    = "evidence.owner_path.violation"
	ReasonOwnerPathConflicted   = "evidence.owner_path.conflicted"
	ReasonRepositoryMismatch    = "evidence.repository.mismatch"
	ReasonResultTreeMismatch    = "evidence.result_tree.mismatch"
	ReasonGraphMismatch         = "evidence.graph.mismatch"
	ReasonRuntimeMissing        = "evidence.runtime.missing"
	ReasonRuntimeTargetMismatch = "evidence.runtime.target_mismatch"
	ReasonReceiptExpired        = "evidence.receipt.expired"
	ReasonFreshnessUnobserved   = "evidence.freshness.unobserved"
	ReasonReceiptRevoked        = "evidence.receipt.revoked"
	ReasonReceiptSuperseded     = "evidence.receipt.superseded"
	ReasonLaneMissing           = "evidence.lane.missing"
)

// Assessment is the validator's verdict for a single receipt against a proof
// request. Status is the *effective* status computed by the validator, which
// deliberately ignores the receipt's self-declared status except for terminal
// registry states (revoked / superseded).
type Assessment struct {
	ReceiptID   string   `json:"receipt_id" yaml:"receipt_id"`
	Status      Status   `json:"status" yaml:"status"`
	ReasonCodes []string `json:"reason_codes,omitempty" yaml:"reason_codes,omitempty"`
}

// OK reports whether the receipt is effectively valid.
func (a Assessment) OK() bool { return a.Status == closureprotocol.ReceiptValid }

func assess(receipt Receipt, status Status, reasons ...string) Assessment {
	out := Assessment{ReceiptID: strings.TrimSpace(receipt.ReceiptID), Status: status}
	for _, r := range reasons {
		if strings.TrimSpace(r) != "" {
			out.ReasonCodes = append(out.ReasonCodes, r)
		}
	}
	return out
}

// ValidateReceipt checks the structural shape of a receipt against the frozen
// evidence-receipt schema. It is the producer-side guard and does not compute
// effective status.
func ValidateReceipt(r Receipt) error {
	return closureprotocol.ValidateEvidenceReceipt(r)
}

// Canonicalize returns a deterministic form of the receipt: set-like fields are
// trimmed, de-duplicated and sorted so equal receipts digest identically
// regardless of input ordering.
func Canonicalize(r Receipt) Receipt {
	r.Conflicts = closureprotocol.NormalizeSet(r.Conflicts)
	return r
}

// CanonicalJSON returns the canonical JSON encoding of a receipt.
func CanonicalJSON(r Receipt) ([]byte, error) {
	return closureprotocol.CanonicalJSON(Canonicalize(r))
}

// Digest returns the semantic SHA-256 digest of a receipt over its canonical
// form. It matches the closureprotocol digest convention.
func Digest(r Receipt) (string, error) {
	return closureprotocol.SemanticDigest(Canonicalize(r))
}

// Conflict records that two receipts disagree about the same subject and cannot
// both be authoritative.
type Conflict struct {
	ReceiptA string `json:"receipt_a" yaml:"receipt_a"`
	ReceiptB string `json:"receipt_b" yaml:"receipt_b"`
	Reason   string `json:"reason" yaml:"reason"`
}

// DetectConflicts returns the set of pairwise conflicts among receipts. Two
// receipts conflict when they observe the same subject (same profile + result
// tree) but carry different payload digests, or when one explicitly lists the
// other in its conflicts field. The result is deterministic (sorted by id).
func DetectConflicts(receipts []Receipt) []Conflict {
	sorted := append([]Receipt(nil), receipts...)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.TrimSpace(sorted[i].ReceiptID) < strings.TrimSpace(sorted[j].ReceiptID)
	})
	var out []Conflict
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			a, b := sorted[i], sorted[j]
			if strings.TrimSpace(a.ReceiptID) == "" || strings.TrimSpace(a.ReceiptID) == strings.TrimSpace(b.ReceiptID) {
				continue
			}
			if sameSubject(a, b) && strings.TrimSpace(a.PayloadDigestSHA256) != strings.TrimSpace(b.PayloadDigestSHA256) {
				out = append(out, Conflict{ReceiptA: a.ReceiptID, ReceiptB: b.ReceiptID, Reason: ReasonOwnerPathConflicted})
				continue
			}
			if referencesConflict(a, b) {
				out = append(out, Conflict{ReceiptA: a.ReceiptID, ReceiptB: b.ReceiptID, Reason: ReasonOwnerPathConflicted})
			}
		}
	}
	return out
}

func sameSubject(a, b Receipt) bool {
	return strings.TrimSpace(a.ProfileID) == strings.TrimSpace(b.ProfileID) &&
		strings.TrimSpace(a.ResultBinding.BaseRevision) == strings.TrimSpace(b.ResultBinding.BaseRevision) &&
		strings.TrimSpace(a.ResultBinding.ResultTreeDigestSHA256) == strings.TrimSpace(b.ResultBinding.ResultTreeDigestSHA256)
}

func referencesConflict(a, b Receipt) bool {
	for _, c := range a.Conflicts {
		if strings.TrimSpace(c) == strings.TrimSpace(b.ReceiptID) {
			return true
		}
	}
	for _, c := range b.Conflicts {
		if strings.TrimSpace(c) == strings.TrimSpace(a.ReceiptID) {
			return true
		}
	}
	return false
}

func isEvidenceKind(v closureprotocol.EvidenceKind) bool {
	for _, k := range closureprotocol.EvidenceKinds {
		if k == v {
			return true
		}
	}
	return false
}
