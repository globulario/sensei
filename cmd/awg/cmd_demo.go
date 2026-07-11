// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// runDemo is the zero-friction golden path: one command that stands up a private
// awareness graph and returns one real briefing. It orchestrates the whole
// machine — Oxigraph, a graph build, the gRPC server, and a briefing — on
// ephemeral ports in a temp dir, then tears it all down. Nothing touches the
// user's ports or any long-lived store.
//
//	sensei demo                     # brief the bundled payment-cold-start example
//	sensei demo --repo .            # brief the current repo (needs docs/awareness/)
//	sensei demo --file src/x.py     # brief a specific file
func runDemo(args []string) int {
	fs := flag.NewFlagSet("sensei demo", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", "", "project dir to demo (default: the current repo if it has docs/awareness/, else the bundled payment-cold-start example)")
	file := fs.String("file", "", "file to brief (default: the first entry in high_risk_files.yaml)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei demo [flags]

Stand up a private awareness graph and return one real briefing — in one command.
Starts Oxigraph + the gRPC server on ephemeral ports in a temp dir, compiles the
project's docs/awareness/ into the store, runs a briefing, and cleans up.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	dir, err := resolveDemoRepo(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: %v\n", err)
		return 1
	}
	awarenessDir := filepath.Join(dir, "docs", "awareness")
	if fi, err := os.Stat(awarenessDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei demo: %s has no docs/awareness/ — run `sensei init` there first, or pass --repo\n", dir)
		return 1
	}

	oxiBin, err := findOxigraphBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: %v\n  Run ./scripts/install.sh to fetch Oxigraph into bin/.\n", err)
		return 1
	}
	srvBin, err := findServerBinary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: %v\n  Run `make sensei` (or ./scripts/install.sh) to build the server into bin/.\n", err)
		return 1
	}
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}

	work, err := os.MkdirTemp("", "sensei-demo-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: %v\n", err)
		return 1
	}
	defer os.RemoveAll(work)

	oxiPort, grpcPort := freePort(), freePort()
	oxiBind := fmt.Sprintf("127.0.0.1:%d", oxiPort)
	oxiQuery := fmt.Sprintf("http://%s/query", oxiBind)
	storeURL := fmt.Sprintf("http://%s/store?default", oxiBind)
	grpcAddr := fmt.Sprintf("localhost:%d", grpcPort)
	marker := filepath.Join(work, "graph-authority.json")

	fmt.Printf("\nSensei demo — %s\n\n", displayPath(dir))

	// 1. Oxigraph (the local RDF store), as a child on an ephemeral port.
	_ = os.MkdirAll(filepath.Join(work, "oxi"), 0o755)
	oxiLog := filepath.Join(work, "oxigraph.log")
	oxi := exec.Command(oxiBin, "serve", "--location", filepath.Join(work, "oxi"), "--bind", oxiBind)
	if err := startWithLog(oxi, oxiLog); err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: start oxigraph: %v\n", err)
		return 1
	}
	defer stopProc(oxi)
	if !waitHTTPReady(oxiQuery, 15*time.Second) {
		fmt.Fprintf(os.Stderr, "sensei demo: oxigraph did not become ready\n%s\n", tailFile(oxiLog))
		return 1
	}
	fmt.Println("  ✓ store started")

	// 2. Compile docs/awareness/ into the store.
	buildOut, err := runSelf(self, "build", "-input", awarenessDir, "-store-url", storeURL, "-graph-marker-file", marker)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: build failed:\n%s\n", buildOut)
		return 1
	}
	fmt.Printf("  ✓ awareness loaded (%s)\n", describeTriples(buildOut))

	// 3. The briefing server, attached to the store we just loaded.
	srvLog := filepath.Join(work, "server.log")
	srv := exec.Command(srvBin, "-addr", fmt.Sprintf(":%d", grpcPort), "-oxigraph-url", oxiQuery,
		"-no-seed", "-allow-stale-seed", "-graph-marker-file", marker)
	if err := startWithLog(srv, srvLog); err != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: start server: %v\n", err)
		return 1
	}
	defer stopProc(srv)
	if !waitTCPReady(grpcAddr, 15*time.Second) {
		fmt.Fprintf(os.Stderr, "sensei demo: server did not become ready\n%s\n", tailFile(srvLog))
		return 1
	}
	fmt.Println("  ✓ briefing server ready")

	// 4. One real briefing.
	target := strings.TrimSpace(*file)
	if target == "" {
		target = firstHighRiskFile(awarenessDir)
	}
	if target == "" {
		fmt.Fprintf(os.Stderr, "sensei demo: no file to brief — pass --file, or add one to high_risk_files.yaml\n")
		return 1
	}
	fmt.Printf("  ✓ briefing generated\n\n")
	fmt.Printf("$ sensei briefing --file %s\n\n", target)

	briefOut, berr := runSelf(self, "briefing", "-addr", grpcAddr, "-file", target)
	clean := cleanBriefing(briefOut)
	fmt.Print(clean)
	if !strings.HasSuffix(clean, "\n") {
		fmt.Println()
	}
	if berr != nil {
		fmt.Fprintf(os.Stderr, "sensei demo: briefing failed\n%s\n", tailFile(srvLog))
		return 1
	}

	fmt.Printf("\nThat came from %s — edit an invariant, add your own, and run `sensei demo --repo .` on your project.\n",
		displayPath(filepath.Join(dir, "docs", "awareness")))
	return 0
}

