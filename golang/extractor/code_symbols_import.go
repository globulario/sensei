// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=extractor
// @awareness file_role=code_symbol_rdf_importer
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority

// This file contains importers for the two generated code-annotation schemas:
//
//	code_symbols.yaml — rich symbol metadata with typed annotation edges
//	code_edges.yaml   — flat (from, relation, to) edge list
//
// Both schemas are produced by the annotation-scanner CLI from @awareness
// comments in Go source. The importer emits:
//  1. Typed aw:CodeSymbol nodes with label, file, risk.
//  2. Typed relation edges (aw:enforces, aw:implements, aw:protectsAgainst,
//     aw:testedBy, aw:relatedTo, aw:forbids).
//  3. Flat aw:implements reverse edges from SourceFile to KnowledgeNode, so
//     the existing ImpactForFile SPARQL query lands code-anchored knowledge
//     without modification.
//
// NOTE: code_edges.yaml encodes the same edges as the annotations block in
// code_symbols.yaml. Importing both is idempotent at the RDF layer; the store
// deduplicates. In practice yaml2nt should import only code_symbols (which
// contains all data) and treat code_edges as a convenience artifact.
package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// ── YAML shapes ────────────────────────────────────────────────────────────

type yamlCodeAnnotations struct {
	Implements        []string `yaml:"implements"`
	Enforces          []string `yaml:"enforces"`
	Protects          []string `yaml:"protects"`
	TestedBy          []string `yaml:"tested_by"`
	RelatesTo         []string `yaml:"relates_to"`
	PartiallyViolates []string `yaml:"partially_violates"`
	ForbiddenFix      []string `yaml:"forbidden_fix"`
}

type yamlCodeSymbol struct {
	ID          string              `yaml:"id"`
	Namespace   string              `yaml:"namespace"`
	Language    string              `yaml:"language"`
	File        string              `yaml:"file"`
	Symbol      string              `yaml:"symbol"`
	Kind        string              `yaml:"kind"`
	Component   string              `yaml:"component"`
	Risk        string              `yaml:"risk"`
	Annotations yamlCodeAnnotations `yaml:"annotations"`
}

type yamlTestSymbol struct {
	ID       string `yaml:"id"`
	File     string `yaml:"file"`
	Symbol   string `yaml:"symbol"`
	Package  string `yaml:"package"`
	Language string `yaml:"language"`
	Doc      string `yaml:"doc"`
}

type codeSymbolsFile struct {
	CodeSymbols []yamlCodeSymbol `yaml:"code_symbols"`
	TestSymbols []yamlTestSymbol `yaml:"test_symbols"`
}

type yamlCodeEdge struct {
	From     string `yaml:"from"`
	Relation string `yaml:"relation"`
	To       string `yaml:"to"`
}

type codeEdgesFile struct {
	CodeEdges []yamlCodeEdge `yaml:"code_edges"`
}

// yamlCodeReference is one symbol→symbol reference edge recovered from a SCIP
// index. ToID is set when the referenced symbol is defined in the same index;
// otherwise only ToName is set and an external CodeSymbol node is minted so
// the edge is still queryable (id "external:<name>").
type yamlCodeReference struct {
	From   string `yaml:"from"`
	ToID   string `yaml:"to_id"`
	ToName string `yaml:"to_name"`
	File   string `yaml:"file"`
}

type codeReferencesFile struct {
	CodeReferences []yamlCodeReference `yaml:"code_references"`
}

// ── Importers ──────────────────────────────────────────────────────────────

// importCodeSymbols reads a code_symbols.yaml file and emits:
//   - aw:CodeSymbol typed nodes
//   - typed relation edges to knowledge graph nodes
//   - flat aw:implements reverse edges from SourceFile for impact compatibility
func importCodeSymbols(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f codeSymbolsFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, sym := range f.CodeSymbols {
		if strings.TrimSpace(sym.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassCodeSymbol, sym.ID)
		e.Typed(subj, rdf.ClassCodeSymbol)

		label := sym.Symbol
		if label == "" {
			label = sym.ID
		}
		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(label)))

		if sym.Risk != "" {
			e.Triple(subj, rdf.IRI(rdf.PropRisk), rdf.Lit(sym.Risk))
		}
		if sym.Component != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit("component: "+sym.Component))
		}
		if sym.Language != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLanguage), rdf.Lit(sym.Language))
		}

		// definedInFile edge: CodeSymbol → SourceFile
		if sym.File != "" {
			ensureNode(e, rdf.ClassSourceFile, sym.File)
			e.Triple(subj, rdf.IRI(rdf.PropDefinedInFile), rdf.MintIRI(rdf.ClassSourceFile, sym.File))
			if sym.Language != "" {
				e.Triple(rdf.MintIRI(rdf.ClassSourceFile, sym.File), rdf.IRI(rdf.PropLanguage), rdf.Lit(sym.Language))
			}
		}

		emitCodeAnnotationEdges(e, subj, sym.File, sym.Annotations)
	}
	for _, test := range f.TestSymbols {
		if strings.TrimSpace(test.ID) == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassTestSymbol, test.ID)
		e.Typed(subj, rdf.ClassTestSymbol)
		if test.Symbol != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(strings.TrimSpace(test.Symbol)))
		}
		if test.Doc != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(strings.TrimSpace(test.Doc)))
		}
		if test.Language != "" {
			e.Triple(subj, rdf.IRI(rdf.PropLanguage), rdf.Lit(test.Language))
		}
		if test.File != "" {
			ensureNode(e, rdf.ClassSourceFile, test.File)
			e.Triple(subj, rdf.IRI(rdf.PropDefinedInFile), rdf.MintIRI(rdf.ClassSourceFile, test.File))
		}
		if test.Package != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit("package: "+test.Package))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	}
	return nil
}

