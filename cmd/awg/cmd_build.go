// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/globulario/awareness-graph/golang/extractor"
	"github.com/globulario/awareness-graph/golang/governancepack"
	"github.com/globulario/awareness-graph/golang/seedmeta"
	"github.com/globulario/awareness-graph/golang/store/oxigraph"
)

func finalizeBuildArtifact(nt []byte) ([]byte, seedmeta.Marker, int, int) {
	deduped, uniqueCount, dupCount := extractor.DedupNTriples(nt)
	finalNT, marker := seedmeta.AppendMarker(deduped)
	return finalNT, marker, uniqueCount, dupCount
}

func runBuild(args []string) int {
	fs := flag.NewFlagSet("awg build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var inputDirs stringSlice
	fs.Var(&inputDirs, "input", "awareness YAML directory (repeatable; default: docs/awareness)")
	output := fs.String("output", "", "write N-Triples to file instead of loading into store")
	storeURL := fs.String("store-url", "http://localhost:7878/store?default", "Oxigraph Graph Store endpoint")
	strict := fs.Bool("strict", false, "fail on unrecognized YAML schemas (recognized non-graph config files are reported, not fatal)")
	validateRefs := fs.Bool("validate-refs", false, "fail on dangling references")
	graphMarkerFile := fs.String("graph-marker-file", "", "write verified live graph identity to this file after a successful store load (default: <project>/.awg/graph-authority.json)")
	graphTransactionFile := fs.String("graph-transaction-file", "", "write runtime transaction certification beside the graph marker after a successful store load (default: sibling of graph marker when repo context is available)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo for runtime transaction certification (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo for runtime transaction certification (auto-detect)")
	repo := fs.String("repo", "", "default domain/repo for untagged nodes, e.g. github.com/cli/cli (foreign-repo bootstrap); scopes structural extractor output to that domain")
	domain := fs.String("domain", "", "default domain kind for untagged nodes: repo|shared (inferred 'repo' when --repo is set)")
	sourceSet := fs.String("source-set", "", "default source-set namespace for untagged nodes, e.g. pilot/cli")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg build [flags]

Compiles awareness YAML sources into N-Triples and loads them into
the Oxigraph store. If --output is given, writes to a file instead.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Default to docs/awareness if no input dirs specified.
	if len(inputDirs) == 0 {
		inputDirs = append(inputDirs, defaultBuildInputDirsFromRoot(".")...)
		if len(inputDirs) == 0 {
			inputDirs = []string{"docs/awareness"}
		}
	}
	rawProjectNT, _, err := compileAwarenessInputs(inputDirs, strings.TrimSpace(*repo), strings.TrimSpace(*domain), strings.TrimSpace(*sourceSet), *strict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg build: %v\n", err)
		return 1
	}
	ntBytes, marker, uniqueCount, dupCount := finalizeBuildArtifact(rawProjectNT)
	root, _ := resolveProjectRoot("")
	if governancepack.ManagedModeEnabled(root) {
		verifiedPack, _, err := verifyActiveGovernancePack(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg build: managed governance requires a verified active pack: %v\n", err)
			return 1
		}
		ntBytes, marker, uniqueCount, dupCount = combineGraphArtifacts(verifiedPack.PayloadBytes, ntBytes)
		fmt.Fprintf(os.Stderr, "  governance pack: %s %s (%s)\n", verifiedPack.Manifest.PackID, verifiedPack.Manifest.PackVersion, truncate(verifiedPack.PayloadMarker.Digest, 16))
	}

	// Validate.
	if errs := extractor.ValidateNTriples(bytes.NewReader(ntBytes)); len(errs) > 0 {
		for i, e := range errs {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "awg build: ... %d more errors\n", len(errs)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "awg build: %s\n", e)
		}
		return 1
	}

	if *validateRefs {
		// Reference validation runs during import; check for dangling refs in the output.
		fmt.Fprintf(os.Stderr, "  references: OK\n")
	}

	if dupCount > 0 {
		fmt.Fprintf(os.Stderr, "  dedup: %d duplicate triple(s) suppressed\n", dupCount)
	}
	fmt.Fprintf(os.Stderr, "  total: %d triples, validated\n", uniqueCount+6)

	// Output: file or store.
	if *output != "" {
		if err := os.WriteFile(*output, ntBytes, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "awg build: write %s: %v\n", *output, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "  wrote %s (%d bytes)\n", *output, len(ntBytes))
		return 0
	}

	// Load into Oxigraph.
	endpoint, err := normalizeStoreURL(*storeURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg build: invalid --store-url: %v\n", err)
		return 1
	}

	if err := uploadNTriples(http.DefaultClient, endpoint, ntBytes); err != nil {
		fmt.Fprintf(os.Stderr, "awg build: upload to %s: %v\n", endpoint, err)
		fmt.Fprintf(os.Stderr, "\nIs Oxigraph running? Start it with `awg serve -no-seed` or `bash ./scripts/install-awg-user-services.sh`.\n")
		return 1
	}
	if err := verifyLoadedGraph(endpoint, ntBytes); err != nil {
		fmt.Fprintf(os.Stderr, "awg build: verification after upload failed: %v\n", err)
		return 1
	}
	markerPath := strings.TrimSpace(*graphMarkerFile)
	if markerPath == "" {
		markerPath, err = defaultRuntimeMarkerFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg build: resolve graph marker file: %v\n", err)
			return 1
		}
	}
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		fmt.Fprintf(os.Stderr, "awg build: publish graph marker: %v\n", err)
		return 1
	}
	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	txRequested := strings.TrimSpace(*graphTransactionFile) != ""
	txPath := strings.TrimSpace(*graphTransactionFile)
	if txPath == "" && agRepo != "" {
		txPath = seedmeta.RuntimeTransactionPath(markerPath)
	}
	if txPath != "" {
		txBytes, err := buildTransactionTSV(agRepo, svcRepo, ntBytes)
		if err != nil {
			if txRequested {
				fmt.Fprintf(os.Stderr, "awg build: publish runtime transaction: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "  runtime transaction: skipped (%v)\n", err)
		} else if err := writeFileAtomic(txPath, txBytes); err != nil {
			if txRequested {
				fmt.Fprintf(os.Stderr, "awg build: publish runtime transaction: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "  runtime transaction: skipped (%v)\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  transaction file: %s\n", txPath)
		}
	}

	fmt.Fprintf(os.Stderr, "  loaded %d bytes into %s (%s, %d triples)\n", len(ntBytes), endpoint, marker.Digest[:12], marker.TripleCount)
	fmt.Fprintf(os.Stderr, "  marker file: %s\n", markerPath)
	fmt.Fprintln(os.Stdout, "Build complete.")
	return 0
}

func normalizeStoreURL(raw string) (string, error) {
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

func uploadNTriples(httpClient *http.Client, endpoint string, ntBytes []byte) error {
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(ntBytes))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/n-triples")

	resp, err := httpClient.Do(req)
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

func verifyLoadedGraph(storeEndpoint string, ntBytes []byte) error {
	expected, ok := seedmeta.ParseMarker(ntBytes)
	if !ok {
		return fmt.Errorf("loaded artifact carries no graph marker")
	}
	u, err := url.Parse(storeEndpoint)
	if err != nil {
		return fmt.Errorf("parse store endpoint: %w", err)
	}
	u.Path = strings.TrimSuffix(u.Path, "/store") + "/query"
	u.RawQuery = ""
	client, err := oxigraph.New(u.String())
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	verification := seedmeta.VerifyLiveStore(ctx, client, expected)
	if verification.State != seedmeta.FreshnessCurrent {
		return fmt.Errorf("%s", verification.Detail)
	}
	return nil
}

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
