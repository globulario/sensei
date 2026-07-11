// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/statedir"
)

// Per-repo enforcement policy (Pillar 2.3). A rule's declared enforcement lives
// in the awareness graph, but WHETHER a given repo treats it as blocking is a
// CLIENT decision — the same forbidden-fix rule can be advisory in one repo and
// blocking in another with no change to the rule or the code. That override
// lives in a small repo-local YAML the gate reads before deciding a verdict.
//
// Absent policy == today's behaviour: every rule uses its own declared level.

// enforcement levels a policy can assign.
const (
	enforceInherit = "inherit" // use the rule's own declared level (the default)
	enforceWarn    = "warn"    // advisory: counts as a warning, never blocks
	enforceBlock   = "block"   // blocking under --enforce
	enforceOff     = "off"     // drop the finding entirely for this repo
)

// gatePolicy is the on-disk shape of <repo>/.sensei/gate-policy.yaml.
type gatePolicy struct {
	// Default enforcement for rules with no explicit entry. Empty or "inherit"
	// keeps each rule's declared level.
	Default string `yaml:"default"`
	// Rules maps a rule id to warn|block|off|inherit, overriding its declared
	// level for this repo.
	Rules map[string]string `yaml:"rules"`
	// loadedFrom is the path the policy came from (for transparency in output);
	// not part of the YAML.
	loadedFrom string `yaml:"-"`
}

// defaultGatePolicyPath is where the gate looks when --policy is not given.
func defaultGatePolicyPath(repoRoot string) string {
	return statedir.Path(repoRoot, "gate-policy.yaml")
}

var validEnforcementLevels = map[string]bool{
	enforceInherit: true, enforceWarn: true, enforceBlock: true, enforceOff: true,
}

// resolveGatePolicy loads the policy from explicitPath, or from the default repo
// path when explicitPath is empty. A missing default path is not an error (it
// yields an empty, no-op policy). A missing EXPLICIT path IS an error — asking
// for a policy that isn't there should be loud, not silently ignored.
func resolveGatePolicy(explicitPath, repoRoot string) (gatePolicy, error) {
	path := explicitPath
	if path == "" {
		path = defaultGatePolicyPath(repoRoot)
		if _, err := os.Stat(path); err != nil {
			return gatePolicy{}, nil // no policy configured — inherit everything
		}
	}
	return loadGatePolicy(path)
}

// loadGatePolicy reads and validates a policy file.
func loadGatePolicy(path string) (gatePolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return gatePolicy{}, fmt.Errorf("read gate policy %s: %w", path, err)
	}
	var p gatePolicy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return gatePolicy{}, fmt.Errorf("parse gate policy %s: %w", path, err)
	}
	p.loadedFrom = path
	if err := p.normalizeAndValidate(); err != nil {
		return gatePolicy{}, fmt.Errorf("gate policy %s: %w", path, err)
	}
	return p, nil
}

// normalizeAndValidate lowercases levels and rejects unknown ones — a typo like
// "blcok" must fail loudly, not silently fall through to advisory.
func (p *gatePolicy) normalizeAndValidate() error {
	p.Default = strings.ToLower(strings.TrimSpace(p.Default))
	if p.Default != "" && !validEnforcementLevels[p.Default] {
		return fmt.Errorf("default: %q is not one of inherit|warn|block|off", p.Default)
	}
	if p.Rules == nil {
		return nil
	}
	normalized := make(map[string]string, len(p.Rules))
	for id, lvl := range p.Rules {
		id = strings.TrimSpace(id)
		lvl = strings.ToLower(strings.TrimSpace(lvl))
		if !validEnforcementLevels[lvl] {
			return fmt.Errorf("rule %q: %q is not one of inherit|warn|block|off", id, lvl)
		}
		normalized[id] = lvl
	}
	p.Rules = normalized
	return nil
}

// effectiveEnforcement resolves the level the gate should apply to one finding:
// an explicit per-rule override wins, then a non-inherit default, else the
// rule's own declared level. Pure so the policy resolution is unit-tested
// exhaustively. An empty declared level is treated as "warn".
func effectiveEnforcement(ruleID, declared string, p gatePolicy) string {
	declared = strings.ToLower(strings.TrimSpace(declared))
	if declared == "" {
		declared = enforceWarn
	}
	if lvl, ok := p.Rules[ruleID]; ok && lvl != enforceInherit {
		return lvl
	}
	if p.Default != "" && p.Default != enforceInherit {
		return p.Default
	}
	return declared
}

// applyGatePolicy rewrites each warning's enforcement to the policy-resolved
// level and drops any the policy turned "off". All downstream tallying and
// printing then reads the EFFECTIVE level with no further branching. Mutates
// and filters the input slice in place (it comes fresh from the RPC response).
func applyGatePolicy(ws []*awarenesspb.EditWarning, p gatePolicy) []*awarenesspb.EditWarning {
	out := ws[:0]
	for _, w := range ws {
		eff := effectiveEnforcement(w.GetRuleId(), w.GetEnforcement(), p)
		if eff == enforceOff {
			continue
		}
		w.Enforcement = eff
		out = append(out, w)
	}
	return out
}
