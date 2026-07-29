// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

type runtimeMarkerStore struct {
	describeFn func(context.Context, string) ([]store.Triple, error)
	countFn    func(context.Context) (int64, error)
	impactFn   func(context.Context, string) ([]store.ImpactFact, error)
}

func (runtimeMarkerStore) Close() error                 { return nil }
func (runtimeMarkerStore) Health(context.Context) error { return nil }
func (runtimeMarkerStore) DescribeInbound(context.Context, string) ([]store.InboundTriple, error) {
	return nil, nil
}
func (s runtimeMarkerStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	return s.describeFn(ctx, iri)
}
func (runtimeMarkerStore) ClassFacts(context.Context, string, int) ([]store.ImpactFact, error) {
	return nil, nil
}
func (runtimeMarkerStore) CodeSymbolFacts(context.Context, string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (runtimeMarkerStore) RenderingGroupsForFile(context.Context, string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}
func (runtimeMarkerStore) DetectFacts(context.Context) ([]store.ImpactFact, error) {
	return nil, nil
}
func (s runtimeMarkerStore) ImpactForFile(ctx context.Context, iri string) ([]store.ImpactFact, error) {
	if s.impactFn != nil {
		return s.impactFn(ctx, iri)
	}
	return nil, nil
}
func (s runtimeMarkerStore) CountTriples(ctx context.Context) (int64, error) {
	return s.countFn(ctx)
}
func (runtimeMarkerStore) CountByClass(_ context.Context, classIRI string) (int64, error) {
	if classIRI == seedmeta.NamespaceIRI+"SeedBuild" {
		return 1, nil
	}
	return 0, nil
}

func TestMetadata_UsesRuntimeMarkerFileForFreshness(t *testing.T) {
	_, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	s := newServer(runtimeMarkerStore{
		describeFn: func(_ context.Context, iri string) ([]store.Triple, error) {
			if iri != marker.IRI {
				t.Fatalf("Describe called with %q, want %q", iri, marker.IRI)
			}
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		},
		countFn: func(context.Context) (int64, error) {
			return marker.TripleCount, nil
		},
	})
	s.graphMarkerFile = markerPath

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatalf("graph freshness=%s, want CURRENT", resp.GetGraphFreshnessState())
	}
	if resp.GetEmbeddedSeedDigestSha256() != marker.Digest {
		t.Fatalf("expected digest=%q, want %q", resp.GetEmbeddedSeedDigestSha256(), marker.Digest)
	}
	if resp.GetLiveStoreGraphDigestSha256() != marker.Digest {
		t.Fatalf("live digest=%q, want %q", resp.GetLiveStoreGraphDigestSha256(), marker.Digest)
	}
}

func TestMetadata_UsesRuntimeTransactionStampWhenPresent(t *testing.T) {
	_, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	txPath := seedmeta.RuntimeTransactionPath(markerPath)
	tx := strings.Join([]string{
		"format\tv1",
		"seed\tdigest_sha256\t" + marker.Digest,
		"seed\ttriple_count\t" + strconv.FormatInt(marker.TripleCount, 10),
		"repo\tawareness-graph\tdeadbeef",
		"repo\tservices\tcafebabe",
	}, "\n") + "\n"
	if err := os.WriteFile(txPath, []byte(tx), 0o644); err != nil {
		t.Fatalf("write transaction file: %v", err)
	}

	s := newServer(runtimeMarkerStore{
		describeFn: func(_ context.Context, iri string) ([]store.Triple, error) {
			if iri != marker.IRI {
				t.Fatalf("Describe called with %q, want %q", iri, marker.IRI)
			}
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		},
		countFn: func(context.Context) (int64, error) {
			return marker.TripleCount, nil
		},
	})
	s.graphMarkerFile = markerPath

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if !resp.GetEmbeddedTransactionStampPresent() {
		t.Fatal("runtime transaction stamp should be present")
	}
	if !resp.GetEmbeddedTransactionMatchesSeed() {
		t.Fatalf("runtime transaction should match graph, detail=%q", resp.GetEmbeddedTransactionDetail())
	}
	if resp.GetCertifiedAwarenessGraphCommit() != "deadbeef" {
		t.Fatalf("awareness commit=%q, want deadbeef", resp.GetCertifiedAwarenessGraphCommit())
	}
	if resp.GetCertifiedServicesRepoCommit() != "cafebabe" {
		t.Fatalf("services commit=%q, want cafebabe", resp.GetCertifiedServicesRepoCommit())
	}
	if !strings.Contains(resp.GetEmbeddedTransactionDetail(), "runtime transaction certifies expected graph") {
		t.Fatalf("detail=%q, want runtime certification message", resp.GetEmbeddedTransactionDetail())
	}
}

func TestMetadata_RuntimeMarkerFileMissingMarksUnknown(t *testing.T) {
	s := newServer(runtimeMarkerStore{
		describeFn: func(context.Context, string) ([]store.Triple, error) { return nil, nil },
		countFn:    func(context.Context) (int64, error) { return 0, nil },
	})
	s.graphMarkerFile = filepath.Join(t.TempDir(), "missing.json")

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNKNOWN {
		t.Fatalf("graph freshness=%s, want UNKNOWN", resp.GetGraphFreshnessState())
	}
	if got := resp.GetGraphFreshnessDetail(); got == "" {
		t.Fatal("graph freshness detail should explain missing runtime marker file")
	}
}

