// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/globulario/sensei/golang/runtimedescriptor"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

// exeName appends the platform executable extension (".exe" on Windows) so the
// same-directory and ./bin/ lookups find e.g. oxigraph.exe. exec.LookPath
// already honors PATHEXT on Windows, so PATH lookups pass the base name.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

const (
	backendHealthPollInterval  = 2 * time.Second
	backendHealthFailThreshold = 2
	// defaultHomeDomain must match golang/server/main.go's defaultHomeDomain
	// — the value a freshly started awareness-graph child resolves to when
	// given no -home-domain flag at all. Kept in sync here (rather than
	// relying on the child's own flag default) so the compatibility
	// comparison in checkAwarenessCompatibility is exact, never a
	// coincidence of two independently-defined defaults agreeing.
	defaultHomeDomain = "globular"
)

var httpHealthClient = &http.Client{Timeout: 1 * time.Second}

func runServe(args []string) int {
	fs := flag.NewFlagSet("sensei serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", defaultServiceListen(), "gRPC listen address")
	oxigraphBind := fs.String("oxigraph-bind", defaultOxigraphBind(), "Oxigraph listen address")
	noSeed := fs.Bool("no-seed", false, "skip the embedded Globular seed (cold-start projects: build your own graph with `sensei build`)")
	allowStaleSeed := fs.Bool("allow-stale-seed", false, "allow startup when the live store is missing the embedded seed marker")
	graphMarkerFile := fs.String("graph-marker-file", "", "runtime graph marker file for live graph authority checks (default: <project>/.sensei/graph-authority.json only with --no-seed; embedded-seed mode uses the embedded marker unless this flag is explicit)")
	dataDir := fs.String("data", "", "Oxigraph data directory (default: ~/.local/share/sensei/oxigraph)")
	noOxigraph := fs.Bool("no-oxigraph", false, "don't start Oxigraph (use an external instance)")
	homeDomain := fs.String("home-domain", "", "domain key for untagged host-project nodes (cold-start non-Globular deployments set their own; default: globular)")
	enablePropose := fs.Bool("enable-propose", false, "enable the Propose RPC agent write path (writes validated candidates under docs/awareness/candidates/; off by default)")
	repoRoot := fs.String("repo-root", "", "repository root for governed briefing feedback (with --repo-domain); resolved to an absolute existing directory. Enables briefing-feedback verification.")
	repoDomain := fs.String("repo-domain", "", "canonical repository domain for briefing feedback (with --repo-root); identifies the filesystem repository whose promotions may be verified (NOT --home-domain).")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei serve [flags]

Starts both Oxigraph and the Sensei gRPC server as a single service.
Oxigraph is managed as a child process — no Docker needed.

On shutdown (SIGINT/SIGTERM), both processes are stopped cleanly.

Use --no-oxigraph to connect to an external Oxigraph instance instead.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Resolve the briefing repository context BEFORE starting any child process so a
	// misconfiguration exits 2 cleanly. The caller explicitly selected this root, so the
	// wrapper may resolve a relative path here; the server binary itself never does.
	serveRepoRoot, serveRepoDomain, rcErr := resolveServeRepoContext(*repoRoot, *repoDomain)
	if rcErr != nil {
		fmt.Fprintf(os.Stderr, "sensei serve: %v\n", rcErr)
		return 2
	}

	// Resolve data directory. Always normalized to an ABSOLUTE path: a
	// relative --data from two different checkouts' working directories can
	// compare equal as strings while naming two different directories,
	// falsely satisfying the compatibility check below
	// (docs/design/serve-runtime-compatibility.md, issue #118).
	data := *dataDir
	if data == "" {
		home, _ := os.UserHomeDir()
		base := filepath.Join(home, ".local", "share")
		data = filepath.Join(base, "sensei", "oxigraph")
		// Honor the pre-rename cache location if it exists and the new one
		// hasn't been created yet, so an upgrade reuses the existing store.
		if _, err := os.Stat(data); err != nil {
			legacy := filepath.Join(base, "awg", "oxigraph")
			if _, lerr := os.Stat(legacy); lerr == nil {
				data = legacy
			}
		}
	}
	if abs, aerr := filepath.Abs(data); aerr == nil {
		data = abs
	}

	oxigraphURL := fmt.Sprintf("http://%s/query", *oxigraphBind)
	var oxiCmd *exec.Cmd

	// ── Start Oxigraph ──────────────────────────────────────────────────
	if !*noOxigraph {
		// Check if something is already listening on the port. An occupied
		// port is reused ONLY when it can be proven compatible (same data
		// directory) — never solely because it responds
		// (docs/design/serve-runtime-compatibility.md, issue #118).
		if conn, err := net.DialTimeout("tcp", *oxigraphBind, 500*time.Millisecond); err == nil {
			conn.Close()
			ok, cerr := checkOxigraphCompatibility(*oxigraphBind, runtimedescriptor.Descriptor{DataDir: data})
			if !ok {
				fmt.Fprintf(os.Stderr, "sensei serve: %v\n", cerr)
				return 1
			}
			fmt.Fprintf(os.Stderr, "sensei serve: port %s already in use — reusing compatible Oxigraph\n", *oxigraphBind)
		} else {
			oxiBin, err := findOxigraphBinary()
			if err != nil {
				fmt.Fprintf(os.Stderr, "sensei serve: %v\n", err)
				fmt.Fprintf(os.Stderr, "  Download: https://github.com/oxigraph/oxigraph/releases\n")
				fmt.Fprintf(os.Stderr, "  Or use --no-oxigraph with an external instance\n")
				return 1
			}

			if err := os.MkdirAll(data, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "sensei serve: mkdir %s: %v\n", data, err)
				return 1
			}

			oxiCmd = exec.Command(oxiBin, oxigraphServeArgs(data, *oxigraphBind)...)
			oxiCmd.Stdout = os.Stderr // Oxigraph logs go to stderr
			oxiCmd.Stderr = os.Stderr
			if err := oxiCmd.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "sensei serve: start oxigraph: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "sensei: oxigraph started (pid %d, data=%s)\n", oxiCmd.Process.Pid, data)

			// Wait for Oxigraph to be ready.
			if !waitForSPARQLHealthy(fmt.Sprintf("http://%s/query", *oxigraphBind), 10*time.Second) {
				fmt.Fprintf(os.Stderr, "sensei serve: oxigraph did not become ready in 10s\n")
				oxiCmd.Process.Kill()
				return 1
			}

			// Oxigraph is a third-party binary and can never self-describe,
			// so the wrapper that started it writes its descriptor — the
			// only durable record of which data directory this listener
			// actually serves.
			if derr := runtimedescriptor.Write(runtimedescriptor.Descriptor{
				Kind:          runtimedescriptor.KindOxigraph,
				PID:           oxiCmd.Process.Pid,
				ListenAddr:    *oxigraphBind,
				DataDir:       data,
				StartedAtUnix: time.Now().Unix(),
				SenseiVersion: Version,
			}); derr != nil {
				fmt.Fprintf(os.Stderr, "sensei serve: write runtime descriptor: %v\n", derr)
			}
		}
	}

	// ── Start Sensei server ─────────────────────────────────────────────
	srvBin, err := findServerBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei serve: %v\n", err)
		if oxiCmd != nil {
			oxiCmd.Process.Signal(syscall.SIGTERM)
			oxiCmd.Wait()
		}
		return 1
	}

	markerPath, err := resolveServeGraphMarkerFile(*graphMarkerFile, *noSeed)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei serve: resolve graph marker file: %v\n", err)
		if oxiCmd != nil {
			oxiCmd.Process.Signal(syscall.SIGTERM)
			oxiCmd.Wait()
		}
		return 1
	}
	// Normalized to absolute for the same reason as the Oxigraph data
	// directory above: the compatibility comparison must never depend on
	// which directory two different invocations happened to be run from.
	if markerPath != "" {
		if abs, aerr := filepath.Abs(markerPath); aerr == nil {
			markerPath = abs
		}
	}

	// wantAwarenessDir mirrors the -awareness-dir flag a fresh start would
	// pass (non-empty enables the Propose RPC write path). Resolved once,
	// up front, so BOTH the compatibility check below (which must run
	// before any child starts) and the fresh-start branch use the identical
	// value — a service running with a different behavioral surface must
	// never be silently reused (docs/design/serve-runtime-compatibility.md
	// §3.4).
	wantAwarenessDir := ""
	if *enablePropose {
		root, rerr := resolveProjectRoot("")
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "sensei serve: --enable-propose: resolve project root: %v\n", rerr)
			if oxiCmd != nil {
				oxiCmd.Process.Signal(syscall.SIGTERM)
				oxiCmd.Wait()
			}
			return 1
		}
		wantAwarenessDir = filepath.Join(root, "docs", "awareness")
	}
	// The wrapper's own --home-domain flag defaults to "" (unset), but a
	// freshly started child that receives no -home-domain flag at all
	// falls back to ITS OWN internal default, defaultHomeDomain
	// ("globular", golang/server/main.go). Comparing the wrapper's raw ""
	// against a running service's self-described "globular" would be a
	// false incompatibility — resolve to the same canonical default here
	// so the comparison (and the value handed to a fresh child) is exact.
	wantHomeDomain := strings.TrimSpace(*homeDomain)
	if wantHomeDomain == "" {
		wantHomeDomain = defaultHomeDomain
	}

	// Check if something is already listening on the gRPC address. An
	// occupied address is reused ONLY when it can be proven compatible
	// (same Oxigraph endpoint, graph-marker-file, repo-root, repo-domain,
	// home-domain, and Propose-RPC surface this invocation would use) —
	// never solely because it responds
	// (docs/design/serve-runtime-compatibility.md, issue #118). Critically,
	// this check runs BEFORE any marker-file mutation: an incompatible or
	// unidentifiable occupant must never cause this checkout's own marker
	// file to be overwritten from a foreign store.
	var srvCmd *exec.Cmd
	if conn, derr := net.DialTimeout("tcp", *addr, 500*time.Millisecond); derr == nil {
		conn.Close()
		ok, cerr := checkAwarenessCompatibility(*addr, runtimedescriptor.Descriptor{
			OxigraphQueryURL: oxigraphURL,
			GraphMarkerFile:  markerPath,
			RepoRoot:         serveRepoRoot,
			RepoDomain:       serveRepoDomain,
			HomeDomain:       wantHomeDomain,
			AwarenessDir:     wantAwarenessDir,
		})
		if !ok {
			fmt.Fprintf(os.Stderr, "sensei serve: %v\n", cerr)
			if oxiCmd != nil {
				oxiCmd.Process.Signal(syscall.SIGTERM)
				oxiCmd.Wait()
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "sensei serve: address %s already in use — reusing compatible awareness-graph service\n", *addr)
	} else {
		srvArgs := []string{"-addr", *addr, "-oxigraph-url", oxigraphURL}
		if *noSeed {
			srvArgs = append(srvArgs, "-no-seed")
		}
		if *allowStaleSeed {
			srvArgs = append(srvArgs, "-allow-stale-seed")
		}
		if markerPath != "" {
			srvArgs = append(srvArgs, "-graph-marker-file", markerPath)
		}
		// Always pass explicitly (wantHomeDomain is never empty — see
		// above) rather than relying on the child's own flag default, so
		// what we requested and what it self-describes are identical by
		// construction, not by two defaults happening to agree.
		srvArgs = append(srvArgs, "-home-domain", wantHomeDomain)
		if serveRepoRoot != "" {
			srvArgs = append(srvArgs, "-repo-root", serveRepoRoot, "-repo-domain", serveRepoDomain)
		}
		if wantAwarenessDir != "" {
			srvArgs = append(srvArgs, "-awareness-dir", wantAwarenessDir)
		}
		srvCmd = exec.Command(srvBin, srvArgs...)
		srvCmd.Stdout = os.Stdout
		srvCmd.Stderr = os.Stderr
		if err := srvCmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "sensei serve: start awareness-graph: %v\n", err)
			if oxiCmd != nil {
				oxiCmd.Process.Signal(syscall.SIGTERM)
				oxiCmd.Wait()
			}
			return 1
		}

		// Confirm the child actually bound its port before treating this
		// invocation as having proven ownership. A dial-time-free port can
		// still race: only a confirmed bind (or the process exiting, which
		// this timeout also bounds) proves we own this address — the
		// marker-file mutation below must never precede that proof
		// (docs/design/serve-runtime-compatibility.md §3.6).
		if !waitForAddrListening(*addr, 10*time.Second) {
			fmt.Fprintf(os.Stderr, "sensei serve: awareness-graph did not become ready in 10s\n")
			srvCmd.Process.Kill()
			srvCmd.Wait()
			if oxiCmd != nil {
				oxiCmd.Process.Signal(syscall.SIGTERM)
				oxiCmd.Wait()
			}
			return 1
		}
		fmt.Fprintf(os.Stderr, "sensei: awareness-graph started (pid %d, addr=%s)\n", srvCmd.Process.Pid, *addr)
		// The awareness-graph process self-describes once its own
		// net.Listen succeeds (golang/server/main.go); this wrapper does
		// not write that descriptor on its behalf.

		// Only now — after confirmed, exclusive ownership of this address —
		// is it safe to refresh the checkout-local marker file from the
		// live store.
		if markerPath != "" {
			fmt.Fprintf(os.Stderr, "sensei serve: graph marker file: %s\n", markerPath)
			if *noSeed && strings.TrimSpace(*graphMarkerFile) == "" {
				syncCtx, syncCancel := context.WithTimeout(context.Background(), 3*time.Second)
				if err := syncDefaultRuntimeMarkerFromLiveStore(syncCtx, markerPath, oxigraphURL, os.Stderr); err != nil {
					fmt.Fprintf(os.Stderr, "sensei serve: runtime marker refresh skipped: %v\n", err)
				}
				syncCancel()
			}
		}
	}

	// ── Signal handling ─────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	monitorCtx, monitorCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer monitorCancel()
	backendErrCh := make(chan error, 1)
	go watchBackendHealth(monitorCtx, oxigraphURL, backendHealthPollInterval, backendHealthFailThreshold, backendErrCh)

	// Wait for either an OWNED child to exit or a signal. A reused piece
	// (srvCmd/oxiCmd nil) contributes no producer to doneCh — this
	// invocation never started it, so it never waits on or stops it.
	doneCh := make(chan error, 2)
	if srvCmd != nil {
		go func() { doneCh <- srvCmd.Wait() }()
	}
	if oxiCmd != nil {
		go func() { doneCh <- oxiCmd.Wait() }()
	}

	exitCode := 0
	select {
	case sig := <-sigCh:
		fmt.Fprintf(os.Stderr, "\nsensei: received %s, shutting down...\n", sig)
	case err := <-backendErrCh:
		exitCode = 1
		fmt.Fprintf(os.Stderr, "sensei: backend became unreachable: %v\n", err)
	case err := <-doneCh:
		if err != nil {
			exitCode = 1
			fmt.Fprintf(os.Stderr, "sensei: child exited: %v\n", err)
		}
	}
	monitorCancel()

	// Stop only processes whose FULL dependency chain this invocation owns.
	// A freshly-started Oxigraph is left running, even though we started
	// it, when the awareness-graph service depending on it was REUSED
	// (not ours) — stopping it here would silently break that dependent
	// service out from under it. Mixed ownership (own one piece, reuse the
	// other) must never orphan a resource a reused service still depends on
	// (docs/design/serve-runtime-compatibility.md, issue #118).
	stopOxigraph := oxiCmd != nil && srvCmd != nil
	if oxiCmd != nil && srvCmd == nil {
		fmt.Fprintf(os.Stderr, "sensei: leaving newly-started Oxigraph running at %s — a reused awareness-graph service still depends on it\n", *oxigraphBind)
	}
	if srvCmd != nil {
		srvCmd.Process.Signal(syscall.SIGTERM)
	}
	if stopOxigraph {
		oxiCmd.Process.Signal(syscall.SIGTERM)
	}

	// Give them a moment to exit cleanly.
	timer := time.AfterFunc(5*time.Second, func() {
		if srvCmd != nil {
			srvCmd.Process.Kill()
		}
		if stopOxigraph {
			oxiCmd.Process.Kill()
		}
	})
	if srvCmd != nil {
		srvCmd.Wait()
	}
	if stopOxigraph {
		oxiCmd.Wait()
		// The awareness-graph process removes its own descriptor on
		// graceful shutdown (golang/server/main.go); Oxigraph is a
		// third-party binary and cannot, so the wrapper that wrote it on
		// its behalf removes it here. A courtesy cleanup only — a stale
		// descriptor still self-heals via dead-PID detection on next read.
		_ = runtimedescriptor.Remove(runtimedescriptor.KindOxigraph, *oxigraphBind)
	}
	timer.Stop()

	fmt.Fprintf(os.Stderr, "sensei: stopped\n")
	return exitCode
}

