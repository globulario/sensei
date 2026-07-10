// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=scipingest
// @awareness file_role=scip_symbol_ingester
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority

// Package scipingest maps a SCIP index (https://github.com/scip-code/scip)
// into AWG's existing code-symbol model. It exists so the structural graph can
// carry per-function/per-method symbol nodes — and, in Phase 3, reference edges
// between them — WITHOUT AWG growing its own multi-language AST extractor. SCIP
// is produced by mature per-language indexers (scip-go, scip-typescript, …); we
// only translate their output into scanner.CodeSymbol / reference records that
// the existing importCodeSymbols RDF path already understands.
//
// A SCIP Document's `symbols` are the symbols DEFINED in that document (SCIP
// spec), so definitions need no range analysis. References are attributed to
// their enclosing definition by range containment (see attributeReferences).
package scipingest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/scip-code/scip/bindings/go/scip"

	"github.com/globulario/awareness-graph/golang/scanner"
)

// Ref is one directed "symbol A references symbol B" fact recovered from SCIP
// occurrences. FromID is the enclosing definition's code-symbol ID; ToID is the
// referenced symbol's code-symbol ID when it is defined in the same index, else
// empty. ToName is a human label for the referenced symbol (always set) so a
// reference to an external symbol (e.g. fmt.Fprintf) is still queryable — this
// is what powers the "N sites emit this convention, you touched M" query.
type Ref struct {
	FromID string
	ToID   string
	ToName string
	File   string
}

// Result is the translated content of one SCIP index.
type Result struct {
	Symbols []scanner.CodeSymbol
	Refs    []Ref
}

// Options tunes the translation.
type Options struct {
	// LanguageOverride, when set, replaces each document's declared language.
	LanguageOverride string
	// ExcludeTestFiles drops symbols and references defined in test files
	// (e.g. *_test.go). Test functions are already modeled as Test nodes via
	// authored required_tests, so ingesting them again as CodeSymbols only
	// creates duplicate ids against the curated corpus without adding signal.
	ExcludeTestFiles bool
}

// Ingest translates a parsed SCIP index into AWG code symbols and reference
// edges. It is pure (no I/O) so it is trivially testable with a hand-built
// index; the CLI wrapper handles reading index.scip and writing YAML.
func Ingest(idx *scip.Index, opts Options) Result {
	var res Result
	// scipToID maps a SCIP symbol string -> our code-symbol ID, for definitions
	// we saw, so references can be resolved to internal targets.
	scipToID := map[string]string{}

	// First pass: definitions (Document.Symbols).
	for _, doc := range idx.GetDocuments() {
		file := doc.GetRelativePath()
		if !isRepoRelative(file) {
			continue // synthetic/generated doc (e.g. go-build cache test-main) — not a repo file
		}
		if opts.ExcludeTestFiles && isTestFile(file) {
			continue // test symbols are modeled as Test nodes, not CodeSymbols
		}
		lang := opts.LanguageOverride
		if lang == "" {
			lang = normalizeLanguage(doc.GetLanguage())
		}
		for _, si := range doc.GetSymbols() {
			name := symbolDisplayName(si)
			if name == "" || isLocalSymbol(si.GetSymbol()) {
				continue // unnamed or function-local symbol — not a stable node
			}
			id := symbolID(file, name)
			scipToID[si.GetSymbol()] = id
			res.Symbols = append(res.Symbols, scanner.CodeSymbol{
				ID:        id,
				Namespace: structuralNamespace,
				Language:  lang,
				File:      file,
				Symbol:    name,
				Kind:      symbolKind(si),
			})
		}
	}

	// Second pass: references, attributed to their enclosing definition.
	for _, doc := range idx.GetDocuments() {
		if !isRepoRelative(doc.GetRelativePath()) {
			continue
		}
		if opts.ExcludeTestFiles && isTestFile(doc.GetRelativePath()) {
			continue // no references originating in test code
		}
		res.Refs = append(res.Refs, attributeReferences(doc, scipToID)...)
	}

	dedupeSymbols(&res)
	sortResult(&res)
	return res
}

// structuralNamespace tags SCIP-ingested symbols. They are structural facts,
// not knowledge nodes tied to an authored namespace; importCodeSymbols ignores
// the namespace field, but it keeps the YAML shape valid and self-describing.
const structuralNamespace = "structural.scip"

