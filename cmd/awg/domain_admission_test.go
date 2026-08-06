// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Admission and closure are DIFFERENT proofs and neither substitutes for the
// other.
//
// Closure proves the published slice is coherent with the input it was given.
// It cannot prove the input belonged to the requested domain: once the
// certified roots are derived from the corpus the build read, a wrong-workspace
// publication is perfectly self-consistent and reports PROVEN. Verified live on
// 2026-08-05, on the third repetition of the same wrong-directory build.
//
// Only an independent domain→repository binding can refuse it, and it must
// refuse BEFORE any mutation — the store was destructively replaced all three
// times while every later verdict was accurate but too late.
//
// invariant: graph.publication_requires_pre_mutation_domain_source_admission

func writeRegistry(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "domains.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// gitRepo creates a real git repository with an origin remote, because
// repository identity must come from the remote rather than the directory name.
func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"remote", "add", "origin", remote},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "docs", "awareness"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "awareness", "invariants.yaml"), []byte("invariants: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-C", dir, "add", "-A")
	_ = cmd.Run()
	cmd = exec.Command("git", "-C", dir, "commit", "-qm", "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}
	return dir
}

const servicesRegistry = `domains:
  globular:
    repository_identity: globulario/services
    allowed_corpus_roots:
      - docs/awareness
`

// TestWrongWorkspaceIsRefusedBeforeMutation is the canonical negative control:
// the exact command that destroyed the services slice three times.
func TestWrongWorkspaceIsRefusedBeforeMutation(t *testing.T) {
	sensei := gitRepo(t, "https://github.com/globulario/sensei.git")
	reg := writeRegistry(t, servicesRegistry)

	err := AdmitPublication("globular", []string{filepath.Join(sensei, "docs", "awareness")}, reg)
	if err == nil {
		t.Fatal("publishing the SENSEI corpus into the services domain was admitted — " +
			"this is the 2026-08-05 incident, and closure cannot catch it because the " +
			"wrong corpus is perfectly self-consistent")
	}
	msg := err.Error()
	for _, want := range []string{
		"PUBLICATION_REFUSED",
		"requested_domain:    globular",
		"expected_repository: globulario/services",
		"actual_repository:   globulario/sensei",
		"mutation_started:    false",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal must state %q; got:\n%s", want, msg)
		}
	}
}

// TestCorrectWorkspaceIsAdmitted is the positive control. Without it the guard
// could pass by refusing everything, which is the failure mode of a gate that
// gets switched off.
func TestCorrectWorkspaceIsAdmitted(t *testing.T) {
	services := gitRepo(t, "https://github.com/globulario/services.git")
	reg := writeRegistry(t, servicesRegistry)

	if err := AdmitPublication("globular", []string{filepath.Join(services, "docs", "awareness")}, reg); err != nil {
		t.Fatalf("the correct repository was refused: %v", err)
	}
}

// TestIdentityComesFromRemoteNotDirectoryName. A checkout copied to any path,
// or a worktree, still has the right identity; a same-named directory from
// another project does not.
func TestIdentityComesFromRemoteNotDirectoryName(t *testing.T) {
	// Correct remote, arbitrary directory name → admitted.
	ok := gitRepo(t, "git@github.com:globulario/services.git")
	reg := writeRegistry(t, servicesRegistry)
	if err := AdmitPublication("globular", []string{filepath.Join(ok, "docs", "awareness")}, reg); err != nil {
		t.Errorf("scp-form remote for the right repo must be admitted: %v", err)
	}

	// An impostor whose remote merely ends in the same repo NAME.
	bad := gitRepo(t, "https://github.com/someoneelse/services.git")
	if err := AdmitPublication("globular", []string{filepath.Join(bad, "docs", "awareness")}, reg); err == nil {
		t.Error("a different owner's repository with the same repo name was admitted — " +
			"identity must be owner/repo, never the basename")
	}
}

func TestNormalizeRepoIdentityForms(t *testing.T) {
	for remote, want := range map[string]string{
		"https://github.com/globulario/services.git": "globulario/services",
		"https://github.com/globulario/services":     "globulario/services",
		"git@github.com:globulario/services.git":     "globulario/services",
		"ssh://git@github.com/globulario/services":   "globulario/services",
		"https://GitHub.com/Globulario/Services.git": "globulario/services",
	} {
		if got := NormalizeRepoIdentity(remote); got != want {
			t.Errorf("NormalizeRepoIdentity(%q) = %q, want %q", remote, got, want)
		}
	}
}

// TestUnverifiableInputsAreRefused: every path that cannot affirmatively prove
// identity must refuse. "I could not verify" is never "verified".
func TestUnverifiableInputsAreRefused(t *testing.T) {
	services := gitRepo(t, "https://github.com/globulario/services.git")
	corpus := filepath.Join(services, "docs", "awareness")

	t.Run("missing registry", func(t *testing.T) {
		if err := AdmitPublication("globular", []string{corpus}, filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
			t.Error("a missing registry admitted the publication — the domain would then be " +
				"resolved from the working directory, which is what published the wrong corpus")
		}
	})

	t.Run("unregistered domain", func(t *testing.T) {
		if err := AdmitPublication("not-registered", []string{corpus}, writeRegistry(t, servicesRegistry)); err == nil {
			t.Error("an unregistered domain was admitted")
		}
	})

	t.Run("corpus root outside allowed list", func(t *testing.T) {
		other := filepath.Join(services, "docs")
		if err := AdmitPublication("globular", []string{other}, writeRegistry(t, servicesRegistry)); err == nil {
			t.Error("a corpus root outside the domain's allowed roots was admitted")
		}
	})

	t.Run("not a git repository", func(t *testing.T) {
		if err := AdmitPublication("globular", []string{t.TempDir()}, writeRegistry(t, servicesRegistry)); err == nil {
			t.Error("a directory with no repository identity was admitted")
		}
	})
}

// TestDirtyWorktreeRefusedUnlessAllowed. A dirty tree must never silently
// certify only the git commit while publishing uncommitted content.
func TestDirtyWorktreeRefusedUnlessAllowed(t *testing.T) {
	services := gitRepo(t, "https://github.com/globulario/services.git")
	corpus := filepath.Join(services, "docs", "awareness")
	if err := os.WriteFile(filepath.Join(corpus, "invariants.yaml"), []byte("invariants: [ {id: uncommitted} ]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := AdmitPublication("globular", []string{corpus}, writeRegistry(t, servicesRegistry))
	if err == nil {
		t.Fatal("a dirty worktree was admitted by default: the publication would certify a " +
			"revision while shipping content that revision does not contain")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Errorf("refusal must name the dirty worktree; got %v", err)
	}

	// Explicitly permitted — the services corpus is edited and published in the
	// same session, so this must remain expressible.
	allowed := writeRegistry(t, servicesRegistry+"    allow_dirty_worktree: true\n")
	if err := AdmitPublication("globular", []string{corpus}, allowed); err != nil {
		t.Errorf("allow_dirty_worktree must permit publishing: %v", err)
	}
}
