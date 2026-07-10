// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"testing"

	"github.com/globulario/sensei/golang/extractor"
)

func contradictionRules(t *testing.T, files map[string]string) map[string]bool {
	t.Helper()
	root := makeDir(t, files)
	cons, err := extractor.ValidateContradictions(root)
	if err != nil {
		t.Fatalf("ValidateContradictions: %v", err)
	}
	out := map[string]bool{}
	for _, c := range cons {
		out[c.Rule] = true
	}
	return out
}

func TestValidatorDetectsSupersededActiveNode(t *testing.T) {
	rules := contradictionRules(t, map[string]string{
		"x.yaml": `
id: x.stale
level: principle
title: Stale
status: active
superseded_by: x.fresh
`,
	})
	if !rules["superseded_active"] {
		t.Errorf("expected superseded_active contradiction, got %v", rules)
	}
}

func TestValidatorDetectsAuthorityOwnerConflict(t *testing.T) {
	rules := contradictionRules(t, map[string]string{
		"domains.yaml": `
authority_domains:
  - id: authority.a
    owner_service: service-a
    owns_state:
      - the shared thing
  - id: authority.b
    owner_service: service-b
    owns_state:
      - the shared thing
`,
	})
	if !rules["authority_owner_conflict"] {
		t.Errorf("expected authority_owner_conflict, got %v", rules)
	}
}

func TestValidatorDetectsRepairPlanInvariantConflict(t *testing.T) {
	rules := contradictionRules(t, map[string]string{
		"r.yaml": `
id: globular.repair.dangerous
class: RepairPlan
label: Dangerous ungated plan
status: active
blast_radius: security
approval_gate: none
must_not_violate_invariants:
  - invariant:security.deny_overrides_allow
`,
	})
	if !rules["repair_plan_unguarded_safety"] {
		t.Errorf("expected repair_plan_unguarded_safety contradiction, got %v", rules)
	}
}

func TestValidatorAllowsDocumentedException(t *testing.T) {
	rules := contradictionRules(t, map[string]string{
		"x.yaml": `
id: x.stale
level: principle
title: Stale but documented
status: active
superseded_by: x.fresh
exception: kept active deliberately during the migration window; see ADR-123
`,
	})
	if rules["superseded_active"] {
		t.Errorf("documented exception should suppress the contradiction, got %v", rules)
	}
}
