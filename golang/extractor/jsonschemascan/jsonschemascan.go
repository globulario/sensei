// SPDX-License-Identifier: AGPL-3.0-only

// Package jsonschemascan finds JSON Schema documents in a repository:
// .json/.yaml/.yml files whose TOP-LEVEL `$schema` key references a
// recognized JSON Schema draft URI (draft-04 through 2020-12, Hyper-Schema
// variants included).
//
// Discovery-only, mirroring golang/extractor/openapiscan's FindSpecFiles —
// a file is a candidate the moment its top level names a `$schema` key
// pointing at a known draft URI; ScanSchema is not needed for that purpose,
// so this package stays intentionally narrow (no content-extraction/contract
// synthesis) until a real consumer needs it.
package jsonschemascan

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
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

// schemaDraftRe matches a recognized JSON Schema draft URI value — draft-03
// through 2020-12, including the http/https and Hyper-Schema variants.
// Applied only to the TOP-LEVEL `$schema` value (see looksLikeSchema), never
// to raw file text, so a nested `$schema` inside a sub-object (e.g. a
// property named "$schema" several levels deep) is never mistaken for the
// document's own schema declaration.
var schemaDraftRe = regexp.MustCompile(`(?i)^https?://json-schema\.org/(draft[-/](0[3-7]|2019-09|2020-12)|draft-\d+/(hyper-)?schema)`)

// FindSchemaFiles walks root and returns candidate JSON Schema files
// (.json/.yaml/.yml whose top level names a recognized `$schema` draft URI).
// Sorted for determinism.
//
// malformed lists every walk error and every file this scan could not open
// or read — an evaluation failure a caller must never silently read as "not
// a schema" (contract §4/§6 correction). A file that opens fine but fails to
// parse as JSON/YAML, or parses but has no matching `$schema` key, is
// legitimately "not a schema" (the common case for most repository files)
// and is not reported as malformed.
func FindSchemaFiles(root string) (files []string, malformed []string, err error) {
	absRoot, absErr := filepath.Abs(root)
	if absErr != nil {
		return nil, nil, absErr
	}
	walkErr := filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			malformed = append(malformed, fmt.Sprintf("%s: %v", p, walkErr))
			return nil // best-effort: one unreadable entry must not abort the whole scan.
		}
		if d.IsDir() {
			if p != absRoot && excludedDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".json", ".yaml", ".yml":
			isSchema, evalErr := looksLikeSchema(p)
			if evalErr != nil {
				malformed = append(malformed, fmt.Sprintf("%s: %v", p, evalErr))
				return nil
			}
			if isSchema {
				files = append(files, p)
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, malformed, walkErr
	}
	sort.Strings(files)
	return files, malformed, nil
}

// looksLikeSchema reports whether path's TOP-LEVEL `$schema` key names a
// recognized JSON Schema draft URI. It parses the whole document (never
// truncated) so a `$schema` key anywhere in the top-level object is found
// regardless of file size or key order.
//
// A file that cannot be opened/read is a genuine evaluation failure
// (returned as an error). A file that opens fine but is not valid JSON/YAML,
// or has no matching top-level `$schema` key, is legitimately "not a
// schema" (ok=false, err=nil) — most JSON/YAML files in a repository are
// exactly that, and reporting all of them as malformed would drown real
// signal.
func looksLikeSchema(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read: %w", err)
	}
	var doc map[string]any
	if strings.ToLower(filepath.Ext(path)) == ".json" {
		if jsonErr := json.Unmarshal(raw, &doc); jsonErr != nil {
			return false, nil
		}
	} else if yamlErr := yaml.Unmarshal(raw, &doc); yamlErr != nil {
		return false, nil
	}
	schema, ok := doc["$schema"]
	if !ok {
		return false, nil
	}
	s, ok := schema.(string)
	if !ok {
		return false, nil
	}
	return schemaDraftRe.MatchString(strings.TrimSpace(s)), nil
}
