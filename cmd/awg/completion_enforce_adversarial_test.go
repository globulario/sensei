// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// ---- Section 4: completion-policy isolation ----------------------------------------

// A domain identity that only RESEMBLES an opted-in domain (case, prefix, suffix,
// parent, whitespace) never inherits its enforcement — exact identity only.
func TestAdversarial_DomainLookalikesDoNotInherit(t *testing.T) {
	dir := t.TempDir()
	pol := writeCompletionPolicy(t, dir, "github.com/globulario/sensei", "enforce", true)
	for _, near := range []string{
		"github.com/globulario/sensei-extra", // suffix collision
		"github.com/globulario/sens",         // prefix collision
		"github.com/globulario",              // parent domain
		"GITHUB.COM/globulario/sensei",       // case variation
		" github.com/globulario/sensei",      // leading whitespace
		"github.com/globulario/SENSEI",       // case variation
	} {
		st, err := resolveCompletionPolicy(pol, "", near)
		if err != nil {
			t.Fatalf("%q: %v", near, err)
		}
		if st != completionPolicyAbsent {
			t.Fatalf("lookalike domain %q must resolve absent, got %s", near, st)
		}
	}
}

// Invalid policy combined with an otherwise authoritative task still fails loudly — it
// never collapses to absent/advisory/not-required and never lets the task pass.
func TestAdversarial_InvalidPolicyNeverCollapses(t *testing.T) {
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	// contradictory: advisory mode with require_completion: true
	if err := os.WriteFile(bad, []byte("completion:\n  domains:\n    d:\n      mode: advisory\n      require_completion: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := runGate([]string{"--completion", "--enforce", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--domain", "d", "--policy", bad})
	if code != 2 {
		t.Fatalf("invalid policy must fail loudly (exit 2) even for an otherwise-authoritative task, got %d", code)
	}
}

// ---- Section 5: publication / verdict laundering -----------------------------------

// No malformed, contradictory, or unknown publication can be laundered into a pass or a
// degraded pass under required completion.
func TestAdversarial_PublicationLaunderingFailsClosed(t *testing.T) {
	req := completionPolicyPresentRequired
	block := func(name string, in completionEnforceInput) {
		t.Run(name, func(t *testing.T) {
			d := decideCompletionEnforcement(in)
			if d.Result != decisionBlock {
				t.Fatalf("%s must block, got %s (%s)", name, d.Result, d.Reason)
			}
		})
	}
	// authoritative verdict but invalid publication → block (cannot launder authority).
	block("authoritative+invalid_pub", completionEnforceInput{PolicyState: req, PublicationValid: false, Availability: completion.CompletionAvailable, Verdict: completion.ClosureAuthoritativeCompletion})
	// empty / unknown verdict → block.
	block("empty_verdict", availIn(req, completion.ClosureVerdict("")))
	block("unknown_verdict", availIn(req, completion.ClosureVerdict("looks_ok")))
	// broken/contradictory/unsupported → block regardless of any outer metadata.
	block("broken", availIn(req, completion.ClosureBroken))
	block("contradictory", availIn(req, completion.ClosureContradictory))
	block("unsupported", availIn(req, completion.ClosureUnsupported))
	// unavailable with missing/unknown/generic cause → block (no positive runtime evidence).
	block("unavailable_missing_cause", unavailIn(req, completion.CompletionUnavailableClass("")))
	block("unavailable_unknown_cause", unavailIn(req, completion.CompletionUnavailableClass("mystery")))
	block("unavailable_generic_cause", unavailIn(req, completion.UnavailableProjectionOwnerError))
	// identity cause (even if it arrived disguised) → block, never degraded.
	block("identity_cause", unavailIn(req, completion.UnavailableProjectionOwnerIdentityError))

	// And the ONLY degraded outcome is a genuine runtime cause.
	if d := decideCompletionEnforcement(unavailIn(req, completion.UnavailableProjectionOwnerRuntimeError)); d.Result != decisionDegradedPass {
		t.Fatalf("runtime cause must degrade, got %s", d.Result)
	}
}