// resolveServeRepoContext validates and resolves the --repo-root/--repo-domain pair for the
// serve wrapper. Both-or-neither (neither ⇒ feedback disabled). Padded root/domain and a
// whitespace domain are configuration errors. Because the CALLER explicitly selected this root,
// the wrapper may resolve a relative path to absolute here (filepath.Abs), then resolve
// symlinks once and require an existing directory; it never resolves an empty value and never
// defaults to the working directory. Only the resulting absolute root is forwarded to the
// server binary (which itself never calls filepath.Abs).
func resolveServeRepoContext(root, domain string) (string, string, error) {
	if root == "" && domain == "" {
		return "", "", nil
	}
	if root == "" || domain == "" {
		return "", "", fmt.Errorf("--repo-root and --repo-domain must be set together")
	}
	if root != strings.TrimSpace(root) {
		return "", "", fmt.Errorf("--repo-root is padded")
	}
	if domain != strings.TrimSpace(domain) || strings.ContainsAny(domain, " \t\r\n") {
		return "", "", fmt.Errorf("--repo-domain is padded or contains whitespace")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("--repo-root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", fmt.Errorf("--repo-root does not resolve: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", fmt.Errorf("--repo-root: %w", err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("--repo-root is not a directory")
	}
	return resolved, domain, nil
}

func resolveServeGraphMarkerFile(configured string, noSeed bool) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured, nil
	}
	defaultPath, err := defaultRuntimeMarkerFile()
	if err != nil {
		if noSeed {
			return "", err
		}
		return "", nil
	}
	return selectServeGraphMarkerFile(configured, defaultPath, pathExists(defaultPath), noSeed), nil
}

func selectServeGraphMarkerFile(configured, defaultPath string, _ bool, noSeed bool) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		return configured
	}
	defaultPath = strings.TrimSpace(defaultPath)
	if defaultPath == "" {
		return ""
	}
	if noSeed {
		return defaultPath
	}
	return ""
}

