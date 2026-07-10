// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/governancepack"
	"github.com/globulario/sensei/golang/seedmeta"
)

func runRebuild(args []string) int {
	fs := flag.NewFlagSet("awg rebuild", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	oxigraphURL := fs.String("oxigraph-url", "http://localhost:7878/store?default", "Oxigraph Graph Store endpoint")
	graphMarkerFile := fs.String("graph-marker-file", "", "write verified live graph identity to this file after a successful reload (default: <project>/.awg/graph-authority.json)")
	checkMode := fs.Bool("check", false, "compare only, exit 1 if stale (CI mode)")
	noReload := fs.Bool("no-runtime-reload", false, "skip Oxigraph PUT")
	strict := fs.Bool("strict", false, "deprecated: rebuild now fails on reload/verification errors unless --no-runtime-reload is set")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg rebuild [flags]

Rebuild awareness.nt from YAML sources and optionally reload Oxigraph.

Steps:
  1. Scan YAML sources from both repos (awareness-graph + services)
  2. Convert to N-Triples via the extractor library
  3. Validate the output
  4. Update embeddata/awareness.nt (idempotent — only writes if changed)
  5. Update embeddata/awareness.transaction.tsv with the certified cross-repo inputs
  6. PUT to Oxigraph if available

Use --check for CI: regenerate in memory, compare with committed seed, exit 1 if stale.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if root, err := resolveProjectRoot(""); err == nil && governancepack.ManagedModeEnabled(root) {
		if _, _, err := verifyActiveGovernancePack(root); err != nil {
			fmt.Fprintf(os.Stderr, "awg rebuild: managed governance requires a verified active pack: %v\n", err)
			return 1
		}
	}

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)

	if svcRepo == "" && agRepo == "" {
		fmt.Fprintln(os.Stderr, "awg rebuild: cannot find services or awareness-graph repo")
		fmt.Fprintln(os.Stderr, "  run from inside a checkout, or set --services-repo / --ag-repo")
		return 1
	}
	if err := ensureCrossRepoRebuildPrereqs(agRepo, svcRepo); err != nil {
		fmt.Fprintf(os.Stderr, "awg rebuild: %v\n", err)
		return 1
	}

	inputDirs, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg rebuild: %v\n", err)
		return 1
	}
	if len(inputDirs) == 0 {
		fmt.Fprintln(os.Stderr, "awg rebuild: no input directories found")
		return 1
	}

	seedPath := ""
	transactionPath := ""
	if agRepo != "" {
		seedPath = filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.nt")
		transactionPath = defaultTransactionPath(agRepo)
	}

	// Generate N-Triples.
	fmt.Println("Scanning YAML sources...")
	ntBytes, totalTriples, yamlCount, genErr := generateNT(inputDirs, intentDir, svcRepo, agRepo)
	if genErr != nil {
		fmt.Fprintf(os.Stderr, "awg rebuild: %v\n", genErr)
		return 1
	}
	fmt.Printf("  YAML files scanned: %d\n", yamlCount)
	fmt.Printf("  triples generated:  %d\n", totalTriples)

	txBytes, err := buildTransactionTSV(agRepo, svcRepo, ntBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg rebuild: build transaction stamp: %v\n", err)
		return 1
	}

	// Validate.
	if errs := extractor.ValidateNTriples(bytes.NewReader(ntBytes)); len(errs) > 0 {
		for i, e := range errs {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "  ... %d more\n", len(errs)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "  validation: %s\n", e)
		}
		fmt.Fprintf(os.Stderr, "awg rebuild: %d validation errors\n", len(errs))
		return 1
	}
	fmt.Println("  validation:         ok")

	// Check mode.
	if *checkMode {
		if seedPath == "" {
			fmt.Fprintln(os.Stderr, "awg rebuild --check: cannot find embeddata path")
			return 1
		}
		committed, err := os.ReadFile(seedPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg rebuild: read seed: %v\n", err)
			return 1
		}
		c := evaluateSeedFreshness(committed, ntBytes, generateAgOnlyNT(agRepo))
		txCommitted, err := os.ReadFile(transactionPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg rebuild: read transaction stamp: %v\n", err)
			return 1
		}
		tx := evaluateBuildTransactionFreshness(txCommitted, txBytes)
		if c.level == auditPASS {
			fmt.Println("\nCheck mode: fresh (no changes)")
			if c.summary != "current" {
				fmt.Printf("  %s\n", c.summary)
			}
			if tx.summary != "current" {
				fmt.Printf("  transaction stamp: %s\n", tx.summary)
				for i, detail := range tx.details {
					if i >= 5 {
						fmt.Printf("    ... %d more transaction drift lines\n", len(tx.details)-i)
						break
					}
					fmt.Printf("    %s\n", detail)
				}
			}
			return 0
		}
		newLines := bytes.Count(ntBytes, []byte("\n"))
		oldLines := bytes.Count(committed, []byte("\n"))
		fmt.Fprintf(os.Stderr, "\n  STALE: embeddata/awareness.nt\n")
		fmt.Fprintf(os.Stderr, "    committed: %d lines\n    generated: %d lines\n", oldLines, newLines)
		if c.summary != "" {
			fmt.Fprintf(os.Stderr, "    %s\n", c.summary)
		}
		for i, detail := range c.details {
			if i >= 5 {
				fmt.Fprintf(os.Stderr, "    ... %d more owned drift lines\n", len(c.details)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "    %s\n", detail)
		}
		fmt.Fprintf(os.Stderr, "\nRun 'awg rebuild' and commit the result.\n")
		return 1
	}

	// Update embeddata.
	seedUpdated := false
	txUpdated := false

	// Oxigraph reload.
	if *noReload {
		if seedPath != "" {
			var err error
			seedUpdated, txUpdated, err = updateArtifactBundle(seedPath, ntBytes, transactionPath, txBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "awg rebuild: persist local artifacts: %v\n", err)
				return 1
			}
		}
		printArtifactUpdateStatus(seedPath, transactionPath, seedUpdated, txUpdated)
		fmt.Println("  Oxigraph reload:    skipped (--no-runtime-reload)")
	} else {
		if *strict {
			fmt.Println("  Oxigraph reload:    strict mode is now implicit when reload is enabled")
		}
		if err := reloadOxigraphStore(ntBytes, *oxigraphURL); err != nil {
			fmt.Fprintf(os.Stderr, "awg rebuild: Oxigraph reload failed: %v\n", err)
			return 1
		}
		if err := verifyLoadedGraph(*oxigraphURL, ntBytes); err != nil {
			fmt.Fprintf(os.Stderr, "awg rebuild: live-store verification failed: %v\n", err)
			return 1
		}
		if seedPath != "" {
			var err error
			seedUpdated, txUpdated, err = updateArtifactBundle(seedPath, ntBytes, transactionPath, txBytes)
			if err != nil {
				fmt.Fprintf(os.Stderr, "awg rebuild: local artifacts could not be promoted after live verification; runtime graph was refreshed but embeddata is unchanged: %v\n", err)
				return 1
			}
		}
		printArtifactUpdateStatus(seedPath, transactionPath, seedUpdated, txUpdated)
		marker, ok := seedmeta.ParseMarker(ntBytes)
		if !ok {
			fmt.Fprintln(os.Stderr, "awg rebuild: rebuilt artifact carries no graph marker")
			return 1
		}
		markerPath := strings.TrimSpace(*graphMarkerFile)
		if markerPath == "" {
			resolved, err := defaultRuntimeMarkerFile()
			if err != nil {
				fmt.Fprintf(os.Stderr, "awg rebuild: resolve graph marker file: %v\n", err)
				return 1
			}
			markerPath = resolved
		}
		if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
			fmt.Fprintf(os.Stderr, "awg rebuild: publish graph marker: %v\n", err)
			return 1
		}
		fmt.Println("  Oxigraph reload:    ok")
		fmt.Println("  Live verification:  ok")
		fmt.Printf("  Graph marker file:  %s\n", markerPath)
	}

	fmt.Println("\nDone.")
	return 0
}

