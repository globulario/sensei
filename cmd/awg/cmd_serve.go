// SPDX-License-Identifier: Apache-2.0

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
	"strings"
	"syscall"
	"time"
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
)

var httpHealthClient = &http.Client{Timeout: 1 * time.Second}

func runServe(args []string) int {
	fs := flag.NewFlagSet("sensei serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", defaultServiceListen(), "gRPC listen address")
	oxigraphBind := fs.String("oxigraph-bind", defaultOxigraphBind(), "Oxigraph listen address")
	noSeed := fs.Bool("no-seed", false, "skip the embedded Globular seed (cold-start projects: build your own graph with `sensei build`)")
	allowStaleSeed := fs.Bool("allow-stale-seed", false, "allow startup when the live store is missing the embedded seed marker")
	graphMarkerFile := fs.String("graph-marker-file", "", "runtime graph marker file for --no-seed authority checks (default: <project>/.sensei/graph-authority.json)")
	dataDir := fs.String("data", "", "Oxigraph data directory (default: ~/.local/share/sensei/oxigraph)")
	noOxigraph := fs.Bool("no-oxigraph", false, "don't start Oxigraph (use an external instance)")
	homeDomain := fs.String("home-domain", "", "domain key for untagged host-project nodes (cold-start non-Globular deployments set their own; default: globular)")
	enablePropose := fs.Bool("enable-propose", false, "enable the Propose RPC agent write path (writes validated candidates under docs/awareness/candidates/; off by default)")
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

	// Resolve data directory.
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

	oxigraphURL := fmt.Sprintf("http://%s/query", *oxigraphBind)
	var oxiCmd *exec.Cmd

	// ── Start Oxigraph ──────────────────────────────────────────────────
	if !*noOxigraph {
		// Check if something is already listening on the port.
		if conn, err := net.DialTimeout("tcp", *oxigraphBind, 500*time.Millisecond); err == nil {
			conn.Close()
			fmt.Fprintf(os.Stderr, "sensei serve: port %s already in use — using existing Oxigraph\n", *oxigraphBind)
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

			oxiCmd = exec.Command(oxiBin, "serve", "--location", data, "--bind", *oxigraphBind)
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

	srvArgs := []string{"-addr", *addr, "-oxigraph-url", oxigraphURL}
	if *noSeed {
		srvArgs = append(srvArgs, "-no-seed")
	}
	if *allowStaleSeed {
		srvArgs = append(srvArgs, "-allow-stale-seed")
	}
	if *noSeed {
		markerPath := *graphMarkerFile
		if markerPath == "" {
			markerPath, err = defaultRuntimeMarkerFile()
			if err != nil {
				fmt.Fprintf(os.Stderr, "sensei serve: resolve graph marker file: %v\n", err)
				if oxiCmd != nil {
					oxiCmd.Process.Signal(syscall.SIGTERM)
					oxiCmd.Wait()
				}
				return 1
			}
		}
		srvArgs = append(srvArgs, "-graph-marker-file", markerPath)
	}
	if *homeDomain != "" {
		srvArgs = append(srvArgs, "-home-domain", *homeDomain)
	}
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
		srvArgs = append(srvArgs, "-awareness-dir", filepath.Join(root, "docs", "awareness"))
	}
	srvCmd := exec.Command(srvBin, srvArgs...)
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
	fmt.Fprintf(os.Stderr, "sensei: awareness-graph started (pid %d, addr=%s)\n", srvCmd.Process.Pid, *addr)

	// ── Signal handling ─────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	monitorCtx, monitorCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer monitorCancel()
	backendErrCh := make(chan error, 1)
	go watchBackendHealth(monitorCtx, oxigraphURL, backendHealthPollInterval, backendHealthFailThreshold, backendErrCh)

	// Wait for either child to exit or a signal.
	doneCh := make(chan error, 2)
	go func() { doneCh <- srvCmd.Wait() }()
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

	// Stop both processes.
	srvCmd.Process.Signal(syscall.SIGTERM)
	if oxiCmd != nil {
		oxiCmd.Process.Signal(syscall.SIGTERM)
	}

	// Give them a moment to exit cleanly.
	timer := time.AfterFunc(5*time.Second, func() {
		srvCmd.Process.Kill()
		if oxiCmd != nil {
			oxiCmd.Process.Kill()
		}
	})
	srvCmd.Wait()
	if oxiCmd != nil {
		oxiCmd.Wait()
	}
	timer.Stop()

	fmt.Fprintf(os.Stderr, "sensei: stopped\n")
	return exitCode
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