// importCodeEdges reads a code_edges.yaml file and emits the flat relation
// edges. Idempotent: safe to run after importCodeSymbols since RDF triples
// deduplicate.
func importCodeEdges(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f codeEdgesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, edge := range f.CodeEdges {
		if edge.From == "" || edge.Relation == "" || edge.To == "" {
			continue
		}
		fromIRI := rdf.MintIRI(rdf.ClassCodeSymbol, edge.From)
		prop, toIRI, ok := codeEdgeRelationIRI(e, edge.Relation, edge.To)
		if !ok {
			continue
		}
		e.Triple(fromIRI, rdf.IRI(prop), toIRI)
	}
	return nil
}

// importCodeReferences reads a code_references.yaml file (produced by
// `awg scip-ingest`) and emits aw:references edges between CodeSymbol nodes.
// Internal targets reuse the existing symbol's IRI; external targets are minted
// as CodeSymbol nodes with id "external:<name>" so a single query can gather
// every site that references a shared symbol (the sibling-convention query).
func importCodeReferences(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f codeReferencesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, ref := range f.CodeReferences {
		if strings.TrimSpace(ref.From) == "" {
			continue
		}
		fromIRI := rdf.MintIRI(rdf.ClassCodeSymbol, ref.From)
		ensureNode(e, rdf.ClassCodeSymbol, ref.From)

		var toID string
		if strings.TrimSpace(ref.ToID) != "" {
			toID = ref.ToID
		} else if name := strings.TrimSpace(ref.ToName); name != "" {
			toID = "external:" + name
		} else {
			continue
		}
		toIRI := rdf.MintIRI(rdf.ClassCodeSymbol, toID)
		ensureNode(e, rdf.ClassCodeSymbol, toID)
		if label := strings.TrimSpace(ref.ToName); label != "" {
			e.Triple(toIRI, rdf.IRI(rdf.PropLabel), rdf.Lit(label))
		}
		e.Triple(fromIRI, rdf.IRI(rdf.PropReferences), toIRI)
	}
	return nil
}

// emitCodeAnnotationEdges emits all annotation edges for one code symbol and
// projects flat SourceFile aw:implements KnowledgeNode edges for impact queries.
func emitCodeAnnotationEdges(e *rdf.Emitter, subj, file string, ann yamlCodeAnnotations) {
	fileIRI := ""
	if file != "" {
		fileIRI = rdf.MintIRI(rdf.ClassSourceFile, file)
	}

	for _, id := range ann.Implements {
		targetIRI, classIRI, ok := qualifiedIDToIRI(id)
		if !ok {
			continue
		}
		e.Triple(subj, rdf.IRI(rdf.PropImplements), targetIRI)
		if fileIRI != "" && isKnowledgeClass(classIRI) {
			ensureNode(e, rdf.ClassSourceFile, file)
			e.Triple(fileIRI, rdf.IRI(rdf.PropImplements), targetIRI)
		}
	}
	for _, id := range ann.Enforces {
		targetIRI, classIRI, ok := qualifiedIDToIRI(id)
		if !ok {
			continue
		}
		e.Triple(subj, rdf.IRI(rdf.PropEnforces), targetIRI)
		if fileIRI != "" && isKnowledgeClass(classIRI) {
			ensureNode(e, rdf.ClassSourceFile, file)
			e.Triple(fileIRI, rdf.IRI(rdf.PropImplements), targetIRI)
		}
	}
	for _, id := range ann.Protects {
		targetIRI, classIRI, ok := qualifiedIDToIRI(id)
		if !ok {
			continue
		}
		e.Triple(subj, rdf.IRI(rdf.PropProtectsAgainst), targetIRI)
		if fileIRI != "" && isKnowledgeClass(classIRI) {
			ensureNode(e, rdf.ClassSourceFile, file)
			e.Triple(fileIRI, rdf.IRI(rdf.PropImplements), targetIRI)
		}
	}
	for _, id := range ann.TestedBy {
		ensureNode(e, rdf.ClassTestSymbol, id)
		testIRI := rdf.MintIRI(rdf.ClassTestSymbol, id)
		e.Triple(subj, rdf.IRI(rdf.PropTestedBy), testIRI)
	}
	for _, id := range ann.RelatesTo {
		targetIRI, _, ok := qualifiedIDToIRI(id)
		if !ok {
			continue
		}
		e.Triple(subj, rdf.IRI(rdf.PropRelatedTo), targetIRI)
	}
	for _, id := range ann.PartiallyViolates {
		targetIRI, classIRI, ok := qualifiedIDToIRI(id)
		if !ok {
			continue
		}
		e.Triple(subj, rdf.IRI(rdf.PropPartiallyViolates), targetIRI)
		if fileIRI != "" && isKnowledgeClass(classIRI) {
			ensureNode(e, rdf.ClassSourceFile, file)
			e.Triple(fileIRI, rdf.IRI(rdf.PropPartiallyViolates), targetIRI)
		}
	}
	for _, id := range ann.ForbiddenFix {
		ensureNode(e, rdf.ClassForbiddenFix, id)
		e.Triple(subj, rdf.IRI(rdf.PropForbids), rdf.MintIRI(rdf.ClassForbiddenFix, id))
	}
}

