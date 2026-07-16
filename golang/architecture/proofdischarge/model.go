// SPDX-License-Identifier: Apache-2.0

// Package proofdischarge implements Phase 5 (proof execution & discharge) of the
// architectural closure protocol. It maps Phase-4-validated evidence receipts to
// the proof slots of a governed proof obligation and emits, per obligation, a
// schema-conformant closureprotocol.ProofDischarge plus a diagnostic
// DischargeReport carrying the per-receipt reason codes the frozen schema has no
// room for.
//
// This package does NOT redefine any frozen closure-protocol type. ProofDischarge,
// ProofSlotResult, their validator (ValidateProofDischarge) and digest
// (ProofDischargeDigest) all live in golang/architecture/closureprotocol and are
// imported and reused here — mirroring how the Phase-4 evidencereceipt package
// aliases the frozen primitives.
//
// Determinism is a hard requirement: the engine never reads the wall clock (the
// caller supplies ObservedAt) and never iterates a map without a subsequent sort.
package proofdischarge

import (
	"os"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/probe"
	"gopkg.in/yaml.v3"
)

const (
	SchemaVersion = "1"
	GeneratedBy   = "sensei discharge-proofs"

	CoverageStaticTest        = "static_test"         // default
	CoverageStaticTestRuntime = "static_test_runtime" // opt-in

	// Slot-kind vocabulary. Copied from
	// docs/awareness/generated/proof_obligations.yaml required_slots[].kind and
	// from probe's ProofSlotKinds groupings. proofdischarge invents no new kinds.
	SlotKindStaticGuard      = "static_guard"
	SlotKindScopeMapping     = "scope_mapping"
	SlotKindBeforeAfter      = "before_after"
	SlotKindArtifact         = "artifact"
	SlotKindInputValidation  = "input_validation"
	SlotKindNegativeContract = "negative_contract"
	SlotKindTestOrRuntime    = "test_or_runtime"
	SlotKindRuntime          = "runtime"
	SlotKindProcessArtifact  = "process_artifact"
	SlotKindLogArtifact      = "log_artifact"
	SlotKindFailureEvidence  = "failure_evidence"
)

// RuntimeOwnerPathSlotKinds are the slot kinds that can only be satisfied by a
// live runtime observation of the owning service — exactly the four kinds the
// `service_lifecycle` proof-obligation template emits. coverage.go downgrades
// these to not_applicable under the default (static_test) profile.
var RuntimeOwnerPathSlotKinds = []string{
	SlotKindRuntime, SlotKindProcessArtifact, SlotKindLogArtifact, SlotKindFailureEvidence,
}

// ProofObligation is the engine's read model of one governed proof obligation,
// produced by LoadFromYAML / LoadFromGraph so the engine never touches RDF or
// extractor internals directly.
type ProofObligation struct {
	ID                          string          `json:"id" yaml:"id"`
	Status                      string          `json:"status" yaml:"status"` // candidate|approved authored lifecycle status; not a receipt_status
	DerivedFromAuthoritySurface string          `json:"derived_from_authority_surface,omitempty" yaml:"derived_from_authority_surface,omitempty"`
	AppliesToAuthoritySurfaces  []string        `json:"applies_to_authority_surfaces,omitempty" yaml:"applies_to_authority_surfaces,omitempty"`
	EvidenceLane                string          `json:"evidence_lane,omitempty" yaml:"evidence_lane,omitempty"`
	// RequiresRuntimeEvidence is the governed-mandate signal: when true, this
	// obligation MANDATES runtime evidence, so its runtime-owner-path slots stay
	// required regardless of coverage profile (including under static_test) and
	// can never be silently relaxed to not_applicable. Default false leaves such
	// slots profile-relaxable (the coverage profile alone decides). This exists
	// because a claim that cannot be established statically or by test — leader
	// election, durability, failover, consistency — must not be rubber-stamped by
	// the default profile.
	RequiresRuntimeEvidence bool            `json:"requires_runtime_evidence,omitempty" yaml:"requires_runtime_evidence,omitempty"`
	RequiredSlots           []ProofSlotSpec `json:"required_slots" yaml:"required_slots"`
}

