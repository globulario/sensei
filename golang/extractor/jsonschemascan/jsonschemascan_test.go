// SPDX-License-Identifier: AGPL-3.0-only

package jsonschemascan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindSchemaFiles_RecognizesDraft07JSON(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schemas/config.json", `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object"
}`)
	write(t, root, "README.md", "# not a schema\n")

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "config.json" {
		t.Fatalf("expected exactly config.json, got %v", got)
	}
}

func TestFindSchemaFiles_Recognizes2020_12AndYAML(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schemas/modern.json", `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "string"}`)
	write(t, root, "schemas/modern.yaml", "$schema: https://json-schema.org/draft/2020-12/schema\ntype: string\n")

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 schema files, got %v", got)
	}
}

func TestFindSchemaFiles_IgnoresUnrelatedJSON(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name": "example", "version": "1.0.0"}`)
	write(t, root, "data/config.json", `{"host": "localhost", "port": 8080}`)

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	if len(got) != 0 {
		t.Fatalf("expected no schema files among unrelated JSON, got %v", got)
	}
}

func TestFindSchemaFiles_ExcludesGeneratedAndVendorDirs(t *testing.T) {
	root := t.TempDir()
	content := `{"$schema": "http://json-schema.org/draft-07/schema#"}`
	write(t, root, "node_modules/pkg/schema.json", content)
	write(t, root, "docs/awareness/generated/schema.json", content)
	write(t, root, "docs/awareness/candidates/schema.json", content)
	write(t, root, "real/schema.json", content)

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "schema.json" || filepath.Base(filepath.Dir(got[0])) != "real" {
		t.Fatalf("expected only real/schema.json, got %v", got)
	}
}

// contract §4 correction: a NESTED `$schema` key (several levels deep, not
// the document's own top-level declaration) must never be mistaken for a
// real schema file — the prior regex-over-raw-bytes approach matched
// anywhere in the file.
func TestFindSchemaFiles_DoesNotMatchNestedSchemaKey(t *testing.T) {
	root := t.TempDir()
	write(t, root, "data/nested.json", `{
  "type": "object",
  "properties": {
    "config": {
      "$schema": "http://json-schema.org/draft-07/schema#"
    }
  }
}`)

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	if len(got) != 0 {
		t.Fatalf("a nested $schema key must not be mistaken for the document's own top-level declaration, got %v", got)
	}
}

// contract §4 correction: the whole document is evaluated, never truncated
// to a fixed head size — a $schema key appearing after a large leading
// block (much larger than the prior 4KiB sniff window) must still be found.
func TestFindSchemaFiles_FindsSchemaKeyBeyondPriorTruncationWindow(t *testing.T) {
	root := t.TempDir()
	padding := strings.Repeat(`    "filler`+strings.Repeat("x", 40)+`": "value",`+"\n", 400) // well over 4KiB
	write(t, root, "large/config.json", "{\n"+padding+`  "$schema": "http://json-schema.org/draft-07/schema#"`+"\n}")

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed) != 0 {
		t.Fatalf("expected no malformed entries, got %v", malformed)
	}
	if len(got) != 1 {
		t.Fatalf("expected the large schema file to be found beyond any fixed head-truncation window, got %v", got)
	}
}

// contract §4/§6 correction: a file this scan cannot read is an evaluation
// failure, never silently treated as "not a schema."
func TestFindSchemaFiles_UnreadableFileIsMalformedNotSilentlySkipped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schemas/locked.json", `{"$schema": "http://json-schema.org/draft-07/schema#"}`)
	full := filepath.Join(root, "schemas", "locked.json")
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(full, 0o644) })
	if os.Geteuid() == 0 {
		t.Skip("running as root: file permissions are not enforced")
	}

	got, malformed, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("an unreadable file must not be reported as a discovered schema, got %v", got)
	}
	if len(malformed) != 1 {
		t.Fatalf("expected exactly one malformed entry for the unreadable file, got %v", malformed)
	}
}
