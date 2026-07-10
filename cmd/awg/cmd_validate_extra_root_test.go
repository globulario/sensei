// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTestRefFile_ParsesPathStyle(t *testing.T) {
	if f, ok := testRefFile("internal/gateway/handlers/config/save_config_test.go:TestSaveConfig_RequiresToken"); !ok || f != "internal/gateway/handlers/config/save_config_test.go" {
		t.Errorf("testRefFile = (%q,%v), want the .go path", f, ok)
	}
	if _, ok := testRefFile("TestBareName"); ok {
		t.Error("a bare test id must not parse as a path-style ref")
	}
}

func TestParseExtraRoot_BareAndNamed(t *testing.T) {
	if got := parseExtraRoot("../Globular"); got != "../Globular" {
		t.Errorf("bare path = %q", got)
	}
	if got := parseExtraRoot("name=globular,path=../Globular"); got != "../Globular" {
		t.Errorf("named form = %q, want ../Globular", got)
	}
}

func TestSiblingRepo_FindsSibling(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "awareness-graph")
	sib := filepath.Join(parent, "Globular")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := siblingRepo(root, "Globular"); got != "" {
		t.Errorf("no sibling yet, got %q", got)
	}
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := siblingRepo(root, "Globular"); got != sib {
		t.Errorf("siblingRepo = %q, want %q", got, sib)
	}
}

// TestCrossRefExists_TestRefResolvesByFileInExtraRoot is the core: a path-style
// test ref (cross-repo gateway test) resolves when the file exists under a
// supplied source root, even though it is not a declared required_test node.
func TestCrossRefExists_TestRefResolvesByFileInExtraRoot(t *testing.T) {
	extra := t.TempDir()
	testRel := "internal/gateway/handlers/config/save_config_test.go"
	if err := os.MkdirAll(filepath.Join(extra, filepath.Dir(testRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, testRel), []byte("package config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := newValIDIndex() // empty — the ref is NOT a declared node
	ref := testRel + ":TestSaveConfig_RequiresToken"

	// without the extra root → unresolved (dangling)
	if crossRefExists(idx, "required_test", "tests", ref, nil) {
		t.Error("test ref must be dangling when no root contains the file")
	}
	// with the extra root → resolved by file existence
	if !crossRefExists(idx, "required_test", "tests", ref, []string{extra}) {
		t.Error("path-style test ref must resolve when the file exists under a source root")
	}
	// a path-style ref whose file does NOT exist stays dangling
	if crossRefExists(idx, "required_test", "tests", "internal/gateway/nope_test.go:TestX", []string{extra}) {
		t.Error("a test ref to a non-existent file must stay dangling")
	}
}
