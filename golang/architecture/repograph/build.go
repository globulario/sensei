// SPDX-License-Identifier: Apache-2.0

package repograph

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/governedmutation"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/seedmeta"
)

const combinedSeedObligation = "combined embedded seed (" + CombinedSeedRelPath +
	") remains a separate cross-repository convergence obligation; not regenerated here"

// markerSchemaVersion mirrors seedmeta's (unexported) marker version emitted into
// the stamped graph; it is surfaced in the projection result for provenance.
const markerSchemaVersion = "v2"

// Build compiles the governed sources into the canonical repository-scoped graph,
// stamps it, atomically persists graph.nt and its authority marker, then
// INDEPENDENTLY reloads both from disk and verifies the persisted world against
// the frozen source manifest — it never trusts the marker produced in the same
// pass. It performs no locking: the caller (the promotion transaction) holds the
// governed-mutation lock continuously across the source mutation and this call.
// It never writes the combined embedded seed.
func Build(ctx context.Context, req BuildRequest) (VerifiedProjection, error) {
	return buildWith(ctx, req, buildDeps{})
}

// buildDeps is immutable dependency injection for tests. afterCompile, when set,
// runs immediately after stamp and before persistence/post-CAS, so a test can
// deterministically simulate a concurrent source mutation during the compile
// window. It is nil in production (no process-global seam).
type buildDeps struct {
	afterCompile func()
}

