// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/runtimedescriptor"
	"github.com/globulario/sensei/golang/seedmeta"
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

// startRealOxigraph starts a real Oxigraph subprocess bound to a fresh
// ephemeral port and waits for it to become healthy, mirroring
// cmd_build_integration_test.go's TestIntegration_ScopedRepoPublication_RealOxigraph.
// Returns the bind address, the SPARQL query URL, and the Graph Store
// endpoint. The process is killed when the test completes.
func startRealOxigraph(t *testing.T) (bindAddr, queryURL, storeURL string) {
	t.Helper()
	oxi, err := findOxigraphBinary()
	if err != nil {
		t.Skipf("Oxigraph binary unavailable: %v", err)
	}
	bindAddr = freeTCPAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, oxi, "serve", "--location", t.TempDir(), "--bind", bindAddr)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start Oxigraph: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	queryURL = "http://" + bindAddr + "/query"
	if !waitForSPARQLHealthy(queryURL, 10*time.Second) {
		t.Fatalf("Oxigraph did not become healthy at %s:\n%s", queryURL, logs.String())
	}
	storeURL = "http://" + bindAddr + "/store?default"
	return bindAddr, queryURL, storeURL
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

// startCheckoutAwarenessGraph starts a real awareness-graph process
// representing one "checkout" — a repo-root/repo-domain/graph-marker-file
// triple — pointed at a real Oxigraph query URL. -no-seed and no
// -require-store: the store may still be empty at this point and startup
// must not depend on pre-existing content.
func startCheckoutAwarenessGraph(t *testing.T, srvBin, addr, queryURL, markerPath, repoRoot, repoDomain string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(srvBin,
		"-addr", addr,
		"-oxigraph-url", queryURL,
		"-no-seed",
		"-graph-marker-file", markerPath,
		"-repo-root", repoRoot,
		"-repo-domain", repoDomain,
	)
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start checkout awareness-graph: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("awareness-graph logs:\n%s", logs.String())
		}
	})
	return cmd
}

