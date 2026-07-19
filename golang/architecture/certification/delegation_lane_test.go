// SPDX-License-Identifier: Apache-2.0

package certification

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// These tests prove the authority lane re-verifies delegated authority
// independently: it resolves the resolution's delegation_chain to concrete
// recorded receipts bound to the actor's committed digests and re-runs the
// governed monotonicity verdict against governed grants — a resolution can never
// certify a delegation the governed grants do not permit, nor invent one whose
// record was never preserved.

// delegatedGovernedIndex is the governed authority the certifier loads
// independently. It grants the owner a delegable repository-edit authority over
// authority.core and models the delegation policy the receipt rides on. It is
// deliberately minimal — the monotonicity verdict consults only grants,
// delegation policies, and mutation paths.
func delegatedGovernedIndex() authority.PolicyIndex {
	return authority.PolicyIndex{
		MutationPaths: map[string]authority.MutationPath{
			"mutation_path.repo_edit": {
				ID:            "mutation_path.repo_edit",
				Status:        "active",
				MechanismKind: closureprotocol.MechanismRepositoryEdit,
				TargetKinds:   []string{"file"},
			},
		},
		DelegationPolicies: map[string]authority.DelegationPolicy{
			"delegation_policy.core": {
				ID:                  "delegation_policy.core",
				Status:              "active",
				MaximumDepth:        1,
				AllowSubdelegation:  false,
				AllowedActions:      []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationRead},
				AllowedMechanismIDs: []string{"mutation_path.repo_edit"},
			},
		},
		AuthorityGrants: map[string]authority.AuthorityGrant{
			"grant.core.owner": {
				ID:                   "grant.core.owner",
				Status:               "active",
				ActorRoleIDs:         []string{"role.core"},
				AuthorityDomainIDs:   []string{"authority.core"},
				Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationRead, closureprotocol.OperationCreate},
				TargetKinds:          []string{"file"},
				RequiredMechanismIDs: []string{"mutation_path.repo_edit"},
				MaximumRiskClass:     "architecture_sensitive",
				ValidFrom:            "2026-07-14T00:00:00Z",
				Delegable:            true,
				DelegationPolicyID:   "delegation_policy.core",
			},
		},
	}
}

// delegatedReceipt is a monotonic one-level delegation under grant.core.owner
// that legitimately authorizes the green modify operation at greenEvaluatedAt.
func delegatedReceipt() closureprotocol.DelegationReceipt {
	return closureprotocol.DelegationReceipt{
		DelegationID:         "delegation.core.agent",
		ParentGrantID:        "grant.core.owner",
		DelegatorPrincipalID: "actor.dave",
		DelegatedPrincipalID: "actor.agent",
		RoleIDs:              []string{"role.core"},
		AuthorityDomainIDs:   []string{"authority.core"},
		Actions:              []closureprotocol.OperationKind{closureprotocol.OperationModify},
		MechanismKinds:       []closureprotocol.MechanismKind{closureprotocol.MechanismRepositoryEdit},
		TargetKinds:          []string{"file"},
		MaximumRiskClass:     "architecture_sensitive",
		PolicyID:             "delegation_policy.core",
		Issuer:               "local-review",
		IssuedAt:             "2026-07-14T00:00:00Z",
		ValidFrom:            "2026-07-14T00:00:00Z",
		ValidUntil:           "2026-07-16T00:00:00Z",
		Status:               closureprotocol.ReceiptValid,
	}
}

type delegatedOpts struct {
	mutateReceipt func(*closureprotocol.DelegationReceipt)
	mutateIndex   func(*authority.PolicyIndex)
	mutateOp      func(*closureprotocol.ChangeOperation)
	extraReceipts []closureprotocol.DelegationReceipt
	// chain overrides the resolution's delegation chain (default: the baseline
	// receipt's id).
	chain []string
	// committed overrides the actor's committed digests (default: the digest of
	// every recorded receipt).
	committed []string
}

