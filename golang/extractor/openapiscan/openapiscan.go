// SPDX-License-Identifier: AGPL-3.0-only

// Package openapiscan parses OpenAPI / Swagger spec files into architecture
// Contract nodes.
//
// One Contract per spec (uml.kind Interface) and one per path×method operation
// (uml.kind Operation). Read/write is inferred from the HTTP method. Every
// contract carries assertion: inferred — derived from the spec, never
// hand-authored. It never invents endpoints: a contract exists only if the spec
// declares it.
//
// Spec-file driven only (OpenAPI 3.0/3.1, Swagger 2.0; .yaml/.yml/.json with a
// top-level openapi:/swagger: key). It does NOT read framework route code. The
// reusable core behind both the `openapi-scan` CLI and `awg bootstrap`.
package openapiscan

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Contract mirrors the fields the architecture_contracts importer reads. Field
// order is the YAML key order — keep it stable so generated files are deterministic.
type Contract struct {
	ID          string   `yaml:"id"`
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Kind        string   `yaml:"kind"`
	Stability   string   `yaml:"stability,omitempty"`
	ReadOrWrite string   `yaml:"read_or_write"`
	Assertion   string   `yaml:"assertion"`
	ExposedBy   []string `yaml:"exposed_by,omitempty"`
	SourceFiles []string `yaml:"source_files"`
	Uml         *UML     `yaml:"uml,omitempty"`
}

// UML is the optional UML profile block emitted on inferred contracts.
type UML struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Confidence string `yaml:"confidence"`
}

// Doc is the top-level `contracts:` document.
type Doc struct {
	Contracts []Contract `yaml:"contracts"`
}

// excludedDir reports whether a directory should be skipped during spec discovery.
func excludedDir(name string) bool {
	switch name {
	case "vendor", "node_modules", ".git", "dist", "build", "bin", "out",
		"third_party", "thirdparty", "generated", "candidates", ".sensei", ".awg", "testdata",
		"target", "example", "examples":
		return true
	}
	return false
}

var specKeyRe = regexp.MustCompile(`(?i)(openapi|swagger)["\s]*:`)

// FindSpecFiles walks root and returns candidate OpenAPI/Swagger spec files
// (.yaml/.yml/.json whose head names an openapi:/swagger: key). Sorted for
// determinism. ScanSpec confirms a candidate is a real spec.
func FindSpecFiles(root string) ([]string, error) {
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
		case ".yaml", ".yml", ".json":
			if looksLikeSpec(p) {
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

// looksLikeSpec sniffs the head of a file for an openapi:/swagger: key.
func looksLikeSpec(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, _ := f.Read(head)
	return specKeyRe.Match(head[:n])
}

// rawSpec is the minimal subset of an OpenAPI/Swagger document we parse.
type rawSpec struct {
	OpenAPI string `yaml:"openapi"`
	Swagger string `yaml:"swagger"`
	Info    struct {
		Title string `yaml:"title"`
	} `yaml:"info"`
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

type rawOp struct {
	OperationID string `yaml:"operationId"`
	Summary     string `yaml:"summary"`
	Description string `yaml:"description"`
	Deprecated  bool   `yaml:"deprecated"`
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"patch": true, "head": true, "options": true, "trace": true,
}

// readMethods are the HTTP methods classified as read (the rest are write).
var readMethods = map[string]bool{"get": true, "head": true, "options": true, "trace": true}

// ScanSpec parses one spec file and returns its Interface + Operation contracts.
// Returns nil (no error) if the file is not actually an OpenAPI/Swagger spec.
func ScanSpec(path, repoRoot string) ([]Contract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// Version probe first (a Paths-free struct), so a non-spec YAML — even one
	// with an unrelated `paths:` of a different shape — returns cleanly rather
	// than erroring on the paths decode.
	var ver struct {
		OpenAPI string `yaml:"openapi"`
		Swagger string `yaml:"swagger"`
	}
	if err := yaml.Unmarshal(data, &ver); err != nil {
		return nil, nil // not parseable as our shape → not a spec we handle
	}
	if strings.TrimSpace(ver.OpenAPI) == "" && strings.TrimSpace(ver.Swagger) == "" {
		return nil, nil // not a spec
	}
	var spec rawSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	relPath := path
	if r, rerr := filepath.Rel(repoRoot, path); rerr == nil {
		relPath = filepath.ToSlash(r)
	}
	title := strings.TrimSpace(spec.Info.Title)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	apiSlug := slug(title)
	if apiSlug == "" {
		apiSlug = slug(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	}

	var ops []Contract
	hasRead, hasWrite := false, false
	for _, p := range sortedMapKeys(spec.Paths) {
		methods := spec.Paths[p]
		for _, m := range sortedMapKeys(methods) {
			lm := strings.ToLower(m)
			if !httpMethods[lm] {
				continue // ignore parameters / $ref / summary / servers
			}
			var op rawOp
			node := methods[m]
			_ = node.Decode(&op) // best-effort; missing fields stay zero

			rw := "write"
			if readMethods[lm] {
				rw = "read"
				hasRead = true
			} else {
				hasWrite = true
			}
			opSlug := slug(op.OperationID)
			if opSlug == "" {
				opSlug = lm + "_" + slug(p)
			}
			desc := strings.TrimSpace(op.Summary)
			if desc == "" {
				desc = strings.TrimSpace(op.Description)
			}
			desc = joinNonEmpty(desc, strings.ToUpper(lm)+" "+p)
			stability := "stable"
			if op.Deprecated {
				stability = "deprecated"
			}
			ops = append(ops, Contract{
				ID:          "contract." + apiSlug + "." + opSlug,
				Name:        strings.ToUpper(lm) + " " + p,
				Description: desc,
				Kind:        "rest",
				Stability:   stability,
				ReadOrWrite: rw,
				Assertion:   "inferred",
				SourceFiles: []string{relPath},
				Uml:         &UML{Kind: "Operation", Stereotype: "rest_endpoint", View: "interaction", Confidence: "inferred"},
			})
		}
	}

	svcRW := "unknown"
	switch {
	case hasWrite && hasRead:
		svcRW = "read_write"
	case hasWrite:
		svcRW = "write"
	case hasRead:
		svcRW = "read"
	}
	svc := Contract{
		ID:          "contract." + apiSlug,
		Name:        title,
		Description: joinNonEmpty("REST API "+title, fmt.Sprintf("%d operation(s).", len(ops))),
		Kind:        "rest",
		Stability:   "stable",
		ReadOrWrite: svcRW,
		Assertion:   "inferred",
		SourceFiles: []string{relPath},
		Uml:         &UML{Kind: "Interface", Stereotype: "rest_api", View: "interaction", Confidence: "inferred"},
	}
	return append([]Contract{svc}, ops...), nil
}

// Render produces the deterministic generated YAML for a contracts document.
func Render(doc Doc) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by cmd/openapi-scan — DO NOT EDIT.\n")
	buf.WriteString("# Contract nodes inferred from OpenAPI/Swagger specs (assertion: inferred).\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// slug lowercases and collapses non-alphanumeric runs to a single "_".
func slug(s string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
		} else if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return strings.Join(out, " — ")
}
