// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

func TestPackagingManifest_ParseAndDefaults(t *testing.T) {
	p := filepath.Join("..", "..", "packaging", "metadata", "awareness-graph", "package.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse manifest json: %v", err)
	}
	if m["type"] != "service" {
		t.Fatalf("type=%v, want service", m["type"])
	}
	if m["service_id"] != "globular.awareness_graph.AwarenessGraph" {
		t.Fatalf("service_id=%v, want globular.awareness_graph.AwarenessGraph", m["service_id"])
	}
	if m["entrypoint"] != "bin/awareness-graph" {
		t.Fatalf("entrypoint=%v, want bin/awareness-graph", m["entrypoint"])
	}
	defaults, ok := m["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("defaults missing or invalid: %T", m["defaults"])
	}
	if defaults["spec"] != "specs/awareness_graph_service.yaml" {
		t.Fatalf("defaults.spec=%v, want specs/awareness_graph_service.yaml", defaults["spec"])
	}
	if defaults["proto"] != "proto/awareness_graph.proto" {
		t.Fatalf("defaults.proto=%v, want proto/awareness_graph.proto", defaults["proto"])
	}
	runtimeDefaults, ok := m["runtime_defaults"].(map[string]any)
	if !ok {
		t.Fatalf("runtime_defaults missing or invalid: %T", m["runtime_defaults"])
	}
	if runtimeDefaults["port"] != float64(10120) || runtimeDefaults["proxy"] != float64(10121) {
		t.Fatalf("runtime_defaults port/proxy=%v/%v, want 10120/10121", runtimeDefaults["port"], runtimeDefaults["proxy"])
	}
	if runtimeDefaults["oxigraph_query_url"] != "http://localhost:7878/query" {
		t.Fatalf("runtime_defaults.oxigraph_query_url=%v, want http://localhost:7878/query", runtimeDefaults["oxigraph_query_url"])
	}
	if runtimeDefaults["query_exposed_as_sparql"] != false {
		t.Fatalf("runtime_defaults.query_exposed_as_sparql=%v, want false", runtimeDefaults["query_exposed_as_sparql"])
	}
}

func TestPackagingSpec_HasExpectedServiceDefaults(t *testing.T) {
	p := filepath.Join("..", "..", "packaging", "specs", "awareness_graph_service.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	s := string(b)
	checks := []string{
		"name: awareness-graph",
		"service_id: globular.awareness_graph.AwarenessGraph",
		"entrypoint: awareness-graph",
		"Environment=AW_OXIGRAPH_QUERY_URL=http://localhost:7878/query",
		"Environment=AW_REQUIRE_STORE=false",
		"globular-awareness-graph.service",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Fatalf("spec missing required content: %q", c)
		}
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "queryrequest.sparql") || strings.Contains(lower, "raw sparql endpoint") {
		t.Fatal("spec must not introduce SPARQL passthrough exposure fields")
	}
}

func TestPackagingPaths_ProtoAndSpecExist(t *testing.T) {
	protoPath := filepath.Join("..", "..", "proto", "awareness_graph.proto")
	if _, err := os.Stat(protoPath); err != nil {
		t.Fatalf("proto path missing: %v", err)
	}
	specPath := filepath.Join("..", "..", "packaging", "specs", "awareness_graph_service.yaml")
	if _, err := os.Stat(specPath); err != nil {
		t.Fatalf("spec path missing: %v", err)
	}
}

func TestPackaging_SeedFileExistsAndHasContent(t *testing.T) {
	seedPath := filepath.Join("embeddata", "awareness.nt")
	info, err := os.Stat(seedPath)
	if err != nil {
		t.Fatalf("seed file missing at %s: %v", seedPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("seed file is empty: %s", seedPath)
	}
}

func TestPackaging_SeedIsNTripleFormat(t *testing.T) {
	seedPath := filepath.Join("embeddata", "awareness.nt")
	b, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 0 {
		t.Fatal("seed has no lines")
	}
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "<") && !strings.HasPrefix(line, "_:") {
			t.Fatalf("line %d is not valid N-Triples (no subject IRI or blank node): %q", i+1, line)
		}
		if !strings.HasSuffix(line, ".") {
			t.Fatalf("line %d does not end with '.': %q", i+1, line)
		}
	}
}

// ── Bootstrap / seedIfEmpty tests ────────────────────────────────────────────

