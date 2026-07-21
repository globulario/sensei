// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/rdf"
)

// Domain scoping keeps truth from crossing repos accidentally. One AWG instance
// can host many domains (Globular's own self-knowledge, a cold-source pilot for
// caddy, etc.); a query scoped to one domain must return ONLY that domain's
// nodes plus shared meta-principles — never another repo's rules.
//
// This file is the PURE resolution + filtering core: no store, no RPC, no I/O,
// so the isolation guarantee is exhaustively unit-testable. The serving layer
// (briefing/impact/resolve handlers) maps store facts to domain keys and calls
// these functions.
//
// Domain key conventions (a "domain key" is the string that identifies a
// selectable domain):
//   - a repo node's key is its repo string, e.g. "github.com/caddyserver/caddy"
//   - the host project's own untagged nodes share a single "home" key chosen by
//     the caller (e.g. the host repo id) — they are a domain like any other
//   - rdf.DomainShared ("shared") is NOT a selectable domain: shared nodes are
//     always visible regardless of the resolved scope, and never count toward
//     ambiguity.

// AmbiguousScopeError is returned (fail closed) when a query provides no domain
// and the graph holds 2+ selectable domains, so attribution to a single domain
// is impossible. Callers MUST surface this as an error, never as mixed-domain
// results — returning another repo's rules to an unscoped query is exactly the
// cross-domain leak this design forbids.
type AmbiguousScopeError struct {
	Available []string // the distinct selectable domain keys, sorted
}

func (e *AmbiguousScopeError) Error() string {
	return fmt.Sprintf("ambiguous domain scope: graph holds %d domains %v — "+
		"specify a domain/repo (fail closed rather than mix domains)", len(e.Available), e.Available)
}

// ResolveScope decides which single domain a query targets.
//
//	available  — distinct selectable domain keys present in the graph (exclude shared).
//	requested  — explicit domain/repo key from the query ("" = none provided).
//
// Rules (matching the agreed policy: ambiguous only if >1 domain):
//   - explicit request → use it verbatim. Server handlers that can enumerate
//     domains validate existence before calling this; this pure helper only
//     resolves the already-admitted scope.
//   - no request, 0 selectable domains → "" (only shared exists). Not ambiguous.
//   - no request, exactly 1 selectable domain → that domain. Trivially unambiguous,
//     so the host project's existing single-domain briefings keep working unchanged.
//   - no request, 2+ selectable domains → fail closed with AmbiguousScopeError.
func ResolveScope(available []string, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}
	uniq := uniqueSorted(available)
	switch len(uniq) {
	case 0:
		return "", nil
	case 1:
		return uniq[0], nil
	default:
		return "", &AmbiguousScopeError{Available: uniq}
	}
}

// InScope reports whether a node with domain key nodeDomain is visible to a
// query resolved to resolvedScope. Shared nodes are always visible; any other
// node is visible only when its domain key equals the resolved scope. An empty
// resolvedScope (only shared in the graph) admits shared nodes and nothing else.
func InScope(nodeDomain, resolvedScope string) bool {
	if nodeDomain == rdf.DomainShared {
		return true
	}
	return nodeDomain != "" && nodeDomain == resolvedScope
}

// briefingScope picks the domain a non-impact briefing section (implementation
// patterns, intent triggers) is scoped to. An explicit request wins; else the
// file's resolved domain; else the home domain — so an unanchored file (no
// resolved domain) still sees home patterns rather than being wrongly filtered
// to shared-only (InScope(home, "") is false).
func briefingScope(requested, resolved, home string) string {
	if requested != "" {
		return requested
	}
	if resolved != "" {
		return resolved
	}
	return home
}

// inScopePatterns keeps only the patterns visible to scope: shared always, a
// repo pattern only when its domain matches. Prevents a domain-scoped briefing
// from leaking another repo's implementation patterns.
func inScopePatterns(loaded []loadedPattern, scope string) []loadedPattern {
	out := make([]loadedPattern, 0, len(loaded))
	for _, p := range loaded {
		if InScope(p.Domain, scope) {
			out = append(out, p)
		}
	}
	return out
}

// inScopeIntents is the intent-trigger analogue of inScopePatterns.
func inScopeIntents(loaded []loadedIntent, scope string) []loadedIntent {
	out := make([]loadedIntent, 0, len(loaded))
	for _, in := range loaded {
		if InScope(in.Domain, scope) {
			out = append(out, in)
		}
	}
	return out
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || s == rdf.DomainShared {
			continue // shared and empties are never selectable domains
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
