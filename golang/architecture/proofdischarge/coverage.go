// SPDX-License-Identifier: Apache-2.0

package proofdischarge

import "github.com/globulario/sensei/golang/architecture/closureprotocol"

// SlotDisposition is the coverage-profile decision for one slot. It is a pure
// function of (slot.Kind, slot.Required, coverageProfile) and is NEVER
// conditioned on whether evidence happens to exist for the slot.
type SlotDisposition int

const (
	SlotRequired SlotDisposition = iota
	SlotNotApplicableUnderProfile
	SlotOptional
)

// ResolveSlotDisposition decides, before any receipt is considered, whether a
// slot must be discharged, is optional, or is not applicable under the coverage
// profile. The decision is obligation-driven, not merely repository-driven.
//
// The coverage profile sets the DEFAULT disposition only for runtime-owner-path
// slots whose runtime requirement is NOT mandated by the obligation. When a
// governed obligation MANDATES runtime evidence (ob.RequiresRuntimeEvidence),
// its runtime-owner-path slots stay required regardless of profile — including
// under static_test — so a missing compatible runtime receipt yields a
// blocked/uncertifiable obligation, NEVER not_applicable. A runtime lane is
// not_applicable only when no applicable required proof slot needs runtime
// evidence AND the profile permits relaxation; it is required when a governed
// obligation requires it OR the claim cannot be established statically or by
// test. Static/test-satisfiable kinds are never relaxed.
func ResolveSlotDisposition(ob ProofObligation, slot ProofSlotSpec, coverageProfile string) SlotDisposition {
	if !slot.Required {
		return SlotOptional
	}
	if !isRuntimeOwnerPathKind(slot.Kind) {
		return SlotRequired
	}
	// Runtime-owner-path kind:
	if ob.RequiresRuntimeEvidence {
		return SlotRequired // governed mandate overrides the profile, incl. static_test
	}
	if coverageProfile == CoverageStaticTestRuntime {
		return SlotRequired
	}
	// static_test (default), not mandated: profile-relaxable.
	return SlotNotApplicableUnderProfile
}

func isRuntimeOwnerPathKind(kind string) bool {
	for _, k := range RuntimeOwnerPathSlotKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// AllowedEvidenceKinds returns the evidence_kinds a slot kind will accept as
// candidates. A receipt whose evidence_kind is not in this set is filtered out
// before the compatibility predicate ever runs. `authority`-kind receipts back
// AuthorityResolution, not ProofDischarge, and are in no set.
func AllowedEvidenceKinds(slotKind string) []closureprotocol.EvidenceKind {
	switch slotKind {
	case SlotKindStaticGuard, SlotKindScopeMapping, SlotKindBeforeAfter,
		SlotKindArtifact, SlotKindInputValidation, SlotKindNegativeContract:
		return []closureprotocol.EvidenceKind{closureprotocol.EvidenceStatic, closureprotocol.EvidenceArtifact}
	case SlotKindProcessArtifact, SlotKindLogArtifact:
		return []closureprotocol.EvidenceKind{closureprotocol.EvidenceArtifact, closureprotocol.EvidenceRuntime}
	case SlotKindTestOrRuntime:
		return []closureprotocol.EvidenceKind{closureprotocol.EvidenceTest, closureprotocol.EvidenceRuntime}
	case SlotKindRuntime:
		return []closureprotocol.EvidenceKind{closureprotocol.EvidenceRuntime}
	case SlotKindFailureEvidence:
		return []closureprotocol.EvidenceKind{closureprotocol.EvidenceRuntime, closureprotocol.EvidenceTest, closureprotocol.EvidenceHybrid}
	default:
		return nil
	}
}

func evidenceKindAllowed(slotKind string, kind closureprotocol.EvidenceKind) bool {
	for _, k := range AllowedEvidenceKinds(slotKind) {
		if k == kind {
			return true
		}
	}
	return false
}
