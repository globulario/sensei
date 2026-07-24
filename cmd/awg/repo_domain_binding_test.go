// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeDomainTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// initGitRepoWithOrigin creates a real git repo at root with the given
// origin URL configured, so gitRemoteDomain (already exercised by
// git_domain_test.go) resolves it exactly as it would for a real checkout.
func initGitRepoWithOrigin(t *testing.T, root, originURL string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if originURL != "" {
		run("remote", "add", "origin", originURL)
	}
}

// ─── Resolver precedence (contract §3.5) ────────────────────────────────────

func TestResolveRepositoryDomain_ExplicitWins(t *testing.T) {
	root := t.TempDir()
	writeDomainTestFile(t, root, ".sensei/config.yaml", "repository:\n  domain: github.com/configured/repo\n")
	t.Setenv("SENSEI_DOMAIN", "example.com/env/repo")
	t.Setenv("AWG_DOMAIN", "example.com/legacy/repo")

	got := resolveRepositoryDomain(root, "github.com/explicit/repo")
	if got.Domain != "github.com/explicit/repo" || got.Source != domainSourceExplicit {
		t.Fatalf("got %+v, want explicit override", got)
	}
}

func TestResolveRepositoryDomain_ConfiguredWinsOverEnv(t *testing.T) {
	root := t.TempDir()
	writeDomainTestFile(t, root, ".sensei/config.yaml", "repository:\n  domain: github.com/configured/repo\n")
	t.Setenv("SENSEI_DOMAIN", "example.com/env/repo")
	t.Setenv("AWG_DOMAIN", "example.com/legacy/repo")

	got := resolveRepositoryDomain(root, "")
	if got.Domain != "github.com/configured/repo" || got.Source != domainSourceConfigured {
		t.Fatalf("got %+v, want configured domain to win over ambient env", got)
	}
}

func TestResolveRepositoryDomain_SenseiDomainUsedOnlyWhenConfigAbsent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SENSEI_DOMAIN", "example.com/env/repo")
	t.Setenv("AWG_DOMAIN", "example.com/legacy/repo")

	got := resolveRepositoryDomain(root, "")
	if got.Domain != "example.com/env/repo" || got.Source != domainSourceEnvNew {
		t.Fatalf("got %+v, want SENSEI_DOMAIN", got)
	}
}

func TestResolveRepositoryDomain_LegacyAwgDomainUsedOnlyWhenConfigAndSenseiDomainAbsent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AWG_DOMAIN", "example.com/legacy/repo")

	got := resolveRepositoryDomain(root, "")
	if got.Domain != "example.com/legacy/repo" || got.Source != domainSourceEnvLegacy {
		t.Fatalf("got %+v, want legacy AWG_DOMAIN fallback", got)
	}
}

func TestResolveRepositoryDomain_UnresolvedWhenNothingApplies(t *testing.T) {
	root := t.TempDir()
	got := resolveRepositoryDomain(root, "")
	if got.Domain != "" || got.Source != domainSourceUnresolved {
		t.Fatalf("got %+v, want unresolved", got)
	}
}

func TestResolveRepositoryDomain_MalformedConfiguredDomainFailsHonestly(t *testing.T) {
	root := t.TempDir()
	writeDomainTestFile(t, root, ".sensei/config.yaml", "repository: [this is not, a mapping\n")
	t.Setenv("SENSEI_DOMAIN", "example.com/env/repo")

	// A malformed config must not silently crash the resolver into a wrong
	// domain; falling through to the next tier (env) is the honest behavior
	// here since loadRepoDomainConfig's error is swallowed by design (a
	// checkout-scoped read failure must never block every command — but it
	// must also never invent a domain from garbage).
	got := resolveRepositoryDomain(root, "")
	if got.Domain == "github.com/configured/repo" {
		t.Fatal("a malformed config must never produce a guessed domain")
	}
}

// ─── Establishment order (contract §3.3/§3.4) ───────────────────────────────

