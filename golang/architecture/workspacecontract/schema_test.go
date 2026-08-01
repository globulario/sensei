// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func schemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "workspace", "v1")
}

func readSchemaDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return doc
}

// TestEmbeddedSchemasMatchCanonicalSource proves the go:embed'd copy under
// schemas/ (what actually ships inside cmd/awareness-mcp and every other
// binary that imports this package) is byte-identical to the canonical,
// cross-repo-pinned source under docs/schemas/workspace/v1/ — go:embed
// cannot reach outside this package directory with a ".." pattern, so the
// canonical file is mechanically copied here at commit time, and this test
// is what stops the two from silently drifting apart.
func TestEmbeddedSchemasMatchCanonicalSource(t *testing.T) {
	canonicalRoot := schemaRoot(t)
	for _, filename := range []string{IdentitySchemaFilename, AdmissionSchemaFilename} {
		t.Run(filename, func(t *testing.T) {
			canonical, err := os.ReadFile(filepath.Join(canonicalRoot, filename))
			if err != nil {
				t.Fatal(err)
			}
			embedded, err := embeddedSchemas.ReadFile("schemas/" + filename)
			if err != nil {
				t.Fatal(err)
			}
			if string(canonical) != string(embedded) {
				t.Fatalf("golang/architecture/workspacecontract/schemas/%s has drifted from docs/schemas/workspace/v1/%s — re-copy the canonical file", filename, filename)
			}
		})
	}
}

// TestSchemasParseAndAreClosed proves both vendored schemas parse as JSON
// and keep additionalProperties:false at their object roots.
func TestSchemasParseAndAreClosed(t *testing.T) {
	root := schemaRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected exactly the 2 adopted schemas (workspace-identity-v1, workspace-admission-v1), got %d entries", len(entries))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		doc := readSchemaDoc(t, filepath.Join(root, entry.Name()))
		if v, ok := doc["additionalProperties"]; !ok || v != false {
			t.Fatalf("%s: root additionalProperties must be false", entry.Name())
		}
	}
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

// TestSchemaVersionIdentifiersMatchAdopted confirms the vendored schemas'
// const identifiers exactly match this package's Go constants.
func TestSchemaVersionIdentifiersMatchAdopted(t *testing.T) {
	root := schemaRoot(t)
	identity := readSchemaDoc(t, filepath.Join(root, IdentitySchemaFilename))
	admission := readSchemaDoc(t, filepath.Join(root, AdmissionSchemaFilename))

	if got := constString(t, identity, "properties", "schema_version", "const"); got != IdentitySchemaVersion {
		t.Fatalf("workspace-identity-v1 schema_version const = %q, want %q", got, IdentitySchemaVersion)
	}
	if got := constString(t, admission, "properties", "schema_version", "const"); got != AdmissionSchemaVersion {
		t.Fatalf("workspace-admission-v1 schema_version const = %q, want %q", got, AdmissionSchemaVersion)
	}
}

// TestValidateIdentitySchemaCompilesAndEnforcesRealConstraints proves
// ValidateIdentitySchema is a real Draft 2020-12 validator, not a stub.
func TestValidateIdentitySchemaCompilesAndEnforcesRealConstraints(t *testing.T) {
	missingRequired := []byte(`{"schema_version":"sensei.workspace.identity.v1"}`)
	if err := ValidateIdentitySchema(missingRequired); err == nil {
		t.Fatal("expected a missing-required-fields instance to fail schema validation")
	}

	wrongVersion := []byte(`{"schema_version":"not-the-right-version"}`)
	if err := ValidateIdentitySchema(wrongVersion); err == nil {
		t.Fatal("expected a wrong schema_version const to fail schema validation")
	}

	minimalUnavailable := []byte(`{
		"schema_version": "sensei.workspace.identity.v1",
		"generated_by": "test",
		"composition_state": "unavailable",
		"binding": {
			"repository_domain": "", "revision": null, "revision_status": "not_requested",
			"tree_digest_sha256": null, "graph_digest_sha256": null, "graph_digest_status": "not_requested"
		},
		"repository_domain_source": "unbound",
		"graph_authority": null,
		"coverage_state": "COVERAGE_STATE_UNSPECIFIED",
		"task_identity": {"state": "not_requested", "task_id": null},
		"limitations": []
	}`)
	if err := ValidateIdentitySchema(minimalUnavailable); err != nil {
		t.Fatalf("expected a minimal-but-complete unavailable instance to pass schema validation, got: %v", err)
	}

	badEnum := []byte(`{
		"schema_version": "sensei.workspace.identity.v1",
		"generated_by": "test",
		"composition_state": "not-a-real-state",
		"binding": {
			"repository_domain": "", "revision": null, "revision_status": "not_requested",
			"tree_digest_sha256": null, "graph_digest_sha256": null, "graph_digest_status": "not_requested"
		},
		"repository_domain_source": "unbound",
		"graph_authority": null,
		"coverage_state": "COVERAGE_STATE_UNSPECIFIED",
		"task_identity": {"state": "not_requested", "task_id": null},
		"limitations": []
	}`)
	if err := ValidateIdentitySchema(badEnum); err == nil {
		t.Fatal("expected an invalid composition_state enum value to fail schema validation")
	}
}

