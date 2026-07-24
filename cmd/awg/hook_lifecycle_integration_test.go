// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// envWithPath returns os.Environ() with any existing PATH replaced by
// pathEnv, never appended alongside it — a duplicate PATH entry is
// ambiguous across platforms and must not be relied on to "win".
func envWithPath(pathEnv string) []string {
	out := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "PATH=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "PATH="+pathEnv)
}

// TestHookLifecycle_DomainBoundBriefingMarkers is the required process-level
// integration proof (contract §13 correction): the ACTUAL .claude/hooks
// shell scripts and the ACTUAL built sensei binary, invoked as real
// subprocesses — not `ClassifyFile` called in-process — must enforce the
// full protected-edit lifecycle, including domain-bound marker isolation
// (contract §3) and fail-closed behavior (contract §9). The prior fixture
// (golang/architecture/protection/foreign_repo_fixture_test.go) explicitly
// substituted code inspection for this; this test is the substitute's
// replacement, proven at the process boundary.
func TestHookLifecycle_DomainBoundBriefingMarkers(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	enforceScript := filepath.Join(repoRoot, ".claude", "hooks", "enforce-briefing.sh")
	recordScript := filepath.Join(repoRoot, ".claude", "hooks", "record-briefing.sh")
	for _, s := range []string{enforceScript, recordScript} {
		if _, statErr := os.Stat(s); statErr != nil {
			t.Fatalf("hook script missing: %s: %v", s, statErr)
		}
	}

	// Build the binary under test into an isolated directory — the fixture
	// must never rely on whatever `sensei`/`awg` happens to already be on
	// the developer's ambient PATH.
	binDir := t.TempDir()
	senseiBin := filepath.Join(binDir, "sensei")
	build := exec.Command("go", "build", "-o", senseiBin, "./cmd/awg")
	build.Dir = repoRoot
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("go build sensei: %v\n%s", buildErr, out)
	}
	availablePath := binDir + ":/usr/local/bin:/usr/bin:/bin"
	unavailablePath := "/usr/local/bin:/usr/bin:/bin" // deliberately excludes binDir

	// Fixture checkout: one governed-relation-protected file, a real git
	// origin, and an established repository domain.
	root := t.TempDir()
	initGitRepoWithOrigin(t, root, "https://github.com/acme/widgets.git")
	writeDomainTestFile(t, root, "docs/awareness/invariants.yaml", `
invariants:
  - id: fixture.hook.lifecycle
    title: Protected core engine
    severity: high
    protects:
      files:
        - src/core/engine.go
`)
	writeDomainTestFile(t, root, "src/core/engine.go", "package core\n")

	if _, estErr := establishRepositoryDomain(root, ""); estErr != nil {
		t.Fatalf("establishRepositoryDomain: %v", estErr)
	}
	cfg, cfgErr := loadRepoDomainConfig(root)
	if cfgErr != nil {
		t.Fatal(cfgErr)
	}
	correctDomain := cfg.Repository.Domain
	if correctDomain == "" {
		t.Fatal("setup: expected establishRepositoryDomain to bind a domain from the git origin")
	}

	markerDirs := map[string]bool{}
	runHook := func(t *testing.T, script, sessionID, pathEnv string, toolInput map[string]any) string {
		t.Helper()
		markerDirs["/tmp/sensei-briefings/"+sessionID] = true
		body, marshalErr := json.Marshal(map[string]any{"tool_input": toolInput})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		// A bounded timeout, not the package's shared 2-minute test-binary
		// deadline: any real hang here must fail fast and specifically,
		// never silently starve every other (possibly parallel) test in the
		// package until the whole binary is killed.
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "bash", script)
		cmd.Dir = root
		cmd.Stdin = strings.NewReader(string(body))
		cmd.Env = envWithPath(pathEnv)
		cmd.Env = append(cmd.Env, "CLAUDE_SESSION_ID="+sessionID)
		// If a descendant process ever outlives bash while still holding the
		// stdout/stderr pipe open, force CombinedOutput to return anyway
		// rather than hang past the context deadline.
		cmd.WaitDelay = 5 * time.Second
		out, _ := cmd.CombinedOutput() // both scripts always exit 0; decision travels in stdout JSON.
		if ctx.Err() != nil {
			t.Fatalf("%s timed out after 20s (session=%s): %v\npartial output:\n%s", script, sessionID, ctx.Err(), out)
		}
		return string(out)
	}
	t.Cleanup(func() {
		for d := range markerDirs {
			_ = os.RemoveAll(d)
		}
	})

	// 1. Protected edit before any briefing → blocked.
	session1 := "hook-lifecycle-" + t.Name() + "-1"
	out := runHook(t, enforceScript, session1, availablePath, map[string]any{"file_path": "src/core/engine.go"})
	if !strings.Contains(out, `"decision": "block"`) {
		t.Fatalf("step 1: expected a block before any briefing, got:\n%s", out)
	}

	// 2. Briefing for the correct domain → marker recorded.
	out = runHook(t, recordScript, session1, availablePath, map[string]any{"file": "src/core/engine.go", "domain": correctDomain})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("step 2: record-briefing.sh must be silent on success, got:\n%s", out)
	}

	// 3. Protected edit afterward → allowed.
	out = runHook(t, enforceScript, session1, availablePath, map[string]any{"file_path": "src/core/engine.go"})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("step 3: expected a silent allow once the correct-domain briefing marker exists, got:\n%s", out)
	}

	// 4. Briefing for another domain → still blocked (marker isolation,
	// contract §3): a briefing scoped to a different domain must never
	// authorize an edit evaluated under this checkout's own domain.
	session2 := "hook-lifecycle-" + t.Name() + "-2"
	out = runHook(t, recordScript, session2, availablePath, map[string]any{"file": "src/core/engine.go", "domain": "github.com/somebody-else/renamed-repo"})
	if strings.TrimSpace(out) != "" {
		t.Fatalf("step 4 setup: record-briefing.sh must be silent on success, got:\n%s", out)
	}
	out = runHook(t, enforceScript, session2, availablePath, map[string]any{"file_path": "src/core/engine.go"})
	if !strings.Contains(out, `"decision": "block"`) {
		t.Fatalf("step 4: a briefing recorded against a DIFFERENT domain must not authorize this checkout's edit, got:\n%s", out)
	}

	// 5. Classifier unavailable → blocked (fail-closed, contract §9.7).
	session3 := "hook-lifecycle-" + t.Name() + "-3"
	out = runHook(t, enforceScript, session3, unavailablePath, map[string]any{"file_path": "src/core/engine.go"})
	if !strings.Contains(out, `"decision": "block"`) || !strings.Contains(out, "not on PATH") {
		t.Fatalf("step 5: expected a fail-closed block when the sensei/awg binary is unavailable, got:\n%s", out)
	}

	// 6. An unbound identity — an absolute path outside the repository,
	// presented directly to the compiled `protection-check` binary rather
	// than through the hook's own realpath pre-filter — is blocked visibly,
	// never silently renormalized as repo-relative (contract §2).
	check := exec.Command(senseiBin, "protection-check", "--path", root, "--file", "/etc/passwd", "--json")
	checkOut, checkErr := check.CombinedOutput()
	if checkErr == nil {
		t.Fatalf("step 6: expected `sensei protection-check` to fail visibly for an absolute path outside the repository, got exit 0:\n%s", checkOut)
	}
	if !strings.Contains(string(checkOut), "outside the repository or could not be normalized") {
		t.Fatalf("step 6: expected a visible, specific error for the unbound path, got:\n%s", checkOut)
	}
}
