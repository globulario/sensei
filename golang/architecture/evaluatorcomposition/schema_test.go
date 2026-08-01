// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func schemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "evaluatorcomposition", "v1")
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
	EvaluationPolicySchemaFilename,
	EvaluatorDescriptorSchemaFilename,
	EvaluationInputSchemaFilename,
	EvaluatorResultSchemaFilename,
	EvaluationReceiptSchemaFilename,
}

// TestEmbeddedSchemasMatchCanonicalSource proves the go:embed'd copy under
// schemas/ is byte-identical to the canonical, cross-repo-pinned source
// under docs/schemas/evaluatorcomposition/v1/ -- go:embed cannot reach
// outside this package directory with a ".." pattern, so the canonical
// file is mechanically copied here at commit time, and this test is what
// stops the two from silently drifting apart.
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
				t.Fatalf("golang/architecture/evaluatorcomposition/schemas/%s has drifted from docs/schemas/evaluatorcomposition/v1/%s -- re-copy the canonical file", filename, filename)
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
		t.Fatalf("expected exactly the %d adopted evaluatorcomposition schemas, got %d entries", len(allSchemaFilenames), len(entries))
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
		{EvaluationPolicySchemaFilename, EvaluationPolicySchemaVersion},
		{EvaluatorDescriptorSchemaFilename, EvaluatorDescriptorSchemaVersion},
		{EvaluationInputSchemaFilename, EvaluationInputSchemaVersion},
		{EvaluatorResultSchemaFilename, EvaluatorResultSchemaVersion},
		{EvaluationReceiptSchemaFilename, EvaluationReceiptSchemaVersion},
	}
	for _, c := range cases {
		doc := readSchemaDoc(t, filepath.Join(root, c.filename))
		if got := constString(t, doc, "properties", "schema_version", "const"); got != c.want {
			t.Errorf("%s schema_version const = %q, want %q", c.filename, got, c.want)
		}
	}
}

// TestValidateSchemasAcceptValidFixtures proves every schema accepts its
// own package's valid fixtures, for every one of the six closed
// dispositions (both evaluated variants, plus a cleanup-failed variant for
// every disposition where cleanup applies), under the real Draft 2020-12
// validator.
func TestValidateSchemasAcceptValidFixtures(t *testing.T) {
	policy := fixtureEvaluationPolicy(t)
	if data, err := json.Marshal(policy); err != nil {
		t.Fatal(err)
	} else if err := ValidateEvaluationPolicySchema(data); err != nil {
		t.Errorf("valid EvaluationPolicy fixture rejected: %v", err)
	}

	descriptor := fixtureEvaluatorDescriptor(t)
	if data, err := json.Marshal(descriptor); err != nil {
		t.Fatal(err)
	} else if err := ValidateEvaluatorDescriptorSchema(data); err != nil {
		t.Errorf("valid EvaluatorDescriptor fixture rejected: %v", err)
	}

	input := fixtureEvaluationInput(t)
	if data, err := json.Marshal(input); err != nil {
		t.Fatal(err)
	} else if err := ValidateEvaluationInputSchema(data); err != nil {
		t.Errorf("valid EvaluationInput fixture rejected: %v", err)
	}

	result := fixtureEvaluatorResult(t)
	if data, err := json.Marshal(result); err != nil {
		t.Fatal(err)
	} else if err := ValidateEvaluatorResultSchema(data); err != nil {
		t.Errorf("valid EvaluatorResult fixture rejected: %v", err)
	}

	for _, d := range AllDispositions() {
		for _, terminal := range []bool{true, false} {
			if d != DispositionEvaluated && !terminal {
				continue // evaluatedTerminal only varies the DispositionEvaluated fixture
			}
			r := fixtureEvaluationReceipt(t, d, terminal)
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateEvaluationReceiptSchema(data); err != nil {
				t.Errorf("valid EvaluationReceipt fixture (disposition %q, evaluatedTerminal=%v) rejected: %v", d, terminal, err)
			}

			if d == DispositionInvalidOutputTerminated || d == DispositionCandidateLoadFailure {
				continue // cleanup not applicable -- no cleanup-failed variant
			}
			cleanupFailed := fixtureEvaluationReceiptCleanupFailed(t, d, terminal)
			data, err = json.Marshal(cleanupFailed)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateEvaluationReceiptSchema(data); err != nil {
				t.Errorf("valid cleanup-failed EvaluationReceipt fixture (disposition %q, evaluatedTerminal=%v) rejected: %v", d, terminal, err)
			}
		}
	}
}

