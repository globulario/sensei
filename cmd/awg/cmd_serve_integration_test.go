// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/runtimedescriptor"
)

// buildAwarenessGraphBinary compiles the CURRENT source tree's awareness-graph
// binary to a throwaway location. findServerBinary()'s PATH fallback can
// resolve to a stale, globally-installed release binary that predates the
// change under test — this test must exercise the code under test, not
// whatever happens to be installed on the developer's machine.
func buildAwarenessGraphBinary(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), exeName("awareness-graph"))
	cmd := exec.Command("go", "build", "-o", out, "./golang/server")
	cmd.Dir = repoRoot
	if buildOut, buildErr := cmd.CombinedOutput(); buildErr != nil {
		t.Fatalf("go build awareness-graph: %v\n%s", buildErr, buildOut)
	}
	return out
}

// freeTCPAddr reserves an ephemeral port by binding then immediately
// releasing it, mirroring the idiom in cmd_build_integration_test.go.
func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

// waitForDescriptor polls until a runtime descriptor is readable for
// kind/addr, or fails the test after a bounded timeout. A real awareness-
// graph process writes its descriptor immediately after net.Listen
// succeeds, so this should resolve within a few scheduler ticks.
func waitForDescriptor(t *testing.T, kind runtimedescriptor.Kind, addr string) runtimedescriptor.Descriptor {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d, err := runtimedescriptor.Read(kind, addr); err == nil {
			return d
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a %s runtime descriptor at %s", kind, addr)
	return runtimedescriptor.Descriptor{}
}

// TestIntegration_ServeReuse_RejectsForeignCheckout is the required
// two-checkout proof (docs/design/serve-runtime-compatibility.md, issue
// #118, acceptance criterion #6): a REAL awareness-graph process started
// for "checkout A" must never be silently reused as "checkout B"'s local
// execution habitat merely because its gRPC address responds.
func TestIntegration_ServeReuse_RejectsForeignCheckout(t *testing.T) {
	srvBin := buildAwarenessGraphBinary(t)

	// Isolate runtimedescriptor's machine-global lookup to a throwaway HOME
	// for the duration of this test — never touch the real
	// ~/.local/share/sensei/runtime directory.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	sharedAddr := freeTCPAddr(t)
	// A deliberately unreachable Oxigraph URL, identical on both "checkouts"
	// so the awareness-graph compatibility check's OxigraphQueryURL
	// comparison passes — isolating this test to the repo-root/repo-domain/
	// marker-file mismatch the issue is actually about. -require-store is
	// not set, so an unreachable backend is logged, not fatal.
	const bogusOxigraphURL = "http://127.0.0.1:1/query"

	rootA := t.TempDir()
	rootB := t.TempDir()
	markerA := filepath.Join(rootA, ".sensei", "graph-authority.json")
	markerB := filepath.Join(rootB, ".sensei", "graph-authority.json")
	const domainA = "github.com/globulario/checkout-a"
	const domainB = "github.com/globulario/checkout-b"

	// ── Start checkout A's real awareness-graph process ──────────────────
	// -no-seed: the Oxigraph URL is deliberately unreachable in this test
	// (only the awareness-graph reuse path is under test), and the
	// embedded-seed enforcement path would otherwise fail startup before
	// ever reaching net.Listen.
	cmdA := exec.Command(srvBin,
		"-addr", sharedAddr,
		"-oxigraph-url", bogusOxigraphURL,
		"-no-seed",
		"-graph-marker-file", markerA,
		"-repo-root", rootA,
		"-repo-domain", domainA,
	)
	var logsA strings.Builder
	cmdA.Stdout = &logsA
	cmdA.Stderr = &logsA
	if err := cmdA.Start(); err != nil {
		t.Fatalf("start checkout A's awareness-graph: %v", err)
	}
	defer func() {
		if cmdA.Process != nil {
			_ = cmdA.Process.Kill()
		}
		_ = cmdA.Wait()
	}()

	descA := waitForDescriptor(t, runtimedescriptor.KindAwarenessGraph, sharedAddr)
	if descA.RepoRoot != rootA || descA.RepoDomain != domainA || descA.GraphMarkerFile != markerA {
		t.Fatalf("checkout A's self-written descriptor does not match what it was started with: %+v\nlogs:\n%s", descA, logsA.String())
	}

	// ── checkout B attempts to `sensei serve` against the same address ───
	oxigraphBindB := "127.0.0.1:1" // derives to bogusOxigraphURL, matching checkout A exactly
	code, _, errOut := captureStdoutStderr(t, func() int {
		return runServe([]string{
			"-addr", sharedAddr,
			"-no-oxigraph",
			"-oxigraph-bind", oxigraphBindB,
			"-no-seed",
			"-graph-marker-file", markerB,
			"-repo-root", rootB,
			"-repo-domain", domainB,
		})
	})

	if code == 0 {
		t.Fatalf("expected checkout B's serve attempt to fail against checkout A's incompatible service, got exit 0:\n%s", errOut)
	}
	for _, want := range []string{sharedAddr, rootA, rootB, domainA, domainB} {
		if !strings.Contains(errOut, want) {
			t.Errorf("expected the diagnostic to name %q, got:\n%s", want, errOut)
		}
	}

	// Neither checkout's marker file may have been created/touched by the
	// rejected reuse attempt.
	if _, statErr := os.Stat(markerA); statErr == nil {
		t.Error("checkout A's marker file must not be created/touched by checkout B's rejected attempt")
	}
	if _, statErr := os.Stat(markerB); statErr == nil {
		t.Error("checkout B's marker file must not be created by a rejected reuse attempt")
	}
}
