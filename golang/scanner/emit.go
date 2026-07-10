// SPDX-License-Identifier: Apache-2.0

package scanner

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// CodeSymbol is one annotated symbol ready for YAML output.
type CodeSymbol struct {
	ID          string
	Namespace   string
	Language    string
	File        string
	Symbol      string
	Kind        string
	Component   string
	Risk        string
	Annotations map[string][]string
}

// CodeEdge is one directed relationship between awareness nodes.
type CodeEdge struct {
	From     string
	Relation string
	To       string
}

// TestSymbol is one discovered concrete test implementation from source code.
// It is evidence from code, not an authoritative required-test contract.
type TestSymbol struct {
	ID       string
	File     string
	Symbol   string
	Package  string
	Language string
	Doc      string
}

// BuildSymbolsAndEdges converts scan results into CodeSymbols and CodeEdges.
func BuildSymbolsAndEdges(result *ScanResult) ([]CodeSymbol, []CodeEdge, []TestSymbol) {
	var symbols []CodeSymbol
	var edges []CodeEdge
	var tests []TestSymbol

	for _, ann := range result.Annotations {
		if ann.Namespace == "" {
			continue // skip unannotatable items
		}
		id := symbolID(ann)

		risk := ""
		if r := ann.Keys["risk"]; len(r) > 0 {
			risk = r[0]
		}

		lang := ann.Language
		if lang == "" {
			lang = "go" // legacy callers predate the Language field
		}
		sym := CodeSymbol{
			ID:          id,
			Namespace:   ann.Namespace,
			Language:    lang,
			File:        ann.File,
			Symbol:      ann.Symbol,
			Kind:        ann.SymbolKind,
			Component:   ann.Component,
			Risk:        risk,
			Annotations: make(map[string][]string),
		}

		// Copy reference-type keys into annotations map; skip meta keys.
		for _, key := range ann.KeyOrder {
			if key == "namespace" || key == "component" || key == "risk" ||
				key == "symbol" || key == "file_role" || key == "owner" {
				continue
			}
			sym.Annotations[key] = ann.Keys[key]
		}
		symbols = append(symbols, sym)

		// Emit edges for each reference value.
		for _, key := range ann.KeyOrder {
			if !refKeys[key] && key != "tested_by" {
				continue
			}
			for _, val := range ann.Keys[key] {
				edges = append(edges, CodeEdge{From: id, Relation: key, To: val})
			}
		}
	}

	// Stable sort by ID.
	sort.Slice(symbols, func(i, j int) bool { return symbols[i].ID < symbols[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].Relation != edges[j].Relation {
			return edges[i].Relation < edges[j].Relation
		}
		return edges[i].To < edges[j].To
	})
	for _, dt := range result.DiscoveredTests {
		lang := dt.Language
		if lang == "" {
			lang = "go"
		}
		tests = append(tests, TestSymbol{
			ID:       discoveredTestID(dt),
			File:     dt.File,
			Symbol:   dt.Symbol,
			Package:  dt.Package,
			Language: lang,
			Doc:      dt.Doc,
		})
	}
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].ID != tests[j].ID {
			return tests[i].ID < tests[j].ID
		}
		return tests[i].File < tests[j].File
	})
	return symbols, edges, tests
}

// symbolID generates the canonical code symbol ID for an annotation.
// Format: <namespace>:code.<lang>.<component_or_file>.<symbol>
// where <lang> is the short language segment (go, ts, js, py, rs). Language lives in the
// ID so a Go and a TypeScript symbol with the same component+name never
// collide, while both still cite the same global knowledge IDs
// (invariant awareness.reference_ids_are_global_across_languages).
func symbolID(ann Annotation) string {
	comp := ann.Component
	if comp == "" {
		// Derive component from file path: last two path segments minus extension.
		parts := strings.Split(ann.File, "/")
		if len(parts) >= 2 {
			dir := parts[len(parts)-2]
			comp = dir
		}
	}
	// Normalize component: replace / and - with .
	comp = strings.ReplaceAll(comp, "/", ".")
	comp = strings.ReplaceAll(comp, "-", "_")

	lang := "go"
	switch ann.Language {
	case "", "go":
		lang = "go"
	case "typescript":
		lang = "ts"
	case "javascript":
		lang = "js"
	case "python":
		lang = "py"
	case "rust":
		lang = "rs"
	default:
		lang = ann.Language
	}

	sym := ann.Symbol
	if sym == "" {
		// File-level: no symbol suffix.
		return fmt.Sprintf("%s:code.%s.%s", ann.Namespace, lang, comp)
	}
	// Normalize symbol: replace . with _ to keep IRI segment clean.
	sym = strings.ReplaceAll(sym, ".", "_")
	return fmt.Sprintf("%s:code.%s.%s.%s", ann.Namespace, lang, comp, sym)
}