// codeEdgeRelationIRI maps a code_edges relation string to the property IRI
// and target IRI. It uses the emitter to ensure typed nodes for test targets.
// Returns ok=false for unknown relations or malformed targets.
func codeEdgeRelationIRI(e *rdf.Emitter, relation, to string) (prop string, toIRI string, ok bool) {
	switch relation {
	case "implements":
		t, _, ok := qualifiedIDToIRI(to)
		return rdf.PropImplements, t, ok
	case "enforces":
		t, _, ok := qualifiedIDToIRI(to)
		return rdf.PropEnforces, t, ok
	case "protects":
		t, _, ok := qualifiedIDToIRI(to)
		return rdf.PropProtectsAgainst, t, ok
	case "tested_by":
		ensureNode(e, rdf.ClassTestSymbol, to)
		return rdf.PropTestedBy, rdf.MintIRI(rdf.ClassTestSymbol, to), true
	case "relates_to":
		t, _, ok := qualifiedIDToIRI(to)
		return rdf.PropRelatedTo, t, ok
	case "partially_violates":
		t, _, ok := qualifiedIDToIRI(to)
		return rdf.PropPartiallyViolates, t, ok
	case "forbidden_fix":
		ensureNode(e, rdf.ClassForbiddenFix, to)
		return rdf.PropForbids, rdf.MintIRI(rdf.ClassForbiddenFix, to), true
	default:
		return "", "", false
	}
}

// qualifiedIDToIRI maps a namespace-qualified ID like
// "globular.awareness_graph:invariant.ntriples_validated_before_write"
// to its IRI and class IRI. Returns ok=false for malformed IDs.
//
// The IRI is minted using the bare slug for knowledge nodes (invariant,
// failure_mode, intent, …) so cross-references match the IRIs produced by
// the YAML importers (which use the bare YAML id field as the slug).
// CodeSymbol nodes are minted with the full qualifiedID because the
// code_symbols importer uses sym.ID as the slug.
func qualifiedIDToIRI(qualifiedID string) (iri string, classIRI string, ok bool) {
	// Split on first colon → namespace + local
	colon := strings.IndexByte(qualifiedID, ':')
	if colon < 0 {
		return "", "", false
	}
	local := qualifiedID[colon+1:] // e.g. "invariant.ntriples_validated_before_write"

	// Split local on first dot → class + slug
	dot := strings.IndexByte(local, '.')
	if dot < 0 {
		return "", "", false
	}
	className := local[:dot] // e.g. "invariant"
	slug := local[dot+1:]    // e.g. "ntriples_validated_before_write"
	classIRI = classIRIForName(className)
	if classIRI == "" {
		return "", "", false
	}
	// CodeSymbol nodes are minted with the full qualifiedID; everything else
	// uses the bare slug so IRIs align with what yaml_import.go produces.
	if classIRI == rdf.ClassCodeSymbol {
		return rdf.MintIRI(classIRI, qualifiedID), classIRI, true
	}
	return rdf.MintIRI(classIRI, slug), classIRI, true
}

// classIRIForName maps a bare class name (as it appears in qualified IDs) to
// its full class IRI constant. Returns "" for unknown names.
func classIRIForName(name string) string {
	switch name {
	case "invariant":
		return rdf.ClassInvariant
	case "failure_mode":
		return rdf.ClassFailureMode
	case "incident_pattern":
		return rdf.ClassIncidentPattern
	case "intent":
		return rdf.ClassIntent
	case "symbol":
		return rdf.ClassSymbol
	case "source_file":
		return rdf.ClassSourceFile
	case "code":
		return rdf.ClassCodeSymbol
	default:
		return ""
	}
}

// isKnowledgeClass reports whether classIRI is a top-level knowledge class
// for which a flat aw:implements reverse edge should be projected so that
// ImpactForFile queries find the anchor.
func isKnowledgeClass(classIRI string) bool {
	switch classIRI {
	case rdf.ClassInvariant, rdf.ClassFailureMode, rdf.ClassIncidentPattern,
		rdf.ClassIntent, rdf.ClassDesignIntent, rdf.ClassOperationalIntent,
		rdf.ClassProductIntent, rdf.ClassConstraintIntent:
		return true
	}
	return false
}
