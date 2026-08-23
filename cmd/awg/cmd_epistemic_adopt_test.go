// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The rule the whole adoption event exists to enforce, end to end: a SUPPORTED
// design whose code canonical architecture already defends stays sediment until
// it is adopted, and adoption is the only thing that clears it.
func TestSupportedCodeStaysSedimentUntilAdopted(t *testing.T) {
	root := t.TempDir()
	aw := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(aw, 0o755); err != nil {
		t.Fatal(err)
	}
	inv := "invariants:\n  - id: invariant.placement_is_deterministic\n    protects:\n      files:\n        - golang/placement/v2/assign.go\n"
	if err := os.WriteFile(filepath.Join(aw, "invariants.yaml"), []byte(inv), 0o644); err != nil {
		t.Fatal(err)
	}

	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	hyp := []string{
		"hypothesize", "--ledger", p, "--id", "h.placement",
		"--question", "dq.test", "--alternative", "b",
		"--prediction", "the v2 placement pass converges without stale ownership",
		"--falsifier", "stale ownership survives two reconciliation cycles while every gate is green",
		"--due", "2000-01-01T00:00:00Z", "--declared-by", "test",
		"--scope", "golang/placement/v2",
	}
	if code := runEpistemic(hyp); code != 0 {
		t.Fatalf("hypothesize exited %d", code)
	}
	obs := []string{
		"observe", "--ledger", p, "--id", "o.support", "--hypothesis", "h.placement",
		"--outcome", "supports", "--what", "converged across 40 fault runs",
		"--evidence", "fault suite F1-F9", "--observed-by", "an independent run",
	}
	if code := runEpistemic(obs); code != 0 {
		t.Fatalf("observe exited %d", code)
	}

	// SUPPORTED, and canonical architecture already defends the code. That is
	// still sediment: reaching SUPPORTED earns the right to be adopted, it does
	// not adopt.
	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", root, "--tripwire"}); code != 1 {
		t.Fatalf("supported-but-unadopted exited %d, want 1", code)
	}

	adopt := []string{
		"adopt", "--ledger", p, "--id", "ad.placement",
		"--question", "dq.test", "--design", "b",
		"--evidence", "h.placement",
		"--remaining-uncertainty", "unmeasured above 10k writes/sec",
		"--adopted-by", "test",
	}
	if code := runEpistemic(adopt); code != 0 {
		t.Fatalf("adopt exited %d", code)
	}
	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", root, "--tripwire"}); code != 0 {
		t.Fatalf("adoption must be the way out; exited %d", code)
	}
}

// Adoption before the evidence is promotion on silence.
func TestAdoptRefusesAnUnobservedBelief(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	hyp := []string{
		"hypothesize", "--ledger", p, "--id", "h.early", "--question", "dq.test", "--alternative", "b",
		"--prediction", "it converges", "--falsifier", "it does not converge while every gate is green",
		"--due", "2027-06-01T00:00:00Z", "--declared-by", "test",
	}
	if code := runEpistemic(hyp); code != 0 {
		t.Fatalf("hypothesize exited %d", code)
	}
	adopt := []string{
		"adopt", "--ledger", p, "--id", "ad.early", "--question", "dq.test", "--design", "b",
		"--evidence", "h.early", "--remaining-uncertainty", "none identified", "--adopted-by", "test",
	}
	if code := runEpistemic(adopt); code == 0 {
		t.Fatal("adopting before the horizon matured is promotion on silence")
	}
}

// An adoption that does not say what is still unknown is how SUPPORTED becomes
// PROVEN later, when nobody remembers which it was.
func TestAdoptRequiresRemainingUncertainty(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p, "--eliminated", "a=inv.one", "--constraint", "inv.one")); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	adopt := []string{
		"adopt", "--ledger", p, "--id", "ad.x", "--question", "dq.test", "--design", "b",
		"--adopted-by", "test",
	}
	if code := runEpistemic(adopt); code == 0 {
		t.Fatal("remaining_uncertainty must be stated, even as \"none identified\"")
	}
	full := append(append([]string{}, adopt...), "--remaining-uncertainty", "none identified")
	if code := runEpistemic(full); code != 0 {
		t.Fatalf("a conservation question adopts on its constraints; exited %d", code)
	}
}
