// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func TestExtract(t *testing.T) {
	res, err := Extract(deterministicFixture(t), Options{CapturedAt: "2026-07-21T14:00:00Z"})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	// 1. Verify facts are extracted
	if len(res.Facts) == 0 {
		t.Errorf("Expected at least some facts to be extracted, got 0")
	}

	// Verify we have all required categories
	kinds := make(map[string]bool)
	for _, f := range res.Facts {
		kinds[f.Kind] = true
	}

	t.Logf("Extracted fact kinds: %v", kinds)

	// The fixture must exercise representative HOW behavior, not merely return.
	if !kinds["topology"] {
		t.Errorf("Expected topology facts to be extracted")
	}
	if !kinds["contract_seam"] {
		t.Errorf("Expected contract_seam facts to be extracted")
	}
	if !kinds["boundary"] {
		t.Errorf("Expected boundary facts to be extracted")
	}
	if !kinds["test_protection"] {
		t.Errorf("Expected test_protection facts to be extracted")
	}
	if !kinds["read"] {
		t.Errorf("Expected state read facts to be extracted")
	}

	// 2. Verify evidence receipts are generated correctly
	if len(res.RawEvidence) == 0 {
		t.Fatalf("Expected at least some raw evidence receipts, got 0")
	}

	for _, rec := range res.RawEvidence {
		if !strings.HasPrefix(rec.ID, "evidence_") {
			t.Errorf("Receipt ID must start with evidence_ prefix, got %q", rec.ID)
		}
		if rec.ProofStrength != investigation.ProofStaticSource {
			t.Errorf("Receipt proof strength must be P1_static_source_citation, got %q", rec.ProofStrength)
		}
		if _, err := time.Parse(time.RFC3339, rec.CapturedAt); err != nil {
			t.Errorf("Receipt captured_at must be RFC3339 formatted, got %q", rec.CapturedAt)
		}
		if rec.SourceIdentity == "" {
			t.Errorf("Receipt source identity must be non-empty")
		}
		if rec.ContentDigestSHA256 == "" {
			t.Errorf("Receipt content digest must be non-empty")
		}
		if rec.CapturedContent != "" {
			computed := sha256Hex(rec.CapturedContent)
			if computed != rec.ContentDigestSHA256 {
				t.Errorf("Receipt content digest mismatch for %s: computed %s, receipt has %s", rec.ID, computed, rec.ContentDigestSHA256)
			}
		}
	}

	// 3. Verify coverage entries
	if len(res.Coverage) != 2 {
		t.Errorf("Expected exactly 2 coverage entries, got %d", len(res.Coverage))
	}

	providers := make(map[string]bool)
	for _, cov := range res.Coverage {
		providers[cov.ProviderID] = true
		if cov.Status != investigation.CoverageSupporting {
			t.Errorf("Expected coverage status to be searched_supporting, got %q", cov.Status)
		}
		if len(cov.ResultEvidenceIDs) == 0 {
			t.Errorf("Expected non-empty result evidence IDs in coverage entry for %s", cov.ProviderID)
		}
	}

	if !providers["go_semantic_extractor"] {
		t.Errorf("Missing go_semantic_extractor coverage entry")
	}
	if !providers["go_ast_extractor"] {
		t.Errorf("Missing go_ast_extractor coverage entry")
	}
}

func TestExtractRequiresExplicitCaptureBinding(t *testing.T) {
	if _, err := Extract(t.TempDir(), Options{}); err == nil {
		t.Fatal("expected missing capture binding to fail")
	}
}

func TestExtractWithSameBindingIsDeterministic(t *testing.T) {
	root := deterministicFixture(t)
	opts := Options{CapturedAt: "2026-07-21T14:00:00Z"}
	first, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Extract(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("same tree and capture binding produced different HOW output")
	}
}

