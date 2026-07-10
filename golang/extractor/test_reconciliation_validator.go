// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type TestReconciliationReport struct {
	AuthoritativeMissingImplementation []string
	ReferencedDiscoveredMissingSpec    []string
}

func ValidateTestReconciliation(r io.Reader) (TestReconciliationReport, error) {
	type testSymbolState struct {
		id         string
		label      string
		discovered bool
	}

	testSymbols := map[string]*testSymbolState{}
	referencedSymbols := map[string]bool{}
	requiredTests := map[string]bool{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	typePredicate := rdf.IRI(rdf.PropType)
	labelPredicate := rdf.IRI(rdf.PropLabel)
	authoredInPredicate := rdf.IRI(rdf.PropAuthoredIn)
	definedInFilePredicate := rdf.IRI(rdf.PropDefinedInFile)
	testedByPredicate := rdf.IRI(rdf.PropTestedBy)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasSuffix(line, ".") {
			continue
		}
		body := strings.TrimSpace(strings.TrimSuffix(line, "."))
		toks := tokenize(body)
		if len(toks) != 3 {
			continue
		}
		subj, pred, obj := toks[0], toks[1], toks[2]
		switch pred {
		case typePredicate:
			switch stripAngleBrackets(obj) {
			case rdf.ClassTestSymbol:
				if _, ok := testSymbols[subj]; !ok {
					testSymbols[subj] = &testSymbolState{id: rdf.DecodeIRIPath(iriLeaf(subj))}
				}
			case rdf.ClassTest:
				if _, ok := requiredTests[subj]; !ok {
					requiredTests[subj] = false
				}
			}
		case labelPredicate:
			if st, ok := testSymbols[subj]; ok {
				st.label = unquoteNTLiteral(obj)
			}
		case definedInFilePredicate:
			if st, ok := testSymbols[subj]; ok {
				st.discovered = true
			}
		case testedByPredicate:
			referencedSymbols[obj] = true
		case authoredInPredicate:
			if _, ok := requiredTests[subj]; ok && strings.HasSuffix(unquoteNTLiteral(obj), "required_tests.yaml") {
				requiredTests[subj] = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		return TestReconciliationReport{}, fmt.Errorf("scan: %w", err)
	}

	discoveredByID := map[string]bool{}
	for _, st := range testSymbols {
		if st.discovered && st.id != "" {
			discoveredByID[normalizeTestAnchor(st.id)] = true
		}
	}

	var report TestReconciliationReport
	for subj, authoritative := range requiredTests {
		if !authoritative {
			continue
		}
		id := normalizeTestAnchor(rdf.DecodeIRIPath(iriLeaf(subj)))
		if !isConcreteDiscoveredTestAnchor(id) {
			continue
		}
		if !discoveredByID[id] {
			report.AuthoritativeMissingImplementation = append(report.AuthoritativeMissingImplementation, id)
		}
	}
	for subj := range referencedSymbols {
		st, ok := testSymbols[subj]
		if !ok || !st.discovered || st.id == "" {
			continue
		}
		if requiredTests[rdf.MintIRI(rdf.ClassTest, st.id)] || requiredTests[rdf.MintIRI(rdf.ClassTest, denormalizeDoubleColon(st.id))] {
			continue
		}
		report.ReferencedDiscoveredMissingSpec = append(report.ReferencedDiscoveredMissingSpec, st.id)
	}

	sort.Strings(report.AuthoritativeMissingImplementation)
	sort.Strings(report.ReferencedDiscoveredMissingSpec)
	return report, nil
}

func (r TestReconciliationReport) HasFindings() bool {
	return len(r.AuthoritativeMissingImplementation) > 0 || len(r.ReferencedDiscoveredMissingSpec) > 0
}

func isConcreteDiscoveredTestAnchor(id string) bool {
	path, _, ok := strings.Cut(normalizeTestAnchor(id), ":")
	if !ok || path == "" {
		return false
	}
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return true
	case strings.HasSuffix(path, ".test.ts"), strings.HasSuffix(path, ".spec.ts"):
		return true
	case strings.HasSuffix(path, ".test.tsx"), strings.HasSuffix(path, ".spec.tsx"):
		return true
	case strings.HasSuffix(path, ".test.js"), strings.HasSuffix(path, ".spec.js"):
		return true
	case strings.HasSuffix(path, ".test.jsx"), strings.HasSuffix(path, ".spec.jsx"):
		return true
	case strings.HasSuffix(path, ".test.mjs"), strings.HasSuffix(path, ".spec.mjs"):
		return true
	case strings.HasSuffix(path, ".test.cjs"), strings.HasSuffix(path, ".spec.cjs"):
		return true
	case strings.HasPrefix(filepath.Base(path), "test_") && strings.HasSuffix(path, ".py"):
		return true
	case strings.HasSuffix(path, "_test.py"):
		return true
	case strings.HasSuffix(path, ".rs"):
		return true
	default:
		return false
	}
}

func normalizeTestAnchor(id string) string {
	if strings.Contains(id, "::") {
		return strings.Replace(id, "::", ":", 1)
	}
	return id
}

func denormalizeDoubleColon(id string) string {
	if strings.Count(id, ":") == 1 {
		return strings.Replace(id, ":", "::", 1)
	}
	return id
}

func iriLeaf(iri string) string {
	trimmed := strings.TrimSuffix(strings.TrimPrefix(iri, "<"), ">")
	idx := strings.LastIndex(trimmed, "/")
	if idx == -1 || idx == len(trimmed)-1 {
		return trimmed
	}
	return trimmed[idx+1:]
}

func unquoteNTLiteral(tok string) string {
	if len(tok) >= 2 && tok[0] == '"' {
		if end := strings.LastIndex(tok, "\""); end > 0 {
			return tok[1:end]
		}
	}
	return tok
}
