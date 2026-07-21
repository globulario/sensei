// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

func TestExtract(t *testing.T) {
	// We run Extract on the local directory
	root, err := filepath.Abs("../../../")
	if err != nil {
		t.Fatalf("Failed to resolve root path: %v", err)
	}

	res, err := ExtractWithOptions(root, Options{CapturedAt: "2026-07-21T14:00:00Z"})
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

	// Since we are running on the entire repository, we should find topology, contract_seam, test_protection
	if !kinds["topology"] {
		t.Errorf("Expected topology facts to be extracted")
	}
	if !kinds["contract_seam"] {
		t.Errorf("Expected contract_seam facts to be extracted")
	}
	if !kinds["test_protection"] {
		t.Errorf("Expected test_protection facts to be extracted")
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
	if _, err := Extract(t.TempDir()); err == nil {
		t.Fatal("expected missing capture binding to fail")
	}
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
