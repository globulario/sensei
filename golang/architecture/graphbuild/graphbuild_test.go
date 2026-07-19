// SPDX-License-Identifier: AGPL-3.0-only

package graphbuild

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/seedmeta"
)

const invariantsAlpha = `invariants:
  - id: test.inv.alpha
    title: Alpha invariant
    severity: high
    status: active
    protects:
      files:
        - golang/alpha.go
`

const invariantsBeta = `invariants:
  - id: test.inv.beta
    title: Beta invariant
    severity: critical
    status: active
    protects:
      files:
        - golang/beta.go
`

const unknownSchemaYAML = "zzz_unrecognized_top_key:\n  - one\n  - two\n"
const knownUnsupportedYAML = "preflight_questions:\n  - id: q1\n    text: unsupported schema\n"
const ignoredYAML = "last_updated: \"2026-01-01T00:00:00Z\"\n"
const invalidYAML = "invariants: [ this is : not valid : yaml\n"

// writeAwareness writes files into <root>/docs/awareness and returns that dir.
func writeAwareness(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, "docs", "awareness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// srcRoot builds a SourceRoot for <root>/docs/awareness identified against root,
// stripping the absolute root so no absolute path leaks into the graph.
func srcRoot(root string) SourceRoot {
	return SourceRoot{
		FilesystemPath:    filepath.Join(root, "docs", "awareness"),
		IdentityRoot:      root,
		StripPathPrefixes: []string{root},
		RepositoryDomain:  "github.com/test/repo",
	}
}

func compileOne(t *testing.T, root string, policy ValidationPolicy) Compilation {
	t.Helper()
	comp, err := Compile(context.Background(), CompileRequest{RepositoryRoot: root, Sources: []SourceRoot{srcRoot(root)}, Policy: policy})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return comp
}

func buildOne(t *testing.T, root string, policy ValidationPolicy) Artifact {
	t.Helper()
	art, err := Build(context.Background(), CompileRequest{RepositoryRoot: root, Sources: []SourceRoot{srcRoot(root)}, Policy: policy}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return art
}

func nonEmptyLines(b []byte) int {
	n := 0
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// 1. minimal valid graph build
func TestBuildMinimalGraph(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	art := buildOne(t, root, ValidationPolicy{})
	if len(art.NTriples) == 0 {
		t.Fatal("empty artifact")
	}
	if art.ProducerID != ProducerID || art.ProducerVersion != ProducerVersion {
		t.Fatalf("producer identity = %s/%s", art.ProducerID, art.ProducerVersion)
	}
	if art.MediaType != NTriplesMediaType {
		t.Fatalf("media type = %s", art.MediaType)
	}
	if _, ok := seedmeta.ParseMarker(art.NTriples); !ok {
		t.Fatal("artifact carries no marker")
	}
}

// 4. duplicate suppression + 5. exactly one final marker + 8. exact triple counts
func TestBuildCountsAndSingleMarker(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	// Compiling the identical source root twice emits every triple twice, forcing
	// canonical duplicate suppression to fire.
	art, err := Build(context.Background(), CompileRequest{
		Sources: []SourceRoot{srcRoot(root), srcRoot(root)},
		Policy:  ValidationPolicy{},
	}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if art.MarkerTripleCount != 6 {
		t.Fatalf("marker triple count = %d, want 6", art.MarkerTripleCount)
	}
	if art.ArtifactTripleCount != art.GraphTripleCount+6 {
		t.Fatalf("artifact %d != graph %d + 6", art.ArtifactTripleCount, art.GraphTripleCount)
	}
	if got := nonEmptyLines(art.NTriples); got != art.ArtifactTripleCount {
		t.Fatalf("artifact triple count %d != non-empty lines %d", art.ArtifactTripleCount, got)
	}
	if art.DuplicateTripleCount == 0 {
		t.Fatal("expected duplicates from the identical second file")
	}
	// Exactly one marker: exactly 6 lines share the marker IRI subject.
	needle := "<" + art.MarkerIRI + "> "
	markerLines := 0
	for _, l := range strings.Split(string(art.NTriples), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), needle) {
			markerLines++
		}
	}
	if markerLines != 6 {
		t.Fatalf("expected exactly 6 marker lines, got %d", markerLines)
	}
}

// 6. marker digest matches semantic graph + 7. byte digest matches artifact
func TestBuildDigests(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	art := buildOne(t, root, ValidationPolicy{})

	marker, ok := seedmeta.ParseMarker(art.NTriples)
	if !ok {
		t.Fatal("no marker")
	}
	if art.GraphSemanticDigestSHA256 != marker.Digest {
		t.Fatalf("graph semantic digest %s != marker digest %s", art.GraphSemanticDigestSHA256, marker.Digest)
	}
	if !isHex64(art.GraphSemanticDigestSHA256) || !isHex64(art.ArtifactByteDigestSHA256) {
		t.Fatal("digests are not 64-hex")
	}
	if art.GraphSemanticDigestSHA256 == art.ArtifactByteDigestSHA256 {
		t.Fatal("semantic and byte digests must be distinct concepts")
	}
}

// 9. regression for the old rebuild "+5" count: the correct artifact total is
// graph + 6, never graph + 5.
func TestBuildTripleCountIsPlusSixNotPlusFive(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	art := buildOne(t, root, ValidationPolicy{})
	marker, _ := seedmeta.ParseMarker(art.NTriples)
	if art.ArtifactTripleCount != int(marker.TripleCount) {
		t.Fatalf("artifact count %d != marker count %d", art.ArtifactTripleCount, marker.TripleCount)
	}
	if art.ArtifactTripleCount != art.GraphTripleCount+6 {
		t.Fatalf("artifact count must be graph+6, got graph=%d artifact=%d", art.GraphTripleCount, art.ArtifactTripleCount)
	}
	// The historic bug returned graph+5; assert we are not one short of the marker.
	if art.ArtifactTripleCount == art.GraphTripleCount+5 {
		t.Fatal("regression: count is graph+5 (the old rebuild off-by-one)")
	}
}

func isHex64(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// 13. absolute path absent from graph (and manifest)
func TestNoAbsolutePathInOutputs(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	art := buildOne(t, root, ValidationPolicy{})
	if bytes.Contains(art.NTriples, []byte(root)) {
		t.Fatal("absolute checkout path leaked into graph triples")
	}
	for _, e := range art.SourceManifest.Sources {
		if strings.Contains(e.LogicalPath, root) || filepath.IsAbs(e.LogicalPath) {
			t.Fatalf("absolute path in manifest logical path: %q", e.LogicalPath)
		}
	}
}

// 10. source manifest deterministic + 11. manifest paths repository-relative
func TestManifestDeterministicAndRelative(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{
		"invariants.yaml": invariantsAlpha,
		"more.yaml":       invariantsBeta,
	})
	a := compileOne(t, root, ValidationPolicy{})
	b := compileOne(t, root, ValidationPolicy{})
	if a.SourceManifest.DigestSHA256 != b.SourceManifest.DigestSHA256 {
		t.Fatal("manifest digest not deterministic")
	}
	if a.SourceManifest.DigestSHA256 == "" || !isHex64(a.SourceManifest.DigestSHA256) {
		t.Fatalf("bad manifest digest %q", a.SourceManifest.DigestSHA256)
	}
	var foundInvariants bool
	for _, e := range a.SourceManifest.Sources {
		if filepath.IsAbs(e.LogicalPath) || strings.HasPrefix(e.LogicalPath, "..") {
			t.Fatalf("non-relative logical path %q", e.LogicalPath)
		}
		if e.LogicalPath == "docs/awareness/invariants.yaml" {
			foundInvariants = true
			if e.Disposition != "imported" {
				t.Fatalf("invariants disposition = %q", e.Disposition)
			}
			if e.ByteDigestSHA256 == "" {
				t.Fatal("missing byte digest")
			}
		}
	}
	if !foundInvariants {
		t.Fatalf("invariants.yaml not in manifest: %+v", a.SourceManifest.Sources)
	}
}

// 14. candidates excluded
func TestCandidatesExcluded(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{
		"invariants.yaml":         invariantsAlpha,
		"candidates/pending.yaml": invariantsBeta,
	})
	comp := compileOne(t, root, ValidationPolicy{})
	for _, e := range comp.SourceManifest.Sources {
		if strings.Contains(e.LogicalPath, "candidates/") {
			t.Fatalf("candidate file entered the manifest: %q", e.LogicalPath)
		}
	}
	if !bytes.Contains(comp.CanonicalNTriples, []byte("test.inv.alpha")) {
		t.Fatal("expected alpha invariant present")
	}
	if bytes.Contains(comp.CanonicalNTriples, []byte("test.inv.beta")) {
		t.Fatal("candidate invariant leaked into the graph")
	}
	found := false
	for _, ex := range comp.SourceManifest.Exclusions {
		if strings.Contains(ex, "candidate") {
			found = true
		}
	}
	if !found {
		t.Fatal("candidate exclusion rule not recorded in manifest")
	}
}

// 16. unknown schema reported; 17. compatibility policy; 18/19/20 closure-strict refusals
func TestPolicyDispositions(t *testing.T) {
	// permissive: unknown schema reported, not fatal.
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{
		"invariants.yaml": invariantsAlpha,
		"weird.yaml":      unknownSchemaYAML,
	})
	comp := compileOne(t, root, ValidationPolicy{})
	sawUnknown := false
	for _, f := range comp.ImportReport.Files {
		if strings.HasSuffix(f.Path, "weird.yaml") && string(f.Status) == "unknown_schema" {
			sawUnknown = true
		}
	}
	if !sawUnknown {
		t.Fatal("unknown schema not reported")
	}

	// compatibility: unknown schema is fatal.
	if _, err := Compile(context.Background(), CompileRequest{Sources: []SourceRoot{srcRoot(root)}, Policy: CompatibilityPolicy()}); err == nil {
		t.Fatal("compatibility policy must reject unknown schema")
	}

	// closure-strict: invalid file is fatal.
	rootInvalid := t.TempDir()
	writeAwareness(t, rootInvalid, map[string]string{"invariants.yaml": invariantsAlpha, "bad.yaml": invalidYAML})
	if _, err := Compile(context.Background(), CompileRequest{Sources: []SourceRoot{srcRoot(rootInvalid)}, Policy: ClosureStrictPolicy()}); err == nil {
		t.Fatal("closure-strict must reject invalid YAML")
	}

	// closure-strict: known-unsupported is fatal.
	rootUnsupported := t.TempDir()
	writeAwareness(t, rootUnsupported, map[string]string{"invariants.yaml": invariantsAlpha, "unsup.yaml": knownUnsupportedYAML})
	if _, err := Compile(context.Background(), CompileRequest{Sources: []SourceRoot{srcRoot(rootUnsupported)}, Policy: ClosureStrictPolicy()}); err == nil {
		t.Fatal("closure-strict must reject known-unsupported schema")
	}

	// ignored config remains allowed and visible under closure-strict.
	rootIgnored := t.TempDir()
	writeAwareness(t, rootIgnored, map[string]string{"invariants.yaml": invariantsAlpha, "tracker.yaml": ignoredYAML})
	c, err := Compile(context.Background(), CompileRequest{Sources: []SourceRoot{srcRoot(rootIgnored)}, Policy: ClosureStrictPolicy()})
	if err != nil {
		t.Fatalf("closure-strict must allow ignored config: %v", err)
	}
	sawIgnored := false
	for _, f := range c.ImportReport.Files {
		if strings.HasSuffix(f.Path, "tracker.yaml") && string(f.Status) == "ignored" {
			sawIgnored = true
		}
	}
	if !sawIgnored {
		t.Fatal("ignored config not visible in report")
	}
}
