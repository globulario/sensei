// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func schemaRoot(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "docs", "schemas", "runnercomposition", "v1")
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
	CandidateArtifactSchemaFilename,
	RunnerReceiptSchemaFilename,
}

// TestEmbeddedSchemasMatchCanonicalSource proves the go:embed'd copy under
// schemas/ is byte-identical to the canonical, cross-repo-pinned source
// under docs/schemas/runnercomposition/v1/ -- go:embed cannot reach outside
// this package directory with a ".." pattern, so the canonical file is
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
				t.Fatalf("golang/architecture/runnercomposition/schemas/%s has drifted from docs/schemas/runnercomposition/v1/%s -- re-copy the canonical file", filename, filename)
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
		t.Fatalf("expected exactly the %d adopted runnercomposition schemas, got %d entries", len(allSchemaFilenames), len(entries))
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
		{CandidateArtifactSchemaFilename, CandidateArtifactSchemaVersion},
		{RunnerReceiptSchemaFilename, RunnerReceiptSchemaVersion},
	}
	for _, c := range cases {
		doc := readSchemaDoc(t, filepath.Join(root, c.filename))
		if got := constString(t, doc, "properties", "schema_version", "const"); got != c.want {
			t.Errorf("%s schema_version const = %q, want %q", c.filename, got, c.want)
		}
	}
}

// TestValidateSchemasAcceptValidFixtures proves both schemas accept their
// own package's valid fixtures, for every one of the eight closed
// dispositions plus a cleanup-failed variant, under the real Draft 2020-12
// validator.
func TestValidateSchemasAcceptValidFixtures(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCandidateArtifactSchema(data); err != nil {
		t.Errorf("valid CandidateArtifact fixture rejected: %v", err)
	}

	for _, d := range AllDispositions() {
		r := fixtureRunnerReceipt(t, d, artifact)
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateRunnerReceiptSchema(data); err != nil {
			t.Errorf("valid RunnerReceipt fixture (disposition %q) rejected: %v", d, err)
		}

		if d == DispositionSnapshotFailure {
			continue
		}
		cleanupFailed := fixtureRunnerReceiptCleanupFailed(t, d, artifact)
		data, err = json.Marshal(cleanupFailed)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateRunnerReceiptSchema(data); err != nil {
			t.Errorf("valid cleanup-failed RunnerReceipt fixture (disposition %q) rejected: %v", d, err)
		}
	}
}

// receiptDigestField names one of RunnerReceipt's six nullable digest
// fields, with get/set accessors -- used to exhaustively flip each field's
// presence against every disposition.
type receiptDigestField struct {
	name string
	get  func(RunnerReceipt) *string
	set  func(*RunnerReceipt, *string)
}

var receiptDigestFields = []receiptDigestField{
	{"result_digest_sha256", func(r RunnerReceipt) *string { return r.ResultDigestSHA256 }, func(r *RunnerReceipt, v *string) { r.ResultDigestSHA256 = v }},
	{"o2_receipt_digest_sha256", func(r RunnerReceipt) *string { return r.O2ReceiptDigestSHA256 }, func(r *RunnerReceipt, v *string) { r.O2ReceiptDigestSHA256 = v }},
	{"input_candidate_digest_sha256", func(r RunnerReceipt) *string { return r.InputCandidateDigestSHA256 }, func(r *RunnerReceipt, v *string) { r.InputCandidateDigestSHA256 = v }},
	{"proposed_change_digest_sha256", func(r RunnerReceipt) *string { return r.ProposedChangeDigestSHA256 }, func(r *RunnerReceipt, v *string) { r.ProposedChangeDigestSHA256 = v }},
	{"final_candidate_content_digest_sha256", func(r RunnerReceipt) *string { return r.FinalCandidateContentDigestSHA256 }, func(r *RunnerReceipt, v *string) { r.FinalCandidateContentDigestSHA256 = v }},
	{"candidate_artifact_digest_sha256", func(r RunnerReceipt) *string { return r.CandidateArtifactDigestSHA256 }, func(r *RunnerReceipt, v *string) { r.CandidateArtifactDigestSHA256 = v }},
}

