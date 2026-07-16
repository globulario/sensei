// SPDX-License-Identifier: AGPL-3.0-only

package certification

import (
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/proofdischarge"
)

// CoverageProfile selects which evidence kinds the evidence lane requires. The
// string values are shared with the Phase 4/5 engines (evidencereceipt,
// proofdischarge); the profile is only ever a DEFAULT — a governed proof
// obligation with RequiresRuntimeEvidence overrides it, including static_test.
type CoverageProfile string

const (
	// CoverageStaticTest is the default: static and test evidence certify.
	// Runtime-kind evidence profiles are recorded not_applicable ONLY when no
	// applicable governed obligation mandates runtime evidence.
	CoverageStaticTest CoverageProfile = CoverageProfile(proofdischarge.CoverageStaticTest)
	// CoverageStaticTestRuntime additionally requires owner-path runtime
	// evidence for every runtime-kind required profile.
	CoverageStaticTestRuntime CoverageProfile = CoverageProfile(proofdischarge.CoverageStaticTestRuntime)
)

// Frozen certification policy IDs shipped by Phase 6. The default matches the
// certification policy binding used across the frozen fixtures.
const (
	PolicyDefaultID = "certification.architectural_closure.v1"
	PolicyRuntimeID = "certification.architectural_closure.v1.runtime"
)

// CertificationPolicy parameterizes lane requirements. PolicyID is the string
// recorded as CertificationReceipt.CertificationPolicy.
type CertificationPolicy struct {
	PolicyID        string          `json:"policy_id" yaml:"policy_id"`
	CoverageProfile CoverageProfile `json:"coverage_profile" yaml:"coverage_profile"`

	// AllowedWaiverDimensions lists the lanes a valid, unexpired, exactly
	// scoped WaiverReceipt may downgrade to pass_with_exception. Scope is
	// deliberately never waivable: the scope lane consults no waiver at all,
	// and listing "scope" here has no effect.
	AllowedWaiverDimensions []closureprotocol.Dimension `json:"allowed_waiver_dimensions,omitempty" yaml:"allowed_waiver_dimensions,omitempty"`

	// RequireHumanReviewForRiskClasses forces review_required (never certified)
	// when an operation in the plan carries one of these risk classes and no
	// lane already blocked.
	RequireHumanReviewForRiskClasses []string `json:"require_human_review_for_risk_classes,omitempty" yaml:"require_human_review_for_risk_classes,omitempty"`
}

// DefaultPolicy returns the static_test default policy. Proof and evidence
// waivers are permitted (matching the frozen completion fixtures' completion
// policy, which allows proof waivers); scope and authority are not.
func DefaultPolicy() CertificationPolicy {
	return CertificationPolicy{
		PolicyID:                PolicyDefaultID,
		CoverageProfile:         CoverageStaticTest,
		AllowedWaiverDimensions: []closureprotocol.Dimension{closureprotocol.DimensionProof, closureprotocol.DimensionEpistemic},
	}
}

// RuntimePolicy returns the opt-in static_test_runtime policy.
func RuntimePolicy() CertificationPolicy {
	p := DefaultPolicy()
	p.PolicyID = PolicyRuntimeID
	p.CoverageProfile = CoverageStaticTestRuntime
	return p
}

// PolicyByID resolves a governed certification policy. Unknown IDs resolve to
// nothing — the engine refuses rather than substituting a default.
func PolicyByID(id string) (CertificationPolicy, bool) {
	switch id {
	case PolicyDefaultID:
		return DefaultPolicy(), true
	case PolicyRuntimeID:
		return RuntimePolicy(), true
	default:
		return CertificationPolicy{}, false
	}
}

// waiverAllowed reports whether policy permits waivers on the given lane
// dimension. The scope lane never consults this.
func (p CertificationPolicy) waiverAllowed(d closureprotocol.Dimension) bool {
	for _, allowed := range p.AllowedWaiverDimensions {
		if allowed == d {
			return true
		}
	}
	return false
}