func generateNT(inputDirs []string, intentDir, svcRepo, agRepo string) ([]byte, int, int, error) {
	return generateNTWithOwnership(inputDirs, intentDir, []string{agRepo, svcRepo}, svcRepo)
}

func generateNTWithOwnership(inputDirs []string, intentDir string, stripPathPrefixes []string, servicesOwnershipRepo string) ([]byte, int, int, error) {
	var buf bytes.Buffer
	opts := extractor.ImportDirOptions{
		StripPathPrefixes:   stripPathPrefixes,
		SkipNestedGenerated: true,
	}
	cleanup := func() {}
	defer cleanup()
	if servicesOwnershipRepo != "" {
		normalized, stripPrefix, nextCleanup, err := normalizeInputDirsForOwnership(inputDirs, servicesOwnershipRepo)
		if err != nil {
			return nil, 0, 0, err
		}
		inputDirs = normalized
		cleanup = nextCleanup
		// The staged services-generated files live under a per-run temp root;
		// strip it so authoredIn is the stable repo-relative path, never a
		// /tmp/awg-services-generated-NNNN/... path that drifts every run and
		// keeps the embeddata-freshness gate permanently un-armable.
		if stripPrefix != "" {
			opts.StripPathPrefixes = append(opts.StripPathPrefixes, stripPrefix)
		}
	}

	var totalTriples, yamlCount int
	for _, dir := range inputDirs {
		emitter, report, err := extractor.ImportAwarenessDirWithOpts(dir, &buf, opts)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("import %s: %w", dir, err)
		}
		totalTriples += emitter.Triples
		yamlCount += len(report.Files)
	}
	if intentDir != "" {
		emitter, report, err := extractor.ImportAwarenessDirWithOpts(intentDir, &buf, opts)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("import intent %s: %w", intentDir, err)
		}
		totalTriples += emitter.Triples
		yamlCount += len(report.Files)
	}
	// Canonical dedup — same computation yaml2nt applies to the committed
	// seed, so freshness comparisons compare like with like.
	deduped, uniqueCount, _ := extractor.DedupNTriples(buf.Bytes())
	stamped, _ := seedmeta.AppendMarker(deduped)
	return stamped, uniqueCount + 5, yamlCount, nil
}

