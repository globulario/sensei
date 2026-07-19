// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
	"github.com/globulario/sensei/internal/resulttestkit"
)

// avail builds an available-envelope decision input with a given verdict.
func availIn(state completionPolicyState, v completion.ClosureVerdict) completionEnforceInput {
	return completionEnforceInput{PolicyState: state, PublicationValid: true, Availability: completion.CompletionAvailable, Verdict: v}
}

// unavailIn builds an unavailable-envelope decision input with a given cause class.
func unavailIn(state completionPolicyState, c completion.CompletionUnavailableClass) completionEnforceInput {
	return completionEnforceInput{PolicyState: state, PublicationValid: true, Availability: completion.CompletionUnavailable, UnavailableClass: c}
}

// The full frozen 9.4b decision table, exercised purely.
func TestDecideCompletionEnforcement_Matrix(t *testing.T) {
	req := completionPolicyPresentRequired
	cases := []struct {
		name   string
		in     completionEnforceInput
		result completionEnforceResult
		reason completionEnforceReason
	}{
		// Policy absent / not required → preserve existing behavior (pass), even for a
		// pathological verdict — the enforce path creates no block.
		{"absent/authoritative", availIn(completionPolicyAbsent, completion.ClosureAuthoritativeCompletion), decisionPass, reasonNotEnforced},
		{"absent/not_completed", availIn(completionPolicyAbsent, completion.ClosureNotCompleted), decisionPass, reasonNotEnforced},
		{"absent/broken", availIn(completionPolicyAbsent, completion.ClosureBroken), decisionPass, reasonNotEnforced},
		{"not_required/not_completed", availIn(completionPolicyPresentNotRequired, completion.ClosureNotCompleted), decisionPass, reasonNotEnforced},
		{"not_required/broken", availIn(completionPolicyPresentNotRequired, completion.ClosureBroken), decisionPass, reasonNotEnforced},

		// require_completion: available verdicts.
		{"required/authoritative", availIn(req, completion.ClosureAuthoritativeCompletion), decisionPass, reasonAuthoritative},
		{"required/not_completed", availIn(req, completion.ClosureNotCompleted), decisionBlock, reasonNotCompletedRequired},
		{"required/broken", availIn(req, completion.ClosureBroken), decisionBlock, reasonBrokenCompletion},
		{"required/contradictory", availIn(req, completion.ClosureContradictory), decisionBlock, reasonContradictoryHistory},
		{"required/unsupported", availIn(req, completion.ClosureUnsupported), decisionBlock, reasonUnsupported},
		{"required/unknown_verdict", availIn(req, completion.ClosureVerdict("bogus")), decisionBlock, reasonUnknownClassification},

		// require_completion: unavailable causes.
		{"required/identity_task_dir", unavailIn(req, completion.UnavailableTaskDirectoryUnresolved), decisionBlock, reasonIdentityInvalid},
		{"required/identity_owner", unavailIn(req, completion.UnavailableProjectionOwnerIdentityError), decisionBlock, reasonIdentityInvalid},
		{"required/runtime", unavailIn(req, completion.UnavailableProjectionOwnerRuntimeError), decisionDegradedPass, reasonRuntimeDegraded},
		{"required/generic_owner_fails_closed", unavailIn(req, completion.UnavailableProjectionOwnerError), decisionBlock, reasonUnknownClassification},
		{"required/unknown_class_fails_closed", unavailIn(req, completion.CompletionUnavailableClass("mystery")), decisionBlock, reasonUnknownClassification},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := decideCompletionEnforcement(c.in)
			if d.Result != c.result || d.Reason != c.reason {
				t.Fatalf("got (%s,%s), want (%s,%s)", d.Result, d.Reason, c.result, c.reason)
			}
		})
	}
}