// receiptDigestField names one of EvaluationReceipt's disposition-conditional
// fields, with get/set accessors -- used to exhaustively flip each field's
// value against every disposition.
type receiptDigestField struct {
	name string
	set  func(*EvaluationReceipt)
}

// TestEvaluationReceiptSchemaEnforcesDispositionMatrixExactly is the
// table-first presence-matrix proof: for every one of the six closed
// dispositions, flipping CandidateArtifactVerified, forcing a non-empty
// EvaluatorResultDigestsSHA256 where it must be empty, or flipping
// EvaluationDigestSHA256's nil-ness away from what FieldPresenceFor(d)
// requires -- each independently, leaving every other field and the
// disposition itself untouched -- must be rejected.
func TestEvaluationReceiptSchemaEnforcesDispositionMatrixExactly(t *testing.T) {
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			base := fixtureEvaluationReceipt(t, d, true)
			if data, err := json.Marshal(base); err != nil {
				t.Fatal(err)
			} else if err := ValidateEvaluationReceiptSchema(data); err != nil {
				t.Fatalf("valid disposition %q fixture rejected: %v", d, err)
			}

			presence, err := FieldPresenceFor(d)
			if err != nil {
				t.Fatal(err)
			}

			var fields []receiptDigestField
			fields = append(fields, receiptDigestField{
				"candidate_artifact_verified",
				func(r *EvaluationReceipt) { r.CandidateArtifactVerified = !presence.CandidateArtifactVerified },
			})
			if presence.EvaluatorResultDigestsMustBeEmpty {
				fields = append(fields, receiptDigestField{
					"evaluator_result_digests_sha256",
					func(r *EvaluationReceipt) { r.EvaluatorResultDigestsSHA256 = []string{zeroDigest} },
				})
			}
			fields = append(fields, receiptDigestField{
				"evaluation_digest_sha256",
				func(r *EvaluationReceipt) {
					if presence.EvaluationDigest {
						r.EvaluationDigestSHA256 = nil
					} else {
						r.EvaluationDigestSHA256 = stringPtr(zeroDigest)
					}
				},
			})

			for _, f := range fields {
				f := f
				t.Run(f.name, func(t *testing.T) {
					mutated := base
					f.set(&mutated)
					// mutated is now schema-invalid for this disposition;
					// recompute nothing -- the schema check must fail
					// before digest recomputation would even matter.
					data, err := json.Marshal(mutated)
					if err != nil {
						t.Fatal(err)
					}
					if err := ValidateEvaluationReceiptSchema(data); err == nil {
						t.Errorf("disposition %q: mutating %s was wrongly accepted", d, f.name)
					}
				})
			}
		})
	}
}

// TestEvaluationReceiptSchemaAllowsBothO1TerminalReceiptVariantsOnlyForEvaluated
// proves O1TerminalReceiptDigestSHA256's ambiguity is scoped exactly to
// DispositionEvaluated: both presence and absence validate for evaluated,
// but every other disposition rejects absence.
func TestEvaluationReceiptSchemaAllowsBothO1TerminalReceiptVariantsOnlyForEvaluated(t *testing.T) {
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			present := fixtureEvaluationReceipt(t, d, true)
			if data, err := json.Marshal(present); err != nil {
				t.Fatal(err)
			} else if err := ValidateEvaluationReceiptSchema(data); err != nil {
				t.Fatalf("disposition %q: o1_terminal_receipt_digest_sha256-present fixture rejected: %v", d, err)
			}

			absent := present
			absent.O1TerminalReceiptDigestSHA256 = nil
			absent = finishEvaluationReceipt(t, absent)
			data, err := json.Marshal(absent)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateEvaluationReceiptSchema(data)
			if d == DispositionEvaluated {
				if err != nil {
					t.Errorf("disposition %q: o1_terminal_receipt_digest_sha256-absent must be valid (ambiguous), got %v", d, err)
				}
			} else if err == nil {
				t.Errorf("disposition %q: o1_terminal_receipt_digest_sha256-absent was wrongly accepted -- required for this disposition", d)
			}
		})
	}
}
