// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture/lifecycleaction"
)

// The closed mapping is keyed by STATUS STRINGS, and lifecycleaction is a leaf
// that cannot import the package declaring them. It therefore restates them, and
// a silent divergence would not break a build: it would drop a row out of the
// mapping, and every task in that state would fail closed to Blocked -- a
// healthy, fully governed task reported as unsafe to act on.
//
// Pinned HERE because this is the only package that can see both vocabularies.
func TestStatusVocabularyMatchesTaskSession(t *testing.T) {
	for _, tc := range []struct {
		owner, session string
	}{
		{lifecycleaction.StatusWaitingGovernance, StatusWaitingGovernance},
		{lifecycleaction.StatusReadyForAdmission, StatusReadyForAdmission},
		{lifecycleaction.StatusReadyForMutation, StatusReadyForMutation},
		{lifecycleaction.StatusAdmitted, StatusAdmitted},
		{lifecycleaction.StatusMutationObserved, StatusMutationObserved},
		{lifecycleaction.StatusScopeVerified, StatusScopeVerified},
		{lifecycleaction.StatusWaitingMechanical, StatusWaitingMechanical},
		{lifecycleaction.StatusRefused, StatusRefused},
	} {
		if tc.owner != tc.session {
			t.Fatalf("lifecycleaction has %q where tasksession has %q; the closed mapping would lose this row silently", tc.owner, tc.session)
		}
	}
}

// EVERY state foldGovernance can produce must be a row the owner names. The
// mapping was written FROM this function; if a fold return is added or its
// phase/status pair edited without the mapping following, the new state fails
// closed to Blocked and nothing else reports it.
//
// Driven through lifecycleDisposition, the single conversion, so the tuple under
// test is the one production actually hands the owner.
func TestEveryFoldOutcomeIsANamedRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		gov  governanceState
		want lifecycleaction.Action
	}{
		{"waiting_governance", governanceState{Phase: "waiting_governance", Status: StatusWaitingGovernance}, lifecycleaction.ResolveAuthority},
		{"scope_verified", governanceState{Phase: "scope_verified", Status: StatusScopeVerified, Resolved: true, Terminal: true}, lifecycleaction.RecordResultTransition},
		{"waiting_mechanical_repair", governanceState{Phase: "waiting_mechanical_repair", Status: StatusWaitingMechanical, Resolved: true}, lifecycleaction.MechanicalRepair},
		{"ready_for_admission", governanceState{Phase: "ready_for_admission", Status: StatusReadyForAdmission, Resolved: true}, lifecycleaction.DecideAdmission},
		{"refused", governanceState{Phase: "refused", Status: StatusRefused, Resolved: true}, lifecycleaction.None},
		{"mutation_observed", governanceState{Phase: "mutation_observed", Status: StatusMutationObserved, Resolved: true}, lifecycleaction.VerifyScope},
		{"admitted / consumed", governanceState{Phase: "admitted", Status: StatusAdmitted, Resolved: true}, lifecycleaction.VerifyAdmission},
		{"admitted / ready_for_mutation", governanceState{Phase: "admitted", Status: StatusReadyForMutation, Resolved: true, GrantModify: true}, lifecycleaction.ConsumeCapability},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lifecycleaction.Select(lifecycleDisposition(tc.gov, true)); got != tc.want {
				t.Fatalf("the fold's %s state selected %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
