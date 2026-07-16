// SPDX-License-Identifier: Apache-2.0

package graphbuild

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// dirRoot writes one awareness file into <root>/<sub> and returns a SourceRoot
// identified against root.
func subRoot(t *testing.T, root, sub, file, content string) SourceRoot {
	t.Helper()
	dir := filepath.Join(root, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return SourceRoot{FilesystemPath: dir, IdentityRoot: root, StripPathPrefixes: []string{root}, RepositoryDomain: "github.com/test/repo"}
}

func buildRoots(t *testing.T, roots ...SourceRoot) Artifact {
	t.Helper()
	art, err := Build(context.Background(), CompileRequest{Sources: roots, Policy: ValidationPolicy{}}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return art
}

// 2. deterministic input-root ordering
func TestDeterministicRootOrdering(t *testing.T) {
	root := t.TempDir()
	a := subRoot(t, root, "docs/awareness", "invariants.yaml", invariantsAlpha)
	b := subRoot(t, root, "docs/extra", "more.yaml", invariantsBeta)
	x := buildRoots(t, a, b)
	y := buildRoots(t, b, a)
	if !bytes.Equal(x.NTriples, y.NTriples) {
		t.Fatal("root ordering changed the artifact bytes")
	}
	if x.SourceManifest.DigestSHA256 != y.SourceManifest.DigestSHA256 {
		t.Fatal("root ordering changed the manifest digest")
	}
}

// 3. deterministic file ordering (two corpora, same content, different creation order)
func TestDeterministicFileOrdering(t *testing.T) {
	r1 := t.TempDir()
	writeAwareness(t, r1, map[string]string{"a.yaml": invariantsAlpha})
	writeAwareness(t, r1, map[string]string{"b.yaml": invariantsBeta})
	r2 := t.TempDir()
	writeAwareness(t, r2, map[string]string{"b.yaml": invariantsBeta})
	writeAwareness(t, r2, map[string]string{"a.yaml": invariantsAlpha})
	x := buildOne(t, r1, ValidationPolicy{})
	y := buildOne(t, r2, ValidationPolicy{})
	if x.GraphSemanticDigestSHA256 != y.GraphSemanticDigestSHA256 {
		t.Fatal("file creation order changed the graph digest")
	}
}

// 12. relocated checkout byte-identical
func TestRelocatedCheckoutByteIdentical(t *testing.T) {
	r1 := t.TempDir()
	writeAwareness(t, r1, map[string]string{"invariants.yaml": invariantsAlpha, "more.yaml": invariantsBeta})
	r2 := t.TempDir()
	writeAwareness(t, r2, map[string]string{"invariants.yaml": invariantsAlpha, "more.yaml": invariantsBeta})
	x := buildOne(t, r1, ValidationPolicy{})
	y := buildOne(t, r2, ValidationPolicy{})
	if !bytes.Equal(x.NTriples, y.NTriples) {
		t.Fatal("relocated checkout produced different artifact bytes")
	}
	if x.ArtifactByteDigestSHA256 != y.ArtifactByteDigestSHA256 || x.SourceManifest.DigestSHA256 != y.SourceManifest.DigestSHA256 {
		t.Fatal("relocated checkout produced different digests")
	}
}

// repeated builds are byte-identical
func TestRepeatedBuildStable(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	a := buildOne(t, root, ValidationPolicy{})
	b := buildOne(t, root, ValidationPolicy{})
	if !bytes.Equal(a.NTriples, b.NTriples) {
		t.Fatal("repeated build is not byte-identical")
	}
}

// 15. nested generated handling
func TestNestedGeneratedSkipped(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{
		"invariants.yaml":     invariantsAlpha,
		"generated/beta.yaml": invariantsBeta,
	})
	kept := srcRoot(root)
	skip := srcRoot(root)
	skip.SkipNestedGenerated = true

	withGen, err := Build(context.Background(), CompileRequest{Sources: []SourceRoot{kept}, Policy: ValidationPolicy{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	withoutGen, err := Build(context.Background(), CompileRequest{Sources: []SourceRoot{skip}, Policy: ValidationPolicy{}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(withGen.NTriples, []byte("test.inv.beta")) {
		t.Fatal("nested generated file should be included by default")
	}
	if bytes.Contains(withoutGen.NTriples, []byte("test.inv.beta")) {
		t.Fatal("SkipNestedGenerated must exclude nested generated/ files")
	}
	found := false
	for _, ex := range withoutGen.SourceManifest.Exclusions {
		if strings.Contains(ex, "nested generated") {
			found = true
		}
	}
	if !found {
		t.Fatal("nested-generated exclusion rule not recorded")
	}
}

// 21. supplemental graph accepted + marker does not survive
func TestSupplementalGraphAccepted(t *testing.T) {
	supRoot := t.TempDir()
	writeAwareness(t, supRoot, map[string]string{"invariants.yaml": invariantsBeta})
	supArt := buildOne(t, supRoot, ValidationPolicy{})

	mainRoot := t.TempDir()
	writeAwareness(t, mainRoot, map[string]string{"invariants.yaml": invariantsAlpha})
	comp := compileOne(t, mainRoot, ValidationPolicy{})

	art, err := Finalize(context.Background(), FinalizeRequest{
		Compilation: comp,
		SupplementalGraphs: []SupplementalGraph{{
			ID: "pack.test", Version: "v1", NTriples: supArt.NTriples,
			ExpectedSemanticDigestSHA256: supArt.GraphSemanticDigestSHA256,
		}},
	})
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if !bytes.Contains(art.NTriples, []byte("test.inv.alpha")) || !bytes.Contains(art.NTriples, []byte("test.inv.beta")) {
		t.Fatal("combined graph missing project or supplemental triples")
	}
	// The supplemental's own marker IRI must not survive; exactly one marker remains.
	if bytes.Contains(art.NTriples, []byte(supArt.MarkerIRI)) {
		t.Fatal("supplemental marker survived into the final artifact")
	}
	var sawPack bool
	for _, s := range art.SourceManifest.SupplementalGraphs {
		if s.ID == "pack.test" && s.DigestSHA256 == supArt.GraphSemanticDigestSHA256 {
			sawPack = true
		}
	}
	if !sawPack {
		t.Fatalf("supplemental graph not recorded in manifest: %+v", art.SourceManifest.SupplementalGraphs)
	}
}

// 22. supplemental marker mismatch refused
func TestSupplementalMarkerMismatchRefused(t *testing.T) {
	supRoot := t.TempDir()
	writeAwareness(t, supRoot, map[string]string{"invariants.yaml": invariantsBeta})
	supArt := buildOne(t, supRoot, ValidationPolicy{})
	mainRoot := t.TempDir()
	writeAwareness(t, mainRoot, map[string]string{"invariants.yaml": invariantsAlpha})
	comp := compileOne(t, mainRoot, ValidationPolicy{})

	_, err := Finalize(context.Background(), FinalizeRequest{
		Compilation: comp,
		SupplementalGraphs: []SupplementalGraph{{
			ID: "pack.bad", NTriples: supArt.NTriples,
			ExpectedSemanticDigestSHA256: strings.Repeat("0", 64),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "marker digest mismatch") {
		t.Fatalf("expected marker mismatch refusal, got %v", err)
	}
}

// 23. supplemental order deterministic
func TestSupplementalOrderDeterministic(t *testing.T) {
	mk := func(content string) SupplementalGraph {
		r := t.TempDir()
		writeAwareness(t, r, map[string]string{"invariants.yaml": content})
		a := buildOne(t, r, ValidationPolicy{})
		return SupplementalGraph{ID: "id-" + content[:12], NTriples: a.NTriples, ExpectedSemanticDigestSHA256: a.GraphSemanticDigestSHA256}
	}
	s1 := mk(invariantsAlpha)
	s2 := mk(invariantsBeta)
	mainRoot := t.TempDir()
	writeAwareness(t, mainRoot, map[string]string{"invariants.yaml": "invariants: []\n"})
	comp := compileOne(t, mainRoot, ValidationPolicy{})

	x, err := Finalize(context.Background(), FinalizeRequest{Compilation: comp, SupplementalGraphs: []SupplementalGraph{s1, s2}})
	if err != nil {
		t.Fatal(err)
	}
	y, err := Finalize(context.Background(), FinalizeRequest{Compilation: comp, SupplementalGraphs: []SupplementalGraph{s2, s1}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(x.NTriples, y.NTriples) {
		t.Fatal("supplemental order changed the artifact bytes")
	}
	if x.SourceManifest.DigestSHA256 != y.SourceManifest.DigestSHA256 {
		t.Fatal("supplemental order changed the manifest digest")
	}
}

// 24. external source symlink refused (closure-strict)
func TestExternalSymlinkRefused(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.yaml")
	if err := os.WriteFile(secret, []byte(invariantsBeta), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir := writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	if err := os.Symlink(secret, filepath.Join(dir, "linked.yaml")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// Permissive policy follows it; closure-strict refuses the escape.
	if _, err := Compile(context.Background(), CompileRequest{Sources: []SourceRoot{srcRoot(root)}, Policy: ClosureStrictPolicy()}); err == nil || !strings.Contains(err.Error(), "symlink_escape") {
		t.Fatalf("closure-strict must refuse external symlink, got %v", err)
	}
}

// 25. path traversal refused (source root escapes identity root)
func TestSourceRootEscapeRefused(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	escaping := SourceRoot{
		FilesystemPath: filepath.Dir(root), // parent of the identity root
		IdentityRoot:   root,
	}
	if _, err := Compile(context.Background(), CompileRequest{Sources: []SourceRoot{escaping}, Policy: ValidationPolicy{}}); err == nil || !strings.Contains(err.Error(), "escapes identity root") {
		t.Fatalf("expected identity-root escape refusal, got %v", err)
	}
}

// 26. context cancellation is respected
func TestContextCancellation(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compile(ctx, CompileRequest{Sources: []SourceRoot{srcRoot(root)}, Policy: ValidationPolicy{}}); err == nil {
		t.Fatal("cancelled context must abort compile")
	}
}

// 27. parallel builds race-clean
func TestParallelBuildsRaceClean(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha})
	want := buildOne(t, root, ValidationPolicy{}).ArtifactByteDigestSHA256
	var wg sync.WaitGroup
	errs := make([]error, 8)
	digests := make([]string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			art, err := Build(context.Background(), CompileRequest{Sources: []SourceRoot{srcRoot(root)}, Policy: ValidationPolicy{}}, nil)
			errs[i] = err
			if err == nil {
				digests[i] = art.ArtifactByteDigestSHA256
			}
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("parallel build %d: %v", i, errs[i])
		}
		if digests[i] != want {
			t.Fatalf("parallel build %d digest %s != %s", i, digests[i], want)
		}
	}
}

// 28. repository filesystem unchanged
func TestFilesystemUnchanged(t *testing.T) {
	root := t.TempDir()
	writeAwareness(t, root, map[string]string{"invariants.yaml": invariantsAlpha, "more.yaml": invariantsBeta})
	before := snapshotTree(t, root)
	buildOne(t, root, ValidationPolicy{})
	after := snapshotTree(t, root)
	if before != after {
		t.Fatal("build mutated the source filesystem")
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if info.IsDir() {
			b.WriteString("d:" + rel + "\n")
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		b.WriteString("f:" + rel + ":" + string(data) + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
