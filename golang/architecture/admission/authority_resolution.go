// SPDX-License-Identifier: Apache-2.0

package admission

import (
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// Phase 3 admission v2, writer substrate: produce a typed AuthorityResolution
// for a task's change plan so it can be recorded on the ledger and later loaded
// by admission. This completes the operational bridge between phase2's authority
// resolver and admission: authority.Resolve is finally invoked for a real task.
//
// Actor verification (authority.VerifyActorBinding) and policy loading
// (authority.LoadPolicyIndex) are the caller's responsibility (the CLI/task
// flow), keeping admission decoupled from the bundle/receipt I/O.

// ResolveAuthorityInput bundles the typed task inputs for an authority
// resolution. The VerifiedActor is produced by the caller from the ActorBinding
// via authority.VerifyActorBinding.
type ResolveAuthorityInput struct {
	Actor                            closureprotocol.ActorBinding
	VerifiedActor                    authority.VerifiedActor
	Base                             closureprotocol.BaseBinding
	ChangePlan                       closureprotocol.ChangePlan
	Applicability                    []authority.AuthorityApplicability
	PolicyID                         string
	ClosureAssessmentDigestSHA256    string
	AuthorityPolicyGraphDigestSHA256 string
	EvaluatedAt                      string
}

// ResolveAuthority assembles a ResolveRequest from typed task inputs, resolves
// it against the authority policy index, and stamps the resolution's self
// digest so downstream admission can bind to it. It binds the resolution to the
// exact actor and base bindings by their canonical digests.
func ResolveAuthority(index authority.PolicyIndex, in ResolveAuthorityInput) (closureprotocol.AuthorityResolution, error) {
	actorDigest, err := closureprotocol.SemanticDigest(in.Actor)
	if err != nil {
		return closureprotocol.AuthorityResolution{}, err
	}
	baseDigest, err := closureprotocol.SemanticDigest(in.Base)
	if err != nil {
		return closureprotocol.AuthorityResolution{}, err
	}
	res, err := authority.Resolve(index, authority.ResolveRequest{
		ActorBindingDigestSHA256:         actorDigest,
		BaseBindingDigestSHA256:          baseDigest,
		ClosureAssessmentDigestSHA256:    in.ClosureAssessmentDigestSHA256,
		Actor:                            in.VerifiedActor,
		Operations:                       in.ChangePlan.Operations,
		Applicability:                    in.Applicability,
		PolicyID:                         in.PolicyID,
		EvaluatedAt:                      in.EvaluatedAt,
		AuthorityPolicyGraphDigestSHA256: in.AuthorityPolicyGraphDigestSHA256,
	})
	if err != nil {
		return closureprotocol.AuthorityResolution{}, err
	}
	digest, err := closureprotocol.AuthorityResolutionDigest(res)
	if err != nil {
		return closureprotocol.AuthorityResolution{}, err
	}
	res.AuthorityResolutionDigestSHA256 = digest
	return res, nil
}
