// SPDX-License-Identifier: AGPL-3.0-only

package dashboardprojection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func schemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "dashboard-projection", "v1")
}

// TestSchemasParseAndAreClosed mirrors closureprotocol's
// TestSchemasParseAndAreClosed: every vendored schema must parse as JSON and
// keep additionalProperties:false at its object roots, matching the strict,
// versioned-evolution policy in architecture-dashboard-v1.md §10.
func TestSchemasParseAndAreClosed(t *testing.T) {
	root := schemaRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected exactly the 2 adopted schemas (dashboard-projection-v1, agent-handoff-v1), got %d entries", len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if v, ok := doc["additionalProperties"]; !ok || v != false {
			t.Fatalf("%s: root additionalProperties must be false", entry.Name())
		}
	}
}

// TestSchemaVersionIdentifiersMatchAdopted confirms the vendored schemas'
// const identifiers exactly match what issue #115 adopted, so a rename in
// upstream sensei-dashboard cannot silently drift the producer's contract.
func TestSchemaVersionIdentifiersMatchAdopted(t *testing.T) {
	root := schemaRoot(t)
	proj := readSchema(t, filepath.Join(root, "dashboard-projection-v1.schema.json"))
	handoff := readSchema(t, filepath.Join(root, "agent-handoff-v1.schema.json"))

	if got := constString(t, proj, "properties", "schema_version", "const"); got != SchemaVersion {
		t.Fatalf("dashboard-projection-v1 schema_version const = %q, want %q", got, SchemaVersion)
	}
	if got := constString(t, handoff, "properties", "schema_version", "const"); got != HandoffSchemaVersion {
		t.Fatalf("agent-handoff-v1 schema_version const = %q, want %q", got, HandoffSchemaVersion)
	}
}

// TestHandoffSchemaRefsResolveAgainstProjectionSchema is issue #115's
// required producer validation item 8: "Agent handoff schema references
// resolve URI-aware against the canonical projection schema." It resolves
// every "dashboard-projection-v1.schema.json#/..." $ref found in the
// agent-handoff schema as a JSON Pointer into the actual vendored projection
// schema document, proving the cross-schema pairing is not just
// textually-plausible but structurally correct.
// TestValidateProjectionSchemaCompilesAndEnforcesRealConstraints proves
// compileSchemas/ValidateProjectionSchema is a real Draft 2020-12 validator,
// not a stub: it rejects a structurally-plausible but incomplete instance,
// and it accepts a minimal genuinely-complete one built to satisfy every
// required field the schema actually declares (not this package's own
// Projection struct, so a bug in the struct's json tags can't hide here).
func TestValidateProjectionSchemaCompilesAndEnforcesRealConstraints(t *testing.T) {
	root := schemaRoot(t)

	missingRequiredFields := []byte(`{"schema_version":"sensei.dashboard.projection.v1"}`)
	if err := ValidateProjectionSchema(root, missingRequiredFields); err == nil {
		t.Fatal("expected a missing-required-fields instance to fail schema validation")
	}

	wrongSchemaVersion := []byte(`{"schema_version":"not-the-right-version"}`)
	if err := ValidateProjectionSchema(root, wrongSchemaVersion); err == nil {
		t.Fatal("expected a wrong schema_version const to fail schema validation")
	}

	badEnum := []byte(`{
		"schema_version": "sensei.dashboard.projection.v1",
		"identity": {
			"projection_id": "p1", "generated_at": "2026-07-23T00:00:00Z",
			"repository": {"key": "k", "display_name": "d"},
			"revision": {"id": "r1"},
			"graph_authority": {"observed": "yes", "current": "not-a-valid-tristate-value", "identity": null, "summary": "s"}
		},
		"availability": {"state": "available", "summary": "s", "limitations": [], "sources": []},
		"assessments": {
			"architecture_health": {"state": "unknown", "label": "l", "summary": "s", "severity": "not_applicable", "provenance": {"evidence_refs": []}},
			"projection_integrity": {"state": "unknown", "label": "l", "summary": "s", "severity": "not_applicable", "provenance": {"evidence_refs": []}},
			"observation_completeness": {"state": "unknown", "label": "l", "summary": "s", "severity": "not_applicable", "coverage": {"observed": null, "total": null, "unit": "u"}, "provenance": {"evidence_refs": []}}
		},
		"briefing": [], "regions": [], "components": [], "boundaries": [], "contracts": [], "flows": [], "attention": [],
		"evolution": {"availability": "available", "base_revision": null, "head_revision": "r1", "changes": []},
		"focus_records": []
	}`)
	if err := ValidateProjectionSchema(root, badEnum); err == nil {
		t.Fatal("expected an invalid graph_authority.current enum value to fail schema validation")
	}

	minimalComplete := []byte(`{
		"schema_version": "sensei.dashboard.projection.v1",
		"identity": {
			"projection_id": "p1", "generated_at": "2026-07-23T00:00:00Z",
			"repository": {"key": "k", "display_name": "d"},
			"revision": {"id": "r1"},
			"graph_authority": {"observed": "unknown", "current": "unknown", "identity": null, "summary": "s"}
		},
		"availability": {"state": "unavailable", "summary": "s", "limitations": [], "sources": []},
		"assessments": {
			"architecture_health": {"state": "unknown", "label": "l", "summary": "s", "severity": "not_applicable", "provenance": {"evidence_refs": []}},
			"projection_integrity": {"state": "unknown", "label": "l", "summary": "s", "severity": "not_applicable", "provenance": {"evidence_refs": []}},
			"observation_completeness": {"state": "unknown", "label": "l", "summary": "s", "severity": "not_applicable", "coverage": {"observed": null, "total": null, "unit": "u"}, "provenance": {"evidence_refs": []}}
		},
		"briefing": [], "regions": [], "components": [], "boundaries": [], "contracts": [], "flows": [], "attention": [],
		"evolution": {"availability": "unavailable", "base_revision": null, "head_revision": "r1", "changes": []},
		"focus_records": []
	}`)
	if err := ValidateProjectionSchema(root, minimalComplete); err != nil {
		t.Fatalf("expected a genuinely minimal-but-complete instance to pass schema validation, got: %v", err)
	}
}

