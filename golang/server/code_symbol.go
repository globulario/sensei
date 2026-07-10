// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.code_symbol
// @awareness file_role=code_context_collector
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/store"
)

// codeSymbol is the parsed representation of one CodeSymbol RDF node.
type codeSymbol struct {
	id                string
	label             string
	component         string
	namespace         string
	language          string
	risk              string
	implements        []string // intent IRIs
	enforces          []string // invariant IRIs
	protects          []string // failure mode IRIs
	partiallyViolates []string // invariant IRIs the code KNOWINGLY violates in part
	testedBy          []string // TestSymbol IRIs
	references        []string // CodeSymbol ids this symbol references (calls/uses), incl. external:<name>
}

// collectCodeSymbols queries for CodeSymbol nodes defined in the given source-file IRI.
// Results are sorted by id for determinism; all slice fields within each symbol are also sorted.
func collectCodeSymbols(ctx context.Context, s store.Store, fileIRI string) ([]codeSymbol, error) {
	facts, err := s.CodeSymbolFacts(ctx, fileIRI)
	if err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, nil
	}

	var order []string
	byIRI := map[string]*codeSymbol{}
	for _, f := range facts {
		sym, exists := byIRI[f.NodeIRI]
		if !exists {
			id, _ := awarenessIDFromIRI(f.NodeIRI)
			ns := ""
			if colon := strings.IndexByte(id, ':'); colon > 0 {
				ns = id[:colon]
			}
			sym = &codeSymbol{id: id, namespace: ns}
			byIRI[f.NodeIRI] = sym
			order = append(order, f.NodeIRI)
		}
		applyCodeSymbolFact(sym, f)
	}

	result := make([]codeSymbol, 0, len(order))
	for _, iri := range order {
		result = append(result, *byIRI[iri])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	for i := range result {
		sort.Strings(result[i].implements)
		sort.Strings(result[i].enforces)
		sort.Strings(result[i].protects)
		sort.Strings(result[i].partiallyViolates)
		sort.Strings(result[i].testedBy)
		sort.Strings(result[i].references)
	}
	return result, nil
}

func applyCodeSymbolFact(sym *codeSymbol, f store.ImpactFact) {
	switch f.Predicate {
	case rdf.PropLabel:
		if !f.ObjectIsIRI && sym.label == "" {
			sym.label = f.Object
		}
	case rdf.PropComment:
		if !f.ObjectIsIRI && sym.component == "" {
			sym.component = strings.TrimPrefix(f.Object, "component: ")
		}
	case rdf.PropRisk:
		if !f.ObjectIsIRI && sym.risk == "" {
			sym.risk = f.Object
		}
	case rdf.PropImplements:
		if f.ObjectIsIRI {
			sym.implements = appendUniqueStr(sym.implements, f.Object)
		}
	case rdf.PropEnforces:
		if f.ObjectIsIRI {
			sym.enforces = appendUniqueStr(sym.enforces, f.Object)
		}
	case rdf.PropProtectsAgainst:
		if f.ObjectIsIRI {
			sym.protects = appendUniqueStr(sym.protects, f.Object)
		}
	case rdf.PropPartiallyViolates:
		if f.ObjectIsIRI {
			sym.partiallyViolates = appendUniqueStr(sym.partiallyViolates, f.Object)
		}
	case rdf.PropTestedBy:
		if f.ObjectIsIRI {
			sym.testedBy = appendUniqueStr(sym.testedBy, f.Object)
		}
	case rdf.PropLanguage:
		if !f.ObjectIsIRI && sym.language == "" {
			sym.language = f.Object
		}
	case rdf.PropReferences:
		if f.ObjectIsIRI {
			if id, ok := awarenessIDFromIRI(f.Object); ok {
				sym.references = appendUniqueStr(sym.references, strings.ReplaceAll(id, "%2F", "/"))
			}
		}
	}
}

// appendUniqueStr appends v to s only if not already present.
func appendUniqueStr(s []string, v string) []string {
	for _, e := range s {
		if e == v {
			return s
		}
	}
	return append(s, v)
}