// Global safety rules that hold regardless of verdict/cause.
func TestDecideCompletionEnforcement_SafetyRules(t *testing.T) {
	req := completionPolicyPresentRequired

	// Invalid publication blocks and can never degrade — even when the (would-be) cause
	// is a runtime error.
	inv := unavailIn(req, completion.UnavailableProjectionOwnerRuntimeError)
	inv.PublicationValid = false
	if d := decideCompletionEnforcement(inv); d.Result != decisionBlock || d.Reason != reasonInvalidPublication {
		t.Fatalf("invalid publication must block: got (%s,%s)", d.Result, d.Reason)
	}

	// An identity cause never reaches the degraded-pass lane.
	if d := decideCompletionEnforcement(unavailIn(req, completion.UnavailableProjectionOwnerIdentityError)); d.Result == decisionDegradedPass {
		t.Fatal("identity failure must never degrade to a pass")
	}

	// unsupported is not reclassified as not_completed — they carry distinct reasons.
	uns := decideCompletionEnforcement(availIn(req, completion.ClosureUnsupported))
	nc := decideCompletionEnforcement(availIn(req, completion.ClosureNotCompleted))
	if uns.Reason == nc.Reason {
		t.Fatal("unsupported and not_completed must carry distinct reasons")
	}

	// The ONLY degraded lane is the runtime cause; nothing else degrades under required.
	degradedCount := 0
	for _, in := range []completionEnforceInput{
		availIn(req, completion.ClosureAuthoritativeCompletion),
		availIn(req, completion.ClosureNotCompleted),
		availIn(req, completion.ClosureBroken),
		unavailIn(req, completion.UnavailableProjectionOwnerIdentityError),
		unavailIn(req, completion.UnavailableTaskDirectoryUnresolved),
		unavailIn(req, completion.UnavailableProjectionOwnerRuntimeError),
	} {
		if decideCompletionEnforcement(in).Result == decisionDegradedPass {
			degradedCount++
		}
	}
	if degradedCount != 1 {
		t.Fatalf("exactly one input (the runtime cause) may degrade; got %d", degradedCount)
	}
}

// writeCompletionPolicy writes a gate-policy file with a completion section for domain.
func writeCompletionPolicy(t *testing.T, dir, domain, mode string, require bool) string {
	t.Helper()
	p := filepath.Join(dir, "gate-policy.yaml")
	content := "completion:\n  domains:\n    " + domain + ":\n      mode: " + mode + "\n"
	if require {
		content += "      require_completion: true\n"
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// End-to-end wiring: the enforce path routes policy + envelope through the decision and
// maps the result to exit codes, without changing advisory or non-opted-in behavior.
func TestGateCompletionEnforce_EndToEnd(t *testing.T) {
	const domain = "test.domain"
	seed, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	polDir := t.TempDir()

	run := func(args ...string) int {
		return runGate(append([]string{"--completion", "--enforce", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--domain", domain}, args...))
	}

	// require_completion:true + a not_completed world → block (exit 1).
	reqPol := writeCompletionPolicy(t, polDir, domain, "enforce", true)
	if code := run("--policy", reqPol); code != 1 {
		t.Fatalf("required completion on a not_completed task must block (exit 1), got %d", code)
	}

	// No adopted policy → not enforced → pass (exit 0). Existing behavior preserved.
	empty := filepath.Join(t.TempDir(), "empty.yaml")
	if err := os.WriteFile(empty, []byte("rules: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run("--policy", empty); code != 0 {
		t.Fatalf("no adopted completion policy must pass (exit 0), got %d", code)
	}

	// require_completion:false (present_not_required) → preserve existing → pass (exit 0),
	// even though the task is not_completed.
	nrPol := writeCompletionPolicy(t, t.TempDir(), domain, "enforce", false)
	if code := run("--policy", nrPol); code != 0 {
		t.Fatalf("require_completion:false must pass (exit 0), got %d", code)
	}

	// A domain not listed in the policy is never gated, even when the policy requires
	// completion for a DIFFERENT domain (one domain never activates another).
	if code := runGate([]string{"--completion", "--enforce", "--repo-root", seed.Repo, "--task-dir", seed.TaskDir, "--domain", "unlisted.domain", "--policy", reqPol}); code != 0 {
		t.Fatalf("an unlisted domain must not be gated (exit 0), got %d", code)
	}

	// Invalid completion policy → loud config error (exit 2), never advisory/absent.
	badPol := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(badPol, []byte("completion:\n  domains:\n    "+domain+":\n      mode: blocking\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run("--policy", badPol); code != 2 {
		t.Fatalf("invalid completion policy must exit 2, got %d", code)
	}
}

// Identity-invalid end-to-end: a cross-repo request under required completion blocks as
// an identity failure — it never degrades to a pass.
func TestGateCompletionEnforce_IdentityInvalidBlocks(t *testing.T) {
	const domain = "test.domain"
	a, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed a: %v", err)
	}
	b, err := resulttestkit.Seed(t.TempDir(), resulttestkit.Options{})
	if err != nil {
		t.Fatalf("seed b: %v", err)
	}
	pol := writeCompletionPolicy(t, t.TempDir(), domain, "enforce", true)
	// Repo A + repo B's task dir → identity invalid (one-world violation).
	code := runGate([]string{"--completion", "--enforce", "--repo-root", a.Repo, "--task-dir", b.TaskDir, "--domain", domain, "--policy", pol})
	if code != 1 {
		t.Fatalf("cross-repo identity under required completion must block (exit 1), got %d", code)
	}
}
