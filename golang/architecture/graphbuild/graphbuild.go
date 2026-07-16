// SPDX-License-Identifier: AGPL-3.0-only

// Package graphbuild is the one canonical, deterministic, offline architecture
// graph builder. It compiles explicitly named governed source roots into a
// canonical marker-free N-Triples graph, composes verified supplemental graphs,
// stamps exactly one seed marker, and reports precise counts, a deterministic
// source manifest, and stable graph and artifact digests.
//
// It is pure and offline. It reads the explicit source paths it is given and
// nothing else. It never writes files, never contacts Oxigraph or any network,
// never resolves the working directory, never reads the clock or Git, never
// reads mutable task or governance-pack state, and never appends ledger events
// or creates result-transition records. Side effects, source discovery,
// governance-pack selection/verification, and persistence remain the caller's
// responsibility.
package graphbuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	// ProducerID is the stable algorithm identity of this builder. It is NOT the
	// binary release version: a routine release must not change artifact identity
	// when graph semantics are unchanged.
	ProducerID = "sensei.graphbuild"
	// ProducerVersion is bumped only when graph-build semantics intentionally change.
	ProducerVersion = "v1"

	// NTriplesMediaType is the media type of the produced artifact.
	NTriplesMediaType = "application/n-triples"

	// ManifestSchemaVersion identifies the source-manifest shape.
	ManifestSchemaVersion = "graphbuild.source-manifest/v1"

	// markerTripleCount is the fixed number of triples in one seed marker.
	markerTripleCount = 6
)

// SourceRoot is one explicitly selected governed source tree. The package never
// discovers source roots on its own; every root is supplied by the caller.
type SourceRoot struct {
	// FilesystemPath is where the tree is read from. It may be absolute and may
	// be a caller-staged mirror; it never participates in identity.
	FilesystemPath string
	// IdentityRoot is the root every source file's stable logical path is taken
	// relative to. It never participates in graph triples or digests directly; it
	// only turns filesystem paths into repository-relative logical paths and
	// confines the tree.
	IdentityRoot string
	// StripPathPrefixes are stripped from emitted authoredIn literals so no
	// absolute path leaks into the graph. The caller passes the checkout root (and
	// any staging root) here.
	StripPathPrefixes []string

	// RepositoryDomain tags otherwise-unscoped structural nodes to this repo.
	RepositoryDomain string
	// DefaultDomain and DefaultSourceSet seed the emitter's default scope.
	DefaultDomain    string
	DefaultSourceSet string

	// SkipNestedGenerated skips nested generated/ directories during the walk.
	SkipNestedGenerated bool
}

// ValidationPolicy selects which recognized-but-unimported dispositions make a
// build fail. A zero policy is permissive (matches plain `sensei build`).
type ValidationPolicy struct {
	RejectUnknownSchemas   bool
	RejectInvalidFiles     bool
	RejectUnsupportedFiles bool
	// RejectExternalSymlinks refuses any source file whose real path escapes its
	// identity root through a symlink. It is opt-in because trusted callers may
	// legitimately stage a source tree with symlinks.
	RejectExternalSymlinks bool
}

// CompatibilityPolicy mirrors the existing `sensei build --strict` behavior:
// only an unrecognized schema is fatal.
func CompatibilityPolicy() ValidationPolicy {
	return ValidationPolicy{RejectUnknownSchemas: true}
}

// ClosureStrictPolicy refuses when any governed source cannot be interpreted and
// refuses symlink escapes. Phase 7 result transitions select it.
func ClosureStrictPolicy() ValidationPolicy {
	return ValidationPolicy{
		RejectUnknownSchemas:   true,
		RejectInvalidFiles:     true,
		RejectUnsupportedFiles: true,
		RejectExternalSymlinks: true,
	}
}

func (p ValidationPolicy) label() string {
	switch {
	case p.RejectInvalidFiles && p.RejectUnsupportedFiles && p.RejectUnknownSchemas:
		return "closure_strict"
	case p.RejectUnknownSchemas && !p.RejectInvalidFiles && !p.RejectUnsupportedFiles:
		return "compatibility"
	case p == (ValidationPolicy{}):
		return "permissive"
	default:
		return "custom"
	}
}

