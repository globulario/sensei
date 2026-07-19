// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"
)

const (
	selfDomain     = "github.com/globulario/sensei"
	foreignDomain  = "github.com/caddyserver/caddy"
	unlistedDomain = "github.com/other/repo"
)

func fixture(name string) string {
	return filepath.Join("testdata", "completion-policy", name)
}

// resolve is a test helper: resolve a fixture's completion state for a domain.
func resolve(t *testing.T, name, domain string) (completionPolicyState, error) {
	t.Helper()
	return resolveCompletionPolicy(fixture(name), "", domain)
}

// Proof 1: a policy with no completion section loads as absent (nil error).
func TestCompletionPolicy_NoSectionIsAbsent(t *testing.T) {
	st, err := resolve(t, "absent.yaml", selfDomain)
	if err != nil {
		t.Fatalf("absent policy must not error: %v", err)
	}
	if st != completionPolicyAbsent {
		t.Fatalf("state = %s, want absent", st)
	}
}

// Proof 8 (part): no policy FILE at all preserves current behavior — absent, no error.
func TestCompletionPolicy_NoFileIsAbsent(t *testing.T) {
	// Empty explicit path + a repo root with no .sensei/gate-policy.yaml.
	st, err := resolveCompletionPolicy("", t.TempDir(), selfDomain)
	if err != nil {
		t.Fatalf("missing policy file must not error: %v", err)
	}
	if st != completionPolicyAbsent {
		t.Fatalf("state = %s, want absent", st)
	}
}

// Proof 2: require_completion:false loads successfully and is DISTINCT from absent.
func TestCompletionPolicy_RequireFalseIsPresentNotRequired(t *testing.T) {
	st, err := resolve(t, "require-false.yaml", selfDomain)
	if err != nil {
		t.Fatalf("require_completion:false must load: %v", err)
	}
	if st != completionPolicyPresentNotRequired {
		t.Fatalf("state = %s, want present_not_required", st)
	}
	if st == completionPolicyAbsent {
		t.Fatal("present_not_required must never collapse into absent")
	}
}

// A domain listed with mode advisory and no require_completion is present, not required.
func TestCompletionPolicy_AdvisoryListedIsPresentNotRequired(t *testing.T) {
	st, err := resolve(t, "advisory-listed.yaml", selfDomain)
	if err != nil {
		t.Fatalf("advisory-listed must load: %v", err)
	}
	if st != completionPolicyPresentNotRequired {
		t.Fatalf("state = %s, want present_not_required", st)
	}
}

// Proof 3: require_completion:true loads successfully as present_required.
func TestCompletionPolicy_RequireTrueIsPresentRequired(t *testing.T) {
	st, err := resolve(t, "require-true.yaml", selfDomain)
	if err != nil {
		t.Fatalf("require_completion:true must load: %v", err)
	}
	if st != completionPolicyPresentRequired {
		t.Fatalf("state = %s, want present_required", st)
	}
}

// Proofs 4 & 5 & malformed variants: every invalid policy fails loudly with a non-nil
// error AND the typed invalid state — never absent, never a silent valid result.
func TestCompletionPolicy_InvalidFailsLoudly(t *testing.T) {
	for _, name := range []string{
		"unknown-field.yaml",    // proof 5: unknown field rejected
		"bad-mode.yaml",         // invalid mode enum
		"contradictory.yaml",    // require_completion:true under advisory
		"duplicate-domain.yaml", // duplicate domain declaration
		"wrong-type.yaml",       // proof 4: malformed (non-bool require_completion)
	} {
		t.Run(name, func(t *testing.T) {
			st, err := resolve(t, name, selfDomain)
			if err == nil {
				t.Fatalf("%s must fail loudly, got nil error (state %s)", name, st)
			}
			if st != completionPolicyInvalid {
				t.Fatalf("%s state = %s, want invalid", name, st)
			}
			if st == completionPolicyAbsent {
				t.Fatal("invalid must never collapse into absent")
			}
		})
	}
}

// The invalid zero value never masquerades as absent: they are distinct constants and
// distinct strings, and an uninitialized result reads as invalid, not absent.
func TestCompletionPolicy_InvalidZeroValueIsNotAbsent(t *testing.T) {
	var zero completionPolicyState
	if zero != completionPolicyInvalid {
		t.Fatalf("zero value = %s, want invalid (a failed/uninitialized result must never read as absent)", zero)
	}
	if completionPolicyInvalid == completionPolicyAbsent {
		t.Fatal("invalid and absent must be distinct values")
	}
	if completionPolicyInvalid.String() == completionPolicyAbsent.String() {
		t.Fatal("invalid and absent must be distinguishable strings")
	}
}

// Proof 6: completion enforcement cannot be activated through the EditCheck Rules map —
// a require_completion rule id, a completion rule id, or a wildcard rule leave the
// completion policy ABSENT (not enabled) for every domain.
func TestCompletionPolicy_RulesMapCannotActivateCompletion(t *testing.T) {
	for _, domain := range []string{selfDomain, foreignDomain, unlistedDomain} {
		st, err := resolve(t, "require-in-rules.yaml", domain)
		if err != nil {
			t.Fatalf("adversarial rules-map policy must load (its completion section is simply absent): %v", err)
		}
		if st != completionPolicyAbsent {
			t.Fatalf("domain %s: EditCheck rules (require_completion/completion/wildcard) must NOT activate completion enforcement; got %s", domain, st)
		}
	}
}