// testSymbolLabel converts a TestSymbol IRI to a display label.
//
//	in:  https://globular.io/awareness#testSymbol/golang%2Fserver%2Fmain_test.go:TestFoo
//	out: golang/server/main_test.go:TestFoo
func testSymbolLabel(iri string) string {
	id, ok := awarenessIDFromIRI(iri)
	if !ok {
		return iri
	}
	return strings.ReplaceAll(id, "%2F", "/")
}

// buildExistingIRISet returns the full IRIs of all awareness nodes already
// present in the impact response, used to deduplicate code-symbol-sourced refs.
func buildExistingIRISet(impact *awarenesspb.ImpactResponse) map[string]bool {
	set := map[string]bool{}
	for _, n := range impact.GetDirectInvariants() {
		set[n.GetIri()] = true
	}
	for _, n := range impact.GetDirectFailureModes() {
		set[n.GetIri()] = true
	}
	for _, n := range impact.GetDirectIncidentPatterns() {
		set[n.GetIri()] = true
	}
	for _, n := range impact.GetDirectIntents() {
		set[n.GetIri()] = true
	}
	return set
}

// codeRefIDsFromSymbols returns referenced_ids entries for code symbols and
// any linked awareness nodes whose IRIs are not in existingIRISet.
func codeRefIDsFromSymbols(syms []codeSymbol, existingIRISet map[string]bool) []string {
	var out []string
	for _, sym := range syms {
		out = append(out, "code_symbol:"+sym.id)
	}
	added := map[string]bool{}
	for _, sym := range syms {
		all := append(append(append(sym.implements, sym.enforces...), sym.protects...), sym.partiallyViolates...)
		for _, iri := range all {
			if !existingIRISet[iri] && !added[iri] {
				if ref, ok := awarenessRelatedID(iri); ok {
					out = append(out, ref)
					added[iri] = true
				}
			}
		}
	}
	return out
}