func TestCaptureBindingOnlyChangesReceiptTimestamp(t *testing.T) {
	root := deterministicFixture(t)
	a, err := Extract(root, Options{CapturedAt: "2026-07-21T14:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Extract(root, Options{CapturedAt: "2026-07-21T15:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range a.RawEvidence {
		if a.RawEvidence[i].CapturedAt != "2026-07-21T14:00:00Z" || b.RawEvidence[i].CapturedAt != "2026-07-21T15:00:00Z" {
			t.Fatal("unexpected captured_at")
		}
	}
	ac, bc := a, b
	for i := range ac.RawEvidence {
		ac.RawEvidence[i].CapturedAt = ""
		bc.RawEvidence[i].CapturedAt = ""
	}
	if !reflect.DeepEqual(ac, bc) {
		t.Fatal("capture binding changed more than captured_at")
	}
}

func deterministicFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/deterministic_repo")); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"remote", "add", "origin", "https://example.com/deterministic.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

func TestReadCapturedLines(t *testing.T) {
	// Create a temp file
	tmpFile, err := os.CreateTemp("", "test_read_lines_*.go")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := "line1\nline2\nline3\n"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// Read lines 1 to 2
	text, err := readCapturedLines(tmpFile.Name(), 1, 2)
	if err != nil {
		t.Fatalf("readCapturedLines failed: %v", err)
	}

	expected := "line1\nline2\n"
	if text != expected {
		t.Errorf("Expected %q, got %q", expected, text)
	}
}

func TestReadCapturedLinesPreservesBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.go")
	want := "first\r\nsecond\r\nthird"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readCapturedLines(path, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "first\r\nsecond\r\n" {
		t.Fatalf("captured bytes changed: %q", got)
	}
}

func TestDeduplicateReceiptsRefusesConflictingID(t *testing.T) {
	base := investigation.EvidenceReceipt{ID: "evidence_same", Scope: architecture.ClaimScope{Repository: "example.test"}}
	other := base
	other.CapturedContent = "different"
	if _, err := deduplicateReceipts([]investigation.EvidenceReceipt{base, other}); err == nil {
		t.Fatal("expected conflicting evidence IDs to fail")
	}
}

func TestDeterministicRepoDataShapes(t *testing.T) {
	root := deterministicFixture(t)
	res, err := Extract(root, Options{CapturedAt: "2026-07-21T14:00:00Z"})
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}
	t.Logf("Extracted %d facts:", len(res.Facts))
	for _, f := range res.Facts {
		t.Logf("Fact: Kind=%s Subject=%s Predicate=%s Object=%s Source=%s:%d", f.Kind, f.Subject, f.Predicate, f.Object, f.Evidence.SourceFile, f.Evidence.LineStart)
	}
	t.Logf("Extracted %d limitations:", len(res.Limitations))
	for _, lim := range res.Limitations {
		t.Logf("Limitation: Scope=%s Reason=%s", lim.Scope, lim.Reason)
	}
	findFact := func(kind, subject, predicate, object string) *architecture.Fact {
		for _, f := range res.Facts {
			if f.Kind == kind && f.Subject == subject && f.Predicate == predicate && f.Object == object {
				return &f
			}
		}
		return nil
	}

	// 1. At least one data_shape fact is emitted
	foundDataShape := false
	for _, f := range res.Facts {
		if f.Kind == "data_shape" {
			foundDataShape = true
			break
		}
	}
	if !foundDataShape {
		t.Errorf("Expected at least one data_shape fact to be emitted")
	}

	// 2. A cross-package request or response shape is detected
	reqDecl := findFact("data_shape", "api.Request", "declares_data_shape", "struct")
	if reqDecl == nil {
		t.Errorf("Expected api.Request declares_data_shape observation to be found")
	}

	// 3. Explicit serialized field names are preserved (and tag type json)
	reqField := findFact("data_shape", "api.Request.UserID", "has_serialized_field", "user_id")
	if reqField == nil {
		t.Errorf("Expected api.Request.UserID has_serialized_field user_id observation to be found")
	} else {
		if reqField.Meta["tag"] != "json" {
			t.Errorf("Expected tag metadata to be 'json', got %q", reqField.Meta["tag"])
		}
		// 4. Field type represented deterministically
		if !strings.Contains(reqField.Meta["field_type"], "string") {
			t.Errorf("Expected field_type metadata to contain 'string', got %q", reqField.Meta["field_type"])
		}
		// 5. Evidence points to exact declaring source
		if reqField.Evidence.SourceFile != "api/api.go" {
			t.Errorf("Expected source file to be api/api.go, got %q", reqField.Evidence.SourceFile)
		}
		if reqField.Evidence.LineStart <= 0 {
			t.Errorf("Expected line start to be positive, got %d", reqField.Evidence.LineStart)
		}
	}

	// Block 1: Verify untagged fields are NOT reported as has_serialized_field but as has_field, and no serialized name guessed
	untaggedField := findFact("data_shape", "impl.Memory.values", "has_field", "values")
	if untaggedField == nil {
		t.Errorf("Expected untagged field impl.Memory.values to have has_field fact")
	}
	badUntaggedField := findFact("data_shape", "impl.Memory.values", "has_serialized_field", "values")
	if badUntaggedField != nil {
		t.Errorf("Falsely guessed serialized name 'values' as serialized field: %+v", badUntaggedField)
	}

	// Block 2: Verify uses_data_shape_across_boundary evidence cites the crossing location, not the declaration
	boundaryUseFact := findFact("data_shape", "api.Request", "uses_data_shape_across_boundary", "impl.Client.Call")
	if boundaryUseFact == nil {
		t.Errorf("Expected uses_data_shape_across_boundary fact to be present for Client.Call")
	} else {
		if boundaryUseFact.Evidence.SourceFile != "impl/impl.go" {
			t.Errorf("Expected boundary crossing evidence file to be impl/impl.go, got %q", boundaryUseFact.Evidence.SourceFile)
		}
		if boundaryUseFact.Evidence.LineStart != 21 {
			t.Errorf("Expected boundary crossing evidence line to be 21, got %d", boundaryUseFact.Evidence.LineStart)
		}
	}

	// 6. A private local-only struct with no serialization or boundary use is not falsely reported as a boundary-crossing shape
	for _, f := range res.Facts {
		if f.Kind == "data_shape" && (strings.Contains(f.Subject, "privateStruct") || strings.Contains(f.Object, "privateStruct")) {
			t.Errorf("Falsely reported privateStruct in data_shape facts: %+v", f)
		}
	}

	// 7. Two runs over same fixture and capture binding produce identical results
	res2, err := Extract(root, Options{CapturedAt: "2026-07-21T14:00:00Z"})
	if err != nil {
		t.Fatalf("Extract run 2 failed: %v", err)
	}
	if !reflect.DeepEqual(res, res2) {
		t.Errorf("Identical runs did not produce deep equal results")
	}

	// 8. Data-shape facts do not claim ownership, intended authority, or historical WHY
	forbiddenSubstrings := []string{"authoritative", "intended", "owner", "why", "intent"}
	for _, f := range res.Facts {
		if f.Kind == "data_shape" {
			summary := strings.ToLower(f.Subject + " " + f.Predicate + " " + f.Object)
			for _, term := range forbiddenSubstrings {
				if strings.Contains(summary, term) {
					t.Errorf("Fact contains architectural intent word %q: %+v", term, f)
				}
			}
		}
	}

	// 9. Evidence capture failure creates a limitation rather than synthetic content
	_, badErr := readCapturedLines("non_existent_file.go", 1, 2)
	if badErr == nil {
		t.Errorf("Expected error reading non-existent file, got nil")
	}

	// 10. Existing topology, boundary, contract-seam, state-read, and test-protection fixture assertions continue to pass
	topologyFact := false
	boundaryFact := false
	contractFact := false
	stateFact := false
	testFact := false

	for _, f := range res.Facts {
		switch f.Kind {
		case "topology":
			if f.Predicate == "defines_symbol" {
				topologyFact = true
			}
		case "boundary":
			if f.Predicate == "crosses_package_boundary_to" {
				boundaryFact = true
			}
		case "contract_seam":
			if f.Predicate == "exports_interface" {
				contractFact = true
			}
		case "read":
			if f.Predicate == "reads" && f.Object == "values" {
				stateFact = true
			}
		case "test_protection":
			if f.Predicate == "test_calls_symbol" {
				testFact = true
			}
		}
	}

	if !topologyFact {
		t.Errorf("Expected topology defines_symbol fact to be present")
	}
	if !boundaryFact {
		t.Errorf("Expected boundary crosses_package_boundary_to fact to be present")
	}
	if !contractFact {
		t.Errorf("Expected contract_seam exports_interface fact to be present")
	}
	if !stateFact {
		t.Errorf("Expected state reads fact to be present")
	}
	if !testFact {
		t.Errorf("Expected test_protection test_calls_symbol fact to be present")
	}
}

func TestCaptureFailurePipelineProof(t *testing.T) {
	validFact := architecture.Fact{
		ID:        "fact.valid",
		Kind:      "topology",
		Subject:   "api.Request",
		Predicate: "declares_data_shape",
		Object:    "struct",
		Extractor: "data_shape_extractor",
		Evidence: architecture.Evidence{
			SourceFile: "api/api.go",
			LineStart:  7,
			LineEnd:    7,
		},
	}
	badFact := architecture.Fact{
		ID:        "fact.bad",
		Kind:      "topology",
		Subject:   "api.NonExistent",
		Predicate: "declares_data_shape",
		Object:    "struct",
		Extractor: "data_shape_extractor",
		Evidence: architecture.Evidence{
			SourceFile: "api/missing.go",
			LineStart:  1,
			LineEnd:    1,
		},
	}

	root := deterministicFixture(t)
	res, err := composeReceiptsAndCoverage(root, []architecture.Fact{validFact, badFact}, "example.test", Options{CapturedAt: "2026-07-21T14:00:00Z"}, nil)
	if err != nil {
		t.Fatalf("composeReceiptsAndCoverage failed: %v", err)
	}

	// 1. Prove that a limitation is created for the bad file
	foundLimitation := false
	for _, lim := range res.Limitations {
		if lim.Scope == "api/missing.go" && (strings.Contains(lim.Reason, "source capture unavailable") || strings.Contains(lim.Reason, "source digest unavailable")) {
			foundLimitation = true
			break
		}
	}
	if !foundLimitation {
		t.Errorf("Expected limitation to be generated for the missing file, got limitations: %+v", res.Limitations)
	}

	// 2. Prove that NO evidence receipt is emitted for the bad fact (emits no fabricated evidence)
	badReceiptID := "evidence_" + sha256Hex(badFact.ID)[:16]
	for _, rec := range res.RawEvidence {
		if rec.ID == badReceiptID {
			t.Errorf("Emitted fabricated evidence receipt for missing file: %+v", rec)
		}
	}

	// 3. Prove that the valid fact's evidence receipt is preserved
	validReceiptID := "evidence_" + sha256Hex(validFact.ID)[:16]
	foundValid := false
	for _, rec := range res.RawEvidence {
		if rec.ID == validReceiptID {
			foundValid = true
			if rec.CapturedContent == "" {
				t.Errorf("Expected captured content for valid fact to be populated")
			}
		}
	}
	if !foundValid {
		t.Errorf("Expected valid fact's evidence receipt to be preserved, but it was missing")
	}
}
