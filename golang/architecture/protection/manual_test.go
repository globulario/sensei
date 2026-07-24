// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeFile(root, rel string) error {
	return os.Remove(filepath.Join(root, filepath.FromSlash(rel)))
}

func TestManualEntries_AbsentFileIsNotAnError(t *testing.T) {
	root := t.TempDir()
	entries, present, err := ManualEntries(root)
	if err != nil {
		t.Fatalf("absent manual registry must not error: %v", err)
	}
	if present {
		t.Fatal("absent manual registry must report present=false")
	}
	if entries != nil {
		t.Fatalf("absent manual registry must yield no entries, got %v", entries)
	}
}

func TestManualEntries_EmptyListIsPresentButEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ManualRegistryFile, "files: []\n")
	entries, present, err := ManualEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("an existing (if empty) manual registry must report present=true")
	}
	if len(entries) != 0 {
		t.Fatalf("expected zero entries, got %v", entries)
	}
}

func TestManualEntries_MalformedYAMLIsTypedError(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ManualRegistryFile, "files: [this is not: valid: yaml:::\n")
	_, present, err := ManualEntries(root)
	if err == nil {
		t.Fatal("malformed manual registry must return a typed error, not silently-empty")
	}
	if !present {
		t.Fatal("a malformed-but-existing file must still report present=true")
	}
}

func TestManualEntries_DropsEscapingEntriesButKeepsGoodOnes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ManualRegistryFile, "files:\n  - ../outside/\n  - src/auth/\n")
	entries, _, err := ManualEntries(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0] != "src/auth" {
		t.Fatalf("expected only the safe entry to survive, got %v", entries)
	}
}
