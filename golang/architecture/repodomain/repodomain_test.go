// SPDX-License-Identifier: AGPL-3.0-only

package repodomain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	full := filepath.Join(root, ".sensei", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigured_ReturnsConfiguredDomain(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "repository:\n  domain: github.com/globulario/sensei\n")
	got, err := Configured(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "github.com/globulario/sensei" {
		t.Fatalf("Configured = %q, want github.com/globulario/sensei", got)
	}
}

func TestConfigured_UnboundWhenNoConfig(t *testing.T) {
	root := t.TempDir()
	got, err := Configured(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("Configured = %q, want empty (unbound)", got)
	}
}

func TestConfigured_UnboundWhenConfigHasNoRepositorySection(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "other:\n  key: value\n")
	got, err := Configured(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("Configured = %q, want empty (unbound)", got)
	}
}

func TestConfigured_ErrorsOnMalformedYAML(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "repository: [unterminated\n")
	if _, err := Configured(root); err == nil {
		t.Fatal("expected an error for malformed config.yaml, got nil")
	}
}

func TestConfigured_ErrorsOnInvalidConfiguredDomain(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "repository:\n  domain: not-a-valid-domain\n")
	if _, err := Configured(root); err == nil {
		t.Fatal("expected an error for an invalid configured domain, got nil")
	}
}

func TestValidate_AcceptsCanonicalShape(t *testing.T) {
	if err := Validate("github.com/globulario/sensei"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_RejectsNonCanonicalForms(t *testing.T) {
	cases := []string{
		"",
		"GitHub.com/globulario/sensei",
		"github.com/globulario/sensei/",
		"github.com/globulario/sensei.git",
		"github.com//sensei",
		"github.com/../sensei",
		"https://github.com/globulario/sensei",
		"localhost/sensei",
		"github.com/globulario/sensei?x=1",
		`github.com\globulario\sensei`,
	}
	for _, c := range cases {
		if err := Validate(c); err == nil {
			t.Errorf("Validate(%q): expected error, got nil", c)
		}
	}
}

func TestConfigPath_UsesStatedir(t *testing.T) {
	root := t.TempDir()
	got := ConfigPath(root)
	want := filepath.Join(root, ".sensei", "config.yaml")
	if got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
}

// --- committed repository-identity declaration (issue #197) ---

func writeDeclaration(t *testing.T, root, domain string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DeclarationPath(root), []byte("repository:\n  domain: "+domain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLocalConfig(t *testing.T, root, domain string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sensei"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(root), []byte("repository:\n  domain: "+domain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The reason the declaration is committed at all: .sensei/ is gitignored, so
// a fresh clone carries none of it. An identity that resolved only from
// local state would make the same file mint a different subject depending on
// where it was checked out -- exactly what #197 requires must not happen.
func TestIdentityResolvesInACheckoutWithNoLocalState(t *testing.T) {
	root := t.TempDir()
	writeDeclaration(t, root, "github.com/globulario/sensei")

	got, err := IdentityForTree(filepath.Join(root, "docs", "awareness"))
	if err != nil {
		t.Fatalf("IdentityForTree: %v", err)
	}
	if got != "github.com/globulario/sensei" {
		t.Fatalf("identity = %q", got)
	}
}

// A checkout that was initialized but has not committed its declaration yet
// still resolves, from the local configuration.
func TestLocalConfigurationResolvesWhenNothingIsDeclared(t *testing.T) {
	root := t.TempDir()
	writeLocalConfig(t, root, "github.com/globulario/sensei")
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := IdentityForTree(filepath.Join(root, "docs", "awareness"))
	if err != nil {
		t.Fatalf("IdentityForTree: %v", err)
	}
	if got != "github.com/globulario/sensei" {
		t.Fatalf("identity = %q", got)
	}
}

// Two signals naming different repositories are never resolved silently:
// afterwards the wrong one is indistinguishable from the right one.
func TestDisagreeingDeclarationsAreRefused(t *testing.T) {
	root := t.TempDir()
	writeDeclaration(t, root, "github.com/globulario/sensei")
	writeLocalConfig(t, root, "github.com/globulario/sensei-code")

	_, err := IdentityForTree(filepath.Join(root, "docs", "awareness"))
	if err == nil {
		t.Fatal("two disagreeing repository identities were resolved silently")
	}
	for _, want := range []string{"github.com/globulario/sensei", "github.com/globulario/sensei-code", "disagree"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q: %v", want, err)
		}
	}
}

func TestAgreeingDeclarationsResolve(t *testing.T) {
	root := t.TempDir()
	writeDeclaration(t, root, "github.com/globulario/sensei")
	writeLocalConfig(t, root, "github.com/globulario/sensei")

	got, err := IdentityForTree(filepath.Join(root, "docs", "awareness"))
	if err != nil {
		t.Fatalf("IdentityForTree: %v", err)
	}
	if got != "github.com/globulario/sensei" {
		t.Fatalf("identity = %q", got)
	}
}

// Identity is a property of the repository, not of where it sits on disk:
// the same declaration resolves the same way from two different checkout
// paths, and from any directory inside the tree.
func TestIdentityIsIndependentOfCheckoutPath(t *testing.T) {
	var got []string
	for _, name := range []string{"checkout-a", "somewhere/else/checkout-b"} {
		root := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(filepath.Join(root, "docs", "awareness", "generated"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeDeclaration(t, root, "github.com/globulario/sensei")
		for _, from := range []string{root, filepath.Join(root, "docs", "awareness"), filepath.Join(root, "docs", "awareness", "generated")} {
			id, err := IdentityForTree(from)
			if err != nil {
				t.Fatalf("IdentityForTree(%s): %v", from, err)
			}
			got = append(got, id)
		}
	}
	for _, id := range got {
		if id != "github.com/globulario/sensei" {
			t.Fatalf("identity varied with checkout path: %v", got)
		}
	}
}

func TestUnresolvedIdentityIsEmptyNotAGuess(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := IdentityForTree(filepath.Join(root, "docs", "awareness"))
	if err != nil {
		t.Fatalf("IdentityForTree: %v", err)
	}
	if got != "" {
		t.Fatalf("a tree declaring no identity was given %q", got)
	}
}
