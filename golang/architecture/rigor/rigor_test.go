// SPDX-License-Identifier: AGPL-3.0-only

package rigor

import (
	"strings"
	"testing"
)

func testManifest(t *testing.T) Manifest {
	t.Helper()
	m, err := ParseManifest([]byte(`
surfaces:
  - id: runtimeboundary.owner
    class: A
    packages: [golang/architecture/runtimeboundary]
  - id: runtimeprobe.ingestion
    class: B
    packages: [golang/architecture/runtimeprobe]
  - id: controlstate.projection
    class: C
    packages: [golang/architecture/controlstate]
  - id: vscode.presentation
    class: D
    packages: [editor/vscode/media]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return m
}

// A changed file owned by NO surface forces Class A (fail closed).
func TestUnclassifiedFileForcesClassA(t *testing.T) {
	m := testManifest(t)
	d := ClassifyChange(m, []string{"some/unknown/path.go"}, ClassUnknown)
	if d.Effective != ClassA {
		t.Fatalf("unclassified file must force Class A, got %q", d.Effective)
	}
	if len(d.Unclassified) != 1 {
		t.Fatalf("the unclassified file must be reported, got %v", d.Unclassified)
	}
}

// Effective rigor is the STRICTEST class among all touched surfaces.
func TestEffectiveIsStrictestTouched(t *testing.T) {
	m := testManifest(t)
	// Touches a D surface and an A surface → strictest (A) wins.
	d := ClassifyChange(m, []string{
		"editor/vscode/media/controlPanel.js",
		"golang/architecture/runtimeboundary/assess.go",
	}, ClassUnknown)
	if d.Effective != ClassA {
		t.Fatalf("touching an A surface must yield Class A, got %q", d.Effective)
	}
	// C + B → B is stricter than C.
	d2 := ClassifyChange(m, []string{
		"golang/architecture/controlstate/snapshot.go",
		"golang/architecture/runtimeprobe/observation.go",
	}, ClassUnknown)
	if d2.Effective != ClassB {
		t.Fatalf("C+B must yield B, got %q", d2.Effective)
	}
}

// A declared C/D class can NEVER downgrade contact with an A/B surface.
func TestDeclaredCannotDowngradeContactWithStrictSurface(t *testing.T) {
	m := testManifest(t)
	d := ClassifyChange(m, []string{"golang/architecture/runtimeboundary/assess.go"}, ClassD)
	if d.Effective != ClassA {
		t.Fatalf("a declared D must not downgrade an A-touching change, got %q", d.Effective)
	}
	if d.DeclaredHonored {
		t.Fatal("the weaker declaration must be recorded as NOT honored")
	}
	if !strings.Contains(strings.Join(d.Reasons, " "), "never downgrade") {
		t.Fatalf("the ignored downgrade must be explained, reasons=%v", d.Reasons)
	}
}

// A declared class can RAISE strictness (never weaken it).
func TestDeclaredCanRaise(t *testing.T) {
	m := testManifest(t)
	d := ClassifyChange(m, []string{"editor/vscode/media/controlPanel.js"}, ClassA)
	if d.Effective != ClassA || !d.DeclaredHonored {
		t.Fatalf("a declared A must raise a D-only change to A, got %q honored=%v", d.Effective, d.DeclaredHonored)
	}
}

// Class D keeps EVERY repository-integrity gate — it lightens only semantic proof.
func TestClassDKeepsIntegrityGates(t *testing.T) {
	obs := ObligationsFor(ClassD)
	for _, gate := range integrityGates {
		if !containsOb(obs, gate) {
			t.Fatalf("Class D must still owe the integrity gate %q", gate)
		}
	}
	// D must NOT owe the heaviest semantic proofs.
	for _, heavy := range []ObligationKind{ObOwnerVerdictProof, ObCertificationProof} {
		if containsOb(obs, heavy) {
			t.Fatalf("Class D must not owe the semantic proof %q", heavy)
		}
	}
}

// An unknown/off-vocabulary class fails closed to Class A's obligations (never weaker).
func TestUnknownClassFailsClosedToA(t *testing.T) {
	got := ObligationsFor(ClassUnknown)
	want := ObligationsFor(ClassA)
	if len(got) != len(want) {
		t.Fatalf("unknown class must owe Class A's obligations, got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unknown-class obligation %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// Obligations are cumulative in semantic weight A > B > C (each stricter class owes a superset of
// the graded proofs), while all keep the integrity gates.
func TestObligationsGradeMonotonically(t *testing.T) {
	a, b, c := len(ObligationsFor(ClassA)), len(ObligationsFor(ClassB)), len(ObligationsFor(ClassC))
	if !(a > b && b > c) {
		t.Fatalf("semantic proof must grade A>B>C, got A=%d B=%d C=%d", a, b, c)
	}
}

// The manifest rejects ambiguity and off-vocabulary input (fail closed).
func TestManifestValidationRejectsAmbiguity(t *testing.T) {
	cases := map[string]string{
		"off-vocab class": "surfaces:\n  - id: x\n    class: Z\n    packages: [a/b]\n",
		"no packages":     "surfaces:\n  - id: x\n    class: A\n    packages: []\n",
		"dup id":          "surfaces:\n  - id: x\n    class: A\n    packages: [a/b]\n  - id: x\n    class: B\n    packages: [c/d]\n",
		"dup package":     "surfaces:\n  - id: x\n    class: A\n    packages: [a/b]\n  - id: y\n    class: B\n    packages: [a/b]\n",
		"empty id":        "surfaces:\n  - id: \"\"\n    class: A\n    packages: [a/b]\n",
	}
	for name, y := range cases {
		if _, err := ParseManifest([]byte(y)); err == nil {
			t.Fatalf("%s: manifest must be rejected", name)
		}
	}
}

// Overlapping prefixes take the STRICTEST matching surface — a narrower but WEAKER surface can
// never trick classification into a lighter class than a broader stricter surface that also owns
// the file (reviewer point 4). Both directions are covered.
func TestOverlappingPrefixesTakeStrictest(t *testing.T) {
	// Broad-strict (A) over narrow-weak (C): the file must stay Class A — the narrow C cannot carve
	// out a weaker zone inside the broader A surface.
	trap, err := ParseManifest([]byte(`
surfaces:
  - id: broad
    class: A
    packages: [golang/architecture]
  - id: narrow
    class: C
    packages: [golang/architecture/controlstate]
`))
	if err != nil {
		t.Fatal(err)
	}
	d := ClassifyChange(trap, []string{"golang/architecture/controlstate/snapshot.go"}, ClassUnknown)
	if d.Effective != ClassA {
		t.Fatalf("a narrow weak surface must NOT downgrade a broad strict one, got %q", d.Effective)
	}
	if len(d.TouchedSurfaces) != 2 {
		t.Fatalf("both overlapping surfaces must be reported as touched, got %v", d.TouchedSurfaces)
	}
	// Broad-weak (C) over narrow-strict (A): still the strictest (A).
	rev, err := ParseManifest([]byte(`
surfaces:
  - id: broad
    class: C
    packages: [golang/architecture]
  - id: narrow
    class: A
    packages: [golang/architecture/runtimeboundary]
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := ClassifyChange(rev, []string{"golang/architecture/runtimeboundary/assess.go"}, ClassUnknown).Effective; got != ClassA {
		t.Fatalf("strictest matching surface must win regardless of prefix length, got %q", got)
	}
}

// FormatDecision surfaces the load-bearing facts: the effective class, that a weaker declaration was
// ignored, and that the integrity gates are owed by every class.
func TestFormatDecisionSurfacesKeyFacts(t *testing.T) {
	m := testManifest(t)
	out := FormatDecision(ClassifyChange(m, []string{"golang/architecture/runtimeboundary/assess.go"}, ClassD))
	for _, want := range []string{"Class A", "IGNORED", "repository integrity", "owner_verdict_proof"} {
		if !strings.Contains(out, want) {
			t.Fatalf("FormatDecision must surface %q; got:\n%s", want, out)
		}
	}
	// A Class D report still lists the integrity gates and never the heavy semantic proofs.
	dOut := FormatDecision(ClassifyChange(m, []string{"editor/vscode/media/x.css"}, ClassUnknown))
	if !strings.Contains(dOut, "Class D") || !strings.Contains(dOut, "ownership_guard") {
		t.Fatalf("Class D report must keep integrity gates:\n%s", dOut)
	}
	if strings.Contains(dOut, "certification_proof") {
		t.Fatalf("Class D must not owe certification_proof:\n%s", dOut)
	}
}

func containsOb(xs []ObligationKind, x ObligationKind) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