// CompileRequest names the source roots and validation policy for a compilation.
type CompileRequest struct {
	RepositoryRoot string
	Sources        []SourceRoot
	Policy         ValidationPolicy
}

// Compilation is the canonical marker-free graph plus its deterministic manifest
// and counts.
type Compilation struct {
	CanonicalNTriples []byte
	SourceManifest    SourceManifest

	UniqueTripleCount    int
	DuplicateTripleCount int
	ImportReport         ImportReport
	Limitations          []string
}

// SupplementalGraph is a verified, already-marked graph (e.g. a governance pack
// payload) to compose into the artifact. The caller performs all trust and
// selection decisions; this package only verifies the marker digest and folds
// the marker-free body in.
type SupplementalGraph struct {
	ID                           string
	Version                      string
	NTriples                     []byte
	ExpectedSemanticDigestSHA256 string
}

// FinalizeRequest composes a compilation with verified supplemental graphs.
type FinalizeRequest struct {
	Compilation        Compilation
	SupplementalGraphs []SupplementalGraph
}

// Artifact is one stamped canonical graph with distinct semantic and byte
// identities and precise counts.
type Artifact struct {
	NTriples  []byte
	MediaType string

	GraphSemanticDigestSHA256 string
	ArtifactByteDigestSHA256  string

	MarkerIRI            string
	GraphTripleCount     int
	MarkerTripleCount    int
	ArtifactTripleCount  int
	DuplicateTripleCount int

	SourceManifest SourceManifest

	ProducerID      string
	ProducerVersion string
}

// Compile turns explicit governed source roots into the canonical marker-free
// graph and its deterministic source manifest. It performs no finalization.
func Compile(ctx context.Context, req CompileRequest) (Compilation, error) {
	if err := ctx.Err(); err != nil {
		return Compilation{}, err
	}
	var raw bytes.Buffer
	var report ImportReport
	var manifestEntries []SourceManifestEntry
	exclusions := baseExclusions
	nestedSkipped := false

	for _, root := range req.Sources {
		if err := ctx.Err(); err != nil {
			return Compilation{}, err
		}
		if err := confineSourceRoot(root, req.Policy); err != nil {
			return Compilation{}, err
		}
		info, err := os.Stat(root.FilesystemPath)
		if err != nil {
			return Compilation{}, err
		}
		if !info.IsDir() {
			return Compilation{}, fmt.Errorf("graphbuild: %s is not a directory", root.FilesystemPath)
		}
		emitter, rep, err := extractor.ImportAwarenessDirWithOpts(root.FilesystemPath, &raw, extractor.ImportDirOptions{
			DefaultRepo:         root.RepositoryDomain,
			DefaultDomain:       root.DefaultDomain,
			DefaultSourceSet:    root.DefaultSourceSet,
			StripPathPrefixes:   root.StripPathPrefixes,
			SkipNestedGenerated: root.SkipNestedGenerated,
		})
		if err != nil {
			return Compilation{}, fmt.Errorf("graphbuild: import %s: %w", root.FilesystemPath, err)
		}
		_ = emitter
		if root.SkipNestedGenerated {
			nestedSkipped = true
		}
		entries, err := manifestEntriesForRoot(root, rep)
		if err != nil {
			return Compilation{}, err
		}
		manifestEntries = append(manifestEntries, entries...)
		report.Files = append(report.Files, rep.Files...)
	}

	if err := enforcePolicy(report, req.Policy); err != nil {
		return Compilation{}, err
	}

	canonical, unique, dup := canonicalGraph(raw.Bytes())
	if errs := extractor.ValidateNTriples(bytes.NewReader(canonical)); len(errs) > 0 {
		return Compilation{}, fmt.Errorf("graphbuild: compiled graph has %d N-Triples validation error(s): %s", len(errs), errs[0].Error())
	}
	if nestedSkipped {
		exclusions = append(append([]string{}, exclusions...), exclusionNestedGenerated)
	}
	manifest := buildManifest(req.Policy, manifestEntries, exclusions, nil)

	return Compilation{
		CanonicalNTriples:    canonical,
		SourceManifest:       manifest,
		UniqueTripleCount:    unique,
		DuplicateTripleCount: dup,
		ImportReport:         report,
	}, nil
}

