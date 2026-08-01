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
	lookupIDs         []string // equivalent graph identities whose evidence belongs to this symbol
	knownCallers      []string // inbound static aw:references sites from the current graph
	targeted          bool     // true only when the task names this symbol exactly
}

// focusCodeSymbolsForTask narrows file-level code context only when the task
// names a symbol exactly. Qualified names (for example Context.Bind) take
// precedence. A simple name is accepted only when it resolves to one symbol in
// the file. Ambiguous or absent matches preserve the full file-level context;
// Sensei never guesses which sibling the caller meant.
func focusCodeSymbolsForTask(task string, syms []codeSymbol) []codeSymbol {
	syms = reconcileEquivalentCodeSymbols(syms)
	if strings.TrimSpace(task) == "" || len(syms) == 0 {
		return syms
	}

	qualified := exactTaskSymbolMatches(task, syms, true)
	if len(qualified) > 0 {
		return markTargetedSymbols(qualified)
	}

	simple := exactTaskSymbolMatches(task, syms, false)
	if len(simple) == 1 {
		return markTargetedSymbols(simple)
	}
	return syms
}

func exactTaskSymbolMatches(task string, syms []codeSymbol, qualified bool) []codeSymbol {
	seen := map[string]bool{}
	var out []codeSymbol
	for _, sym := range syms {
		for _, name := range codeSymbolCandidateNames(sym) {
			isQualified := strings.Contains(name, ".")
			if isQualified != qualified || !containsExactSymbolName(task, name) {
				continue
			}
			if !seen[sym.id] {
				seen[sym.id] = true
				out = append(out, sym)
			}
			break
		}
	}
	return out
}

func codeSymbolCandidateNames(sym codeSymbol) []string {
	var names []string
	for _, name := range codeSymbolDirectNames(sym) {
		names = appendUniqueStr(names, name)
		if leaf := codeSymbolLeafName(name); leaf != name {
			names = appendUniqueStr(names, leaf)
		}
	}
	return names
}

func codeSymbolDirectNames(sym codeSymbol) []string {
	var names []string
	if name := strings.TrimSpace(sym.label); name != "" && name != sym.id {
		names = appendUniqueStr(names, name)
	}
	if colon := strings.LastIndex(sym.id, ":"); colon >= 0 && colon+1 < len(sym.id) {
		if name := strings.TrimSpace(sym.id[colon+1:]); name != "" {
			names = appendUniqueStr(names, name)
		}
	}
	return names
}

func codeSymbolLeafName(name string) string {
	name = strings.TrimSpace(name)
	if dot := strings.LastIndex(name, "."); dot >= 0 && dot+1 < len(name) {
		return name[dot+1:]
	}
	return name
}

func codeSymbolFileKey(id string) string {
	if colon := strings.LastIndex(id, ":"); colon >= 0 {
		return id[:colon]
	}
	return id
}

