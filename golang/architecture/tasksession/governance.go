// SPDX-License-Identifier: Apache-2.0

package tasksession

import (
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// admissionV2Window bounds how long an admission decision stays valid.
const admissionV2Window = 24 * time.Hour

// governanceState is the admission-v2 disposition derived read-only from a
// task's typed ledger, plus the modify envelope it grants.
type governanceState struct {
	Status      string
	ModifyPaths []string
	// Resolved reports whether the task has engaged typed governance (an
	// authority_resolved event exists). Un-engaged tasks retain their legacy
	// disposition rather than being silently forced to waiting_governance.
	Resolved bool
}

// governanceDisposition inspects the typed admission-v2 ledger without mutating
// it and returns the task's current disposition. The append-only ledger is the
// authoritative truth surface: the disposition reflects the furthest recorded
// state (scope verification, then decision, then resolution).
func governanceDisposition(taskDir string, now time.Time) governanceState {
	rec, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		// No typed authority resolved: the task has not engaged v2 governance.
		return governanceState{Status: StatusWaitingGovernance}
	}
	targets := changePlanTargets(rec.ChangePlan)

	// Terminal: a scope verification was recorded post-mutation.
	if v, err := admission.LoadRecordedScopeVerification(taskDir); err == nil {
		if admission.ScopeVerified(v) {
			return governanceState{Status: StatusReadyForMutation, ModifyPaths: targets, Resolved: true}
		}
		return governanceState{Status: StatusWaitingMechanical, Resolved: true}
	}

	req := closureprotocol.AdmissionRequest{
		ActorBinding:                    rec.Actor,
		BaseBinding:                     rec.Base,
		ChangePlan:                      rec.ChangePlan,
		AuthorityResolutionDigestSHA256: rec.Resolution.AuthorityResolutionDigestSHA256,
		PolicyID:                        strings.TrimSpace(rec.Base.Policies.Admission),
	}
	policy := admission.AdmissionV2Policy{
		PolicyID:           strings.TrimSpace(rec.Base.Policies.Admission),
		CompletionPolicyID: strings.TrimSpace(rec.Base.Policies.Completion),
		ValidityWindow:     admissionV2Window,
	}
	decision, err := admission.DecideAdmission(req, rec.Resolution, policy, now.UTC().Format(time.RFC3339))
	if err != nil || !admission.AllAdmitted(decision) {
		return governanceState{Status: StatusRefused, Resolved: true}
	}
	return governanceState{Status: StatusReadyForMutation, ModifyPaths: targets, Resolved: true}
}

func changePlanTargets(plan closureprotocol.ChangePlan) []string {
	out := make([]string, 0, len(plan.Operations))
	for _, op := range plan.Operations {
		if t := strings.TrimSpace(op.Target); t != "" {
			out = append(out, t)
		}
	}
	return out
}