// buildDelegated assembles a delegated certification bundle from the all-green
// baseline, applies the requested mutations, and re-derives every dependent
// digest so Request and Records stay a verifiable pair.
func buildDelegated(t *testing.T, opts delegatedOpts) (Request, Records) {
	t.Helper()
	_, rec := greenBundle(t)

	index := delegatedGovernedIndex()
	if opts.mutateIndex != nil {
		opts.mutateIndex(&index)
	}
	rec.GovernedAuthority = index

	if opts.mutateOp != nil {
		opts.mutateOp(&rec.AdmissionRequest.ChangePlan.Operations[0])
	}

	receipt := delegatedReceipt()
	if opts.mutateReceipt != nil {
		opts.mutateReceipt(&receipt)
	}
	receipts := append([]closureprotocol.DelegationReceipt{receipt}, opts.extraReceipts...)
	rec.DelegationReceipts = receipts

	committed := opts.committed
	if committed == nil {
		for _, r := range receipts {
			committed = append(committed, mustDigest(t, r))
		}
	}
	rec.AdmissionRequest.ActorBinding.ActorKind = closureprotocol.ActorAgent
	rec.AdmissionRequest.ActorBinding.DelegationReceiptDigests = committed
	rec.CapabilityConsumption.ConsumerActor = rec.AdmissionRequest.ActorBinding

	chain := opts.chain
	if chain == nil {
		chain = []string{receipt.DelegationID}
	}
	rec.AuthorityResolutions[0].OperationResults[0].DelegationChain = chain

	// Re-derive the digests that depend on the mutated actor and operation.
	requestDigest := mustDigest(t, rec.AdmissionRequest)
	rec.AdmissionDecision.RequestDigestSHA256 = requestDigest
	decisionDigest := mustDigest(t, rec.AdmissionDecision)
	rec.CapabilityConsumption.DecisionDigestSHA256 = decisionDigest
	rec.ScopeVerification.DecisionDigestSHA256 = decisionDigest

	return rebindGreen(t, rec), rec
}

