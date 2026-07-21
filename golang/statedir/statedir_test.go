// SPDX-License-Identifier: AGPL-3.0-only

package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestName_FreshRepoDefaultsToSensei(t *testing.T) {
	root := t.TempDir()
	if got := Name(root); got != DefaultName {
		t.Fatalf("fresh repo: Name = %q, want %q", got, DefaultName)
	}
}

func TestName_LegacyAwgIsHonored(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, LegacyName), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Name(root); got != LegacyName {
		t.Fatalf("legacy repo: Name = %q, want %q (no split-brain)", got, LegacyName)
	}
}

func TestName_SenseiWinsOverLegacy(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{DefaultName, LegacyName} {
		if err := os.Mkdir(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if got := Name(root); got != DefaultName {
		t.Fatalf("both present: Name = %q, want %q", got, DefaultName)
	}
}

func TestPath_JoinsResolvedDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, LegacyName), 0o755); err != nil {
		t.Fatal(err)
	}
	got := Path(root, "governance", "active.json")
	want := filepath.Join(root, LegacyName, "governance", "active.json")
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestPath_EmptyRootIsRelativeDefault(t *testing.T) {
	got := Path("", "graph-authority.json")
	want := filepath.Join(DefaultName, "graph-authority.json")
	if got != want {
		t.Fatalf("Path(\"\") = %q, want %q", got, want)
	}
}