// fakeCounterLoader implements the counter+loader interfaces used by
// seedIfEmpty so the test runs without a live Oxigraph.
type fakeCounterLoader struct {
	count     int64
	countErr  error
	loadCalls int
	loadErr   error
	loaded    []byte
}

func (f *fakeCounterLoader) CountTriples(_ context.Context) (int64, error) {
	return f.count, f.countErr
}
func (f *fakeCounterLoader) Load(_ context.Context, r io.Reader) error {
	f.loadCalls++
	if f.loadErr != nil {
		return f.loadErr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.loaded = b
	return nil
}

// seedStore wraps fakeCounterLoader to also satisfy store.Store so we
// can pass it to seedIfEmpty via the concrete parameter type.
type seedStore struct {
	nopStore
	fcl *fakeCounterLoader
}

func (s seedStore) CountTriples(ctx context.Context) (int64, error) {
	return s.fcl.CountTriples(ctx)
}
func (seedStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	if classIRI == seedmeta.NamespaceIRI+"SeedBuild" {
		return 1, nil
	}
	return 0, nil
}
func (s seedStore) Load(ctx context.Context, r io.Reader) error {
	return s.fcl.Load(ctx, r)
}

type seedVerifierStore struct {
	nopStore
	describeFn func(context.Context, string) ([]store.Triple, error)
	countFn    func(context.Context) (int64, error)
}

func (s seedVerifierStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	if s.describeFn == nil {
		marker, ok := normalizedEmbeddedSeedMarker()
		if !ok {
			return nil, nil
		}
		if iri == marker.IRI {
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		}
		return nil, nil
	}
	return s.describeFn(ctx, iri)
}
func (s seedVerifierStore) CountTriples(ctx context.Context) (int64, error) {
	if s.countFn != nil {
		return s.countFn(ctx)
	}
	marker, ok := normalizedEmbeddedSeedMarker()
	if !ok {
		return 0, nil
	}
	return marker.TripleCount, nil
}
func (seedVerifierStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	if classIRI == seedmeta.NamespaceIRI+"SeedBuild" {
		return 1, nil
	}
	return 0, nil
}

func TestSeedIfEmpty_LoadsWhenStoreEmpty(t *testing.T) {
	fcl := &fakeCounterLoader{count: 0}
	ss := seedStore{fcl: fcl}
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	if err := seedIfEmpty(context.Background(), ss, logger); err != nil {
		t.Fatalf("seedIfEmpty: %v", err)
	}
	if fcl.loadCalls != 1 {
		t.Fatalf("Load called %d times, want 1", fcl.loadCalls)
	}
	seedBytes, _ := seedmeta.AppendMarker(seedNT)
	if len(fcl.loaded) != len(seedBytes) {
		t.Fatalf("loaded %d bytes, want %d (restamped embedded seed)", len(fcl.loaded), len(seedBytes))
	}
	if !strings.Contains(logBuf.String(), "store is empty") {
		t.Fatalf("expected 'store is empty' in log; got: %q", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "seed loaded successfully") {
		t.Fatalf("expected 'seed loaded successfully' in log; got: %q", logBuf.String())
	}
}

func TestSeedIfEmpty_SkipsWhenStoreNonEmpty(t *testing.T) {
	fcl := &fakeCounterLoader{count: 9745}
	ss := seedStore{fcl: fcl}
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	if err := seedIfEmpty(context.Background(), ss, logger); err != nil {
		t.Fatalf("seedIfEmpty: %v", err)
	}
	if fcl.loadCalls != 0 {
		t.Fatalf("Load called %d times, want 0 (store non-empty)", fcl.loadCalls)
	}
	if !strings.Contains(logBuf.String(), "skipping seed load") {
		t.Fatalf("expected 'skipping seed load' in log; got: %q", logBuf.String())
	}
}

func TestSeedIfEmpty_SkipsWhenStoreCountErrors(t *testing.T) {
	fcl := &fakeCounterLoader{countErr: errors.New("backend down")}
	ss := seedStore{fcl: fcl}
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	err := seedIfEmpty(context.Background(), ss, logger)
	if err == nil {
		t.Fatal("expected error when CountTriples fails")
	}
	if fcl.loadCalls != 0 {
		t.Fatalf("Load called %d times, want 0 (count error)", fcl.loadCalls)
	}
}

func TestSeedIfEmpty_ReturnsErrorOnLoadFailure(t *testing.T) {
	fcl := &fakeCounterLoader{count: 0, loadErr: errors.New("upload failed")}
	ss := seedStore{fcl: fcl}
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	err := seedIfEmpty(context.Background(), ss, logger)
	if err == nil {
		t.Fatal("expected error when Load fails")
	}
	if !strings.Contains(err.Error(), "seed load") {
		t.Fatalf("error should mention seed load: %v", err)
	}
}

func TestSeedIfEmpty_SkipsNonOxigraphStore(t *testing.T) {
	// nopStore doesn't implement counter or loader — seedIfEmpty must be a no-op.
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)

	if err := seedIfEmpty(context.Background(), nopStore{}, logger); err != nil {
		t.Fatalf("seedIfEmpty on nopStore: %v", err)
	}
	if logBuf.Len() > 0 {
		t.Fatalf("expected no log output for non-Oxigraph store; got: %q", logBuf.String())
	}
}

func TestEnforceCurrentSeed_PassesWhenMarkerPresent(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	store := seedVerifierStore{
		describeFn: func(_ context.Context, iri string) ([]store.Triple, error) {
			marker, ok := normalizedEmbeddedSeedMarker()
			if !ok {
				t.Fatal("embedded seed marker missing in test fixture")
			}
			if iri != marker.IRI {
				t.Fatalf("Describe called with %q, want %q", iri, marker.IRI)
			}
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		},
	}

	if err := enforceCurrentSeed(context.Background(), store, "http://test/query", false, logger); err != nil {
		t.Fatalf("enforceCurrentSeed: %v", err)
	}
	if !strings.Contains(logBuf.String(), "verified live graph authority") {
		t.Fatalf("expected success log, got %q", logBuf.String())
	}
}

func TestEnforceCurrentSeed_FailsClosedWhenMarkerMissing(t *testing.T) {
	store := seedVerifierStore{
		describeFn: func(_ context.Context, _ string) ([]store.Triple, error) {
			return nil, nil
		},
	}

	err := enforceCurrentSeed(context.Background(), store, "http://test/query", false, log.New(io.Discard, "", 0))
	if err == nil {
		t.Fatal("expected stale-seed error")
	}
	if !strings.Contains(err.Error(), "not authoritative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnforceCurrentSeed_AllowStaleSeedWarns(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	store := seedVerifierStore{
		describeFn: func(_ context.Context, _ string) ([]store.Triple, error) {
			return nil, nil
		},
	}

	if err := enforceCurrentSeed(context.Background(), store, "http://test/query", true, logger); err != nil {
		t.Fatalf("enforceCurrentSeed with allow stale: %v", err)
	}
	out := logBuf.String()
	if !strings.Contains(out, "WARNING") || !strings.Contains(out, "-allow-stale-seed") {
		t.Fatalf("expected stale warning with escape hatch, got %q", out)
	}
}

// TestNoSQLiteDependency pins that go.mod carries no SQLite driver —
// awareness.no_legacy_sqlite_graph forbids the legacy SQLite graph storage from returning.
func TestNoSQLiteDependency(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, bad := range []string{
		"mattn/go-sqlite3",
		"modernc.org/sqlite",
		"crawshaw.io/sqlite",
		"zombiezen.com/go/sqlite",
	} {
		if bytes.Contains(b, []byte(bad)) {
			t.Errorf("go.mod must not contain sqlite dependency %q (awareness.no_legacy_sqlite_graph)", bad)
		}
	}
}

func TestNormalizedEmbeddedSeedMarker_HasTripleCount(t *testing.T) {
	raw, ok := seedmeta.ParseMarker(seedNT)
	if !ok {
		t.Fatal("raw embedded seed marker missing")
	}
	normalized, ok := normalizedEmbeddedSeedMarker()
	if !ok {
		t.Fatal("normalized embedded seed marker missing")
	}
	if normalized.Digest != raw.Digest {
		t.Fatalf("normalized digest=%s, want raw digest=%s", normalized.Digest, raw.Digest)
	}
	if normalized.TripleCount < raw.TripleCount {
		t.Fatalf("normalized triple count=%d, want >= raw triple count=%d", normalized.TripleCount, raw.TripleCount)
	}
}

func TestPackaging_SeedTripleCount(t *testing.T) {
	const minTriples = 50
	seedPath := filepath.Join("embeddata", "awareness.nt")
	b, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	count := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			count++
		}
	}
	if count < minTriples {
		t.Fatalf("seed has only %d triples, want at least %d — graph may be empty or stale", count, minTriples)
	}
}