func discoveredTestID(dt DiscoveredTest) string {
	return dt.File + ":" + dt.Symbol
}

// WriteSymbolsYAML serializes code symbols to YAML format.
func WriteSymbolsYAML(w io.Writer, symbols []CodeSymbol, tests []TestSymbol) error {
	fmt.Fprintln(w, "code_symbols:")
	for _, s := range symbols {
		fmt.Fprintf(w, "  - id: %s\n", s.ID)
		fmt.Fprintf(w, "    namespace: %s\n", s.Namespace)
		fmt.Fprintf(w, "    language: %s\n", s.Language)
		fmt.Fprintf(w, "    file: %s\n", s.File)
		if s.Symbol != "" {
			fmt.Fprintf(w, "    symbol: %s\n", s.Symbol)
		}
		if s.Kind != "" {
			fmt.Fprintf(w, "    kind: %s\n", s.Kind)
		}
		if s.Component != "" {
			fmt.Fprintf(w, "    component: %s\n", s.Component)
		}
		if s.Risk != "" {
			fmt.Fprintf(w, "    risk: %s\n", s.Risk)
		}
		if len(s.Annotations) > 0 {
			fmt.Fprintln(w, "    annotations:")
			// Stable key order.
			var keys []string
			for k := range s.Annotations {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(w, "      %s:\n", k)
				for _, v := range s.Annotations[k] {
					fmt.Fprintf(w, "        - %s\n", v)
				}
			}
		}
	}
	fmt.Fprintln(w, "test_symbols:")
	for _, t := range tests {
		fmt.Fprintf(w, "  - id: %s\n", t.ID)
		fmt.Fprintf(w, "    file: %s\n", t.File)
		fmt.Fprintf(w, "    symbol: %s\n", t.Symbol)
		if t.Package != "" {
			fmt.Fprintf(w, "    package: %s\n", t.Package)
		}
		if t.Language != "" {
			fmt.Fprintf(w, "    language: %s\n", t.Language)
		}
		if t.Doc != "" {
			fmt.Fprintf(w, "    doc: %q\n", t.Doc)
		}
	}
	return nil
}

// WriteEdgesYAML serializes code edges to YAML format.
func WriteEdgesYAML(w io.Writer, edges []CodeEdge) error {
	fmt.Fprintln(w, "code_edges:")
	for _, e := range edges {
		fmt.Fprintf(w, "  - from: %s\n", e.From)
		fmt.Fprintf(w, "    relation: %s\n", e.Relation)
		fmt.Fprintf(w, "    to: %s\n", e.To)
	}
	return nil
}

// WriteReportYAML serializes the annotation scan report.
func WriteReportYAML(w io.Writer, result *ScanResult) error {
	fmt.Fprintf(w, "scanned_files: %d\n", result.ScannedFiles)
	fmt.Fprintf(w, "annotated_symbols: %d\n", result.AnnotatedSymbols)
	fmt.Fprintf(w, "imported_annotations: %d\n", len(result.Annotations))
	fmt.Fprintf(w, "discovered_tests: %d\n", len(result.DiscoveredTests))

	// Count problems by type.
	var missingNS, unknownNS, unqualified, unsupported []string
	for _, e := range result.Errors {
		msg := e.Message
		switch {
		case strings.Contains(msg, "missing namespace"):
			missingNS = append(missingNS, e.Error())
		case strings.Contains(msg, "unknown namespace"):
			unknownNS = append(unknownNS, e.Error())
		case strings.Contains(msg, "fully-qualified"):
			unqualified = append(unqualified, e.Error())
		case strings.Contains(msg, "unsupported annotation key"):
			unsupported = append(unsupported, e.Error())
		}
	}
	writeList := func(label string, items []string) {
		fmt.Fprintf(w, "%s:\n", label)
		if len(items) == 0 {
			fmt.Fprintln(w, "  []")
			return
		}
		for _, it := range items {
			fmt.Fprintf(w, "  - %s\n", it)
		}
	}
	writeList("missing_namespace", missingNS)
	writeList("unknown_namespace", unknownNS)
	writeList("unqualified_ids", unqualified)
	writeList("unsupported_keys", unsupported)

	var dangling []string // placeholder — requires graph lookup to detect
	writeList("dangling_references", dangling)

	var skipped []string
	for _, w2 := range result.Warnings {
		skipped = append(skipped, w2.Error())
	}
	writeList("skipped_files", skipped)
	return nil
}
