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
// own package's valid fixtures under the real Draft 2020-12 validator.
func TestValidateSchemasAcceptValidFixtures(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)
	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCandidateArtifactSchema(data); err != nil {
		t.Errorf("valid CandidateArtifact fixture rejected: %v", err)
	}

	for _, r := range []RunnerReceipt{
		fixtureRunnerReceiptVerified(t, artifact),
		fixtureRunnerReceiptDigestMismatch(t, artifact),
		fixtureRunnerReceiptCleanupFailure(t, artifact),
		fixtureRunnerReceiptSnapshotFailure(t),
		fixtureRunnerReceiptSealFailure(t, artifact),
	} {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateRunnerReceiptSchema(data); err != nil {
			t.Errorf("valid RunnerReceipt fixture (disposition %q) rejected: %v", r.Disposition, err)
		}
	}
}

// TestRunnerReceiptSchemaRejectsWrongNullShapePerDisposition proves the
// schema's disposition conditionals are load-bearing: each fixture, with its
// digest-field nullability pattern flipped to what a DIFFERENT disposition
// requires, must be rejected.
func TestRunnerReceiptSchemaRejectsWrongNullShapePerDisposition(t *testing.T) {
	artifact := fixtureCandidateArtifact(t)

	verified := fixtureRunnerReceiptVerified(t, artifact)
	verified.CandidateArtifactDigestSHA256 = nil // seal-failure's shape, not verified's
	if data, err := json.Marshal(verified); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected a verified receipt with a nil candidate_artifact_digest_sha256 to be rejected")
	}

	snapshotFailure := fixtureRunnerReceiptSnapshotFailure(t)
	snapshotFailure.ResultDigestSHA256 = stringPtr(zeroDigest) // verified's shape, not snapshot-failure's
	if data, err := json.Marshal(snapshotFailure); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected a snapshot-failure receipt with a non-nil result_digest_sha256 to be rejected")
	}

	sealFailure := fixtureRunnerReceiptSealFailure(t, artifact)
	sealFailure.CandidateArtifactDigestSHA256 = stringPtr(zeroDigest) // verified's shape, not seal-failure's
	if data, err := json.Marshal(sealFailure); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected a seal-failure receipt with a non-nil candidate_artifact_digest_sha256 to be rejected")
	}

	verifiedWithFailureDetail := fixtureRunnerReceiptVerified(t, artifact)
	verifiedWithFailureDetail.FailureDetail = "should not be populated when verified"
	if data, err := json.Marshal(verifiedWithFailureDetail); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected a verified receipt with a non-empty failure_detail to be rejected")
	}

	mismatchWithoutFailureDetail := fixtureRunnerReceiptDigestMismatch(t, artifact)
	mismatchWithoutFailureDetail.FailureDetail = ""
	if data, err := json.Marshal(mismatchWithoutFailureDetail); err != nil {
		t.Fatal(err)
	} else if err := ValidateRunnerReceiptSchema(data); err == nil {
		t.Error("expected a digest-mismatch receipt with an empty failure_detail to be rejected")
	}
}

const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"