// resolveDemoRepo picks what to demo: an explicit --repo, else the current repo
// if it looks like a Sensei project, else the bundled payment-cold-start example
// (resolved relative to the binary so it works from anywhere after install).
func resolveDemoRepo(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return filepath.Abs(explicit)
	}
	// Use the current directory only if it's a briefing-ready project — a
	// scaffolded docs/awareness/ with a high_risk_files.yaml to brief against.
	// (The Sensei repo root itself has docs/awareness/ but no high-risk list, so
	// a bare `sensei demo` there falls through to the bundled example.)
	if cwd, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(cwd, "docs", "awareness", "high_risk_files.yaml")); err == nil {
			return cwd, nil
		}
	}
	// Bundled example: <repo-root>/examples/payment-cold-start, where the binary
	// lives in <repo-root>/bin/. Try a couple of layouts.
	if exe, err := os.Executable(); err == nil {
		binDir := filepath.Dir(exe)
		for _, cand := range []string{
			filepath.Join(binDir, "..", "examples", "payment-cold-start"),
			filepath.Join(binDir, "examples", "payment-cold-start"),
		} {
			if fi, err := os.Stat(filepath.Join(cand, "docs", "awareness")); err == nil && fi.IsDir() {
				return filepath.Abs(cand)
			}
		}
	}
	if fi, err := os.Stat(filepath.Join("examples", "payment-cold-start", "docs", "awareness")); err == nil && fi.IsDir() {
		return filepath.Abs(filepath.Join("examples", "payment-cold-start"))
	}
	return "", fmt.Errorf("no project found — pass --repo <dir> (a directory with docs/awareness/)")
}

// firstHighRiskFile returns the first path listed in high_risk_files.yaml, so the
// demo briefs a file the project already marked as load-bearing.
func firstHighRiskFile(awarenessDir string) string {
	data, err := os.ReadFile(filepath.Join(awarenessDir, "high_risk_files.yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "- ") {
			return strings.TrimSpace(strings.Trim(strings.TrimPrefix(s, "- "), `"'`))
		}
	}
	return ""
}

var tripleRe = regexp.MustCompile(`(\d[\d,]*)\s+triples`)

// describeTriples pulls a human count out of the build output ("… 42 triples …").
func describeTriples(buildOut string) string {
	best := ""
	for _, m := range tripleRe.FindAllStringSubmatch(buildOut, -1) {
		best = m[1] // last match = the total load line
	}
	if best == "" {
		return "graph compiled"
	}
	return best + " triples"
}

// cleanBriefing drops the graph-provenance detail (digests, triple counts,
// transaction stamps) from a briefing so the demo shows the knowledge, not the
// plumbing. The single "Authority:" trust line and the briefing body stay.
func cleanBriefing(s string) string {
	noise := []string{"Live digest:", "Live triples:", "Seed digest:", "Build commit:", "Build time:", "Tx awg:", "Tx services:", "Tx detail:", "Detail:"}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		skip := false
		for _, n := range noise {
			if strings.HasPrefix(t, n) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, line)
		}
	}
	// collapse any run of blank lines left behind
	var packed []string
	for i, line := range out {
		if strings.TrimSpace(line) == "" && i > 0 && strings.TrimSpace(out[i-1]) == "" {
			continue
		}
		packed = append(packed, line)
	}
	return strings.TrimLeft(strings.Join(packed, "\n"), "\n")
}

func runSelf(self string, args ...string) (string, error) {
	cmd := exec.Command(self, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func startWithLog(cmd *exec.Cmd, logPath string) error {
	f, err := os.Create(logPath)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = f, f
	return cmd.Start()
}

func stopProc(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitTCPReady(addr string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			c.Close()
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

func waitHTTPReady(queryURL string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		resp, err := http.Post(queryURL, "application/sparql-query", strings.NewReader("ASK{}"))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func tailFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return "  " + strings.Join(lines, "\n  ")
}

func displayPath(p string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return filepath.Base(p)
			}
			return rel
		}
	}
	return p
}