// reconcileEquivalentCodeSymbols joins the annotated semantic node and the
// SCIP structural node only when identity is unambiguous inside one file.
// The canonical symbol retains every graph id as a lookup alias, so evidence
// and inbound references attached to either representation remain visible.
func reconcileEquivalentCodeSymbols(syms []codeSymbol) []codeSymbol {
	out := append([]codeSymbol(nil), syms...)
	removed := make([]bool, len(out))
	for i := range out {
		if len(out[i].lookupIDs) == 0 {
			out[i].lookupIDs = []string{out[i].id}
		}
	}

	exactQualified := map[string][]int{}
	for i, sym := range out {
		for _, name := range codeSymbolDirectNames(sym) {
			if strings.Contains(name, ".") {
				key := codeSymbolFileKey(sym.id) + "\x00" + name
				exactQualified[key] = append(exactQualified[key], i)
			}
		}
	}
	for _, idxs := range exactQualified {
		if len(idxs) < 2 {
			continue
		}
		dst := idxs[0]
		for _, src := range idxs[1:] {
			mergeEquivalentCodeSymbol(&out[dst], out[src])
			removed[src] = true
		}
	}

	qualifiedByLeaf := map[string][]int{}
	for i, sym := range out {
		if removed[i] {
			continue
		}
		seen := map[string]bool{}
		for _, name := range codeSymbolDirectNames(sym) {
			if !strings.Contains(name, ".") {
				continue
			}
			key := codeSymbolFileKey(sym.id) + "\x00" + codeSymbolLeafName(name)
			if !seen[key] {
				qualifiedByLeaf[key] = append(qualifiedByLeaf[key], i)
				seen[key] = true
			}
		}
	}
	for i, sym := range out {
		if removed[i] {
			continue
		}
		var bareLeaves []string
		for _, name := range codeSymbolDirectNames(sym) {
			if !strings.Contains(name, ".") {
				bareLeaves = appendUniqueStr(bareLeaves, name)
			}
		}
		for _, leaf := range bareLeaves {
			candidates := qualifiedByLeaf[codeSymbolFileKey(sym.id)+"\x00"+leaf]
			if len(candidates) != 1 || candidates[0] == i {
				continue
			}
			mergeEquivalentCodeSymbol(&out[candidates[0]], out[i])
			removed[i] = true
			break
		}
	}

	result := make([]codeSymbol, 0, len(out))
	for i := range out {
		if removed[i] {
			continue
		}
		sort.Strings(out[i].lookupIDs)
		sort.Strings(out[i].implements)
		sort.Strings(out[i].enforces)
		sort.Strings(out[i].protects)
		sort.Strings(out[i].partiallyViolates)
		sort.Strings(out[i].testedBy)
		sort.Strings(out[i].references)
		result = append(result, out[i])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func mergeEquivalentCodeSymbol(dst *codeSymbol, src codeSymbol) {
	if dst.label == "" || (!strings.Contains(dst.label, ".") && strings.Contains(src.label, ".")) {
		dst.label = src.label
	}
	if dst.component == "" {
		dst.component = src.component
	}
	if dst.namespace == "" {
		dst.namespace = src.namespace
	}
	if dst.language == "" {
		dst.language = src.language
	}
	if dst.risk == "" {
		dst.risk = src.risk
	}
	for _, id := range append([]string{src.id}, src.lookupIDs...) {
		if id != "" {
			dst.lookupIDs = appendUniqueStr(dst.lookupIDs, id)
		}
	}
	for _, value := range src.implements {
		dst.implements = appendUniqueStr(dst.implements, value)
	}
	for _, value := range src.enforces {
		dst.enforces = appendUniqueStr(dst.enforces, value)
	}
	for _, value := range src.protects {
		dst.protects = appendUniqueStr(dst.protects, value)
	}
	for _, value := range src.partiallyViolates {
		dst.partiallyViolates = appendUniqueStr(dst.partiallyViolates, value)
	}
	for _, value := range src.testedBy {
		dst.testedBy = appendUniqueStr(dst.testedBy, value)
	}
	for _, value := range src.references {
		dst.references = appendUniqueStr(dst.references, value)
	}
}

func containsExactSymbolName(text, name string) bool {
	if name == "" {
		return false
	}
	for offset := 0; offset <= len(text)-len(name); {
		rel := strings.Index(text[offset:], name)
		if rel < 0 {
			return false
		}
		start := offset + rel
		end := start + len(name)
		beforeOK := start == 0 || !isSymbolNameByte(text[start-1])
		afterOK := end == len(text) || !isSymbolNameByte(text[end])
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
	}
	return false
}

func isSymbolNameByte(b byte) bool {
	return b == '_' || b == '.' ||
		(b >= '0' && b <= '9') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z')
}

func markTargetedSymbols(syms []codeSymbol) []codeSymbol {
	out := append([]codeSymbol(nil), syms...)
	for i := range out {
		out[i].targeted = true
	}
	return out
}

// attachKnownStaticCallers enriches only explicitly targeted symbols. The
// result is deliberately labelled static and graph-bounded when rendered;
// interface dispatch, callbacks, reflection, and generated registration may
// not produce aw:references edges and therefore remain unknown.
func (s *server) attachKnownStaticCallers(ctx context.Context, syms []codeSymbol, scope string) ([]codeSymbol, error) {
	out := append([]codeSymbol(nil), syms...)
	for i := range out {
		if !out[i].targeted {
			continue
		}
		lookupIDs := append([]string(nil), out[i].lookupIDs...)
		if len(lookupIDs) == 0 {
			lookupIDs = []string{out[i].id}
		}
		var callers []string
		for _, lookupID := range lookupIDs {
			sites, err := s.referencingSitesInScope(ctx, rdf.DecodeIRIPath(lookupID), scope)
			if err != nil {
				return nil, err
			}
			for _, site := range sites {
				callers = appendUniqueStr(callers, site)
			}
		}
		sort.Strings(callers)
		out[i].knownCallers = callers
	}
	return out, nil
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
	for _, s := range syms {
		if !s.targeted {
			continue
		}
		name := codeSymbolDisplayName(s)
		fmt.Fprintf(b, "\n  Target symbol: %s", name)
		if visibility, ok := goSymbolVisibility(s.language, name); ok {
			fmt.Fprintf(b, "\n  Go visibility: %s", visibility)
			if visibility == "exported" {
				b.WriteString("\n  Supported public API contract: unknown (exported visibility alone is not compatibility authority)")
			} else {
				b.WriteString("\n  External package API surface: no (unexported)")
			}
		}
		if len(s.knownCallers) == 0 {
			b.WriteString("\n  Known static callers: none found in the current graph")
		} else {
			b.WriteString("\n  Known static callers:")
			for _, caller := range capStrings(s.knownCallers, maxEntries) {
				fmt.Fprintf(b, "\n  - %s", caller)
			}
		}
		b.WriteString("\n  Caller coverage: static aw:references only; interface dispatch, callbacks, reflection, and generated registration may be incomplete")
	}
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

func codeSymbolDisplayName(sym codeSymbol) string {
	if name := strings.TrimSpace(sym.label); name != "" && name != sym.id {
		return name
	}
	if colon := strings.LastIndex(sym.id, ":"); colon >= 0 && colon+1 < len(sym.id) {
		return sym.id[colon+1:]
	}
	return sym.id
}

func goSymbolVisibility(language, name string) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(language), "go") {
		return "", false
	}
	leaf := name
	if dot := strings.LastIndex(leaf, "."); dot >= 0 {
		leaf = leaf[dot+1:]
	}
	leaf = strings.TrimLeft(leaf, "*()")
	if leaf == "" {
		return "", false
	}
	switch {
	case leaf[0] >= 'A' && leaf[0] <= 'Z':
		return "exported", true
	case leaf[0] >= 'a' && leaf[0] <= 'z':
		return "unexported", true
	default:
		return "", false
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
