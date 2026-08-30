// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/packcustody"
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/governancepack"
	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/seedmeta"
)

func httpDefaultClient() *http.Client {
	return http.DefaultClient
}

func extractorValidate(nt []byte) []error {
	raw := extractor.ValidateNTriples(bytes.NewReader(nt))
	out := make([]error, 0, len(raw))
	for _, err := range raw {
		out = append(out, err)
	}
	return out
}

func defaultBuildInputDirsFromRoot(root string) []string {
	dirs := appendExistingDir(nil,
		filepath.Join(root, "docs", "awareness"),
		filepath.Join(root, "eval", "multi-swe-bench", "contracts"),
		filepath.Join(root, "eval", "multi-swe-bench", "notes", "learning_events"),
	)
	if len(dirs) == 0 {
		dirs = []string{filepath.Join(root, "docs", "awareness")}
	}
	return dirs
}

// compileAwarenessInputs selects the explicit source roots and delegates all
// graph semantics to graphbuild. It returns the canonical marker-free graph;
// callers finalize it (idempotently) to stamp the whole-graph marker. Source
// discovery stays here; compilation/canonicalization/validation live in the
// package.
// compileAwarenessInputs compiles inputDirs into N-Triples.
//
// repositoryIdentity, when non-empty, is the canonical repository identity
// every SourceFile subject in these trees is scoped to (issue #197). Only a
// caller that genuinely knows which repository a tree belongs to passes it
// -- `sensei import`, which is handed the identity of the repository it is
// importing. An empty value means "resolve it from each tree's own
// checkout", which is what `sensei build` does, so a cross-repo input is
// attributed to the repository it lives in rather than to the domain this
// build happens to publish under.
// consumedFrom lifts the per-file records into the witness shape: what the
// compilation actually read, with the digest taken from the parsed buffer.
func consumedFrom(rep extractor.ImportReport) []publication.ConsumedFile {
	var out []publication.ConsumedFile
	for _, f := range rep.Files {
		if f.ConsumedDigest == "" {
			if f.Status == extractor.StatusImported {
				// A file that CONTRIBUTED TRIPLES with no recorded digest
				// cannot be proven, and skipping it would let its bytes escape
				// the proof entirely. Skipping was the hole: "not digested"
				// silently became "not consumed", so an importer that never
				// recorded a digest published facts nothing checked.
				out = append(out, publication.ConsumedFile{Path: f.Path})
			}
			continue
		}
		out = append(out, publication.ConsumedFile{Path: f.Path, Digest: f.ConsumedDigest})
	}
	return out
}

func compileAwarenessInputs(inputDirs []string, repositoryIdentity, repo, domain, sourceSet string, strict bool) ([]byte, int, []publication.ConsumedFile, error) {
	scopedRepo := strings.TrimSpace(repo)
	sources := make([]graphbuild.SourceRoot, 0, len(inputDirs))
	for _, dir := range inputDirs {
		root := graphbuild.SourceRoot{
			FilesystemPath:     dir,
			IdentityRoot:       dir,
			RepositoryDomain:   scopedRepo,
			DefaultDomain:      strings.TrimSpace(domain),
			DefaultSourceSet:   strings.TrimSpace(sourceSet),
			RepositoryIdentity: strings.TrimSpace(repositoryIdentity),
		}
		// Custody derivation is enabled exactly when a repository domain is
		// named, because that is exactly when the build attributes authorship:
		// RepositoryDomain above tags every otherwise-unscoped node in this tree
		// to one repository, which is correct for the repository's own knowledge
		// and wrong for shared knowledge merely installed into its checkout.
		// A build with no repository domain claims no authorship, so there is
		// nothing to mis-attribute and nothing to derive.
		if scopedRepo != "" {
			if custodyRoot, ok := packcustody.ProjectRootFor(dir); ok {
				root.CustodyRoot = custodyRoot
			}
		}
		sources = append(sources, root)
	}
	policy := graphbuild.ValidationPolicy{}
	if strict {
		policy = graphbuild.CompatibilityPolicy()
	}
	comp, err := graphbuild.Compile(context.Background(), graphbuild.CompileRequest{Sources: sources, Policy: policy})
	if err != nil {
		return nil, 0, nil, err
	}
	// Exclusions are announced, never silent. A document dropped from a
	// publication without a word looks exactly like knowledge that quietly went
	// missing, and telling those two apart after the fact is the expensive kind
	// of debugging this mechanism exists to prevent.
	if out := comp.ImportReport.FormatCustodyExclusions(); out != "" {
		fmt.Fprint(os.Stderr, out)
		if comp.ImportReport.HasCustodyRefusal() {
			fmt.Fprintln(os.Stderr,
				"  custody: at least one managed projection has no governed provenance and was published by nobody.")
		}
	}
	return comp.CanonicalNTriples, comp.UniqueTripleCount, consumedFrom(comp.ImportReport), nil
}

func buildProjectArtifact(root string) ([]byte, error) {
	inputDirs := defaultBuildInputDirsFromRoot(root)
	raw, _, _, err := compileAwarenessInputs(inputDirs, "", "", "", "", false)
	if err != nil {
		return nil, err
	}
	projectNT, _, _, _ := finalizeBuildArtifact(raw)
	return projectNT, nil
}

func stripGraphMarkerLines(nt []byte) []byte {
	return graphbuild.StripMarker(nt)
}

func combineGraphArtifacts(governanceNT, projectNT []byte) ([]byte, seedmeta.Marker, int, int) {
	var merged bytes.Buffer
	merged.Write(stripGraphMarkerLines(governanceNT))
	merged.Write(stripGraphMarkerLines(projectNT))
	return finalizeBuildArtifact(merged.Bytes())
}

func verifyActiveGovernancePack(root string) (*governancepack.VerifiedPack, *governancepack.ActiveRecord, error) {
	active, err := readActiveGovernance(root)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", governancepack.FailureActivePackMissing, err)
	}
	manifestPath := active.ManifestPath
	if !filepath.IsAbs(manifestPath) {
		manifestPath = filepath.Join(root, filepath.FromSlash(manifestPath))
	}
	verified, err := governancepack.VerifyPack(manifestPath, governancepack.TrustedKeysPath(root), Version)
	if err != nil {
		return nil, active, err
	}
	return &verified, active, nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