// TestRunnerReceiptSchemaEnforcesDispositionMatrixExactly is the
// table-first presence-matrix proof the architect asked for: for every one
// of the eight closed dispositions, and for every one of the six nullable
// digest fields, flipping that single field's presence (nil<->non-nil)
// while leaving every other field and the disposition itself untouched
// must be rejected. This exhaustively covers all 8*6 = 48 combinations, not
// a sampled subset -- including proving that digest-mismatch and
// (the former "cleanup-failure" role now split across) every
// fully-completed disposition require ALL six fields, and that
// seal-failure requires every field except candidate_artifact_digest_sha256.
func TestRunnerReceiptSchemaEnforcesDispositionMatrixExactly(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			base := fixtureRunnerReceipt(t, d, artifact)
			if data, err := json.Marshal(base); err != nil {
				t.Fatal(err)
			} else if err := ValidateRunnerReceiptSchema(data); err != nil {
				t.Fatalf("valid disposition %q fixture rejected: %v", d, err)
			}

			for _, f := range receiptDigestFields {
				f := f
				t.Run(f.name, func(t *testing.T) {
					tampered := base
					if f.get(tampered) == nil {
						f.set(&tampered, stringPtr(zeroDigest))
					} else {
						f.set(&tampered, nil)
					}
					data, err := json.Marshal(tampered)
					if err != nil {
						t.Fatal(err)
					}
					if err := ValidateRunnerReceiptSchema(data); err == nil {
						t.Errorf("flipping %s's nullability for disposition %q was accepted, want rejected", f.name, d)
					}
				})
			}
		})
	}
}

// TestRunnerReceiptSchemaEnforcesFailureDetailPresence proves
// failure_detail's presence rule holds for every disposition, not just
// verified/digest-mismatch.
func TestRunnerReceiptSchemaEnforcesFailureDetailPresence(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			r := fixtureRunnerReceipt(t, d, artifact)
			if d == DispositionVerified {
				r.FailureDetail = "should not be populated when verified"
			} else {
				r.FailureDetail = ""
			}
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRunnerReceiptSchema(data); err == nil {
				t.Errorf("disposition %q with wrong failure_detail presence was accepted", d)
			}
		})
	}
}

// TestRunnerReceiptSchemaEnforcesCleanupSucceededPresence proves
// cleanup_succeeded is null only for snapshot-failure and a boolean for
// every other disposition -- orthogonal to, but still checked per,
// Disposition.
func TestRunnerReceiptSchemaEnforcesCleanupSucceededPresence(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)
	for _, d := range AllDispositions() {
		d := d
		t.Run(string(d), func(t *testing.T) {
			r := fixtureRunnerReceipt(t, d, artifact)
			if d == DispositionSnapshotFailure {
				r.CleanupSucceeded = boolPtr(true)
			} else {
				r.CleanupSucceeded = nil
			}
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateRunnerReceiptSchema(data); err == nil {
				t.Errorf("disposition %q with wrong cleanup_succeeded presence was accepted", d)
			}
		})
	}
}

// TestRunnerReceiptSchemaEnforcesCleanupFailureDetailPresence proves
// cleanup_failure_detail is required non-empty exactly when
// cleanup_succeeded is false.
func TestRunnerReceiptSchemaEnforcesCleanupFailureDetailPresence(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)

	failedWithoutDetail := fixtureRunnerReceiptCleanupFailed(t, DispositionVerified, artifact)
	failedWithoutDetail.CleanupFailureDetail = ""
	if data, err := json.Marshal(failedWithoutDetail); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected cleanup_succeeded=false with empty cleanup_failure_detail to be rejected")
	}

	succeededWithDetail := fixtureRunnerReceipt(t, DispositionVerified, artifact)
	succeededWithDetail.CleanupFailureDetail = "should be empty when cleanup succeeded"
	if data, err := json.Marshal(succeededWithDetail); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected cleanup_succeeded=true with non-empty cleanup_failure_detail to be rejected")
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