func buildWith(ctx context.Context, req BuildRequest, deps buildDeps) (VerifiedProjection, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	if root == "" {
		return VerifiedProjection{}, &InvalidRequestError{Detail: "repository root is required"}
	}
	if strings.TrimSpace(req.ExpectedManifestDigestSHA256) == "" {
		return VerifiedProjection{}, &InvalidRequestError{Detail: "expected governed-source manifest digest is required"}
	}

	// Compare-and-swap BEFORE compile: the frozen manifest must still be current.
	preManifest, err := governedmutation.GovernedManifestDigest(root)
	if err != nil {
		return VerifiedProjection{}, &CompileError{Detail: "pre-compile manifest: " + err.Error()}
	}
	if preManifest != req.ExpectedManifestDigestSHA256 {
		return VerifiedProjection{}, &StaleManifestError{Phase: "before_compile", Expected: req.ExpectedManifestDigestSHA256, Actual: preManifest}
	}

	// Compile the governed sources (self-only; no services checkout, no seed edit).
	comp, err := graphbuild.Compile(ctx, graphbuild.CompileRequest{
		RepositoryRoot: root,
		Sources: []graphbuild.SourceRoot{{
			FilesystemPath:      filepath.Join(root, "docs", "awareness"),
			IdentityRoot:        filepath.Join(root, "docs", "awareness"),
			StripPathPrefixes:   []string{root},
			RepositoryDomain:    req.RepositoryDomain,
			SkipNestedGenerated: true,
		}},
		Policy: graphbuild.ValidationPolicy{},
	})
	if err != nil {
		return VerifiedProjection{}, &CompileError{Detail: err.Error()}
	}
	buildInputDigest := comp.SourceManifest.DigestSHA256

	// Stamp: projection authority. Stamped bytes = canonicalized body + 6 marker
	// triples, so they differ from the compile artifact bytes.
	art, err := graphbuild.Stamp(ctx, graphbuild.FinalizeRequest{Compilation: comp})
	if err != nil {
		return VerifiedProjection{}, &CompileError{Detail: "stamp: " + err.Error()}
	}
	stamped := art.NTriples
	byteDigest := art.ArtifactByteDigestSHA256
	semanticDigest := art.GraphSemanticDigestSHA256
	marker := seedmeta.Marker{Digest: semanticDigest, IRI: art.MarkerIRI, TripleCount: int64(art.ArtifactTripleCount)}

	if deps.afterCompile != nil {
		deps.afterCompile()
	}

	graphPath := filepath.Join(root, filepath.FromSlash(GraphRelPath))
	markerPath := seedmeta.RuntimeMarkerPath(root)

	// Exact replay: the persisted pair already matches this exact build.
	preGraphMatches := fileDigest(graphPath) == byteDigest
	preMarkerMatches := markerFileMatches(markerPath, marker)
	disposition := DispositionBuilt
	if preGraphMatches && preMarkerMatches {
		disposition = DispositionReplayed
	} else {
		// Atomic persistence. The graph is written first; a marker write failure
		// after a durable graph is a distinguishable incomplete state.
		if !preGraphMatches {
			if err := writeFileAtomic(graphPath, stamped); err != nil {
				return VerifiedProjection{}, &PersistError{Target: "graph", Detail: err.Error()}
			}
		}
		if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
			return VerifiedProjection{}, &PersistError{Target: "marker", GraphDurable: true, Detail: err.Error()}
		}
	}

	// Compare-and-swap AFTER compile/persist: a concurrent writer that changed the
	// governed sources during compile invalidates this graph world.
	postManifest, err := governedmutation.GovernedManifestDigest(root)
	if err != nil {
		return VerifiedProjection{}, &ReloadVerifyError{Aspect: "post_manifest", Detail: err.Error()}
	}
	if postManifest != req.ExpectedManifestDigestSHA256 {
		return VerifiedProjection{}, &StaleManifestError{Phase: "after_compile", Expected: req.ExpectedManifestDigestSHA256, Actual: postManifest}
	}

	// INDEPENDENT reload + verify from disk — never trust the in-pass marker.
	graphTriples, err := verifyPersisted(ctx, graphPath, markerPath, byteDigest, semanticDigest)
	if err != nil {
		return VerifiedProjection{}, err
	}

	return VerifiedProjection{
		RepositoryRoot:                root,
		Disposition:                   disposition,
		GraphPath:                     GraphRelPath,
		MarkerPath:                    MarkerRelPath,
		SourceManifestDigestSHA256:    preManifest,
		GraphBuildInputDigestSHA256:   buildInputDigest,
		CompiledGraphByteDigestSHA256: byteDigest,
		GraphSemanticDigestSHA256:     semanticDigest,
		MarkerDigestSHA256:            marker.Digest,
		MarkerIRI:                     marker.IRI,
		MarkerSchemaVersion:           markerSchemaVersion,
		GraphTripleCount:              graphTriples,
		ProducerID:                    ProducerID,
		ProducerVersion:               ProducerVersion,
		Verified:                      true,
		CombinedSeedObligation:        combinedSeedObligation,
	}, nil
}

// VerifyPersisted independently reloads and SELF-verifies the persisted
// repository graph and marker from disk with no frozen inputs: it recomputes the
// byte digest and the marker-free semantic digest from the file and requires the
// embedded marker, the marker file, and the recomputed semantic digest to agree,
// plus N-Triples parse validity and freshness. Tampering with the graph body, the
// embedded marker, or the marker file breaks the agreement and fails closed. This
// is the reload-verification primitive the later promotion transaction reuses.
func VerifyPersisted(ctx context.Context, root string) (VerifiedProjection, error) {
	graphPath := filepath.Join(root, filepath.FromSlash(GraphRelPath))
	markerPath := seedmeta.RuntimeMarkerPath(root)
	data, err := os.ReadFile(graphPath)
	if err != nil {
		return VerifiedProjection{}, &ReloadVerifyError{Aspect: "graph_byte", Detail: err.Error()}
	}
	byteDigest := sha256hex(data)
	base := graphbuild.StripMarker(data)
	_, recomputed := seedmeta.AppendMarker(base)
	tripleCount, err := verifyPersisted(ctx, graphPath, markerPath, byteDigest, recomputed.Digest)
	if err != nil {
		return VerifiedProjection{}, err
	}
	return VerifiedProjection{
		RepositoryRoot:                root,
		Disposition:                   DispositionReplayed,
		GraphPath:                     GraphRelPath,
		MarkerPath:                    MarkerRelPath,
		CompiledGraphByteDigestSHA256: byteDigest,
		GraphSemanticDigestSHA256:     recomputed.Digest,
		MarkerDigestSHA256:            recomputed.Digest,
		MarkerIRI:                     recomputed.IRI,
		MarkerSchemaVersion:           markerSchemaVersion,
		GraphTripleCount:              tripleCount,
		ProducerID:                    ProducerID,
		ProducerVersion:               ProducerVersion,
		Verified:                      true,
		CombinedSeedObligation:        combinedSeedObligation,
	}, nil
}