func syncDefaultRuntimeMarkerFromLiveStore(ctx context.Context, markerPath, queryURL string, out io.Writer) error {
	markerPath = strings.TrimSpace(markerPath)
	if markerPath == "" {
		return nil
	}
	client, err := oxigraph.New(queryURL)
	if err != nil {
		return err
	}
	defer client.Close()
	markers, err := client.SeedMarkers(ctx)
	if err != nil {
		return err
	}
	if len(markers) == 0 {
		return nil
	}
	if len(markers) > 1 {
		return fmt.Errorf("live store has %d graph markers; refusing to choose one", len(markers))
	}
	liveCount, err := client.CountTriples(ctx)
	if err != nil {
		return err
	}
	marker := markers[0]
	if marker.TripleCount != liveCount {
		return fmt.Errorf("live marker triple count %d does not match live store count %d", marker.TripleCount, liveCount)
	}
	current, err := seedmeta.ReadMarkerFile(markerPath)
	if err == nil && current.Digest == marker.Digest && current.IRI == marker.IRI && current.TripleCount == marker.TripleCount {
		if err := reconcileRuntimeTransactionStamp(markerPath, marker, out); err != nil {
			return err
		}
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		return err
	}
	if err := reconcileRuntimeTransactionStamp(markerPath, marker, out); err != nil {
		return err
	}
	if out != nil {
		fmt.Fprintf(out, "sensei serve: refreshed runtime graph marker %s from live store (%s, %d triples)\n", markerPath, truncate(marker.Digest, 12), marker.TripleCount)
	}
	return nil
}

