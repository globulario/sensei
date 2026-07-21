// SPDX-License-Identifier: AGPL-3.0-only

package runtimeprobe

import (
	"fmt"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ToEvidenceReceipt maps an honest probe projection to the FROZEN closureprotocol.EvidenceReceipt
// (kind = runtime) — CP2 reuses that immutable, content-addressed receipt rather than inventing one.
// The payload digest is the content identity of exactly the files the probe read; two receipts with
// different payloads are tamper-evidently distinct.
func ToEvidenceReceipt(in ProbeObservationInput) (closureprotocol.EvidenceReceipt, error) {
	if err := ValidateInput(in); err != nil {
		return closureprotocol.EvidenceReceipt{}, err
	}
	payload, err := closureprotocol.SemanticDigest(sortedArtifacts(in.Artifacts))
	if err != nil {
		return closureprotocol.EvidenceReceipt{}, err
	}
	r := closureprotocol.EvidenceReceipt{
		ReceiptID:           "runtime-probe-receipt:" + in.ResultID,
		EvidenceKind:        closureprotocol.EvidenceRuntime,
		ProfileID:           profileID(in),
		Producer:            in.ExecutedBy,
		ObservationPath:     in.ObservationSource,
		ObservedAt:          in.ObservedAt,
		ExpiresAt:           in.ExpiresAt,
		Status:              receiptStatus(in),
		PayloadDigestSHA256: payload,
		// A probe establishes NO concrete runtime target (no deployment/node/instance). Leaving it nil
		// is honest; the owner read-path lives in ObservationPath. An owner-service NAME is not a
		// runtime target and is never elevated into one here.
		RuntimeTarget: nil,
	}
	if err := closureprotocol.ValidateEvidenceReceipt(r); err != nil {
		return closureprotocol.EvidenceReceipt{}, fmt.Errorf("runtimeprobe produced an invalid evidence receipt: %w", err)
	}
	return r, nil
}

// profileID is the evidence obligation the probe served — the Evidence node it targets, else the
// probe id. Never fabricated: it is a probe-supplied anchor.
func profileID(in ProbeObservationInput) string {
	if trimmed(in.EvidenceID) {
		return in.EvidenceID
	}
	return "probe:" + in.ProbeID
}

// receiptStatus reports the RECEIPT's validity (not the evidence outcome): a completed, fresh probe
// yields a valid receipt; a stale/historical one is stale; an inconclusive/unavailable/failed/
// rejected one is unknown. It never claims valid for an incomplete probe.
func receiptStatus(in ProbeObservationInput) closureprotocol.ReceiptStatus {
	switch in.ResultStatus {
	case "completed":
		switch in.EvidenceFreshness {
		case "stale", "historical":
			return closureprotocol.ReceiptStale
		case "unknown":
			return closureprotocol.ReceiptUnknown
		default:
			return closureprotocol.ReceiptValid
		}
	default:
		return closureprotocol.ReceiptUnknown
	}
}
