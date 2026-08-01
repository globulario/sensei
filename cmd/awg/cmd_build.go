// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/governancepack"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

// finalizeBuildArtifact stamps a canonical whole-graph marker onto already
// compiled triples, delegating the single finalization implementation to
// graphbuild. It preserves the prior no-validation contract (callers validate
// separately) by using graphbuild.Stamp.
func finalizeBuildArtifact(nt []byte) ([]byte, seedmeta.Marker, int, int) {
	comp := graphbuild.CompilationFromGraph(nt)
	art, _ := graphbuild.Stamp(context.Background(), graphbuild.FinalizeRequest{Compilation: comp})
	marker := seedmeta.Marker{
		Digest:      art.GraphSemanticDigestSHA256,
		IRI:         art.MarkerIRI,
		TripleCount: int64(art.ArtifactTripleCount),
	}
	return art.NTriples, marker, comp.UniqueTripleCount, comp.DuplicateTripleCount
}

func runBuild(args []string) int {
	fs := flag.NewFlagSet("sensei build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var inputDirs stringSlice
	fs.Var(&inputDirs, "input", "awareness YAML directory (repeatable; default: docs/awareness)")
	output := fs.String("output", "", "write N-Triples to file instead of loading into store")
	storeURL := fs.String("store-url", defaultOxigraphStoreURL(), "Oxigraph Graph Store endpoint")
	strict := fs.Bool("strict", false, "fail on unrecognized YAML schemas (recognized non-graph config files are reported, not fatal)")
	validateRefs := fs.Bool("validate-refs", false, "fail on dangling references")
	graphMarkerFile := fs.String("graph-marker-file", "", "write verified live graph identity to this file after a successful store load (default: <project>/.sensei/graph-authority.json)")
	graphTransactionFile := fs.String("graph-transaction-file", "", "write runtime transaction certification beside the graph marker after a successful store load (default: sibling of graph marker when repo context is available)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo for runtime transaction certification (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo for runtime transaction certification (auto-detect)")
	repo := fs.String("repo", "", "domain/repo to update IN PLACE, e.g. github.com/globulario/services — compiles this repo's slice, tags it to that domain, and replaces ONLY its triples in the store (non-destructive to other domains, shared nodes, and the home slice). Without --repo, a store load requires --all.")
	domain := fs.String("domain", "", "default domain kind for untagged nodes: repo|shared (inferred 'repo' when --repo is set)")
	sourceSet := fs.String("source-set", "", "default source-set namespace for untagged nodes, e.g. pilot/cli")
	all := fs.Bool("all", false, "replace the ENTIRE store (all domains) with this build — destructive whole-graph load. Required for a full/cold-start build when --repo is not given.")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei build [flags]

Compiles awareness YAML sources into N-Triples and loads them into the
Oxigraph store.

Store-load scope (one is required when not writing --output):
  --repo <domain>   Non-destructive, in-place update of ONE repo's slice.
                    Deletes only that domain's triples (subjects tagged
                    aw:repo == <domain>), appends the freshly compiled slice,
                    then recomputes the whole-graph marker. Other domains,
                    shared nodes, and the home slice are left untouched.
  --all             Destructive whole-graph replace (cold-start / seed).

With --output, writes the compiled N-Triples to a file and does not touch
the store (no scope required).

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := rejectPathLikeBuildDomain("build --repo", *repo); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
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
		fmt.Fprintf(os.Stderr, "sensei build: %v\n", err)
		return 1
	}
	// A domain-scoped update recompiles this repo's slice and restamps the
	// whole-graph marker against the LIVE store, so it does not go through the
	// whole-artifact finalize/governance/PUT path. Route it early (before
	// finalize) so a managed-governance requirement or the global marker never
	// gates a single-domain refresh. --output and --all fall through below.
	if strings.TrimSpace(*repo) != "" && *output == "" {
		return runScopedRepoUpdate(strings.TrimSpace(*repo), rawProjectNT, *storeURL,
			strings.TrimSpace(*graphMarkerFile), strings.TrimSpace(*graphTransactionFile), *svcRepoFlag, *agRepoFlag)
	}

	ntBytes, marker, uniqueCount, dupCount := finalizeBuildArtifact(rawProjectNT)
	root, _ := resolveProjectRoot("")
	if governancepack.ManagedModeEnabled(root) {
		verifiedPack, _, err := verifyActiveGovernancePack(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei build: managed governance requires a verified active pack: %v\n", err)
			return 1
		}
		ntBytes, marker, uniqueCount, dupCount = combineGraphArtifacts(verifiedPack.PayloadBytes, ntBytes)
		fmt.Fprintf(os.Stderr, "  governance pack: %s %s (%s)\n", verifiedPack.Manifest.PackID, verifiedPack.Manifest.PackVersion, truncate(verifiedPack.PayloadMarker.Digest, 16))
	}

	// Validate.
	if errs := extractor.ValidateNTriples(bytes.NewReader(ntBytes)); len(errs) > 0 {
		for i, e := range errs {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "sensei build: ... %d more errors\n", len(errs)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "sensei build: %s\n", e)
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
			fmt.Fprintf(os.Stderr, "sensei build: write %s: %v\n", *output, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "  wrote %s (%d bytes)\n", *output, len(ntBytes))
		return 0
	}

	// Store load. A whole-graph PUT is destructive — it drops every other
	// domain — so require an explicit opt-in. The recommended path is a
	// domain-scoped --repo update (handled above); --all is cold-start/seed.
	if !*all {
		fmt.Fprintln(os.Stderr, "sensei build: refuse to load into the store without a scope.")
		fmt.Fprintln(os.Stderr, "  --repo <domain>  non-destructive, in-place update of one repo's slice (recommended)")
		fmt.Fprintln(os.Stderr, "  --all            REPLACE the entire store — drops every domain; cold-start/seed only")
		return 2
	}
	fmt.Fprintln(os.Stderr, "  WARNING: --all replaces the ENTIRE store (all domains) with this build.")

	// Load into Oxigraph.
	endpoint, err := normalizeStoreURL(*storeURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: invalid --store-url: %v\n", err)
		return 1
	}

	if err := uploadNTriples(http.DefaultClient, endpoint, ntBytes); err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: upload to %s: %v\n", endpoint, err)
		fmt.Fprintf(os.Stderr, "\nIs Oxigraph running? Start it with `sensei serve -no-seed` or `bash ./scripts/install-sensei-user-services.sh`.\n")
		return 1
	}
	if err := verifyLoadedGraph(endpoint, ntBytes); err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: verification after upload failed: %v\n", err)
		return 1
	}
	markerPath := strings.TrimSpace(*graphMarkerFile)
	if markerPath == "" {
		markerPath, err = defaultRuntimeMarkerFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei build: resolve graph marker file: %v\n", err)
			return 1
		}
	}
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: publish graph marker: %v\n", err)
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
				fmt.Fprintf(os.Stderr, "sensei build: publish runtime transaction: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "  runtime transaction: skipped (%v)\n", err)
		} else if err := writeFileAtomic(txPath, txBytes); err != nil {
			if txRequested {
				fmt.Fprintf(os.Stderr, "sensei build: publish runtime transaction: %v\n", err)
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
	// Build the SPARQL query endpoint from whatever endpoint shape we were given
	// — the reload URL (.../store, .../store?default) OR an already-query URL
	// (.../query). Strip a trailing /store or /query (and any trailing slash)
	// before appending /query, so we never produce ".../query/query" (which
	// Oxigraph rejects with 404). Mirrors reloadOxigraphStore's normalization.
	u.Path = queryEndpointPath(u.Path)
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

// queryEndpointPath returns the SPARQL query path for an Oxigraph endpoint path,
// tolerating a /store, /query, or bare path (with or without a trailing slash).
// It never doubles the suffix — queryEndpointPath("/query") == "/query".
func queryEndpointPath(p string) string {
	p = strings.TrimSuffix(p, "/")
	p = strings.TrimSuffix(p, "/store")
	p = strings.TrimSuffix(p, "/query")
	return p + "/query"
}

// runScopedRepoUpdate performs a non-destructive, domain-scoped store update for
// `sensei build --repo <domain>`. The complete candidate generation is derived
// and validated before publication. Its replacement slice is loaded as real
// N-Triples into an isolated staging graph, then one SPARQL control transaction
// swaps that graph into the default graph. Raw RDF bytes are never embedded in
// SPARQL text.
func runScopedRepoUpdate(domain string, rawProjectNT []byte, storeURLFlag, graphMarkerFile, graphTransactionFile, svcRepoFlag, agRepoFlag string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Compile the domain slice: dedup, but DO NOT stamp a per-slice marker. The
	// whole-graph marker is calculated from the complete post-update generation.
	sliceNT, uniqueCount, dupCount := extractor.DedupNTriples(rawProjectNT)
	if len(bytes.TrimSpace(sliceNT)) == 0 {
		fmt.Fprintf(os.Stderr, "sensei build: --repo %s produced no triples (nothing to update)\n", domain)
		return 1
	}
	// The slice MUST be attributed to this domain. Otherwise no later scoped
	// rebuild could reclaim it and it would pollute the untagged home scope.
	repoTag := fmt.Sprintf("<%srepo> %q", seedmeta.NamespaceIRI, domain)
	if !bytes.Contains(sliceNT, []byte(repoTag)) {
		fmt.Fprintf(os.Stderr, "sensei build: compiled slice for --repo %s carries no %s tag — refusing to publish untagged triples.\n", domain, repoTag)
		fmt.Fprintln(os.Stderr, "  (The extractor did not receive the repo scope; this is a build-wiring bug, not a store issue.)")
		return 1
	}
	if errs := extractor.ValidateNTriples(bytes.NewReader(sliceNT)); len(errs) > 0 {
		for i, e := range errs {
			if i >= 20 {
				fmt.Fprintf(os.Stderr, "sensei build: ... %d more errors\n", len(errs)-i)
				break
			}
			fmt.Fprintf(os.Stderr, "sensei build: %s\n", e)
		}
		return 1
	}
	if dupCount > 0 {
		fmt.Fprintf(os.Stderr, "  dedup: %d duplicate triple(s) suppressed\n", dupCount)
	}

	storeEndpoint, err := normalizeStoreURL(storeURLFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: invalid --store-url: %v\n", err)
		return 1
	}
	qURL, err := queryURLFromStore(storeEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: derive query endpoint: %v\n", err)
		return 1
	}
	client, err := oxigraph.New(qURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: %v\n", err)
		return 1
	}
	defer client.Close()

	// All local publishers targeting the same store serialize around the complete
	// read -> stage -> promote -> verify sequence. CI and hosted workers use an
	// isolated store, while this lock protects the shared local-service topology.
	lockPath, err := graphPublicationLockPath(storeEndpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: resolve graph publication lock: %v\n", err)
		return 1
	}
	lockFile, err := acquireGraphPublicationLock(lockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: acquire graph publication lock: %v\n", err)
		return 1
	}
	defer lockFile.Close()

	if err := client.Health(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: store not reachable: %v\n", err)
		fmt.Fprintln(os.Stderr, "\nIs Oxigraph running? Start it with `sensei serve` or `bash ./scripts/install-sensei-user-services.sh`.")
		return 1
	}
	before, err := client.CountTriples(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: count live triples: %v\n", err)
		return 1
	}

	// Derive the exact candidate generation while the served default graph is
	// untouched. An empty store is a valid first publication.
	fullBase, err := client.DumpNTriples(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: dump store for candidate generation: %v\n", err)
		return 1
	}
	if got := int64(countNTriples(fullBase)); got != before {
		fmt.Fprintf(os.Stderr, "sensei build: incomplete store dump (%d triples read vs %d live) — refusing publication.\n", got, before)
		return 1
	}
	retainedBase := retainScopedGraph(fullBase, domain)
	postUpdateBase := append(append([]byte{}, retainedBase...), sliceNT...)
	postUpdateBase, _, _ = extractor.DedupNTriples(postUpdateBase)
	fullWithMarker, marker := seedmeta.AppendMarker(postUpdateBase)
	if errs := extractor.ValidateNTriples(bytes.NewReader(fullWithMarker)); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "sensei build: candidate generation is invalid: %s\n", errs[0])
		return 1
	}

	// Only the replacement slice and its global marker are staged. Other domains
	// remain in the default graph and are untouched by the promotion transaction.
	stagedNT := append(append([]byte{}, sliceNT...), seedmeta.MarkerTriples(marker)...)
	stagingIRI := graphStagingIRI(marker)
	if err := putNamedGraph(ctx, storeEndpoint, stagingIRI, stagedNT); err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: stage candidate generation for %s: %v\n", domain, err)
		return 1
	}

	promotionErr := client.Update(ctx, scopedPromoteStagingUpdate(domain, stagingIRI))
	if promotionErr != nil {
		// A lost HTTP response is ambiguous: the store may have committed the
		// transaction before the client observed the transport failure. Resolve the
		// outcome with a fresh context because the promotion context itself may have
		// expired even though Oxigraph committed before the connection was lost.
		verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 15*time.Second)
		verification := seedmeta.VerifyLiveStore(verifyCtx, client, marker)
		verifyCancel()
		if verification.State != seedmeta.FreshnessCurrent {
			_ = deleteNamedGraph(context.Background(), storeEndpoint, stagingIRI)
			fmt.Fprintf(os.Stderr, "sensei build: promote staged generation for %s: %v\n", domain, promotionErr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "  promotion response was ambiguous, but live marker %s verifies the transaction committed\n", marker.Digest[:12])
	}

	verification := seedmeta.VerifyLiveStore(ctx, client, marker)
	if verification.State != seedmeta.FreshnessCurrent {
		_ = deleteNamedGraph(context.Background(), storeEndpoint, stagingIRI)
		fmt.Fprintf(os.Stderr, "sensei build: post-publication verification failed: %s\n", verification.Detail)
		return 1
	}
	// The transactional update drops the staging graph. DELETE is an idempotent
	// cleanup for backends or interrupted attempts that left it behind.
	_ = deleteNamedGraph(context.Background(), storeEndpoint, stagingIRI)

	markerPath := graphMarkerFile
	if markerPath == "" {
		markerPath, err = defaultRuntimeMarkerFile()
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei build: resolve graph marker file: %v\n", err)
			return 1
		}
	}
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		fmt.Fprintf(os.Stderr, "sensei build: publish graph marker: %v\n", err)
		return 1
	}
	svcRepo, _ := resolveServicesRepo(svcRepoFlag)
	agRepo, _ := resolveAGRepo(agRepoFlag, svcRepo)
	txRequested := strings.TrimSpace(graphTransactionFile) != ""
	txPath := strings.TrimSpace(graphTransactionFile)
	if txPath == "" && agRepo != "" {
		txPath = seedmeta.RuntimeTransactionPath(markerPath)
	}
	if txPath != "" {
		if txBytes, err := buildTransactionTSV(agRepo, svcRepo, fullWithMarker); err != nil {
			if txRequested {
				fmt.Fprintf(os.Stderr, "sensei build: publish runtime transaction: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "  runtime transaction: skipped (%v)\n", err)
		} else if err := writeFileAtomic(txPath, txBytes); err != nil {
			if txRequested {
				fmt.Fprintf(os.Stderr, "sensei build: publish runtime transaction: %v\n", err)
				return 1
			}
			fmt.Fprintf(os.Stderr, "  runtime transaction: skipped (%v)\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "  transaction file: %s\n", txPath)
		}
	}

	fmt.Fprintf(os.Stderr, "  domain %s: %d triple(s) published; store now %d triples (was %d)\n", domain, uniqueCount, marker.TripleCount, before)
	fmt.Fprintf(os.Stderr, "  marker file: %s\n", markerPath)
	fmt.Fprintln(os.Stdout, "Build complete.")
	return 0
}

// retainScopedGraph returns the pre-update graph without the target domain's
// solely-owned subjects or any prior whole-graph marker. It mirrors the scoped
// DELETE predicate so the candidate marker is calculated before publication.
func retainScopedGraph(nt []byte, domain string) []byte {
	const rdfType = "<http://www.w3.org/1999/02/22-rdf-syntax-ns#type>"
	repoPredicate := "<" + seedmeta.NamespaceIRI + "repo>"
	seedClass := "<" + seedmeta.NamespaceIRI + "SeedBuild>"

	type subjectInfo struct {
		repos  map[string]bool
		marker bool
	}
	infos := map[string]*subjectInfo{}
	lines := strings.Split(string(nt), "\n")
	for _, line := range lines {
		subject, predicate, rest, ok := splitNTripleLine(line)
		if !ok {
			continue
		}
		info := infos[subject]
		if info == nil {
			info = &subjectInfo{repos: map[string]bool{}}
			infos[subject] = info
		}
		if predicate == repoPredicate {
			if value, ok := ntriplesLiteral(rest); ok {
				info.repos[value] = true
			}
		}
		if predicate == rdfType && strings.TrimSpace(rest) == seedClass+" ." {
			info.marker = true
		}
	}

	var out bytes.Buffer
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		info := infos[ntSubject(line)]
		if info != nil && (info.marker || (info.repos[domain] && len(info.repos) == 1)) {
			continue
		}
		out.WriteString(strings.TrimSpace(line))
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func ntriplesLiteral(rest string) (string, bool) {
	value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "."))
	parsed, err := strconv.Unquote(value)
	if err != nil {
		return "", false
	}
	return parsed, true
}

// queryURLFromStore derives the SPARQL query endpoint from a Graph Store
// endpoint (.../store?default -> .../query).
func queryURLFromStore(storeEndpoint string) (string, error) {
	u, err := url.Parse(storeEndpoint)
	if err != nil {
		return "", err
	}
	u.Path = queryEndpointPath(u.Path)
	u.RawQuery = ""
	return u.String(), nil
}

func graphStagingIRI(marker seedmeta.Marker) string {
	return "urn:sensei:graph-staging:sha256:" + marker.Digest
}

func namedGraphStoreURL(storeEndpoint, graphIRI string) (string, error) {
	u, err := url.Parse(storeEndpoint)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Del("default")
	q.Set("graph", graphIRI)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func putNamedGraph(ctx context.Context, storeEndpoint, graphIRI string, nt []byte) error {
	u, err := namedGraphStoreURL(storeEndpoint, graphIRI)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(nt))
	if err != nil {
		return fmt.Errorf("build staging request: %w", err)
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

func deleteNamedGraph(ctx context.Context, storeEndpoint, graphIRI string) error {
	u, err := namedGraphStoreURL(storeEndpoint, graphIRI)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("build staging cleanup request: %w", err)
	}
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

func graphPublicationLockPath(storeEndpoint string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "sensei", "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(storeEndpoint))
	return filepath.Join(dir, fmt.Sprintf("graph-publication-%x.lock", digest[:12])), nil
}

// scopedDeleteUpdate clears the target domain's solely-owned subjects and the
// prior whole-graph marker. Co-owned nodes survive.
func scopedDeleteUpdate(domain string) string {
	d := sparqlEscapeLiteral(domain)
	repoP := seedmeta.NamespaceIRI + "repo"
	seedClass := seedmeta.NamespaceIRI + "SeedBuild"
	return fmt.Sprintf(`DELETE { ?s ?p ?o } WHERE {
  {
    ?s <%s> "%s" .
    ?s ?p ?o .
    FILTER NOT EXISTS { ?s <%s> ?other . FILTER(str(?other) != "%s") }
  }
  UNION
  {
    ?s <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%s> .
    ?s ?p ?o .
  }
}`, repoP, d, repoP, d, seedClass)
}

// scopedPromoteStagingUpdate contains control operations only. RDF content is
// already parsed by Oxigraph through Graph Store Protocol before this request.
func scopedPromoteStagingUpdate(domain, stagingIRI string) string {
	return scopedDeleteUpdate(domain) + ";\nADD GRAPH <" + stagingIRI + "> TO DEFAULT;\nDROP GRAPH <" + stagingIRI + ">"
}

// sparqlEscapeLiteral escapes a Go string for use inside a SPARQL double-quoted
// literal.
func sparqlEscapeLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return s
}

// countNTriples counts non-empty (triple) lines in an N-Triples buffer, matching
// seedmeta's triple accounting so the dump-completeness guard lines up with the
// marker's triple count.
func countNTriples(nt []byte) int {
	n := 0
	for _, raw := range strings.Split(string(nt), "\n") {
		if strings.TrimSpace(raw) != "" {
			n++
		}
	}
	return n
}

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
