// SPDX-License-Identifier: AGPL-3.0-only

package runtimeprobe

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	rb "github.com/globulario/sensei/golang/architecture/runtimeboundary"
)

// ToRuntimeObservation maps the honest probe projection + its receipt into a runtimeboundary
// observation. THE HONESTY IS IN WHAT IS LEFT EMPTY:
//   - CallerIdentity = "" — a probe has no governed caller. Unresolved, NEVER invented.
//   - EndpointOrContractIdentity = the Evidence-node anchor, NOT a contract (probes don't reference
//     contracts). It is honest provenance, not a governed contract identity.
//   - CalleeIdentity = the single owner service, only if the probe genuinely names one.
//
// Because the caller is genuinely unresolved, runtimeboundary.admitObservations will refuse this
// observation (ambiguous_identity) — by design. The mapper NEVER sets caller/contract to force
// admission; evidence-level proof must not become a crossing verdict.
func ToRuntimeObservation(in ProbeObservationInput, receipt closureprotocol.EvidenceReceipt) (rb.RuntimeObservation, error) {
	if err := ValidateInput(in); err != nil {
		return rb.RuntimeObservation{}, err
	}
	avail, reason := availability(in)
	obs := rb.RuntimeObservation{
		ObservationID:              in.ResultID,
		SchemaVersion:              rb.ObservationSchema,
		Direction:                  rb.DirectionUnknown,
		CallerIdentity:             "", // no governed caller — honest, not invented
		CalleeIdentity:             in.OwnerService,
		EndpointOrContractIdentity: in.EvidenceID, // evidence anchor, not a contract
		InteractionKind:            rb.InteractionRead,
		AuthContextPresent:         false,
		AuthorityClass:             "",
		RuntimeTarget:              targetValue(receipt.RuntimeTarget),
		CollectorID:                in.ExecutedBy,
		EvidenceDigestSHA256:       receipt.PayloadDigestSHA256,
		Provenance:                 sortedUnique([]string{receipt.ReceiptID, in.EvidenceID, in.ProbeID}),
		Availability:               avail,
		Freshness:                  freshness(in.EvidenceFreshness),
		IntegrityVerified:          integrityVerified(in),
		Truncated:                  in.BudgetExhausted,
		ReasonCode:                 reason,
	}
	if err := rb.ValidateObservation(obs); err != nil {
		return rb.RuntimeObservation{}, err
	}
	return obs, nil
}

// availability maps the probe result status honestly. It NEVER reports available for an incomplete
// probe. An available observation carries the collector + evidence digest (both present here); a
// non-available one carries a typed reason.
func availability(in ProbeObservationInput) (rb.SourceAvailability, string) {
	switch in.ResultStatus {
	case "completed":
		return rb.SourceAvailable, ""
	case "unavailable":
		return rb.SourceUnavailable, "probe_unavailable"
	case "rejected":
		return rb.SourceInvalid, "probe_rejected"
	default: // inconclusive, failed
		return rb.SourceDegraded, "probe_" + in.ResultStatus
	}
}

// freshness maps declared evidence freshness. Unknown/absent freshness stays unknown, never fresh.
func freshness(f string) rb.Freshness {
	switch f {
	case "current":
		return rb.FreshnessFresh
	case "stale", "historical":
		return rb.FreshnessStale
	default:
		return rb.FreshnessUnknown
	}
}

// integrityVerified is true only when the probe completed and produced at least one content digest —
// content integrity, honestly. It never asserts integrity for an incomplete probe or an empty read.
func integrityVerified(in ProbeObservationInput) bool {
	if in.ResultStatus != "completed" {
		return false
	}
	for _, a := range in.Artifacts {
		if trimmed(a.SHA256) {
			return true
		}
	}
	return false
}

func targetValue(t *closureprotocol.RuntimeTarget) closureprotocol.RuntimeTarget {
	if t == nil {
		return closureprotocol.RuntimeTarget{}
	}
	return *t
}