// attributeReferences walks one document's occurrences and, for every reference
// occurrence (a non-definition use of a symbol), finds the enclosing definition
// by range containment. Definitions expose an enclosing_range covering their
// whole body; a reference whose start falls inside that range belongs to that
// definition. Returns one Ref per (enclosing-def, referenced-symbol) pair.
func attributeReferences(doc *scip.Document, scipToID map[string]string) []Ref {
	type span struct {
		symbol string
		lo, hi position
	}
	var defs []span
	for _, occ := range doc.GetOccurrences() {
		if !hasRole(occ.GetSymbolRoles(), scip.SymbolRole_Definition) {
			continue
		}
		lo, hi, ok := enclosingSpan(occ)
		if !ok {
			continue
		}
		defs = append(defs, span{symbol: occ.GetSymbol(), lo: lo, hi: hi})
	}
	// Larger enclosing spans first would misattribute nested defs; sort by span
	// size ascending so the tightest enclosing definition wins.
	sort.Slice(defs, func(i, j int) bool { return spanSize(defs[i].lo, defs[i].hi) < spanSize(defs[j].lo, defs[j].hi) })

	seen := map[string]bool{}
	var refs []Ref
	for _, occ := range doc.GetOccurrences() {
		if hasRole(occ.GetSymbolRoles(), scip.SymbolRole_Definition) {
			continue
		}
		if isLocalSymbol(occ.GetSymbol()) {
			continue
		}
		start, ok := occStart(occ)
		if !ok {
			continue
		}
		var enclosing string
		for _, d := range defs {
			if d.symbol == occ.GetSymbol() {
				continue // a symbol referencing itself in its own body is noise
			}
			if within(d.lo, d.hi, start) {
				enclosing = d.symbol
				break
			}
		}
		if enclosing == "" {
			continue // top-level / unattributable reference
		}
		fromID := scipToID[enclosing]
		if fromID == "" {
			continue
		}
		toName := symbolStringName(occ.GetSymbol())
		key := fromID + "\x00" + occ.GetSymbol()
		if seen[key] || toName == "" {
			continue
		}
		seen[key] = true
		refs = append(refs, Ref{
			FromID: fromID,
			ToID:   scipToID[occ.GetSymbol()], // "" if external to the index
			ToName: toName,
			File:   doc.GetRelativePath(),
		})
	}
	return refs
}

// position is a (line, character) point in a document, 0-based per SCIP.
type position struct{ line, char int32 }

// enclosingSpan returns the body span of a definition occurrence: its
// enclosing_range if present, else its own range. ok=false if neither parses.
func enclosingSpan(occ *scip.Occurrence) (lo, hi position, ok bool) {
	if r := occ.GetEnclosingRange(); len(r) >= 3 {
		return decodeRange(r)
	}
	if r := occ.GetRange(); len(r) >= 3 {
		return decodeRange(r)
	}
	return position{}, position{}, false
}

func occStart(occ *scip.Occurrence) (position, bool) {
	r := occ.GetRange()
	if len(r) < 3 {
		return position{}, false
	}
	return position{line: r[0], char: r[1]}, true
}

// decodeRange decodes a SCIP range array. SCIP ranges are [startLine,
// startChar, endChar] (3 elems, same line) or [startLine, startChar, endLine,
// endChar] (4 elems).
func decodeRange(r []int32) (lo, hi position, ok bool) {
	switch len(r) {
	case 3:
		return position{r[0], r[1]}, position{r[0], r[2]}, true
	case 4:
		return position{r[0], r[1]}, position{r[2], r[3]}, true
	default:
		return position{}, position{}, false
	}
}

func within(lo, hi, p position) bool {
	if p.line < lo.line || p.line > hi.line {
		return false
	}
	if p.line == lo.line && p.char < lo.char {
		return false
	}
	if p.line == hi.line && p.char > hi.char {
		return false
	}
	return true
}

// spanSize is a monotonic proxy for span extent, used only for ordering.
func spanSize(lo, hi position) int64 {
	return int64(hi.line-lo.line)*100000 + int64(hi.char-lo.char)
}

func hasRole(roles int32, role scip.SymbolRole) bool { return roles&int32(role) != 0 }

// symbolDisplayName returns the best human name for a defined symbol: SCIP's
// display_name when the indexer set it, else the last meaningful descriptor.
func symbolDisplayName(si *scip.SymbolInformation) string {
	if n := strings.TrimSpace(si.GetDisplayName()); n != "" {
		return n
	}
	return symbolStringName(si.GetSymbol())
}

