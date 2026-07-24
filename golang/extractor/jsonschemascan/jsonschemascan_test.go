// SPDX-License-Identifier: AGPL-3.0-only

package jsonschemascan

import (
	"os"
	"path/filepath"
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

	got, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "config.json" {
		t.Fatalf("expected exactly config.json, got %v", got)
	}
}

func TestFindSchemaFiles_Recognizes2020_12AndYAML(t *testing.T) {
	root := t.TempDir()
	write(t, root, "schemas/modern.json", `{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "string"}`)
	write(t, root, "schemas/modern.yaml", "$schema: https://json-schema.org/draft/2020-12/schema\ntype: string\n")

	got, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 schema files, got %v", got)
	}
}

func TestFindSchemaFiles_IgnoresUnrelatedJSON(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"name": "example", "version": "1.0.0"}`)
	write(t, root, "data/config.json", `{"host": "localhost", "port": 8080}`)

	got, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
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

	got, err := FindSchemaFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || filepath.Base(got[0]) != "schema.json" || filepath.Base(filepath.Dir(got[0])) != "real" {
		t.Fatalf("expected only real/schema.json, got %v", got)
	}
}
