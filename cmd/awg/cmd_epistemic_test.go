// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/epistemic"
)

func ledgerPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ledger.yaml")
}

func declareArgs(path string, extra ...string) []string {
	return append([]string{
		"declare", "--ledger", path,
		"--id", "dq.test",
		"--question", "How should the thing be established?",
		"--declared-by", "test",
		"--alternative", "a=marker plus a triple count",
		"--alternative", "b=a canonical content digest recomputed at load",
		"--consequence", "a full scan on startup",
	}, extra...)
}

func TestDeclareWritesALedgerAndComputesTheDisposition(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	l, err := epistemic.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Questions) != 1 || l.Questions[0].ID != "dq.test" {
		t.Fatalf("ledger: %+v", l.Questions)
	}
	if l.Questions[0].DeclaredAt == "" {
		t.Fatal("declared_at must be stamped by the tool, not supplied by the caller")
	}
	if d, _ := epistemic.Dispose(l.Questions[0]); d != epistemic.DispositionExploration {
		t.Fatalf("disposition = %q", d)
	}
}

// The disposition is computed, so there is no flag that could set it. A flag
// named for a regime, or for the difficulty of the question, would be the
// escape hatch the whole design refuses -- so the surface is asserted rather
// than described.
func TestDeclareExposesNoFlagThatCouldAuthorADisposition(t *testing.T) {
	var out strings.Builder
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	runEpistemic([]string{"declare", "-h"})
	w.Close()
	os.Stderr = old
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	out.Write(buf[:n])
	help := strings.ToLower(out.String())

	for _, forbidden := range []string{
		"-disposition", "-regime", "-exploration", "-authority",
		"-conservation", "-difficulty", "-needs-human", "-escalate",
	} {
		if strings.Contains(help, forbidden) {
			t.Fatalf("declare exposes %q: a disposition is computed from structure, and AUTHORITY is reached by consequence rather than by difficulty", forbidden)
		}
	}
	if !strings.Contains(help, "irreversible-consequence") {
		t.Fatal("declare must expose the consequence boundary, which is the only route to AUTHORITY")
	}
}

func TestDeclareRefusesASingleAlternative(t *testing.T) {
	p := ledgerPath(t)
	args := []string{
		"declare", "--ledger", p, "--id", "dq.solo",
		"--question", "q", "--declared-by", "test",
		"--alternative", "a=only one",
		"--consequence", "none to speak of",
	}
	if code := runEpistemic(args); code == 0 {
		t.Fatal("one alternative is a decision already made; it must be refused")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a rejected declaration must not create a ledger")
	}
}

func TestIrreversibleConsequenceReachesAuthority(t *testing.T) {
	p := ledgerPath(t)
	code := runEpistemic(declareArgs(p, "--irreversible-consequence", "publishes a release artifact"))
	if code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	b, _ := os.ReadFile(p)
	l, _ := epistemic.Decode(b)
	d, why := epistemic.Dispose(l.Questions[0])
	if d != epistemic.DispositionAuthority {
		t.Fatalf("disposition = %q, want %q", d, epistemic.DispositionAuthority)
	}
	if !strings.Contains(why, "release artifact") {
		t.Fatalf("the reason must name the consequence: %q", why)
	}
}

// The tripwire is the only reason the liveness counters are worth keeping. A
// hypothesis whose horizon passed with nothing observed must make the command
// fail, or the overdue count is a number nobody is obliged to look at.
func TestStatusTripwireFailsOnAnOverdueHypothesis(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	hyp := []string{
		"hypothesize", "--ledger", p,
		"--id", "h.test", "--question", "dq.test", "--alternative", "b",
		"--prediction", "recomputing the digest detects a count-preserving mutation",
		"--falsifier", "a drifted store is still reported authoritative while every gate is green",
		"--due", "2000-01-01T00:00:00Z",
		"--declared-by", "test",
	}
	if code := runEpistemic(hyp); code != 0 {
		t.Fatalf("hypothesize exited %d", code)
	}

	if code := runEpistemic([]string{"status", "--ledger", p}); code != 0 {
		t.Fatalf("status without --tripwire must report, not fail; exited %d", code)
	}
	if code := runEpistemic([]string{"status", "--ledger", p, "--tripwire"}); code != 1 {
		t.Fatalf("--tripwire on an overdue hypothesis exited %d, want 1", code)
	}

	obs := []string{
		"observe", "--ledger", p, "--id", "o.test", "--hypothesis", "h.test",
		"--outcome", "refutes", "--what", "a drifted store was reported authoritative",
		"--evidence", "seedmeta verify_test.go", "--observed-by", "test",
		"--conditions", "a count-preserving mutation, with the marker untouched",
	}
	if code := runEpistemic(obs); code != 0 {
		t.Fatalf("observe exited %d", code)
	}
	// Refuted is settled: it leaves the active denominator, so the tripwire
	// clears. Observing is the only thing that clears it.
	if code := runEpistemic([]string{"status", "--ledger", p, "--tripwire"}); code != 0 {
		t.Fatalf("--tripwire after the belief was settled exited %d, want 0", code)
	}
}