type ProofSlotSpec struct {
	ID          string `json:"id" yaml:"id"`
	Kind        string `json:"kind" yaml:"kind"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required" yaml:"required"` // authored flag; the coverage profile may still relax it to not_applicable
}

// Context is everything Discharge needs. Every collection is expected
// pre-validated by its own package (closureprotocol validators / Phase-4
// evidencereceipt). Discharge re-checks defensively (fail-closed) but does not
// attempt to repair invalid input.
type Context struct {
	Obligations   []ProofObligation
	ResultBinding closureprotocol.ResultBinding // the exact result being proved
	Task          closureprotocol.TaskBinding

	CoverageProfile string                         // CoverageStaticTest (default) | CoverageStaticTestRuntime
	RuntimeTarget   *closureprotocol.RuntimeTarget // required only under static_test_runtime when an obligation has a runtime-owner-path required slot

	Profiles map[string]closureprotocol.EvidenceProfile // profile_id -> profile, Phase-4-validated
	Receipts []closureprotocol.EvidenceReceipt          // Phase-4-validated receipts

	Waivers              []closureprotocol.WaiverReceipt
	GovernanceExceptions map[string]GovernanceException // keyed by WaiverReceipt.PolicyID

	RevokedReceiptIDs map[string]bool // receipt_id -> true

	ObservedAt string // RFC3339, the deterministic "now" for every freshness/expiry comparison
}

// BuildRevokedSet reduces a list of revocation receipts to the receipt-id set
// Discharge consumes. Phase 5 provides this helper so a caller that only has raw
// RevocationReceipts does not have to reimplement the reduction.
func BuildRevokedSet(revocations []closureprotocol.RevocationReceipt) map[string]bool {
	out := make(map[string]bool, len(revocations))
	for _, r := range revocations {
		if id := r.RevokedTargetID; id != "" {
			out[id] = true
		}
	}
	return out
}

// LoadFromYAML loads proof obligations from the authored YAML shape emitted by
// `sensei extract-proof-obligations` (docs/awareness/generated/proof_obligations.yaml).
func LoadFromYAML(path string) ([]ProofObligation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		ProofObligations []ProofObligation `yaml:"proof_obligations"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.ProofObligations, nil
}

// LoadFromGraph is a thin adapter from the compiled graph read model: it joins
// each proof_obligation node with its proof_slot nodes over the ProofSlots edge.
// It does not duplicate GraphIndex's triple-walking logic.
func LoadFromGraph(g probe.GraphIndex) ([]ProofObligation, error) {
	var out []ProofObligation
	for _, node := range g.Class("proof_obligation") {
		ob := ProofObligation{
			ID:                          node.ID,
			Status:                      node.Status,
			DerivedFromAuthoritySurface: node.DerivedFromAuthoritySurface,
			AppliesToAuthoritySurfaces:  node.AppliesToAuthoritySurfaces,
			EvidenceLane:                node.EvidenceLane,
			// A non-empty requiresRuntimeEvidence on the obligation node is the
			// governed mandate that its runtime-owner-path slots stay required.
			RequiresRuntimeEvidence: len(node.RequiresRuntimeEvidence) > 0,
		}
		for _, slotID := range node.ProofSlots {
			slotNode, ok := g.Node("proof_slot", slotID)
			if !ok {
				continue
			}
			ob.RequiredSlots = append(ob.RequiredSlots, ProofSlotSpec{
				ID:          slotNode.ID,
				Kind:        slotNode.SlotKind,
				Description: slotNode.Comment,
				Required:    slotNode.Required,
			})
		}
		out = append(out, ob)
	}
	return out, nil
}