// verifyPersisted reopens the graph and marker from disk and independently
// recomputes and cross-checks every identity: file byte digest, N-Triples parse
// validity, the marker-free semantic digest, the marker file, and marker↔graph
// correspondence via the same freshness semantics the live store uses. It returns
// the governed-graph triple count (excluding the 6 marker triples).
func verifyPersisted(ctx context.Context, graphPath, markerPath, wantByteDigest, wantSemanticDigest string) (int, error) {
	data, err := os.ReadFile(graphPath)
	if err != nil {
		return 0, &ReloadVerifyError{Aspect: "graph_byte", Detail: err.Error()}
	}
	if sha256hex(data) != wantByteDigest {
		return 0, &ReloadVerifyError{Aspect: "graph_byte", Detail: "reloaded graph byte digest does not match"}
	}

	// N-Triples parse validity (malformed RDF fails closed).
	adapter, err := ReadGraph(bytes.NewReader(data))
	if err != nil {
		return 0, &ReloadVerifyError{Aspect: "nt_parse", Detail: err.Error()}
	}

	// Recompute the marker-free semantic digest from disk, independent of the
	// in-pass marker: strip the marker, re-stamp the body, compare.
	base := graphbuild.StripMarker(data)
	_, recomputed := seedmeta.AppendMarker(base)
	embedded, ok := seedmeta.ParseMarker(data)
	if !ok {
		return 0, &ReloadVerifyError{Aspect: "semantic_digest", Detail: "no marker in the persisted graph"}
	}
	if recomputed.Digest != wantSemanticDigest || embedded.Digest != wantSemanticDigest {
		return 0, &ReloadVerifyError{Aspect: "semantic_digest", Detail: "recomputed semantic digest does not match"}
	}

	// The marker FILE must correspond to the exact persisted graph.
	fileMarker, err := seedmeta.ReadMarkerFile(markerPath)
	if err != nil {
		return 0, &ReloadVerifyError{Aspect: "marker_file", Detail: err.Error()}
	}
	if fileMarker.Digest != recomputed.Digest || fileMarker.TripleCount != recomputed.TripleCount || fileMarker.IRI != embedded.IRI {
		return 0, &ReloadVerifyError{Aspect: "marker_file", Detail: "marker file does not correspond to the persisted graph"}
	}

	// Marker↔graph correspondence through the same freshness/authority semantics
	// the normal graph owner uses.
	ver := seedmeta.VerifyLiveStore(ctx, adapter, fileMarker)
	if ver.State != seedmeta.FreshnessCurrent {
		return 0, &ReloadVerifyError{Aspect: "freshness", Detail: ver.Detail}
	}

	return int(recomputed.TripleCount) - markerTripleCount, nil
}

const markerTripleCount = 6

func sha256hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fileDigest(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256hex(data)
}

func markerFileMatches(path string, m seedmeta.Marker) bool {
	fm, err := seedmeta.ReadMarkerFile(path)
	if err != nil {
		return false
	}
	return fm.Digest == m.Digest && fm.IRI == m.IRI && fm.TripleCount == m.TripleCount
}

// writeFileAtomic writes data to a sibling temp file, fsyncs it, then renames it
// over path — so a partial file is never observed as the persisted graph.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
