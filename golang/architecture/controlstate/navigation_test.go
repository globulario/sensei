// SPDX-License-Identifier: AGPL-3.0-only

package controlstate

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestBuildNavigationDescriptor_MechanicallyDerived(t *testing.T) {
	reg := DefaultRegistry()
	d, err := BuildNavigationDescriptor(reg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := ValidateNavigationDescriptor(d); err != nil {
		t.Fatalf("descriptor invalid: %v", err)
	}
	regDigest, _ := reg.Digest()
	if d.RegistryDigest != regDigest {
		t.Fatalf("descriptor not bound to registry digest")
	}
	// Every non-fallback registry class appears exactly once across families, with capabilities
	// copied verbatim (no invented policy).
	seen := map[string]NavigationClass{}
	for _, f := range d.Families {
		for _, c := range f.Classes {
			if _, dup := seen[c.ClassIRI]; dup {
				t.Fatalf("class %q appears twice", c.ClassIRI)
			}
			seen[c.ClassIRI] = c
		}
	}
	for _, rc := range reg.Classes {
		if rc.Unclassified {
			continue
		}
		nc, ok := seen[rc.ClassIRI]
		if !ok {
			t.Fatalf("registry class %q missing from descriptor", rc.ClassIRI)
		}
		if nc.QueryCapable != rc.QueryCapable || nc.ResolveCapable != rc.ResolveCapable ||
			nc.InspectorCapable != rc.InspectorCapable || nc.QuestionCapable != rc.QuestionCapable ||
			nc.AssessableArtifact != (rc.Coverage == CoverageAssessable) {
			t.Fatalf("class %q capabilities not copied verbatim from the registry", rc.ClassIRI)
		}
	}
}

func TestBuildNavigationDescriptor_UnknownFallbackVisibleAndUnknown(t *testing.T) {
	d, _ := BuildNavigationDescriptor(DefaultRegistry())
	if !d.UnknownClassFallback.DefaultVisible || d.UnknownClassFallback.Coverage != CoverageUnknown {
		t.Fatalf("unknown-class fallback must be visible + unknown: %+v", d.UnknownClassFallback)
	}
	if d.UnknownClassFallback.AssessableArtifact {
		t.Fatal("unknown fallback must not be assessable")
	}
}

func TestBuildNavigationDescriptor_Deterministic(t *testing.T) {
	a, _ := BuildNavigationDescriptor(DefaultRegistry())
	b, _ := BuildNavigationDescriptor(DefaultRegistry())
	if a.DigestSHA256 != b.DigestSHA256 {
		t.Fatal("navigation descriptor is not deterministic")
	}
	// Tamper-evident.
	bad := a
	bad.Families = append([]NavigationFamily(nil), a.Families...)
	bad.Families[0].Label = "forged"
	if ValidateNavigationDescriptor(bad) == nil {
		t.Fatal("mutated descriptor must fail validation")
	}
}

func TestBuildNavigationDescriptor_MetaPrincipleUnderPatterns(t *testing.T) {
	d, _ := BuildNavigationDescriptor(DefaultRegistry())
	for _, f := range d.Families {
		for _, c := range f.Classes {
			if c.ClassIRI == rdf.ClassMetaPrinciple && f.ID != famPatterns {
				t.Fatalf("meta-principle in family %q, want patterns", f.ID)
			}
		}
	}
}