// Finalize composes a compilation with verified supplemental graphs into exactly
// one stamped artifact.
func Finalize(ctx context.Context, req FinalizeRequest) (Artifact, error) {
	if err := ctx.Err(); err != nil {
		return Artifact{}, err
	}
	var merged bytes.Buffer
	var supEntries []SupplementalManifestEntry
	for _, sup := range req.SupplementalGraphs {
		marker, ok := seedmeta.ParseMarker(sup.NTriples)
		if !ok {
			return Artifact{}, fmt.Errorf("graphbuild: supplemental graph %q carries no seed marker", sup.ID)
		}
		if want := sup.ExpectedSemanticDigestSHA256; want == "" || marker.Digest != want {
			return Artifact{}, fmt.Errorf("graphbuild: supplemental graph %q marker digest mismatch (marker %s, expected %s)", sup.ID, marker.Digest, want)
		}
		merged.Write(stripMarkerLines(sup.NTriples))
		supEntries = append(supEntries, SupplementalManifestEntry{ID: sup.ID, Version: sup.Version, DigestSHA256: marker.Digest})
	}
	merged.Write(req.Compilation.CanonicalNTriples)

	deduped, _, dup := extractor.DedupNTriples(merged.Bytes())
	finalNT, marker := seedmeta.AppendMarker(deduped)
	if errs := extractor.ValidateNTriples(bytes.NewReader(finalNT)); len(errs) > 0 {
		return Artifact{}, fmt.Errorf("graphbuild: final artifact has %d N-Triples validation error(s): %s", len(errs), errs[0].Error())
	}

	byteSum := sha256.Sum256(finalNT)
	artifactTriples := int(marker.TripleCount)
	graphTriples := artifactTriples - markerTripleCount

	manifest := req.Compilation.SourceManifest
	manifest.SupplementalGraphs = supEntries
	manifest.DigestSHA256 = ""
	digest, err := sourceManifestDigest(manifest)
	if err != nil {
		return Artifact{}, err
	}
	manifest.DigestSHA256 = digest

	return Artifact{
		NTriples:                  finalNT,
		MediaType:                 NTriplesMediaType,
		GraphSemanticDigestSHA256: marker.Digest,
		ArtifactByteDigestSHA256:  hex.EncodeToString(byteSum[:]),
		MarkerIRI:                 marker.IRI,
		GraphTripleCount:          graphTriples,
		MarkerTripleCount:         markerTripleCount,
		ArtifactTripleCount:       artifactTriples,
		DuplicateTripleCount:      req.Compilation.DuplicateTripleCount + dup,
		SourceManifest:            manifest,
		ProducerID:                ProducerID,
		ProducerVersion:           ProducerVersion,
	}, nil
}

// Build is the Compile+Finalize convenience composition.
func Build(ctx context.Context, req CompileRequest, supplemental []SupplementalGraph) (Artifact, error) {
	comp, err := Compile(ctx, req)
	if err != nil {
		return Artifact{}, err
	}
	return Finalize(ctx, FinalizeRequest{Compilation: comp, SupplementalGraphs: supplemental})
}

// canonicalGraph dedups raw triples and returns the sorted, marker-free canonical
// body (exactly the base seedmeta.AppendMarker hashes), plus the dedup counts.
func canonicalGraph(raw []byte) (canonical []byte, unique, dup int) {
	deduped, unique, dup := extractor.DedupNTriples(raw)
	marked, _ := seedmeta.AppendMarker(deduped)
	return stripMarkerLines(marked), unique, dup
}