// One domain's opt-in never activates another domain's gate, and an unlisted domain is
// absent even when the file enforces for a different domain.
func TestCompletionPolicy_OneDomainDoesNotActivateAnother(t *testing.T) {
	if st, err := resolve(t, "multi-domain.yaml", selfDomain); err != nil || st != completionPolicyPresentRequired {
		t.Fatalf("self domain: state %s err %v, want present_required", st, err)
	}
	if st, err := resolve(t, "multi-domain.yaml", foreignDomain); err != nil || st != completionPolicyPresentNotRequired {
		t.Fatalf("foreign advisory domain: state %s err %v, want present_not_required", st, err)
	}
	if st, err := resolve(t, "multi-domain.yaml", unlistedDomain); err != nil || st != completionPolicyAbsent {
		t.Fatalf("unlisted domain: state %s err %v, want absent (a foreign-domain verdict never gates this repo)", st, err)
	}
}

// Domain matching is exact identity — no case folding, prefix stripping, basename, or
// other fallback lookup path.
func TestCompletionPolicy_DomainMatchIsExactNoFallback(t *testing.T) {
	for _, near := range []string{
		"GitHub.com/globulario/sensei",  // case variant
		"globulario/sensei",             // prefix stripped
		"sensei",                        // basename
		"github.com/globulario/sensei/", // trailing slash
	} {
		st, err := resolve(t, "require-true.yaml", near)
		if err != nil {
			t.Fatalf("%q: %v", near, err)
		}
		if st != completionPolicyAbsent {
			t.Fatalf("near-miss domain %q must NOT match via a fallback lookup; got %s", near, st)
		}
	}
}

// Proof 7: resolution is deterministic — the same configuration resolves identically
// every time, for valid and invalid inputs alike.
func TestCompletionPolicy_Deterministic(t *testing.T) {
	cases := []struct {
		name   string
		domain string
	}{
		{"require-true.yaml", selfDomain},
		{"require-false.yaml", selfDomain},
		{"absent.yaml", selfDomain},
		{"multi-domain.yaml", foreignDomain},
		{"contradictory.yaml", selfDomain},
	}
	for _, c := range cases {
		first, firstErr := resolve(t, c.name, c.domain)
		for i := 0; i < 5; i++ {
			st, err := resolve(t, c.name, c.domain)
			if st != first || (err == nil) != (firstErr == nil) {
				t.Fatalf("%s/%s not deterministic: (%s,%v) vs (%s,%v)", c.name, c.domain, first, firstErr, st, err)
			}
		}
	}
}

// Proof 8 (part): a gate-policy file that ADDS a completion section is still parsed
// unchanged by the existing EditCheck loader — the completion section does not disturb
// the Rules map, and the completion loader does not require the EditCheck keys.
func TestCompletionPolicy_DoesNotDisturbEditCheckPolicy(t *testing.T) {
	// require-true.yaml carries default + rules + completion.
	gp, err := loadGatePolicy(fixture("require-true.yaml"))
	if err != nil {
		t.Fatalf("EditCheck loader must still parse a file with a completion section: %v", err)
	}
	if gp.Default != enforceInherit {
		t.Fatalf("EditCheck default = %q, want inherit", gp.Default)
	}
	if gp.Rules["closure.some_rule"] != enforceBlock {
		t.Fatalf("EditCheck rule lost/altered by the completion section: %+v", gp.Rules)
	}
	// And the completion loader ignores the EditCheck keys entirely.
	if st, err := resolve(t, "require-true.yaml", selfDomain); err != nil || st != completionPolicyPresentRequired {
		t.Fatalf("completion loader must read only its own section: state %s err %v", st, err)
	}
}

// A missing EXPLICIT policy path is loud (an error), matching resolveGatePolicy — asking
// for a policy that isn't there must not be silently treated as absent.
func TestCompletionPolicy_MissingExplicitPathIsLoud(t *testing.T) {
	st, err := resolveCompletionPolicy(filepath.Join(t.TempDir(), "nope.yaml"), "", selfDomain)
	if err == nil {
		t.Fatal("a missing explicit policy path must error, not silently resolve absent")
	}
	if st != completionPolicyInvalid {
		t.Fatalf("state = %s, want invalid on a read failure", st)
	}
}

// The resolved per-domain policy (the typed API the enforce checkpoint will consume)
// exposes the effective mode and require flag without enforcing anything here.
func TestCompletionPolicy_ExposesTypedDomainPolicy(t *testing.T) {
	p, err := loadCompletionGatePolicy(fixture("multi-domain.yaml"), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	dp, ok := p.domainPolicy(selfDomain)
	if !ok || dp.Mode != completionModeEnforce || !dp.RequireCompletion {
		t.Fatalf("self domain policy = %+v ok=%v, want enforce+require", dp, ok)
	}
	fdp, ok := p.domainPolicy(foreignDomain)
	if !ok || fdp.Mode != completionModeAdvisory || fdp.RequireCompletion {
		t.Fatalf("foreign domain policy = %+v ok=%v, want advisory+!require", fdp, ok)
	}
	if _, ok := p.domainPolicy(unlistedDomain); ok {
		t.Fatal("unlisted domain must not resolve to a domain policy")
	}
}
