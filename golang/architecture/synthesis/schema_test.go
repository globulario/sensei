// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func schemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "synthesis", "v1")
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

var allSchemaFilenames = []string{
	SessionSchemaFilename,
	InterpretationSchemaFilename,
	PlanSchemaFilename,
	AttemptSchemaFilename,
	EvaluationSchemaFilename,
	ReceiptSchemaFilename,
}

// TestEmbeddedSchemasMatchCanonicalSource proves the go:embed'd copy under
// schemas/ is byte-identical to the canonical, cross-repo-pinned source
// under docs/schemas/synthesis/v1/ — go:embed cannot reach outside this
// package directory with a ".." pattern, so the canonical file is
// mechanically copied here at commit time, and this test is what stops the
// two from silently drifting apart.
func TestEmbeddedSchemasMatchCanonicalSource(t *testing.T) {
	canonicalRoot := schemaRoot(t)
	for _, filename := range allSchemaFilenames {
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
				t.Fatalf("golang/architecture/synthesis/schemas/%s has drifted from docs/schemas/synthesis/v1/%s — re-copy the canonical file", filename, filename)
			}
		})
	}
}

// TestSchemasParseAndAreClosed proves every vendored schema parses as JSON
// and keeps additionalProperties:false at its object root.
func TestSchemasParseAndAreClosed(t *testing.T) {
	root := schemaRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(allSchemaFilenames) {
		t.Fatalf("expected exactly the %d adopted synthesis schemas, got %d entries", len(allSchemaFilenames), len(entries))
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

// TestSchemaVersionIdentifiersMatchAdopted confirms every vendored schema's
// const schema_version exactly matches this package's Go constant.
func TestSchemaVersionIdentifiersMatchAdopted(t *testing.T) {
	root := schemaRoot(t)
	cases := []struct {
		filename string
		want     string
	}{
		{SessionSchemaFilename, SessionSchemaVersion},
		{InterpretationSchemaFilename, InterpretationSchemaVersion},
		{PlanSchemaFilename, PlanSchemaVersion},
		{AttemptSchemaFilename, AttemptSchemaVersion},
		{EvaluationSchemaFilename, EvaluationSchemaVersion},
		{ReceiptSchemaFilename, ReceiptSchemaVersion},
	}
	for _, c := range cases {
		doc := readSchemaDoc(t, filepath.Join(root, c.filename))
		if got := constString(t, doc, "properties", "schema_version", "const"); got != c.want {
			t.Errorf("%s schema_version const = %q, want %q", c.filename, got, c.want)
		}
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

// TestValidateSessionSchemaCompilesAndEnforcesRealConstraints proves
// ValidateSessionSchema is a real Draft 2020-12 validator, not a stub.
func TestValidateSessionSchemaCompilesAndEnforcesRealConstraints(t *testing.T) {
	missingRequired := []byte(`{"schema_version":"sensei.synthesis.session.v1"}`)
	if err := ValidateSessionSchema(missingRequired); err == nil {
		t.Fatal("expected a missing-required-fields instance to fail schema validation")
	}

	wrongVersion := []byte(`{"schema_version":"not-the-right-version"}`)
	if err := ValidateSessionSchema(wrongVersion); err == nil {
		t.Fatal("expected a wrong schema_version const to fail schema validation")
	}

	valid, err := json.Marshal(fixtureSession(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSessionSchema(valid); err != nil {
		t.Fatalf("expected a minimal valid session to pass schema validation, got: %v", err)
	}

	extraProperty := []byte(`{
		"schema_version": "sensei.synthesis.session.v1",
		"session_id": "s", "generated_by": "sensei synthesis",
		"repository_domain": "github.com/example/repo", "base_revision": "abc123",
		"workspace_identity_digest_sha256": "` + zeroDigest + `",
		"graph_authority_digest_sha256": "` + zeroDigest + `",
		"task_session_digest_sha256": "` + zeroDigest + `",
		"closure_digest_sha256": "` + zeroDigest + `",
		"proof_obligation_digests": [], "objective": "o",
		"retry_budget": 3, "replan_budget": 1, "created_at": "2026-01-01T00:00:00Z",
		"session_digest_sha256": "` + zeroDigest + `",
		"unexpected_extra_field": true
	}`)
	if err := ValidateSessionSchema(extraProperty); err == nil {
		t.Fatal("expected an unexpected extra property to fail schema validation (additionalProperties:false)")
	}

	badRetryBudget := []byte(`{
		"schema_version": "sensei.synthesis.session.v1",
		"session_id": "s", "generated_by": "sensei synthesis",
		"repository_domain": "github.com/example/repo", "base_revision": "abc123",
		"workspace_identity_digest_sha256": "` + zeroDigest + `",
		"graph_authority_digest_sha256": "` + zeroDigest + `",
		"task_session_digest_sha256": "` + zeroDigest + `",
		"closure_digest_sha256": "` + zeroDigest + `",
		"proof_obligation_digests": [], "objective": "o",
		"retry_budget": -1, "replan_budget": 1, "created_at": "2026-01-01T00:00:00Z",
		"session_digest_sha256": "` + zeroDigest + `"
	}`)
	if err := ValidateSessionSchema(badRetryBudget); err == nil {
		t.Fatal("expected a negative retry_budget to fail schema validation")
	}
}

// TestValidateReceiptSchemaEnforcesAdmissionDigestGating proves the
// allOf/if/else rule: admission digests must be null whenever terminal_reason
// is not candidate-ready-for-admission.
func TestValidateReceiptSchemaEnforcesAdmissionDigestGating(t *testing.T) {
	nonAdmissionWithDigest := []byte(`{
		"schema_version": "sensei.synthesis.receipt.v1",
		"receipt_id": "r", "session_digest_sha256": "` + zeroDigest + `",
		"terminal_reason": "retry-budget-exhausted",
		"final_attempt_digest_sha256": null, "final_evaluation_digest_sha256": null,
		"admission_decision_digest_sha256": "` + zeroDigest + `",
		"admission_verification_digest_sha256": null,
		"retry_count": 3, "replan_count": 0, "summary": "s", "limitations": [],
		"completed_at": "2026-01-01T00:00:00Z", "receipt_digest_sha256": "` + zeroDigest + `"
	}`)
	if err := ValidateReceiptSchema(nonAdmissionWithDigest); err == nil {
		t.Fatal("expected a non-admission terminal_reason with a non-null admission_decision_digest_sha256 to fail schema validation")
	}

	valid, err := json.Marshal(fixtureReceipt(t, ReasonRetryBudgetExhausted))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptSchema(valid); err != nil {
		t.Fatalf("expected a valid non-admission receipt to pass schema validation, got: %v", err)
	}

	admissionReady, err := json.Marshal(fixtureReceiptCandidateReady(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptSchema(admissionReady); err != nil {
		t.Fatalf("expected a valid candidate-ready-for-admission receipt with non-null admission digests to pass schema validation, got: %v", err)
	}
}
