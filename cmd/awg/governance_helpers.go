// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

func compileAwarenessInputs(inputDirs []string, repo, domain, sourceSet string, strict bool) ([]byte, int, error) {
	var buf bytes.Buffer
	totalTriples := 0
	for _, dir := range inputDirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, 0, err
		}
		if !info.IsDir() {
			return nil, 0, fmt.Errorf("%s is not a directory", dir)
		}
		emitter, report, err := extractor.ImportAwarenessDirWithOpts(dir, &buf, extractor.ImportDirOptions{
			DefaultRepo:      strings.TrimSpace(repo),
			DefaultDomain:    strings.TrimSpace(domain),
			DefaultSourceSet: strings.TrimSpace(sourceSet),
		})
		if err != nil {
			return nil, 0, fmt.Errorf("import %s: %w", dir, err)
		}
		if strict && report.HasUnknown() {
			for _, f := range report.Skipped() {
				return nil, 0, fmt.Errorf("--strict: %s: %s", f.Path, f.Status)
			}
			return nil, 0, fmt.Errorf("--strict: unrecognized YAML schema(s) in %s", dir)
		}
		totalTriples += emitter.Triples
	}
	return buf.Bytes(), totalTriples, nil
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
	marker, ok := seedmeta.ParseMarker(nt)
	if !ok {
		return bytes.TrimSpace(nt)
	}
	needle := "<" + marker.IRI + ">"
	var kept []string
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, needle+" ") {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return nil
	}
	return append([]byte(strings.Join(kept, "\n")), '\n')
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
