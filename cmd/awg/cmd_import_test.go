// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveDomain(t *testing.T) {
	cases := map[string]string{
		"https://github.com/gin-gonic/gin":     "github.com/gin-gonic/gin",
		"https://github.com/gin-gonic/gin.git": "github.com/gin-gonic/gin",
		"http://gitlab.com/a/b/c":              "gitlab.com/a/b/c",
		"git@github.com:gin-gonic/gin.git":     "github.com/gin-gonic/gin",
		"github.com/owner/repo":                "github.com/owner/repo",
		"not-a-url":                            "", // no slash → cannot derive
	}
	for in, want := range cases {
		if got := deriveDomain(in); got != want {
			t.Errorf("deriveDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveSlug(t *testing.T) {
	cases := map[string]string{
		"github.com/gin-gonic/gin": "gin-gonic/gin",
		"gitlab.com/a/b/c":         "b/c",
		"owner/repo":               "owner/repo",
		"single":                   "",
	}
	for in, want := range cases {
		if got := deriveSlug(in); got != want {
			t.Errorf("deriveSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDeriveRepoBaseAndSanitize(t *testing.T) {
	if got := deriveRepoBase("https://github.com/gin-gonic/gin.git"); got != "gin" {
		t.Errorf("deriveRepoBase = %q, want gin", got)
	}
	if got := sanitizeName("gin-gonic/gin@x"); got != "gin-gonicginx" {
		t.Errorf("sanitizeName = %q", got)
	}
	if got := sanitizeName("////"); got != "repo" {
		t.Errorf("sanitizeName empty fallback = %q, want repo", got)
	}
}

func TestResolveImportRefreshCheckoutRequiresExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	got, code := resolveImportRefreshCheckout(dir)
	if code != 0 {
		t.Fatalf("resolveImportRefreshCheckout code=%d, want 0", code)
	}
	if got != dir {
		t.Fatalf("checkout=%q, want %q", got, dir)
	}

	_, code = resolveImportRefreshCheckout(filepath.Join(dir, "missing"))
	if code == 0 {
		t.Fatal("missing refresh checkout should fail")
	}
}

func TestResolveImportDomainUsesExplicitTargetThenRemote(t *testing.T) {
	old := gitRemoteDomain
	defer func() { gitRemoteDomain = old }()
	checkout := filepath.Join(t.TempDir(), "owner", "repo")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRemoteDomain = func(path string) string {
		if path == checkout {
			return "github.com/from/remote"
		}
		return ""
	}

	if got := resolveImportDomain("github.com/explicit/repo", "not-a-url", checkout); got != "github.com/explicit/repo" {
		t.Fatalf("explicit domain=%q", got)
	}
	if got := resolveImportDomain("", "https://github.com/from/url.git", checkout); got != "github.com/from/url" {
		t.Fatalf("target-derived domain=%q", got)
	}
	if got := resolveImportDomain("", checkout, checkout); got != "github.com/from/remote" {
		t.Fatalf("remote-derived domain=%q", got)
	}

	gitRemoteDomain = func(path string) string { return "" }
	if got := resolveImportDomain("", checkout, checkout); got != "" {
		t.Fatalf("local path without remote should not become domain, got %q", got)
	}
}
