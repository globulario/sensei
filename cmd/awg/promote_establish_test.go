// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/derive"
)

func withDeriver(t *testing.T, outcome derive.Outcome) {
	t.Helper()
	prev := deriveForPromotion
	deriveForPromotion = func(_ context.Context, _, _ string, p derive.Proposition) (derive.Outcome, string) {
		return outcome, "stub for " + p.String()
	}
	t.Cleanup(func() { deriveForPromotion = prev })
	t.Setenv("SENSEI_PROMOTION_BASE", "HEAD")
}

// Verified citations alone do not cross the second boundary.
func TestVerifiedEvidenceAloneIsNotEstablished(t *testing.T) {
	got := establishCandidate(context.Background(), "../..", t.TempDir(), map[string]interface{}{
		"id": "x.y", "evidence_refs": []interface{}{}})
	if got.Verdict != notEstablished {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Detail, "verified citations alone do not cross") {
		t.Fatalf("the refusal must say what is missing: %s", got.Detail)
	}
}

// A derivation, re-run here, establishes -- and only when it DERIVES.
func TestADerivationEstablishesOnlyWhenItDerives(t *testing.T) {
	cand := map[string]interface{}{"id": "x.y", "derivation": map[string]interface{}{
		"kind": "field_access_under_lock", "dir": "internal/event", "type": "Bus", "field": "subs", "lock": "mu"}}
	withDeriver(t, derive.Derived)
	if got := establishCandidate(context.Background(), "../..", t.TempDir(), cand); got.Verdict != establishedByDerivation {
		t.Fatalf("DERIVED did not establish: %+v", got)
	}
	for _, o := range []derive.Outcome{derive.Refuted, derive.Unresolved, derive.Unknown} {
		withDeriver(t, o)
		if got := establishCandidate(context.Background(), "../..", t.TempDir(), cand); got.Verdict != notEstablished {
			t.Fatalf("%s established the proposition: %+v", o, got)
		}
	}
}

// An existing governed entry establishes; a candidate-status or unknown one does not.
func TestAGovernedEntryEstablishesOnlyIfCanonical(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "invariants.yaml"), []byte(`invariants:
  - id: real.one
    status: active
  - status: candidate
    id: candidate.two
`), 0o644)
	os.MkdirAll(filepath.Join(dir, "candidates"), 0o755)
	os.WriteFile(filepath.Join(dir, "candidates", "x.yaml"), []byte("invariants:\n  - id: sneaky.three\n    status: active\n"), 0o644)
	cases := map[string]establishmentVerdict{
		"real.one": establishedByAuthority, "invariant:real.one": establishedByAuthority,
		"candidate.two": notEstablished, "sneaky.three": notEstablished, "nope.four": notEstablished,
	}
	for id, want := range cases {
		got := establishCandidate(context.Background(), "../..", dir, map[string]interface{}{"id": "x.y", "governed_by": id})
		if got.Verdict != want {
			t.Fatalf("governed_by %q: got %s, want %s (%s)", id, got.Verdict, want, got.Detail)
		}
	}
}

// The refused state is written INTO the candidate, so it is represented.
func TestNotEstablishedIsRecordedOnTheCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	os.WriteFile(path, []byte("candidates:\n  - id: x.y\n    class: invariant\n    status: candidate\n"), 0o644)
	cand := map[string]interface{}{"id": "x.y", "class": "invariant", "status": "candidate"}
	err := recordNotEstablished(path, cand,
		evidenceResult{Verdict: evidenceVerified, Detail: "2 citations exist"},
		establishment{notEstablished, "nothing independent establishes the proposition"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{"status: candidate", "establishment: 'NOT_ESTABLISHED", "evidence_verification: 'EVIDENCE_VERIFIED"} {
		if !strings.Contains(string(b), want) && !strings.Contains(string(b), strings.ReplaceAll(want, "'", "")) {
			t.Fatalf("candidate file does not record %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "\x00") {
		t.Fatal("an internal verdict key reached disk")
	}
}

// runPromote must consult the second boundary before transforming, and there
// must be no flag that crosses it.
func TestRunPromoteConsultsEstablishmentAndOffersNoEscape(t *testing.T) {
	src := readSourceFile(t, "cmd_promote.go")
	i := strings.Index(src, "func runPromote(")
	body := src[i:]
	e := strings.Index(body, "establishCandidate(")
	if e < 0 {
		t.Fatal("runPromote does not consult establishCandidate")
	}
	if e > strings.Index(body, "toCanonicalEntry(") {
		t.Fatal("establishment is consulted after the entry is transformed")
	}
	if strings.Contains(body, "established\"") || strings.Contains(body, "\"established,") || strings.Contains(body, "fs.Bool(\"established") {
		t.Fatal("a claimant-controlled --established flag exists; that is the self-approval this boundary refuses")
	}
}