func reconcileRuntimeTransactionStamp(markerPath string, marker seedmeta.Marker, out io.Writer) error {
	txPath := seedmeta.RuntimeTransactionPath(markerPath)
	if strings.TrimSpace(txPath) == "" {
		return nil
	}
	data, err := os.ReadFile(txPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read runtime transaction stamp: %w", err)
	}
	stamp := seedmeta.ParseTransactionStamp(data)
	if runtimeTransactionMatchesMarker(stamp, marker) {
		return nil
	}
	if err := os.Remove(txPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale runtime transaction stamp: %w", err)
	}
	if out != nil {
		fmt.Fprintf(out, "sensei serve: removed stale runtime transaction stamp %s\n", txPath)
	}
	return nil
}

func runtimeTransactionMatchesMarker(stamp seedmeta.TransactionStamp, marker seedmeta.Marker) bool {
	if !stamp.Present {
		return false
	}
	if strings.TrimSpace(stamp.SeedDigest) != marker.Digest {
		return false
	}
	if strings.TrimSpace(stamp.SeedTripleCount) == "" {
		return true
	}
	n, err := strconv.ParseInt(strings.TrimSpace(stamp.SeedTripleCount), 10, 64)
	return err == nil && n == marker.TripleCount
}

// findServerBinary locates the awareness-graph server binary.
func findServerBinary() (string, error) {
	name := exeName("awareness-graph")
	// Check next to the sensei binary itself.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Check ./bin/
	if _, err := os.Stat("./bin/" + name); err == nil {
		return "./bin/" + name, nil
	}
	// Check PATH.
	if path, err := exec.LookPath("awareness-graph"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("awareness-graph binary not found (checked bin/, PATH)")
}

// findOxigraphBinary locates the oxigraph binary.
func findOxigraphBinary() (string, error) {
	name := exeName("oxigraph")
	// Check next to the sensei binary.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Check ./bin/
	if _, err := os.Stat("./bin/" + name); err == nil {
		return "./bin/" + name, nil
	}
	// Check PATH.
	if path, err := exec.LookPath("oxigraph"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("oxigraph binary not found (checked bin/, PATH)")
}

// waitForSPARQLHealthy polls until the endpoint answers a trivial ASK query.
func waitForSPARQLHealthy(url string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := checkSPARQLHealth(url); err == nil {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// waitForAddrListening polls addr until a bare TCP connection succeeds or
// timeout elapses. Used to confirm a freshly started awareness-graph child
// actually bound its port before this invocation treats itself as having
// proven exclusive ownership of that address — a dial-time-free port can
// still race, and marker-file mutation must never precede that proof
// (docs/design/serve-runtime-compatibility.md §3.6).
func waitForAddrListening(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func checkSPARQLHealth(url string) error {
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader("ASK {}"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := httpHealthClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func watchBackendHealth(ctx context.Context, url string, interval time.Duration, failThreshold int, errCh chan<- error) {
	if failThreshold < 1 {
		failThreshold = 1
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		err := checkSPARQLHealth(url)
		if err == nil {
			failures = 0
		} else {
			failures++
			if failures >= failThreshold {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// oxigraphServeArgs builds the Oxigraph child-process argv.
//
// Extracted so the flags are ASSERTABLE. A mutation removing a flag from an
// inline exec.Command survived the entire test suite: nothing proved we set it.
//
// --union-default-graph is DELIBERATELY ABSENT, and must stay absent until
// staging-graph visibility is resolved.
//
// It is tempting to add: once domains publish into their own named graphs, an
// unqualified { ?s ?p ?o } matches the DEFAULT graph only and returns ZERO
// rows, so every query in this codebase would silently empty. But named graphs
// ALREADY EXIST and are not domains. `sensei build` PUTs a candidate slice and
// a SECOND seed marker into urn:sensei:graph-staging:<marker> and then promotes
// it in one transaction (cmd_build.go). Union reads would expose that
// in-flight candidate -- and a duplicate marker -- to every concurrent briefing
// and metadata query, destroying the atomicity the staging design exists to
// provide.
//
// So the carrier (LoadGraph) lands proven while the read surface stays
// default-only. Enabling union reads requires first making staging graphs
// invisible to readers -- a separate change with its own proof.
func oxigraphServeArgs(location, bind string) []string {
	return []string{"serve", "--location", location, "--bind", bind}
}