func normalizeInputDirsForOwnership(inputDirs []string, svcRepo string) ([]string, string, func(), error) {
	svcGenerated := filepath.Clean(servicesGeneratedDir(svcRepo))
	// The repo-relative subpath we want to see in authoredIn (e.g.
	// docs/awareness/generated), mirrored under the temp root below so that
	// stripping the root yields a stable repo-relative path.
	relSub, err := filepath.Rel(svcRepo, svcGenerated)
	if err != nil || strings.HasPrefix(relSub, "..") {
		relSub = filepath.Join("docs", "awareness", "generated") // canonical fallback
	}
	filtered, stripPrefix, cleanup, err := filteredServicesGeneratedDir(svcGenerated, relSub)
	if err != nil {
		return nil, "", nil, err
	}
	out := make([]string, 0, len(inputDirs))
	for _, dir := range inputDirs {
		if filepath.Clean(dir) == svcGenerated {
			out = append(out, filtered)
			continue
		}
		out = append(out, dir)
	}
	return out, stripPrefix, cleanup, nil
}

// filteredServicesGeneratedDir stages the services-generated YAML (minus the
// awareness-graph-owned awareness_graph_* files) under a temp root, MIRRORING the
// repo-relative subpath relSub so that stripping the temp root (registered as a
// path-prefix by the caller) yields a stable repo-relative authoredIn
// (docs/awareness/generated/<f>) rather than a per-run /tmp/...-NNNN/<f> path.
// Returns the dir to import, the temp root to register as a strip prefix, and a
// cleanup func.
func filteredServicesGeneratedDir(src, relSub string) (string, string, func(), error) {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return src, "", func() {}, nil
		}
		return "", "", nil, fmt.Errorf("stat services generated: %w", err)
	}
	tmpDir, err := os.MkdirTemp("", "awg-services-generated-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("mktemp services generated: %w", err)
	}
	mirror := filepath.Join(tmpDir, relSub)
	if err := os.MkdirAll(mirror, 0o755); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", "", nil, fmt.Errorf("mkdir mirror: %w", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", "", nil, fmt.Errorf("read services generated: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		if strings.HasPrefix(name, "awareness_graph_") {
			continue
		}
		if err := os.Symlink(filepath.Join(src, name), filepath.Join(mirror, name)); err != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", nil, fmt.Errorf("link %s: %w", name, err)
		}
	}
	return mirror, tmpDir, func() { _ = os.RemoveAll(tmpDir) }, nil
}

