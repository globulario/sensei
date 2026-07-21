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
