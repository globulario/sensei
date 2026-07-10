// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"google.golang.org/protobuf/proto"

	"github.com/scip-code/scip/bindings/go/scip"

	"github.com/globulario/awareness-graph/golang/scanner"
	"github.com/globulario/awareness-graph/golang/scipingest"
)

// ingestScipFile reads a SCIP index file and renders the two awareness YAML
// documents it maps to: code_symbols.yaml (function/method/type nodes) and
// code_references.yaml (symbol→symbol reference edges). Shared by the
// `awg scip-ingest` CLI and `awg bootstrap`. Returns the parsed Result and the
// document count for reporting.
func ingestScipFile(scipPath, langOverride string, excludeTests bool) (symbolsYAML, refsYAML []byte, res scipingest.Result, nDocs int, err error) {
	data, err := os.ReadFile(scipPath)
	if err != nil {
		return nil, nil, res, 0, err
	}
	var idx scip.Index
	if err = proto.Unmarshal(data, &idx); err != nil {
		return nil, nil, res, 0, fmt.Errorf("parse %s: %w", scipPath, err)
	}
	res = scipingest.Ingest(&idx, scipingest.Options{LanguageOverride: langOverride, ExcludeTestFiles: excludeTests})
	var sb bytes.Buffer
	if err = scanner.WriteSymbolsYAML(&sb, res.Symbols, nil); err != nil {
		return nil, nil, res, 0, err
	}
	return sb.Bytes(), renderReferencesYAML(res.Refs), res, len(idx.GetDocuments()), nil
}

// renderReferencesYAML serializes reference edges into the code_references
// schema consumed by importCodeReferences. Deterministic ordering.
func renderReferencesYAML(refs []scipingest.Ref) []byte {
	sorted := append([]scipingest.Ref(nil), refs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].FromID != sorted[j].FromID {
			return sorted[i].FromID < sorted[j].FromID
		}
		return sorted[i].ToName < sorted[j].ToName
	})
	var b bytes.Buffer
	fmt.Fprintln(&b, "code_references:")
	for _, r := range sorted {
		fmt.Fprintf(&b, "  - from: %s\n", r.FromID)
		if r.ToID != "" {
			fmt.Fprintf(&b, "    to_id: %s\n", r.ToID)
		}
		fmt.Fprintf(&b, "    to_name: %s\n", r.ToName)
		if r.File != "" {
			fmt.Fprintf(&b, "    file: %s\n", r.File)
		}
	}
	return b.Bytes()
}

// runScipIngest translates a SCIP index (index.scip) into the awareness YAML
// that `awg build` already ingests: code_symbols.yaml (per-function/method
// nodes) and code_references.yaml (symbol→symbol reference edges). This gives
// the structural graph symbol-level granularity without AWG owning a
// multi-language AST extractor — the SCIP index is produced by mature
// per-language indexers (scip-go, scip-typescript, …).
func runScipIngest(args []string) int {
	fs := flag.NewFlagSet("awg scip-ingest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	scipPath := fs.String("scip", "index.scip", "path to a SCIP index file")
	outDir := fs.String("out", ".", "directory to write code_symbols.yaml / code_references.yaml")
	lang := fs.String("language", "", "override the language for every document (optional)")
	excludeTests := fs.Bool("exclude-tests", false, "skip symbols/references defined in test files (*_test.go, *.test.ts, test_*.py, …)")
	quiet := fs.Bool("quiet", false, "suppress the summary line")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg scip-ingest --scip <index.scip> [--out <dir>]

Reads a SCIP index and emits awareness YAML consumed by `+"`awg build`"+`:
  code_symbols.yaml     one aw:CodeSymbol per defined function/method/type
  code_references.yaml  symbol→symbol reference edges (for sibling-site queries)

Produce the index first with a language indexer, e.g.:
  scip-go            # Go   -> index.scip
  scip-typescript    # TS/JS

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	symbolsYAML, refsYAML, res, nDocs, err := ingestScipFile(*scipPath, *lang, *excludeTests)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg scip-ingest: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "awg scip-ingest: mkdir %s: %v\n", *outDir, err)
		return 1
	}
	symbolsPath := filepath.Join(*outDir, "code_symbols.yaml")
	if err := os.WriteFile(symbolsPath, symbolsYAML, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "awg scip-ingest: write %s: %v\n", symbolsPath, err)
		return 1
	}
	refsPath := filepath.Join(*outDir, "code_references.yaml")
	if err := os.WriteFile(refsPath, refsYAML, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "awg scip-ingest: write %s: %v\n", refsPath, err)
		return 1
	}

	if !*quiet {
		fmt.Printf("scip-ingest: %d symbols, %d references from %d document(s) → %s\n",
			len(res.Symbols), len(res.Refs), nDocs, *outDir)
	}
	return 0
}