// unsupported is never reclassified as not_completed — distinct reasons, both block.
func TestAdversarial_UnsupportedNotLaunderedToNotCompleted(t *testing.T) {
	req := completionPolicyPresentRequired
	uns := decideCompletionEnforcement(availIn(req, completion.ClosureUnsupported))
	nc := decideCompletionEnforcement(availIn(req, completion.ClosureNotCompleted))
	if uns.Reason == nc.Reason {
		t.Fatal("unsupported and not_completed must carry distinct reasons")
	}
	if uns.Result != decisionBlock || nc.Result != decisionBlock {
		t.Fatal("both must block")
	}
}

// ---- Section 6: CLI exit-code contract ---------------------------------------------

func TestAdversarial_ExitCodeContract(t *testing.T) {
	if exitCodeForDecision(completionDecision{Result: decisionBlock}) != 1 {
		t.Fatal("block → exit 1")
	}
	if exitCodeForDecision(completionDecision{Result: decisionPass}) != 0 {
		t.Fatal("pass → exit 0")
	}
	if exitCodeForDecision(completionDecision{Result: decisionDegradedPass}) != 0 {
		t.Fatal("authorized degraded runtime pass → exit 0")
	}
}

// The JSON enforce result carries a STABLE typed reason code, asserted independently of
// any human-readable prose.
func TestAdversarial_StableReasonCodeInJSON(t *testing.T) {
	const domain = "test.domain"
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	pol := writeCompletionPolicy(t, t.TempDir(), domain, "enforce", true)
	var out string
	code := 0
	out = captureStdout(t, func() {
		code = runGate([]string{"--completion", "--enforce", "--json", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--domain", domain, "--policy", pol})
	})
	if code != 1 {
		t.Fatalf("required completion on a not_completed task must block (exit 1), got %d", code)
	}
	var r completionEnforceResultJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if r.Result != "block" || r.Reason != string(reasonNotCompletedRequired) {
		t.Fatalf("stable reason code expected block/not_completed_required, got %s/%s", r.Result, r.Reason)
	}
}

// ---- Section 7: determinism and isolation ------------------------------------------

// The pure decision is deterministic across repeated evaluation.
func TestAdversarial_DecisionDeterministic(t *testing.T) {
	inputs := []completionEnforceInput{
		availIn(completionPolicyPresentRequired, completion.ClosureNotCompleted),
		unavailIn(completionPolicyPresentRequired, completion.UnavailableProjectionOwnerRuntimeError),
		unavailIn(completionPolicyPresentRequired, completion.UnavailableProjectionOwnerIdentityError),
		availIn(completionPolicyAbsent, completion.ClosureBroken),
	}
	for _, in := range inputs {
		first := decideCompletionEnforcement(in)
		for i := 0; i < 10; i++ {
			if d := decideCompletionEnforcement(in); d != first {
				t.Fatalf("nondeterministic: %+v vs %+v", first, d)
			}
		}
	}
}

// Evaluating one domain does not activate completion for another, and policy resolution
// is not mutated by repeated evaluation (loader holds no cross-call state).
func TestAdversarial_DomainIsolationStable(t *testing.T) {
	pol := writeCompletionPolicy(t, t.TempDir(), "github.com/globulario/sensei", "enforce", true)
	for i := 0; i < 5; i++ {
		if st, _ := resolveCompletionPolicy(pol, "", "github.com/globulario/sensei"); st != completionPolicyPresentRequired {
			t.Fatal("opted-in domain must stay required across repeated resolution")
		}
		if st, _ := resolveCompletionPolicy(pol, "", "github.com/other/repo"); st != completionPolicyAbsent {
			t.Fatal("a different domain must stay absent across repeated resolution")
		}
	}
}
