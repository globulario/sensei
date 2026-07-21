// SPDX-License-Identifier: AGPL-3.0-only

package runtimeprobe

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	rb "github.com/globulario/sensei/golang/architecture/runtimeboundary"
)

// ToRuntimeObservation maps the honest probe projection + its receipt into a runtimeboundary
// observation. THE HONESTY IS IN WHAT IS LEFT UNRESOLVED: a probe establishes NONE of the governed
// crossing identity, so the mapper populates none of it —
//   - CallerIdentity, CalleeIdentity, EndpointOrContractIdentity = "" (unresolved, never invented);
//   - InteractionKind = unknown; Direction = unknown; RuntimeTarget = empty.
//
// The evidence-node anchor and owner-service are carried ONLY in Provenance — never in the
// contract/endpoint field — so an evidence anchor can never be mistaken for a governed contract or
// endpoint authority. What a probe DOES establish (collector, evidence digest, freshness, integrity,
// truncation) is carried in the fields that mean exactly that. Because the crossing identity is
// unresolved, runtimeboundary.admitObservations refuses this observation (ambiguous_identity) — by
// design; evidence-level proof must not become a crossing verdict.
func ToRuntimeObservation(in ProbeObservationInput, receipt closureprotocol.EvidenceReceipt) (rb.RuntimeObservation, error) {
	if err := ValidateInput(in); err != nil {
		return rb.RuntimeObservation{}, err
	}
	avail, reason := availability(in)
	obs := rb.RuntimeObservation{
		ObservationID:              in.ResultID,
		SchemaVersion:              rb.ObservationSchema,
		Direction:                  rb.DirectionUnknown,
		CallerIdentity:             "", // no governed caller — unresolved, never invented
		CalleeIdentity:             "", // a probe names no governed callee — provenance only
		EndpointOrContractIdentity: "", // NO contract/endpoint — the evidence anchor is provenance only
		InteractionKind:            rb.InteractionUnknown,
		AuthContextPresent:         false,
		AuthorityClass:             "",
		RuntimeTarget:              closureprotocol.RuntimeTarget{}, // a probe establishes no runtime target
		CollectorID:                in.ExecutedBy,
		EvidenceDigestSHA256:       receipt.PayloadDigestSHA256,
		Provenance:                 sortedUnique([]string{receipt.ReceiptID, in.EvidenceID, in.OwnerService, in.ProbeID}),
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