func updateSeedFile(ntBytes []byte, seedPath string) (bool, error) {
	newHash := sha256.Sum256(ntBytes)
	existing, err := os.ReadFile(seedPath)
	if err == nil {
		oldHash := sha256.Sum256(existing)
		if newHash == oldHash {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(seedPath), 0o755); err != nil {
		return false, fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(seedPath, ntBytes, 0o644); err != nil {
		return false, fmt.Errorf("write: %w", err)
	}
	return true, nil
}

func updateArtifactBundle(seedPath string, seedBytes []byte, transactionPath string, txBytes []byte) (bool, bool, error) {
	type fileUpdate struct {
		path    string
		content []byte
		updated bool
		tmpPath string
	}
	updates := []*fileUpdate{
		{path: seedPath, content: seedBytes},
		{path: transactionPath, content: txBytes},
	}
	for _, update := range updates {
		existing, err := os.ReadFile(update.path)
		if err == nil {
			if sha256.Sum256(existing) == sha256.Sum256(update.content) {
				continue
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, false, fmt.Errorf("read %s: %w", update.path, err)
		}
		if err := os.MkdirAll(filepath.Dir(update.path), 0o755); err != nil {
			return false, false, fmt.Errorf("mkdir %s: %w", filepath.Dir(update.path), err)
		}
		tmp, err := os.CreateTemp(filepath.Dir(update.path), filepath.Base(update.path)+".tmp-*")
		if err != nil {
			return false, false, fmt.Errorf("temp %s: %w", update.path, err)
		}
		if _, err := tmp.Write(update.content); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return false, false, fmt.Errorf("write temp %s: %w", update.path, err)
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmp.Name())
			return false, false, fmt.Errorf("close temp %s: %w", update.path, err)
		}
		update.updated = true
		update.tmpPath = tmp.Name()
	}
	for _, update := range updates {
		if !update.updated {
			continue
		}
		if err := os.Rename(update.tmpPath, update.path); err != nil {
			_ = os.Remove(update.tmpPath)
			return updates[0].updated, updates[1].updated, fmt.Errorf("promote %s: %w", update.path, err)
		}
	}
	return updates[0].updated, updates[1].updated, nil
}

func printArtifactUpdateStatus(seedPath, transactionPath string, seedUpdated, txUpdated bool) {
	if seedPath == "" {
		return
	}
	if seedUpdated {
		fmt.Printf("  embeddata updated:  yes (%s)\n", seedPath)
	} else {
		fmt.Println("  embeddata updated:  no (already current)")
	}
	if txUpdated {
		fmt.Printf("  transaction stamp: yes (%s)\n", transactionPath)
	} else {
		fmt.Println("  transaction stamp: no (already current)")
	}
}

func reloadOxigraphStore(ntBytes []byte, rawURL string) error {
	endpoint, err := normalizeOxigraphURL(rawURL)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	// PUT REPLACES the default graph (SPARQL Graph Store Protocol); POST would
	// merge/append, so removed triples (e.g. a candidate promoted to a
	// realization) would linger in the live store. A rebuild is a clean refresh,
	// so it must replace.
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(ntBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/n-triples")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func normalizeOxigraphURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("host is required")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/store"
	}
	if strings.HasSuffix(u.Path, "/query") {
		u.Path = strings.TrimSuffix(u.Path, "/query") + "/store"
	}
	if u.RawQuery == "" {
		u.RawQuery = "default"
	}
	return u.String(), nil
}
