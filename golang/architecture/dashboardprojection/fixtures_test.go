// SPDX-License-Identifier: AGPL-3.0-only

package dashboardprojection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "fixtures", "dashboard-projection", "v1")
}

func loadFixture(t *testing.T, rel string) Projection {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixtureRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	var p Projection
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return p
}

// TestValidFixturesPassProducerValidation proves every fixture labeled valid
// (including the real-repo one) has zero producer-validation errors — not
// just that it parses as JSON.
func TestValidFixturesPassProducerValidation(t *testing.T) {
	for _, rel := range []string{
		"real-repo/projection.json",
		"public-redacted/projection.json",
		"partial/projection.json",
		"unavailable/projection.json",
		"contested/projection.json",
		"evolution-first-revision/projection.json",
	} {
		t.Run(rel, func(t *testing.T) {
			p := loadFixture(t, rel)
			if errs := Validate(p); len(errs) != 0 {
				t.Fatalf("expected 0 validation errors, got %v", errs)
			}
		})
	}
}

// TestPublicRedactedFixtureIsRedacted confirms the public-redacted fixture
// actually demonstrates what its name claims.
func TestPublicRedactedFixtureIsRedacted(t *testing.T) {
	p := loadFixture(t, "public-redacted/projection.json")
	if p.ActiveContext != nil {
		t.Fatalf("expected active_context: null in a public-redacted fixture, got %+v", p.ActiveContext)
	}
	if errs := ValidatePublicRedaction(p); len(errs) != 0 {
		t.Fatalf("ValidatePublicRedaction: %v", errs)
	}
	if p.Capabilities.AgentHandoff == HandoffLive {
		t.Fatalf("a public/static fixture must never report the live handoff capability")
	}
}

// TestInvalidFixturesAreJSONSchemaValidButProducerInvalid proves the two
// "invalid" fixtures are exactly what issue #115 asked for: instances a
// generic JSON Schema validator would accept, that this producer's
// cross-record validation correctly rejects. If a JSON Schema validator
// dependency is later added to this repo, that half of the claim should be
// re-verified in CI too; today it is verified out-of-band (see PR
// description) since this repo does not carry a JSON Schema library.
func TestInvalidFixturesAreJSONSchemaValidButProducerInvalid(t *testing.T) {
	cases := []struct {
		rel  string
		rule string
	}{
		{"invalid/missing-focus-record.json", "missing_focus_record"},
		{"invalid/duplicate-focus-record.json", "duplicate_focus_record"},
	}
	for _, c := range cases {
		t.Run(c.rel, func(t *testing.T) {
			p := loadFixture(t, c.rel)
			errs := Validate(p)
			if !hasRule(errs, c.rule) {
				t.Fatalf("expected rule %q among validation errors, got %v", c.rule, errs)
			}
		})
	}
}

func TestHandoffFixturesParseAndMatchTheirLabel(t *testing.T) {
	load := func(rel string) HandoffEnvelope {
		data, err := os.ReadFile(filepath.Join(fixtureRoot(t), rel))
		if err != nil {
			t.Fatal(err)
		}
		var h HandoffEnvelope
		if err := json.Unmarshal(data, &h); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		return h
	}

	ro := load("handoff/read-only.json")
	if ro.SchemaVersion != HandoffSchemaVersion {
		t.Fatalf("read-only.json schema_version = %q, want %q", ro.SchemaVersion, HandoffSchemaVersion)
	}
	if ro.Capability != HandoffReadOnly {
		t.Fatalf("read-only.json capability = %q, want %q", ro.Capability, HandoffReadOnly)
	}

	propose := load("handoff/propose.json")
	if propose.Capability != HandoffPropose {
		t.Fatalf("propose.json capability = %q, want %q", propose.Capability, HandoffPropose)
	}
}

func TestFixturesDirectoryHasNoUnexpectedContent(t *testing.T) {
	root := fixtureRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"real-repo": true, "public-redacted": true, "partial": true, "unavailable": true,
		"contested": true, "evolution-first-revision": true, "invalid": true, "handoff": true,
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !want[e.Name()] {
			t.Errorf("unexpected fixture directory %q; update this test's allowlist if it's intentional", e.Name())
		}
	}
}