// symbolStringName parses a SCIP symbol string and returns a readable name from
// its trailing descriptors, e.g. "(*MergeOptions).Run" or "Fprintf".
func symbolStringName(symbol string) string {
	sym, err := scip.ParseSymbol(symbol)
	if err != nil || len(sym.GetDescriptors()) == 0 {
		return ""
	}
	descs := sym.GetDescriptors()
	last := descs[len(descs)-1]
	name := strings.TrimSpace(last.GetName())
	if name == "" {
		return ""
	}
	// Prefix an immediately-preceding Type descriptor so methods read as
	// Recv.Method rather than a bare, ambiguous method name.
	if last.GetSuffix() == scip.Descriptor_Method && len(descs) >= 2 {
		if prev := descs[len(descs)-2]; prev.GetSuffix() == scip.Descriptor_Type {
			if recv := strings.TrimSpace(prev.GetName()); recv != "" {
				return recv + "." + name
			}
		}
	}
	return name
}

// symbolKind maps SCIP's symbol kind (or descriptor suffix) to AWG's kind
// vocabulary {function, method, type, var, const, ...}.
func symbolKind(si *scip.SymbolInformation) string {
	switch si.GetKind() {
	case scip.SymbolInformation_Function:
		return "function"
	case scip.SymbolInformation_Method, scip.SymbolInformation_StaticMethod:
		return "method"
	case scip.SymbolInformation_Class, scip.SymbolInformation_Struct,
		scip.SymbolInformation_Interface, scip.SymbolInformation_Type,
		scip.SymbolInformation_Enum:
		return "type"
	case scip.SymbolInformation_Constant:
		return "const"
	case scip.SymbolInformation_Variable, scip.SymbolInformation_Field:
		return "var"
	}
	// Fall back to the descriptor suffix when kind is unset.
	if sym, err := scip.ParseSymbol(si.GetSymbol()); err == nil && len(sym.GetDescriptors()) > 0 {
		switch sym.GetDescriptors()[len(sym.GetDescriptors())-1].GetSuffix() {
		case scip.Descriptor_Method:
			return "method"
		case scip.Descriptor_Type:
			return "type"
		case scip.Descriptor_Term:
			return "var"
		}
	}
	return "function"
}

// isLocalSymbol reports whether a SCIP symbol string denotes a function-local
// symbol (scheme "local ..."), which is not worth a graph node.
func isLocalSymbol(symbol string) bool {
	return strings.HasPrefix(symbol, "local ")
}

// symbolID builds a stable, readable, IRI-safe-after-encoding id for a symbol
// defined in file. Format mirrors the existing TestSymbol convention
// (file:Name) so ids stay human-legible in impact output.
func symbolID(file, name string) string {
	return fmt.Sprintf("%s:%s", file, name)
}

// isRepoRelative reports whether a SCIP document path is a real repo-relative
// source file. scip-go also emits synthetic documents for generated packages
// (e.g. go-build cache test-main stubs) with absolute or ../-escaping paths;
// those are not repo files and must not become graph nodes.
func isRepoRelative(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasPrefix(path, "../") {
		return false
	}
	return !strings.Contains(path, "/.cache/")
}

// isTestFile reports whether a repo-relative source path is a test file, across
// the languages SCIP indexers cover. Kept conservative — only well-known test
// conventions match, so production sources are never dropped.
func isTestFile(path string) bool {
	base := path
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	switch {
	case strings.HasSuffix(base, "_test.go"): // Go
		return true
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"),
		strings.HasSuffix(base, ".spec.ts"), strings.HasSuffix(base, ".spec.tsx"),
		strings.HasSuffix(base, ".test.js"), strings.HasSuffix(base, ".spec.js"): // TS/JS
		return true
	case strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_"): // Python
		return true
	}
	return false
}

func normalizeLanguage(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch l {
	case "go", "golang":
		return "go"
	case "typescript", "ts":
		return "typescript"
	case "javascript", "js":
		return "javascript"
	case "python", "py":
		return "python"
	case "rust", "rs":
		return "rust"
	case "":
		return "go"
	default:
		return l
	}
}

func dedupeSymbols(res *Result) {
	seen := map[string]bool{}
	out := res.Symbols[:0]
	for _, s := range res.Symbols {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	res.Symbols = out
}

func sortResult(res *Result) {
	sort.Slice(res.Symbols, func(i, j int) bool { return res.Symbols[i].ID < res.Symbols[j].ID })
	sort.Slice(res.Refs, func(i, j int) bool {
		if res.Refs[i].FromID != res.Refs[j].FromID {
			return res.Refs[i].FromID < res.Refs[j].FromID
		}
		return res.Refs[i].ToName < res.Refs[j].ToName
	})
}
