// SPDX-License-Identifier: AGPL-3.0-only

// Package jsonschemascan finds JSON Schema documents in a repository:
// .json/.yaml/.yml files whose top-level `$schema` key references a
// recognized JSON Schema draft URI (draft-04 through 2020-12, Hyper-Schema
// variants included).
//
// Discovery-only, mirroring golang/extractor/openapiscan's FindSpecFiles —
// a file is a candidate the moment its head names a `$schema` key pointing
// at a known draft URI; ScanSchema is not needed for that purpose, so this
// package stays intentionally narrow (no content-extraction/contract
// synthesis) until a real consumer needs it.
package jsonschemascan

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// excludedDir mirrors protoscan/openapiscan's discovery exclusions.
func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".sensei", ".awg", "testdata",
		"target", "example", "examples":
		return true
	}
	return false
}

// schemaKeyRe matches a `"$schema"` (JSON) or `$schema:` (YAML) key whose
// value is a recognized JSON Schema draft URI — draft-03 through 2020-12,
// including the http/https and the Hyper-Schema variants.
var schemaKeyRe = regexp.MustCompile(`(?i)["]?\$schema["]?\s*:\s*["']?https?://json-schema\.org/(draft[-/](0[3-7]|2019-09|2020-12)|draft-\d+/(hyper-)?schema)`)

// FindSchemaFiles walks root and returns candidate JSON Schema files
// (.json/.yaml/.yml whose head names a recognized `$schema` draft URI).
// Sorted for determinism.
func FindSchemaFiles(root string) ([]string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var out []string
	walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != absRoot && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".json", ".yaml", ".yml":
			if looksLikeSchema(p) {
				out = append(out, p)
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(out)
	return out, nil
}

// looksLikeSchema sniffs the head of a file for a recognized $schema key.
func looksLikeSchema(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := f.Read(head)
	return schemaKeyRe.Match(head[:n])
}