// TestMetadata_EmbeddedModeStaleExplainsWhyAndHowToFix is the regression
// ratchet for a live incident: with no -no-seed / -graph-marker-file at
// startup (the default), the server compares the live store against the
// marker COMPILED INTO THE BINARY. Running `sensei build` updates the live
// store and the RUNTIME marker file, but never the embedded one, so every
// freshness check kept reporting stale with a bare "live store missing
// expected graph marker <hash>" — no indication that a rebuild had already
// happened, or that -no-seed was the fix. The detail string must name the
// actual cause and the actual fix.
func TestMetadata_EmbeddedModeStaleExplainsWhyAndHowToFix(t *testing.T) {
	s := newServer(runtimeMarkerStore{
		// The live store has REAL data (a rebuild happened) but its marker
		// IRI doesn't match whatever is embedded in this test binary —
		// exactly the "I rebuilt, but the server is still stale" shape.
		describeFn: func(context.Context, string) ([]store.Triple, error) { return nil, nil },
		countFn:    func(context.Context) (int64, error) { return 42, nil },
	})
	// s.graphMarkerFile is left empty: embedded-seed mode, the default.

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	detail := resp.GetGraphFreshnessDetail()
	if resp.GetGraphFreshnessState() == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Skip("embedded marker in this test binary happened to match — nothing to annotate")
	}
	if !strings.Contains(detail, "EMBEDDED-SEED mode") {
		t.Fatalf("detail = %q, want it to name embedded-seed mode as the cause", detail)
	}
	if !strings.Contains(detail, "-no-seed") {
		t.Fatalf("detail = %q, want it to name -no-seed as the fix", detail)
	}
}

// TestMetadata_RuntimeMarkerStaleDoesNotMentionEmbeddedMode is the inverse
// ratchet: once a caller HAS opted into runtime-marker mode
// (s.graphMarkerFile set, e.g. via -no-seed), a genuine staleness (the
// runtime marker file itself is out of date relative to the live store)
// must NOT be annotated with the embedded-mode hint — that hint is only
// correct advice when embedded mode is actually the cause.
func TestMetadata_RuntimeMarkerStaleDoesNotMentionEmbeddedMode(t *testing.T) {
	_, marker := seedmeta.AppendMarker([]byte("<https://example.test/s> <https://example.test/p> <https://example.test/x> .\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	s := newServer(runtimeMarkerStore{
		// Live store does not carry the expected marker triples — a genuine
		// stale live store, unrelated to embedded-vs-runtime mode.
		describeFn: func(context.Context, string) ([]store.Triple, error) { return nil, nil },
		countFn:    func(context.Context) (int64, error) { return 7, nil },
	})
	s.graphMarkerFile = markerPath

	resp, err := s.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if resp.GetGraphFreshnessState() == awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatal("expected a stale/unknown verdict for this fixture")
	}
	if detail := resp.GetGraphFreshnessDetail(); strings.Contains(detail, "EMBEDDED-SEED mode") {
		t.Fatalf("detail = %q, must not blame embedded-seed mode when running with a runtime marker file", detail)
	}
}

func TestBriefing_RuntimeMarkerAllowsNonEmbeddedAuthoritativeGraph(t *testing.T) {
	_, marker := seedmeta.AppendMarker([]byte(strings.Join([]string{
		"<https://example.test/invariant/caddy.forwardauth> <http://www.w3.org/2000/01/rdf-schema#label> \"Caddy forwardauth invariant\" .",
		"<https://example.test/invariant/caddy.forwardauth> <https://globular.io/awareness#severity> \"high\" .",
		"<https://example.test/invariant/caddy.forwardauth> <https://globular.io/awareness#definedInFile> <https://globular.io/awareness#sourceFile/modules%2Fcaddyhttp%2Freverseproxy%2Fforwardauth%2Fcaddyfile.go> .",
	}, "\n") + "\n"))
	markerPath := filepath.Join(t.TempDir(), "graph-authority.json")
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	s := newServer(runtimeMarkerStore{
		describeFn: func(_ context.Context, iri string) ([]store.Triple, error) {
			if iri != marker.IRI {
				t.Fatalf("Describe called with %q, want %q", iri, marker.IRI)
			}
			return []store.Triple{
				{Predicate: seedmeta.NamespaceIRI + "seedDigestSha256", Object: marker.Digest},
				{Predicate: seedmeta.NamespaceIRI + "seedTripleCount", Object: strconv.FormatInt(marker.TripleCount, 10)},
			}, nil
		},
		countFn: func(context.Context) (int64, error) {
			return marker.TripleCount, nil
		},
		impactFn: func(_ context.Context, sourceFileIRI string) ([]store.ImpactFact, error) {
			want := fileIRI("modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go")
			if sourceFileIRI != want {
				t.Fatalf("ImpactForFile called with %q, want %q", sourceFileIRI, want)
			}
			return invariantFacts("caddy.forwardauth", "Caddy forwardauth invariant", "high"), nil
		},
	})
	s.graphMarkerFile = markerPath
	s.briefingRepo = &briefingRepositoryContext{Root: t.TempDir(), Domain: defaultHomeDomain}

	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.GetAuthority().GetGraphFreshnessState() != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT {
		t.Fatalf("graph freshness=%s, want CURRENT", resp.GetAuthority().GetGraphFreshnessState())
	}
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("briefing status=%s, want OK", resp.GetStatus())
	}
	if !strings.Contains(resp.GetProse(), "Caddy forwardauth invariant") {
		t.Fatalf("briefing prose=%q, want invariant label", resp.GetProse())
	}
}
