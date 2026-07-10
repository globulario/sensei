// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestEffectiveEnforcement(t *testing.T) {
	cases := []struct {
		name     string
		ruleID   string
		declared string
		policy   gatePolicy
		want     string
	}{
		{"no policy inherits declared block", "r1", "block", gatePolicy{}, enforceBlock},
		{"no policy inherits declared warn", "r1", "warn", gatePolicy{}, enforceWarn},
		{"empty declared treated as warn", "r1", "", gatePolicy{}, enforceWarn},
		{"per-rule override wins over declared", "r1", "warn",
			gatePolicy{Rules: map[string]string{"r1": enforceBlock}}, enforceBlock},
		{"per-rule off silences", "r1", "block",
			gatePolicy{Rules: map[string]string{"r1": enforceOff}}, enforceOff},
		{"per-rule inherit falls back to declared", "r1", "block",
			gatePolicy{Rules: map[string]string{"r1": enforceInherit}}, enforceBlock},
		{"default applies when no rule entry", "r2", "warn",
			gatePolicy{Default: enforceBlock, Rules: map[string]string{"r1": enforceWarn}}, enforceBlock},
		{"rule entry beats default", "r1", "warn",
			gatePolicy{Default: enforceBlock, Rules: map[string]string{"r1": enforceWarn}}, enforceWarn},
		{"default inherit is a no-op", "r1", "warn",
			gatePolicy{Default: enforceInherit}, enforceWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveEnforcement(tc.ruleID, tc.declared, tc.policy); got != tc.want {
				t.Errorf("effectiveEnforcement(%q,%q,%+v) = %q, want %q",
					tc.ruleID, tc.declared, tc.policy, got, tc.want)
			}
		})
	}
}

// The 2.3 acceptance: the SAME rule (declared advisory) is blocking in one repo
// and advisory in another, purely by policy — the warning objects are identical
// going in; only the policy differs.
func TestApplyGatePolicy_SameRuleDifferentReposDifferentVerdict(t *testing.T) {
	mk := func() []*awarenesspb.EditWarning {
		return []*awarenesspb.EditWarning{{RuleId: "shared.rule", Enforcement: "warn"}}
	}

	blocking := applyGatePolicy(mk(), gatePolicy{Rules: map[string]string{"shared.rule": enforceBlock}})
	if len(blocking) != 1 || blocking[0].GetEnforcement() != enforceBlock {
		t.Fatalf("repo A: want the rule re-leveled to block, got %+v", blocking)
	}

	advisory := applyGatePolicy(mk(), gatePolicy{}) // no policy — inherit "warn"
	if len(advisory) != 1 || advisory[0].GetEnforcement() != enforceWarn {
		t.Fatalf("repo B: want the rule advisory (warn), got %+v", advisory)
	}
}

func TestApplyGatePolicy_OffDropsFinding(t *testing.T) {
	ws := []*awarenesspb.EditWarning{
		{RuleId: "keep", Enforcement: "block"},
		{RuleId: "silence", Enforcement: "block"},
	}
	out := applyGatePolicy(ws, gatePolicy{Rules: map[string]string{"silence": enforceOff}})
	if len(out) != 1 || out[0].GetRuleId() != "keep" {
		t.Fatalf("off must drop the silenced finding, got %+v", out)
	}
}

func TestLoadGatePolicy_ValidatesLevels(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yaml")
	if err := os.WriteFile(good, []byte("default: warn\nrules:\n  a.b: block\n  c.d: OFF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := loadGatePolicy(good)
	if err != nil {
		t.Fatalf("valid policy failed: %v", err)
	}
	if p.Rules["c.d"] != enforceOff { // case-normalized
		t.Errorf("expected OFF normalized to off, got %q", p.Rules["c.d"])
	}
	if p.loadedFrom != good {
		t.Errorf("loadedFrom = %q, want %q", p.loadedFrom, good)
	}

	bad := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("rules:\n  a.b: blcok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGatePolicy(bad); err == nil {
		t.Error("a typo'd level must fail loudly, not silently pass")
	}
}

func TestResolveGatePolicy_MissingDefaultIsNoOp_MissingExplicitErrors(t *testing.T) {
	dir := t.TempDir() // no .awg/gate-policy.yaml here
	p, err := resolveGatePolicy("", dir)
	if err != nil {
		t.Fatalf("missing default policy must be a no-op, got %v", err)
	}
	if p.loadedFrom != "" || p.Default != "" || len(p.Rules) != 0 {
		t.Errorf("missing default must yield empty policy, got %+v", p)
	}

	if _, err := resolveGatePolicy(filepath.Join(dir, "nope.yaml"), dir); err == nil {
		t.Error("an explicitly requested but missing policy must error")
	}
}