func TestEstablishRepositoryDomain_GitOriginWhenUnconfigured(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/globulario/example-repo.git")

	res, err := establishRepositoryDomain(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "github.com/globulario/example-repo" || res.Source != "git_origin" {
		t.Fatalf("got %+v, want git-origin-derived domain", res)
	}
	if !res.Written {
		t.Fatal("expected config.yaml to be written for a newly-established domain")
	}

	cfg, err := loadRepoDomainConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repository.Domain != "github.com/globulario/example-repo" {
		t.Fatalf("expected the established domain to persist in config.yaml, got %q", cfg.Repository.Domain)
	}
}

func TestEstablishRepositoryDomain_NoOriginLeavesUnbound(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "") // git repo, no origin configured

	res, err := establishRepositoryDomain(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "" || res.Source != "unbound" || res.Written {
		t.Fatalf("got %+v, want unbound with no write", res)
	}
}

func TestEstablishRepositoryDomain_ExplicitFlagWins(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/globulario/example-repo.git")

	res, err := establishRepositoryDomain(root, "github.com/explicit/override")
	if err != nil {
		t.Fatal(err)
	}
	if res.Domain != "github.com/explicit/override" || res.Source != "explicit" {
		t.Fatalf("got %+v, want explicit flag to win over git origin", res)
	}
}

// contract §3.4: re-running establishment must NOT rewrite an existing
// configured domain merely because the git remote changed — it must report
// a mismatch instead of silently following the remote.
func TestEstablishRepositoryDomain_ExistingConfigNotRewrittenOnRemoteChange(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/globulario/original-repo.git")

	first, err := establishRepositoryDomain(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Domain != "github.com/globulario/original-repo" {
		t.Fatalf("setup: got %+v", first)
	}

	// The remote changes (e.g. a fork, a rename) after initial establishment.
	if out, err := exec.Command("git", "-C", root, "remote", "set-url", "origin",
		"https://github.com/somebody-else/renamed-repo.git").CombinedOutput(); err != nil {
		t.Fatalf("git remote set-url: %v\n%s", err, out)
	}

	second, err := establishRepositoryDomain(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Domain != "github.com/globulario/original-repo" {
		t.Fatalf("existing configured domain must be preserved, got %q", second.Domain)
	}
	if second.Source != "existing_config" {
		t.Fatalf("expected source=existing_config, got %q", second.Source)
	}
	if !second.Mismatch {
		t.Fatal("expected a mismatch to be reported when the git remote disagrees with the configured domain")
	}
	if second.Written {
		t.Fatal("a preserved existing domain must not trigger a config write")
	}
}

// Re-running establishment with nothing changed must be idempotent: no write
// the second time, same resolved domain.
func TestEstablishRepositoryDomain_IdempotentOnRerun(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/globulario/example-repo.git")

	first, err := establishRepositoryDomain(root, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := establishRepositoryDomain(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if second.Domain != first.Domain {
		t.Fatalf("re-running establishment must resolve the same domain: %q != %q", second.Domain, first.Domain)
	}
	if second.Written {
		t.Fatal("re-running establishment with an unchanged origin must not rewrite config.yaml")
	}
}

// ─── sensei repo-domain CLI (shared resolver surface for shell hooks) ──────

func TestRunRepoDomain_PrintsResolvedDomain(t *testing.T) {
	root := t.TempDir()
	writeDomainTestFile(t, root, ".sensei/config.yaml", "repository:\n  domain: github.com/configured/repo\n")

	out := captureStdout(t, func() {
		if code := runRepoDomain([]string{"--path", root}); code != 0 {
			t.Fatalf("runRepoDomain exit=%d", code)
		}
	})
	if strings.TrimSpace(out) != "github.com/configured/repo" {
		t.Fatalf("got %q, want the configured domain on its own line", out)
	}
}

func TestRunRepoDomain_ExplicitFlagOverridesConfig(t *testing.T) {
	root := t.TempDir()
	writeDomainTestFile(t, root, ".sensei/config.yaml", "repository:\n  domain: github.com/configured/repo\n")

	out := captureStdout(t, func() {
		if code := runRepoDomain([]string{"--path", root, "--domain", "github.com/explicit/repo"}); code != 0 {
			t.Fatalf("runRepoDomain exit=%d", code)
		}
	})
	if strings.TrimSpace(out) != "github.com/explicit/repo" {
		t.Fatalf("got %q, want the explicit override to win", out)
	}
}

func TestRunRepoDomain_UnresolvedPrintsBlankLine(t *testing.T) {
	root := t.TempDir()
	out := captureStdout(t, func() {
		if code := runRepoDomain([]string{"--path", root}); code != 0 {
			t.Fatalf("runRepoDomain exit=%d", code)
		}
	})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("got %q, want an empty (unresolved) domain, not a guess", out)
	}
}

// ─── sensei init establishes the repository domain (contract §3.3) ────────

func TestRunInit_EstablishesDomainFromGitOrigin(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/globulario/example-repo.git")

	out := captureStdout(t, func() {
		if code := runInit([]string{"--dir", root, "--hooks=false", "--claude-md=false", "--agents-md=false", "--cursor=false", "--skills=false"}); code != 0 {
			t.Fatalf("runInit exit=%d", code)
		}
	})
	if !strings.Contains(out, "github.com/globulario/example-repo") {
		t.Fatalf("expected init to report the established domain, got:\n%s", out)
	}
	cfg, err := loadRepoDomainConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repository.Domain != "github.com/globulario/example-repo" {
		t.Fatalf("expected the git-origin-derived domain to be persisted, got %q", cfg.Repository.Domain)
	}
}

func TestRunInit_ExplicitDomainFlagWins(t *testing.T) {
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/globulario/example-repo.git")

	captureStdout(t, func() {
		if code := runInit([]string{"--dir", root, "--hooks=false", "--claude-md=false", "--agents-md=false", "--cursor=false", "--skills=false", "--domain", "github.com/explicit/override"}); code != 0 {
			t.Fatalf("runInit exit=%d", code)
		}
	})
	cfg, err := loadRepoDomainConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repository.Domain != "github.com/explicit/override" {
		t.Fatalf("expected the explicit --domain flag to win, got %q", cfg.Repository.Domain)
	}
}

// writeRepositoryDomain must not destroy unrelated existing config.yaml
// sections (contract §3.2/§6: non-destructive to unrelated fields).
func TestWriteRepositoryDomain_PreservesUnrelatedSections(t *testing.T) {
	root := t.TempDir()
	writeDomainTestFile(t, root, ".sensei/config.yaml", "sources:\n  - docs/awareness\nserver:\n  addr: localhost:10120\n")

	if err := writeRepositoryDomain(root, "github.com/globulario/example-repo"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(repoDomainConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	for _, want := range []string{"docs/awareness", "localhost:10120", "github.com/globulario/example-repo"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q to survive in the rewritten config, got:\n%s", want, content)
		}
	}
}