func TestHypothesizeRefusesAGateRestatingFalsifier(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	args := []string{
		"hypothesize", "--ledger", p,
		"--id", "h.weak", "--question", "dq.test", "--alternative", "b",
		"--prediction", "the abstraction reduces authority leakage",
		"--falsifier", "the tests fail",
		"--due", "2027-01-01T00:00:00Z", "--declared-by", "test",
	}
	if code := runEpistemic(args); code == 0 {
		t.Fatal("a falsifier that restates the merge gate says nothing about the architectural claim")
	}
}

func TestUnknownSubcommandIsAUsageError(t *testing.T) {
	if code := runEpistemic([]string{"promote"}); code != 2 {
		t.Fatalf("exited %d, want 2 — there is no promotion in this lane", code)
	}
	if code := runEpistemic(nil); code != 2 {
		t.Fatalf("exited %d, want 2", code)
	}
}

// The anti-sediment check, end to end through the CLI and against a corpus on
// disk. An agent guesses B, implements B, extraction records that B exists, B
// becomes architecture, and the agent can no longer replace its own guess —
// this is what notices.
func TestScopeReportsExperimentalCodeDefendedAsArchitecture(t *testing.T) {
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
		"--due", "2027-06-01T00:00:00Z", "--declared-by", "test",
	}
	if code := runEpistemic(hyp); code != 0 {
		t.Fatalf("hypothesize exited %d", code)
	}
	// Declare the experimental scope by hand: there is no flag for it yet, and
	// the check must work on a ledger however it was written.
	b, _ := os.ReadFile(p)
	l, err := epistemic.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	l.Hypotheses[0].ExperimentalScope = []string{"golang/placement/v2"}
	if err := saveLedger(p, l); err != nil {
		t.Fatal(err)
	}

	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", root}); code != 0 {
		t.Fatalf("scope without --tripwire must report, not fail; exited %d", code)
	}
	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", root, "--tripwire"}); code != 1 {
		t.Fatalf("--tripwire on sediment exited %d, want 1", code)
	}

	// Experimental code nothing canonical claims is clean. Naming a scope does
	// not remove governance — the established envelope still holds — it says
	// the design inside it has not silently become law.
	l.Hypotheses[0].ExperimentalScope = []string{"golang/somewhere/else"}
	if err := saveLedger(p, l); err != nil {
		t.Fatal(err)
	}
	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", root, "--tripwire"}); code != 0 {
		t.Fatalf("unclaimed experimental code exited %d, want 0", code)
	}
}

// A repository with no such surface anchors nothing. Reporting that as an error
// would make the check unusable everywhere except this repository.
func TestScopeOnARepositoryWithNoAwarenessCorpus(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", t.TempDir(), "--tripwire"}); code != 0 {
		t.Fatalf("exited %d, want 0", code)
	}
}

func TestObserveRefutationNeedsItsConditionsAndKeepsWhatSurvives(t *testing.T) {
	p := ledgerPath(t)
	if code := runEpistemic(declareArgs(p)); code != 0 {
		t.Fatalf("declare exited %d", code)
	}
	hyp := []string{
		"hypothesize", "--ledger", p, "--id", "h.r", "--question", "dq.test", "--alternative", "b",
		"--prediction", "the v2 pass converges without stale ownership",
		"--falsifier", "stale ownership survives two reconciliation cycles while every gate is green",
		"--due", "2027-06-01T00:00:00Z", "--declared-by", "test",
		"--scope", "golang/placement/v2",
	}
	if code := runEpistemic(hyp); code != 0 {
		t.Fatalf("hypothesize exited %d", code)
	}

	bare := []string{
		"observe", "--ledger", p, "--id", "o.r", "--hypothesis", "h.r", "--outcome", "refutes",
		"--what", "stale ownership survived two cycles", "--evidence", "fault run F7", "--observed-by", "test",
	}
	if code := runEpistemic(bare); code == 0 {
		t.Fatal("a refutation without its conditions turns one experiment into a universal prohibition")
	}

	full := append(append([]string{}, bare...),
		"--conditions", "network partition with leader turnover",
		"--still-viable-for", "non-authoritative cache placement")
	if code := runEpistemic(full); code != 0 {
		t.Fatalf("observe exited %d", code)
	}

	b, _ := os.ReadFile(p)
	l, err := epistemic.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Observations) != 1 || l.Observations[0].RemainingApplicability == "" {
		t.Fatalf("what survives a refutation must be kept: %+v", l.Observations)
	}
	if len(l.Hypotheses[0].ExperimentalScope) != 1 {
		t.Fatalf("--scope must reach the ledger: %+v", l.Hypotheses[0])
	}
	// The code written to test a now-refuted belief is orphaned: keeping it may
	// be deliberate, but the reason it existed is gone.
	if code := runEpistemic([]string{"scope", "--ledger", p, "--repo-root", t.TempDir(), "--tripwire"}); code != 1 {
		t.Fatalf("scope exited %d, want 1", code)
	}
}