// TestValidateAdmissionSchemaCompilesAndEnforcesRealConstraints proves
// ValidateAdmissionSchema is a real Draft 2020-12 validator, not a stub.
func TestValidateAdmissionSchemaCompilesAndEnforcesRealConstraints(t *testing.T) {
	missingRequired := []byte(`{"schema_version":"sensei.workspace.admission.v1"}`)
	if err := ValidateAdmissionSchema(missingRequired); err == nil {
		t.Fatal("expected a missing-required-fields instance to fail schema validation")
	}

	decisionWithVerification := []byte(minimalDecisionJSON(`"verification": {
		"status": "scope_compliant", "verification_digest_sha256": "`+zeroDigest+`",
		"iteration_digest_sha256": "`+zeroDigest+`", "patch_digest_sha256": "`+zeroDigest+`",
		"changes": [], "violations": [], "pending_condition_ids": [], "pending_test_ids": [],
		"pending_proof_obligation_ids": [], "pending_runtime_evidence_ids": [], "reasons": [],
		"limitations": [], "scope_only": true, "correctness_certified": false
	}`, "decision"))
	if err := ValidateAdmissionSchema(decisionWithVerification); err == nil {
		t.Fatal("expected a decision record with non-null verification to fail schema validation")
	}

	validDecision := []byte(minimalDecisionJSON(`null`, "decision"))
	if err := ValidateAdmissionSchema(validDecision); err != nil {
		t.Fatalf("expected a minimal valid decision record to pass schema validation, got: %v", err)
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func minimalDecisionJSON(verification, recordKind string) string {
	return `{
		"schema_version": "sensei.workspace.admission.v1",
		"record_kind": "` + recordKind + `",
		"admission_id": "admission.test",
		"decision_digest_sha256": "` + zeroDigest + `",
		"policy_id": "admission.strict.v1",
		"policy_version": "v1",
		"decision": "admitted",
		"requested_mode": "modify",
		"binding": {
			"repository_domain": "github.com/globulario/sensei", "revision": "abc123", "revision_status": "resolved",
			"tree_digest_sha256": "` + zeroDigest + `", "graph_digest_sha256": "` + zeroDigest + `", "graph_digest_status": "resolved"
		},
		"session_receipt": {
			"session_id": "session.1", "latest_iteration": 1, "iteration_digest_sha256": "` + zeroDigest + `",
			"semantic_state_digest_sha256": "` + zeroDigest + `", "status": "converged", "closure_verdict": "closed"
		},
		"request_receipt": {
			"digest_sha256": "` + zeroDigest + `",
			"scope": {"files": [], "symbols": [], "components": [], "claim_ids": [], "proposition_keys": []},
			"mode": "modify", "task_class": "test"
		},
		"inspection_capability": "admitted",
		"mutation_capability": "admitted",
		"envelope": {
			"read_paths": [], "modify_paths": [], "symbols": [], "components": [],
			"claim_ids": [], "proposition_keys": [], "unsupported_operations": []
		},
		"reasons": [], "limitations": [], "scope_only": true, "correctness_certified": false,
		"verification": ` + verification + `
	}`
}
