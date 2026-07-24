// SPDX-License-Identifier: AGPL-3.0-only

package protection

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/extractor/openapiscan"
	"github.com/globulario/sensei/golang/extractor/protoscan"
	"github.com/globulario/sensei/golang/scanner"
)

// StructuralContractReasons returns definite (non-provisional) protection
// reasons for files a supported deterministic scanner identifies as a
// machine-readable contract surface or an annotated contract/invariant
// definition (contract §3.2):
//
//   - protobuf service/message definitions (protoscan.FindProtoFiles);
//   - OpenAPI/Swagger spec files (openapiscan.FindSpecFiles);
//   - source files carrying at least one @awareness annotation
//     (golang/scanner) — an authored annotation is an explicit assertion,
//     not a candidate.
//
// JSON Schema is named in the contract as a supported source, but this
// repository has no existing deterministic JSON-Schema scanner to source
// from; per §15 ("do not fill semantic gaps with filename heuristics beyond
// the explicitly supported structural contract sources") this function does
// not invent one. Callers should treat that as a documented coverage gap,
// not silently-covered.
func StructuralContractReasons(repoRoot string) (map[string][]ProtectionReason, error) {
	out := map[string][]ProtectionReason{}
	add := func(target, kind, source string) {
		norm, ok := NormalizePath(target)
		if !ok {
			return
		}
		out[norm] = append(out[norm], ProtectionReason{
			Origin: OriginStructuralContract,
			Kind:   kind,
			Source: source,
		})
	}

	protoFiles, err := protoscan.FindProtoFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	for _, f := range protoFiles {
		rel, ok := relTo(repoRoot, f)
		if !ok {
			continue
		}
		add(rel, "protobuf_contract", rel)
	}

	specFiles, err := openapiscan.FindSpecFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	for _, f := range specFiles {
		rel, ok := relTo(repoRoot, f)
		if !ok {
			continue
		}
		add(rel, "openapi_contract", rel)
	}

	annotated, err := annotatedSourceFiles(repoRoot)
	if err != nil {
		return nil, err
	}
	for _, f := range annotated {
		add(f, "awareness_annotation", f)
	}

	return out, nil
}

// relTo converts an absolute or repo-root-relative path returned by a
// scanner into a normalized repo-relative path.
func relTo(repoRoot, p string) (string, bool) {
	if filepath.IsAbs(p) {
		r, err := filepath.Rel(repoRoot, p)
		if err != nil {
			return "", false
		}
		p = r
	}
	return NormalizePath(p)
}

// annotatedSourceFiles returns the deduplicated, normalized set of files
// carrying at least one @awareness annotation, via the existing golang/
// scanner package (the same scanner `sensei bootstrap` uses for code-symbol
// extraction). A missing/absent namespace registry falls back to
// scanner.LoadRegistry's own default rather than failing derivation.
func annotatedSourceFiles(repoRoot string) ([]string, error) {
	registryPath := findNamespaceRegistry(repoRoot)
	if registryPath == "" {
		// No namespace registry at all: a legitimate "nothing to scan with"
		// state (e.g. a fresh repository), not a hard failure — LoadRegistry
		// itself requires a real file and errors on "".
		return nil, nil
	}
	reg, err := scanner.LoadRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	sc := &scanner.Scanner{Registry: reg, RepoRoot: repoRoot, Strict: false}
	result, err := sc.Scan(repoRoot)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, a := range result.Annotations {
		norm, ok := NormalizePath(a.File)
		if !ok || seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	return out, nil
}

func findNamespaceRegistry(repoRoot string) string {
	p := joinRepo(repoRoot, AwarenessDir+"/namespaces.yaml")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// candidateFile is the loose generic shape shared by every
// docs/awareness/candidates/*.yaml file: exactly one top-level key whose
// value has a `candidates:` list, each entry carrying an `id` and either
// `source_files` or `files`.
type candidateEntry struct {
	ID          string   `yaml:"id"`
	SourceFiles []string `yaml:"source_files"`
	Files       []string `yaml:"files"`
}

// CandidateSignalReasons returns PROVISIONAL protection reasons for files
// referenced by any docs/awareness/candidates/*.yaml entry (authority
// surface, invariant, boundary, or contract-realization candidates). This is
// procedural caution only: it never promotes the candidate, and a rejected/
// deleted candidate signal removes the provisional reason on the next
// derivation (contract §3.2, §12).
func CandidateSignalReasons(repoRoot string) (map[string][]ProtectionReason, error) {
	dir := joinRepo(repoRoot, candidatesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]ProtectionReason{}, nil
		}
		return nil, err
	}
	out := map[string][]ProtectionReason{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".yaml" && filepath.Ext(name) != ".yml" {
			continue
		}
		relSource, ok := NormalizePath(candidatesDir + "/" + name)
		if !ok {
			continue
		}
		raw, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}
		var doc map[string]struct {
			Candidates []candidateEntry `yaml:"candidates"`
		}
		if yaml.Unmarshal(raw, &doc) != nil {
			continue // malformed candidate file: skip rather than fail whole derivation.
		}
		for _, section := range doc {
			for _, c := range section.Candidates {
				targets := c.SourceFiles
				targets = append(targets, c.Files...)
				for _, target := range targets {
					norm, ok := NormalizePath(target)
					if !ok {
						continue
					}
					out[norm] = append(out[norm], ProtectionReason{
						Origin:       OriginCandidateSignal,
						Kind:         "candidate_source_file",
						Source:       relSource,
						KnowledgeRef: c.ID,
						Provisional:  true,
					})
				}
			}
		}
	}
	return out, nil
}