func hasReasonSubstring(lane LaneResult, sub string) bool {
	for _, r := range lane.ReasonCodes {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

// (1) A direct owner grant with no delegation still certifies — the new lane
// logic only engages when the actor asserts delegation.
func TestDelegation_DirectGrantStillCertifies(t *testing.T) {
	req, rec := greenBundle(t)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified", result.Receipt.CertificationVerdict)
	}
}

// (2) A valid, monotonic one-level delegation certifies.
func TestDelegation_ValidOneLevelCertifies(t *testing.T) {
	req, rec := buildDelegated(t, delegatedOpts{})
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	lane := laneByName(t, result, LaneAuthority)
	if lane.Status != closureprotocol.DimensionPass {
		t.Fatalf("authority lane = %s %v, want pass", lane.Status, lane.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified", result.Receipt.CertificationVerdict)
	}
}

// (3-8) Broadening / lifecycle violations block with the precise governed
// monotonicity verdict.
func TestDelegation_MonotonicityViolationsBlock(t *testing.T) {
	cases := []struct {
		name    string
		opts    delegatedOpts
		verdict authority.DelegationVerdict
	}{
		{
			name: "action broadening",
			opts: delegatedOpts{mutateReceipt: func(r *closureprotocol.DelegationReceipt) {
				r.Actions = []closureprotocol.OperationKind{closureprotocol.OperationModify, closureprotocol.OperationDelete}
			}},
			verdict: authority.DelegationActionBroadening,
		},
		{
			name: "domain broadening",
			opts: delegatedOpts{mutateReceipt: func(r *closureprotocol.DelegationReceipt) {
				r.AuthorityDomainIDs = []string{"authority.core", "authority.extra"}
			}},
			verdict: authority.DelegationDomainBroadening,
		},
		{
			name: "mechanism broadening",
			opts: delegatedOpts{mutateReceipt: func(r *closureprotocol.DelegationReceipt) {
				r.MechanismKinds = []closureprotocol.MechanismKind{closureprotocol.MechanismOwnerRPC}
			}},
			verdict: authority.DelegationMechanismBroadening,
		},
		{
			name: "time broadening beyond parent",
			opts: delegatedOpts{
				mutateIndex: func(i *authority.PolicyIndex) {
					g := i.AuthorityGrants["grant.core.owner"]
					g.ValidUntil = "2026-07-15T18:00:00Z"
					i.AuthorityGrants["grant.core.owner"] = g
				},
				mutateReceipt: func(r *closureprotocol.DelegationReceipt) { r.ValidUntil = "2026-07-16T00:00:00Z" },
			},
			verdict: authority.DelegationTimeBroadening,
		},
		{
			name:    "expired",
			opts:    delegatedOpts{mutateReceipt: func(r *closureprotocol.DelegationReceipt) { r.ValidUntil = "2026-07-15T06:00:00Z" }},
			verdict: authority.DelegationExpired,
		},
		{
			name:    "revoked",
			opts:    delegatedOpts{mutateReceipt: func(r *closureprotocol.DelegationReceipt) { r.Status = closureprotocol.ReceiptRevoked }},
			verdict: authority.DelegationRevoked,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, rec := buildDelegated(t, tc.opts)
			result := mustEvaluate(t, req, rec, DefaultPolicy())
			lane := laneByName(t, result, LaneAuthority)
			if lane.Status != closureprotocol.DimensionBlocked {
				t.Fatalf("authority lane = %s, want blocked", lane.Status)
			}
			if !hasReasonSubstring(lane, string(tc.verdict)) {
				t.Fatalf("reasons %v do not carry verdict %q", lane.ReasonCodes, tc.verdict)
			}
			if result.Receipt.CertificationVerdict == closureprotocol.Certified {
				t.Fatal("a broadening delegation must not certify")
			}
		})
	}
}

// (9/13) A delegation chain referencing an id with no recorded receipt is never
// reconstructed — it blocks, and certification does not invent it.
func TestDelegation_UnrecordedChainBlocks(t *testing.T) {
	req, rec := buildDelegated(t, delegatedOpts{chain: []string{"delegation.core.agent", "delegation.core.phantom"}})
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	lane := laneByName(t, result, LaneAuthority)
	if lane.Status != closureprotocol.DimensionBlocked || !hasReasonSubstring(lane, "delegation.core.phantom") {
		t.Fatalf("authority lane = %s %v, want blocked on phantom", lane.Status, lane.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("an unrecorded delegation must not certify")
	}
}

// (10) A committed digest that does not match the recorded receipt cannot bind:
// the receipt is not admissible and the chain fails to resolve.
func TestDelegation_DigestMismatchBlocks(t *testing.T) {
	req, rec := buildDelegated(t, delegatedOpts{committed: []string{"0000000000000000000000000000000000000000000000000000000000000000"}})
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	lane := laneByName(t, result, LaneAuthority)
	if lane.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(lane, ReasonAuthorityDelegationUnresolved) {
		t.Fatalf("authority lane = %s %v, want blocked on unresolved", lane.Status, lane.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("an unbound delegation receipt must not certify")
	}
}

// (11) A resolution with an empty delegation chain but a delegating actor did
// not resolve through delegation — it blocks.
func TestDelegation_EmptyChainForDelegatingActorBlocks(t *testing.T) {
	req, rec := buildDelegated(t, delegatedOpts{chain: []string{}})
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	lane := laneByName(t, result, LaneAuthority)
	if lane.Status != closureprotocol.DimensionBlocked || !hasReasonPrefix(lane, ReasonAuthorityDelegationUnresolved) {
		t.Fatalf("authority lane = %s %v, want blocked", lane.Status, lane.ReasonCodes)
	}
}

// (12) The certification identity is invariant to the order of the recorded
// delegation receipts — reordering the artifact set yields the same receipt.
func TestDelegation_ReorderedReceiptsSameIdentity(t *testing.T) {
	// A second, unused-but-committed valid delegation, so reordering is meaningful.
	second := delegatedReceipt()
	second.DelegationID = "delegation.core.agent.secondary"
	reqA, recA := buildDelegated(t, delegatedOpts{extraReceipts: []closureprotocol.DelegationReceipt{second}})
	resultA := mustEvaluate(t, reqA, recA, DefaultPolicy())

	// Rebuild with the recorded receipts reversed.
	reqB, recB := buildDelegated(t, delegatedOpts{extraReceipts: []closureprotocol.DelegationReceipt{second}})
	recB.DelegationReceipts[0], recB.DelegationReceipts[1] = recB.DelegationReceipts[1], recB.DelegationReceipts[0]
	reqB = rebindGreen(t, recB)
	resultB := mustEvaluate(t, reqB, recB, DefaultPolicy())

	digA, err := closureprotocol.CertificationReceiptDigest(resultA.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	digB, err := closureprotocol.CertificationReceiptDigest(resultB.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if digA != digB {
		t.Fatalf("certification identity changed under receipt reordering: %s != %s", digA, digB)
	}
	if resultA.Receipt.CertificationVerdict != closureprotocol.Certified {
		t.Fatalf("verdict = %s, want certified", resultA.Receipt.CertificationVerdict)
	}
}

// (13) With no governed grants loaded, a delegated operation cannot be
// independently verified and fails closed — certification never trusts the
// resolution's claim in the absence of governed truth.
func TestDelegation_NoGovernedGrantsFailsClosed(t *testing.T) {
	req, rec := buildDelegated(t, delegatedOpts{})
	rec.GovernedAuthority = authority.PolicyIndex{}
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	lane := laneByName(t, result, LaneAuthority)
	if lane.Status != closureprotocol.DimensionBlocked || !hasReasonSubstring(lane, "no_governed_grants") {
		t.Fatalf("authority lane = %s %v, want blocked on no_governed_grants", lane.Status, lane.ReasonCodes)
	}
	if result.Receipt.CertificationVerdict == closureprotocol.Certified {
		t.Fatal("a delegation with no governed grants must not certify")
	}
}
