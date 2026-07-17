// SPDX-License-Identifier: Apache-2.0

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
	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/governancepack"
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
func compileAwarenessInputs(inputDirs []string, repo, domain, sourceSet string, strict bool) ([]byte, int, error) {
	sources := make([]graphbuild.SourceRoot, 0, len(inputDirs))
	for _, dir := range inputDirs {
		sources = append(sources, graphbuild.SourceRoot{
			FilesystemPath:   dir,
			IdentityRoot:     dir,
			RepositoryDomain: strings.TrimSpace(repo),
			DefaultDomain:    strings.TrimSpace(domain),
			DefaultSourceSet: strings.TrimSpace(sourceSet),
		})
	}
	policy := graphbuild.ValidationPolicy{}
	if strict {
		policy = graphbuild.CompatibilityPolicy()
	}
	comp, err := graphbuild.Compile(context.Background(), graphbuild.CompileRequest{Sources: sources, Policy: policy})
	if err != nil {
		return nil, 0, err
	}
	return comp.CanonicalNTriples, comp.UniqueTripleCount, nil
}

func buildProjectArtifact(root string) ([]byte, error) {
	inputDirs := defaultBuildInputDirsFromRoot(root)
	raw, _, err := compileAwarenessInputs(inputDirs, "", "", "", false)
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