func TestHandoffSchemaRefsResolveAgainstProjectionSchema(t *testing.T) {
	root := schemaRoot(t)
	projRaw, err := os.ReadFile(filepath.Join(root, "dashboard-projection-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	handoffRaw, err := os.ReadFile(filepath.Join(root, "agent-handoff-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var proj any
	if err := json.Unmarshal(projRaw, &proj); err != nil {
		t.Fatal(err)
	}

	refPattern := regexp.MustCompile(`"\$ref":\s*"dashboard-projection-v1\.schema\.json#([^"]*)"`)
	matches := refPattern.FindAllSubmatch(handoffRaw, -1)
	if len(matches) == 0 {
		t.Fatal("expected at least one cross-schema $ref from agent-handoff-v1 into dashboard-projection-v1; found none — the pairing this test exists to prove may have been removed")
	}
	seen := map[string]bool{}
	for _, m := range matches {
		pointer := string(m[1])
		if seen[pointer] {
			continue
		}
		seen[pointer] = true
		if _, err := resolveJSONPointer(proj, pointer); err != nil {
			t.Errorf("dashboard-projection-v1.schema.json#%s does not resolve: %v", pointer, err)
		}
	}
}

func readSchema(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func constString(t *testing.T, doc map[string]any, path ...string) string {
	t.Helper()
	var cur any = doc
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %v: %q is not an object", path, p)
		}
		cur, ok = m[p]
		if !ok {
			t.Fatalf("path %v: missing key %q", path, p)
		}
	}
	s, ok := cur.(string)
	if !ok {
		t.Fatalf("path %v: not a string", path)
	}
	return s
}

// resolveJSONPointer resolves an RFC 6901 JSON Pointer (the "#/a/b/0" part of
// a $ref, already stripped of its leading document reference) against an
// already-decoded JSON document.
func resolveJSONPointer(doc any, pointer string) (any, error) {
	if pointer == "" || pointer == "#" {
		return doc, nil
	}
	pointer = strings.TrimPrefix(pointer, "#")
	pointer = strings.TrimPrefix(pointer, "/")
	if pointer == "" {
		return doc, nil
	}
	cur := doc
	for _, raw := range strings.Split(pointer, "/") {
		tok := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, errNotResolvable(tok)
		}
		v, ok := m[tok]
		if !ok {
			return nil, errNotResolvable(tok)
		}
		cur = v
	}
	return cur, nil
}

type pointerError string

func (e pointerError) Error() string { return "no such key: " + string(e) }

func errNotResolvable(tok string) error { return pointerError(tok) }
