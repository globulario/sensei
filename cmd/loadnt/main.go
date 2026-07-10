// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.loadnt
// @awareness file_role=build_tool_graph_loader
// @awareness implements=globular.awareness_graph:intent.awareness.loadnt_validates_before_loading
// @awareness implements=globular.awareness_graph:intent.awareness.oxigraph_is_external_runtime_state

// Command loadnt validates and loads an N-Triples file into Oxigraph.
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

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

const (
	loadExitOK        = 0
	loadExitRuntime   = 1
	loadExitUserError = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable core of loadnt.
// It validates N-Triples content before uploading — invalid data is rejected
// before any HTTP request reaches Oxigraph.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=build.loadnt
// @awareness implements=globular.awareness_graph:intent.awareness.loadnt_validates_before_loading
// @awareness enforces=globular.awareness_graph:invariant.awareness.rdf.ntriples_validated_before_write
// @awareness protects=globular.awareness_graph:failure_mode.awareness.rdf.unvalidated_ntriples_corrupt_store
// @awareness tested_by=cmd/loadnt/main_test.go:TestRunValidatesBeforeUpload
// @awareness risk=medium
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("loadnt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "path to .nt file to load (required)")
	oxigraphURL := fs.String("oxigraph-url", "http://localhost:7878/store?default", "Oxigraph Graph Store endpoint")
	fs.Usage = func() {
		fmt.Fprint(stderr, "usage: loadnt -input <file.nt> [-oxigraph-url <store-endpoint>]\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return loadExitUserError
	}
	if *input == "" {
		fmt.Fprintln(stderr, "loadnt: -input is required")
		return loadExitUserError
	}

	ntBytes, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(stderr, "loadnt: read input: %v\n", err)
		return loadExitUserError
	}
	if len(ntBytes) == 0 {
		fmt.Fprintln(stderr, "loadnt: input is empty")
		return loadExitUserError
	}
	if errs := extractor.ValidateNTriples(bytes.NewReader(ntBytes)); len(errs) > 0 {
		const maxReported = 20
		for i, e := range errs {
			if i >= maxReported {
				fmt.Fprintf(stderr, "loadnt: ... %d more validation errors omitted\n", len(errs)-i)
				break
			}
			fmt.Fprintf(stderr, "loadnt: %s\n", e)
		}
		fmt.Fprintf(stderr, "loadnt: %d N-Triples validation errors\n", len(errs))
		return loadExitRuntime
	}
	ntBytes, _ = seedmeta.AppendMarker(ntBytes)

	endpoint, err := normalizeStoreURL(*oxigraphURL)
	if err != nil {
		fmt.Fprintf(stderr, "loadnt: invalid -oxigraph-url: %v\n", err)
		return loadExitUserError
	}
	if err := uploadNTriples(http.DefaultClient, endpoint, ntBytes); err != nil {
		fmt.Fprintf(stderr, "loadnt: upload failed: %v\n", err)
		return loadExitRuntime
	}
	if err := verifyLoadedGraph(endpoint, ntBytes); err != nil {
		fmt.Fprintf(stderr, "loadnt: verification failed: %v\n", err)
		return loadExitRuntime
	}

	fmt.Fprintf(stdout, "loadnt: loaded %d bytes into %s\n", len(ntBytes), endpoint)
	return loadExitOK
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

// uploadNTriples PUTs validated N-Triples to the Oxigraph Graph Store endpoint.
// Oxigraph is external runtime state — this function does not cache, retry, or
// maintain any local copy. A failed upload leaves Oxigraph unchanged.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=build.loadnt
// @awareness implements=globular.awareness_graph:intent.awareness.oxigraph_is_external_runtime_state
// @awareness protects=globular.awareness_graph:failure_mode.awareness.rdf.unvalidated_ntriples_corrupt_store
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
	queryURL := u.String()
	client, err := oxigraph.New(queryURL)
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