// appendCodeContextSection writes the "Code context:" block to b when code symbols are present.
func appendCodeContextSection(b *strings.Builder, syms []codeSymbol, maxEntries int) {
	if len(syms) == 0 {
		return
	}

	// Shared namespace and component (first non-empty wins).
	ns, component := "", ""
	for _, s := range syms {
		if ns == "" {
			ns = s.namespace
		}
		if component == "" {
			component = s.component
		}
	}

	b.WriteString("\n\nCode context:")
	if ns != "" {
		fmt.Fprintf(b, "\n  Namespace: %s", ns)
	}
	if component != "" {
		fmt.Fprintf(b, "\n  Component: %s", component)
	}

	// Named symbols (those whose label differs from the full qualified ID).
	var symLines []string
	for _, s := range syms {
		if s.label == "" || s.label == s.id {
			continue
		}
		line := s.label
		if s.risk != "" {
			line += " (risk: " + s.risk + ")"
		}
		symLines = append(symLines, line)
	}
	if len(symLines) > 0 {
		fmt.Fprintf(b, "\n  Symbols:   %s", strings.Join(capStrings(symLines, maxEntries), ", "))
	}

	// Collect and deduplicate IRIs across all symbols.
	var implIRIs, enfIRIs, protIRIs, partialIRIs, testIRIs []string
	for _, s := range syms {
		for _, iri := range s.implements {
			implIRIs = appendUniqueStr(implIRIs, iri)
		}
		for _, iri := range s.enforces {
			enfIRIs = appendUniqueStr(enfIRIs, iri)
		}
		for _, iri := range s.protects {
			protIRIs = appendUniqueStr(protIRIs, iri)
		}
		for _, iri := range s.partiallyViolates {
			partialIRIs = appendUniqueStr(partialIRIs, iri)
		}
		for _, iri := range s.testedBy {
			testIRIs = appendUniqueStr(testIRIs, iri)
		}
	}
	sort.Strings(implIRIs)
	sort.Strings(enfIRIs)
	sort.Strings(protIRIs)
	sort.Strings(partialIRIs)
	sort.Strings(testIRIs)

	if len(implIRIs) > 0 {
		b.WriteString("\n\n  Implements:")
		for _, iri := range capStrings(implIRIs, maxEntries) {
			if ref, ok := awarenessRelatedID(iri); ok {
				fmt.Fprintf(b, "\n  - %s", ref)
			}
		}
	}
	if len(enfIRIs) > 0 {
		b.WriteString("\n\n  Enforces:")
		for _, iri := range capStrings(enfIRIs, maxEntries) {
			if ref, ok := awarenessRelatedID(iri); ok {
				fmt.Fprintf(b, "\n  - %s", ref)
			}
		}
	}
	if len(protIRIs) > 0 {
		b.WriteString("\n\n  Guards against:")
		for _, iri := range capStrings(protIRIs, maxEntries) {
			if ref, ok := awarenessRelatedID(iri); ok {
				fmt.Fprintf(b, "\n  - %s", ref)
			}
		}
	}
	if len(partialIRIs) > 0 {
		b.WriteString("\n\n  Partially violates (KNOWN GAP):")
		for _, iri := range capStrings(partialIRIs, maxEntries) {
			if ref, ok := awarenessRelatedID(iri); ok {
				fmt.Fprintf(b, "\n  - %s", ref)
			}
		}
	}
	if len(testIRIs) > 0 {
		b.WriteString("\n\n  Tested by:")
		for _, iri := range capStrings(testIRIs, maxEntries) {
			fmt.Fprintf(b, "\n  - %s", testSymbolLabel(iri))
		}
	}

	// Shared call conventions: targets referenced by >=2 sibling symbols in this
	// file (from SCIP reference edges). This is the completeness signal — change
	// one site of a convention and the siblings likely need the same change.
	// Bounded so briefing stays prose, not a reference dump.
	if conv := sharedConventionLines(syms, maxConventionGroups); len(conv) > 0 {
		b.WriteString("\n\n  Shared call conventions (siblings referencing the same symbol — change together):")
		for _, line := range conv {
			fmt.Fprintf(b, "\n  - %s", line)
		}
	}
}

// maxConventionGroups bounds how many shared-reference groups briefing renders.
const maxConventionGroups = 8

// sharedConventionLines groups this file's symbols by a reference target they
// share; a target used by >=2 sibling symbols is a "convention" worth surfacing
// so a change to one site prompts checking the others. Returns up to maxGroups
// lines sorted by sibling count (desc), each listing up to 10 sibling names.
func sharedConventionLines(syms []codeSymbol, maxGroups int) []string {
	symsByTarget := map[string][]string{}
	for _, s := range syms {
		name := s.label
		if name == "" {
			name = s.id
		}
		for _, ref := range s.references {
			symsByTarget[ref] = appendUniqueStr(symsByTarget[ref], name)
		}
	}
	type grp struct {
		target string
		syms   []string
	}
	var groups []grp
	for target, names := range symsByTarget {
		if len(names) >= 2 {
			sort.Strings(names)
			groups = append(groups, grp{target, names})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].syms) != len(groups[j].syms) {
			return len(groups[i].syms) > len(groups[j].syms)
		}
		return groups[i].target < groups[j].target
	})
	if maxGroups > 0 && len(groups) > maxGroups {
		groups = groups[:maxGroups]
	}
	var out []string
	for _, g := range groups {
		names := g.syms
		if len(names) > 10 {
			names = names[:10]
		}
		out = append(out, fmt.Sprintf("%d symbols reference %s: %s", len(g.syms), refDisplay(g.target), strings.Join(names, ", ")))
	}
	return out
}

// refDisplay renders a reference-target id as a readable symbol name:
// "external:Fprintf" → "Fprintf", "command/issue.go:issueClose" → "issueClose".
func refDisplay(id string) string {
	id = strings.TrimPrefix(id, "external:")
	if i := strings.LastIndex(id, ":"); i >= 0 {
		return id[i+1:]
	}
	return id
}