// TestIntegration_ServeReuse_RejectsForeignCheckout is the required
// two-checkout proof (docs/design/serve-runtime-compatibility.md, issue
// #118, acceptance criterion #6): a REAL awareness-graph process, pointed
// at a REAL Oxigraph — the complete two-process habitat — started for
// "checkout A" must never be silently reused as "checkout B"'s local
// execution habitat merely because its gRPC address responds.
func TestIntegration_ServeReuse_RejectsForeignCheckout(t *testing.T) {
	srvBin := buildAwarenessGraphBinary(t)

	// Isolate runtimedescriptor's machine-global lookup to a throwaway HOME
	// for the duration of this test — never touch the real
	// ~/.local/share/sensei/runtime directory.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	_, queryURL, _ := startRealOxigraph(t)
	sharedAddr := freeTCPAddr(t)

	rootA := t.TempDir()
	rootB := t.TempDir()
	markerA := filepath.Join(rootA, ".sensei", "graph-authority.json")
	markerB := filepath.Join(rootB, ".sensei", "graph-authority.json")
	const domainA = "github.com/globulario/checkout-a"
	const domainB = "github.com/globulario/checkout-b"

	startCheckoutAwarenessGraph(t, srvBin, sharedAddr, queryURL, markerA, rootA, domainA)
	descA := waitForDescriptor(t, runtimedescriptor.KindAwarenessGraph, sharedAddr)
	if descA.RepoRoot != rootA || descA.RepoDomain != domainA || descA.GraphMarkerFile != markerA {
		t.Fatalf("checkout A's self-written descriptor does not match what it was started with: %+v", descA)
	}

	// ── checkout B attempts to `sensei serve` against the same address,
	// reusing the SAME real Oxigraph (--no-oxigraph) but with a different
	// repo-root/repo-domain/marker ───────────────────────────────────────
	oxigraphBindB := strings.TrimSuffix(strings.TrimPrefix(queryURL, "http://"), "/query")
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

// TestIntegration_ServeReuse_FreshnessCurrentAfterCompatibleReuseAndScopedBuild
// is the required freshness-after-compatible-reuse proof (docs/design/
// serve-runtime-compatibility.md, issue #118, acceptance criterion #5): once
// a scoped build updates the store and marker a running, COMPATIBLY-REUSABLE
// awareness-graph service already watches, that same (never restarted)
// service must report graph freshness as current on its next query — proving
// end-to-end that the fix keeps the scoped-build marker and the serving
// process's marker as the exact same file, not two that happen to coincide.
func TestIntegration_ServeReuse_FreshnessCurrentAfterCompatibleReuseAndScopedBuild(t *testing.T) {
	srvBin := buildAwarenessGraphBinary(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	_, queryURL, storeURL := startRealOxigraph(t)
	addr := freeTCPAddr(t)

	root := t.TempDir()
	markerPath := filepath.Join(root, ".sensei", "graph-authority.json")
	const domain = "github.com/globulario/checkout-freshness"

	startCheckoutAwarenessGraph(t, srvBin, addr, queryURL, markerPath, root, domain)
	waitForDescriptor(t, runtimedescriptor.KindAwarenessGraph, addr)

	// checkAwarenessCompatibility must judge THIS running service compatible
	// for a "checkout B" requesting the exact same configuration — proving
	// the wrapper's reuse decision (not just its rejection path) is correct
	// for the scenario this test exercises end-to-end.
	ok, cerr := checkAwarenessCompatibility(addr, runtimedescriptor.Descriptor{
		OxigraphQueryURL: queryURL,
		GraphMarkerFile:  markerPath,
		RepoRoot:         root,
		RepoDomain:       domain,
		// startCheckoutAwarenessGraph passes no -home-domain, so the
		// process resolved (and self-described) the server's own default —
		// mirroring what runServe's wantHomeDomain normalization now also
		// resolves to for an equally-unset --home-domain.
		HomeDomain: defaultHomeDomain,
	})
	if !ok {
		t.Fatalf("expected the running service to be judged compatible for its own exact configuration, got: %v", cerr)
	}

	// A pre-existing foreign domain must survive the scoped build, exactly
	// as proven in TestIntegration_ScopedRepoPublication_RealOxigraph.
	baseline, _ := seedmeta.AppendMarker([]byte(
		"<https://example.test/foreign> <https://globular.io/awareness#repo> \"github.com/test/foreign\" .\n" +
			"<https://example.test/foreign> <https://example.test/p> \"must survive\" .\n"))
	if err := uploadNTriples(httpHealthClient, storeURL, baseline); err != nil {
		t.Fatalf("load baseline: %v", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	txPath := seedmeta.RuntimeTransactionPath(markerPath)
	if code := runBuild([]string{
		"-input", filepath.Join(repoRoot, "docs", "awareness"),
		"-repo", domain,
		"-store-url", storeURL,
		"-graph-marker-file", markerPath,
		"-graph-transaction-file", txPath,
		"-ag-repo", repoRoot,
	}); code != 0 {
		t.Fatalf("scoped build against the running service's own marker/store failed, code=%d", code)
	}

	written, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		t.Fatalf("scoped build did not leave a readable marker file: %v", err)
	}
	if written.Digest == "" {
		t.Fatal("scoped build wrote an incomplete marker")
	}

	// The awareness-graph process was NEVER restarted — it is the exact
	// same process started above. Its Metadata RPC re-reads both the
	// marker file and the live store on every call, so it must now report
	// the freshness state as current for the marker the scoped build just
	// wrote.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := client.Dial(addr)
	if err != nil {
		t.Fatalf("dial awareness-graph: %v", err)
	}
	defer c.Close()

	var resp *awarenesspb.MetadataResponse
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err = c.Metadata(ctx)
		if err == nil && resp.GetGraphFreshnessState() == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
			break
		}
		if time.Now().After(deadline) {
			state := "unknown"
			if resp != nil {
				state = resp.GetGraphFreshnessState().String()
			}
			t.Fatalf("expected freshness state CURRENT after the scoped build, got %s (err=%v)", state, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
