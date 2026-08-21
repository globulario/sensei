// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"testing"

	"github.com/globulario/sensei/golang/architecture"
)

// #230: a claim about a local identifier is not a claim about the architecture,
// and it does not stay quiet — it becomes an OpenQuestion nobody can answer,
// standing in front of a bounded change.
func TestArchitecturalSubject(t *testing.T) {
	kept := []string{
		"doctor.configuredCommands",           // package-qualified, unexported: a real declaration
		"internal/doctor/doctor.go",           // a file
		"component.golang.architecture.rigor", // an id
		"ProtectedPaths",                      // exported bare identifier
		"cluster/state",                       // a resource path
		"invariant:sensei.some_rule",          // a namespaced id
	}
	for _, s := range kept {
		if !isArchitecturalSubject(s) {
			t.Errorf("%q was rejected; it names something the architecture can be asked about", s)
		}
	}

	// Observed live in this repository's own derived corpus: 4,458 distinct
	// subjects of this shape out of 14,477.
	dropped := []string{"seen", "lit", "m", "id", "fn", "t", "v", "tip", "scope", "cred", "subParts", "graphProj", ""}
	for _, s := range dropped {
		if isArchitecturalSubject(s) {
			t.Errorf("%q was accepted; a bare unexported name exists only inside one function body", s)
		}
	}
}

// The engine drops them wherever they come from, so a rule added later cannot
// reintroduce the class by not knowing about it.
func TestEngineDropsClaimsAboutLocalIdentifiers(t *testing.T) {
	ctx := supportedContext([]architecture.Fact{
		testFact("a", "write", "svc.A", "writes", "seen", 0.5),
		testFact("b", "write", "svc.B", "writes", "cluster/state", 0.5),
	})
	engine := NewEngine([]Rule{ObservedWriterSetRule{}})
	apps, err := engine.Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 {
		t.Fatalf("apps=%d, want 1 — only the architectural subject may be claimed", len(apps))
	}
	if apps[0].Claim.Statement.Subject != "cluster/state" {
		t.Fatalf("kept the wrong subject: %q", apps[0].Claim.Statement.Subject)
	}
	if engine.DroppedNonArchitecturalSubjects() != 1 {
		t.Fatalf("dropped=%d, want 1 — a run that derived nothing about a local and one that never looked must be distinguishable",
			engine.DroppedNonArchitecturalSubjects())
	}
}

// The FACTS are untouched. A local write is a real observation and stays one;
// what stops is promoting it into a proposition about the architecture.
func TestDroppingAClaimDoesNotDropItsFact(t *testing.T) {
	facts := []architecture.Fact{testFact("a", "write", "svc.A", "writes", "seen", 0.5)}
	ctx := supportedContext(facts)
	apps, err := NewEngine([]Rule{ObservedWriterSetRule{}}).Apply(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Fatalf("apps=%d, want 0", len(apps))
	}
	doc, err := BuildClaimDocument(ctx, apps)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Claims) != 0 {
		t.Fatalf("claims=%d, want 0", len(doc.Claims))
	}
	if len(ctx.Facts) != 1 {
		t.Fatalf("the premise fact was discarded with the claim: %d fact(s)", len(ctx.Facts))
	}
}
