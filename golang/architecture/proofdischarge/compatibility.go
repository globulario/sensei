// SPDX-License-Identifier: AGPL-3.0-only

package proofdischarge

import (
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// CheckCompatibility reports whether a single validated receipt may discharge a
// single slot, and — when it may not — a machine-readable reason. Checks run
// most-fundamental first and short-circuit on the first failure so the outcome
// is deterministic. It assumes the receipt already passed Phase-4 validation and
// the Step-0.5 re-validation; it does not re-validate structural shape here.
//
// The predicate is the compatibility gate the brief describes: same result /
// shared proof context, correct evidence kind, governed observation path,
// correct authority domain, correct runtime target, current freshness, adequate
// trust, no unresolved conflict, and no revocation.
func CheckCompatibility(
	ob ProofObligation,
	slot ProofSlotSpec,
	receipt closureprotocol.EvidenceReceipt,
	profile closureprotocol.EvidenceProfile,
	ctx Context,
) (ok bool, reasons []string) {
	fail := func(code string) (bool, []string) { return false, []string{code} }

	// 1. Revocation.
	if ctx.RevokedReceiptIDs[strings.TrimSpace(receipt.ReceiptID)] {
		return fail(ReasonReceiptRevoked)
	}

	// 2. Evidence kind admitted by the slot kind (rejects `authority` kind too).
	if !evidenceKindAllowed(slot.Kind, receipt.EvidenceKind) {
		return fail(ReasonEvidenceKindMismatch)
	}

	// 3. Governing profile must be known and identify the receipt.
	if strings.TrimSpace(profile.ProfileID) == "" ||
		strings.TrimSpace(profile.ProfileID) != strings.TrimSpace(receipt.ProfileID) {
		return fail(ReasonProfileUnknown)
	}

	// 4. Observation path must be the profile's legal (owner) path — no
	//    arbitrary shell / broadened invocation.
	if !observationPathSatisfies(profile.LegalObservationPath, receipt.ObservationPath) {
		return fail(ReasonObservationPathUngoverned)
	}

	// 5. Same result / shared proof context.
	if !binding.ResultBindingEqual(receipt.ResultBinding, ctx.ResultBinding) {
		return fail(ReasonResultBindingMismatch)
	}

	// 6. Authority domain: a pinned profile target must intersect the
	//    obligation's authority surfaces.
	if !authorityDomainMatches(ob, profile) {
		return fail(ReasonAuthorityDomainMismatch)
	}

	// 7. Runtime target: runtime evidence must come from the target under proof.
	if code, okRT := checkRuntimeTarget(receipt, ctx); !okRT {
		return fail(code)
	}

	// 8. Trust must be attested.
	if !trustAttested(receipt.Trust) {
		return fail(ReasonTrustInsufficient)
	}

	// 9. Freshness: an explicit expiry at or before ObservedAt is stale.
	if code, okF := checkFreshness(receipt, ctx.ObservedAt); !okF {
		return fail(code)
	}

	// 10. No unresolved conflict with another live receipt.
	if hasUnresolvedConflict(receipt, ctx) {
		return fail(ReasonConflictUnresolved)
	}

	return true, nil
}

// observationPathSatisfies mirrors the owner-path rule the Phase-4 validator
// uses: the legal path may be namespaced as "<mechanism>.<path>"; a receipt path
// equal to the full string, to a trailing segment, or extending it is accepted.
// An empty legal or actual path never satisfies an owner-only profile.
func observationPathSatisfies(legal, actual string) bool {
	legal = strings.TrimSpace(legal)
	actual = strings.TrimSpace(actual)
	if legal == "" || actual == "" {
		return false
	}
	if legal == actual {
		return true
	}
	if strings.HasSuffix(legal, "."+actual) {
		return true
	}
	if strings.HasPrefix(actual, legal+".") {
		return true
	}
	return false
}

// authorityDomainMatches returns true unless the obligation pins authority
// surfaces and the profile pins a governed target that is not among them.
func authorityDomainMatches(ob ProofObligation, profile closureprotocol.EvidenceProfile) bool {
	target := strings.TrimSpace(profile.GovernedTarget)
	if target == "" || len(ob.AppliesToAuthoritySurfaces) == 0 {
		return true
	}
	for _, s := range ob.AppliesToAuthoritySurfaces {
		if strings.TrimSpace(s) == target {
			return true
		}
	}
	return false
}

// checkRuntimeTarget compares a runtime-bearing receipt's target against the one
// under proof. Non-runtime receipts, and runs with no pinned runtime target, are
// not gated here.
func checkRuntimeTarget(receipt closureprotocol.EvidenceReceipt, ctx Context) (string, bool) {
	if ctx.RuntimeTarget == nil {
		return "", true
	}
	if receipt.EvidenceKind != closureprotocol.EvidenceRuntime && receipt.RuntimeTarget == nil {
		return "", true
	}
	if !runtimeTargetEqual(receipt.RuntimeTarget, ctx.RuntimeTarget) {
		return ReasonRuntimeTargetMismatch, false
	}
	return "", true
}

func runtimeTargetEqual(a, b *closureprotocol.RuntimeTarget) bool {
	if a == nil || b == nil {
		return a == b
	}
	return strings.TrimSpace(a.Platform) == strings.TrimSpace(b.Platform) &&
		strings.TrimSpace(a.EnvironmentID) == strings.TrimSpace(b.EnvironmentID) &&
		strings.TrimSpace(a.DeploymentID) == strings.TrimSpace(b.DeploymentID) &&
		strings.TrimSpace(a.ReleaseRevision) == strings.TrimSpace(b.ReleaseRevision) &&
		strings.TrimSpace(a.ConfigurationGeneration) == strings.TrimSpace(b.ConfigurationGeneration) &&
		stringSetEqual(a.NodeIDs, b.NodeIDs) &&
		stringSetEqual(a.ServiceInstances, b.ServiceInstances)
}

func trustAttested(trust string) bool {
	t := strings.TrimSpace(trust)
	return t != "" && t != TrustUnattested
}

// checkFreshness treats an explicit expiry at or before ObservedAt as stale,
// regardless of the receipt's self-declared status (an upstream staleness sweep
// may not have run yet). ObservedAt is the deterministic evaluation time.
func checkFreshness(receipt closureprotocol.EvidenceReceipt, observedAt string) (string, bool) {
	expires := strings.TrimSpace(receipt.ExpiresAt)
	if expires == "" {
		return "", true
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return ReasonFreshnessExpired, false
	}
	now, err := time.Parse(time.RFC3339, strings.TrimSpace(observedAt))
	if err != nil {
		// Caller guarantees ObservedAt is valid (Step 0); fail closed anyway.
		return ReasonFreshnessExpired, false
	}
	if !exp.After(now) {
		return ReasonFreshnessExpired, false
	}
	return "", true
}

// hasUnresolvedConflict reports whether the receipt is in a conflict with
// another receipt in the context that is still live (not superseded, invalid,
// revoked, or in the revocation index). Conflicts are read from either side's
// Conflicts list.
func hasUnresolvedConflict(receipt closureprotocol.EvidenceReceipt, ctx Context) bool {
	id := strings.TrimSpace(receipt.ReceiptID)
	for _, other := range ctx.Receipts {
		oid := strings.TrimSpace(other.ReceiptID)
		if oid == "" || oid == id {
			continue
		}
		if !conflictReferenced(receipt, other) {
			continue
		}
		if receiptLive(other, ctx) {
			return true
		}
	}
	return false
}

func conflictReferenced(a, b closureprotocol.EvidenceReceipt) bool {
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

func receiptLive(r closureprotocol.EvidenceReceipt, ctx Context) bool {
	if ctx.RevokedReceiptIDs[strings.TrimSpace(r.ReceiptID)] {
		return false
	}
	switch r.Status {
	case closureprotocol.ReceiptSuperseded, closureprotocol.ReceiptInvalid, closureprotocol.ReceiptRevoked:
		return false
	}
	return true
}

func stringSetEqual(a, b []string) bool {
	as := closureprotocol.NormalizeSet(a)
	bs := closureprotocol.NormalizeSet(b)
	if len(as) != len(bs) {
		return false
	}
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
