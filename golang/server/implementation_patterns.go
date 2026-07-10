// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.implementation_patterns
// @awareness file_role=briefing_pattern_matcher
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// implementation_patterns.go — surfaces ImplementationPattern nodes in the
// briefing response. The matcher is intentionally narrow: it scores task
// text against authored aw:activationTrigger literals, plus an optional
// "narrow file neighbour" rule that only fires when the briefing file's
// last two path segments share shape with a reference file (e.g.
// foo_client/foo_client.go ↔ echo_client/echo_client.go).
//
// What this file does NOT do:
//   - broad path glob matching (would fire on every *_client/*.go)
//   - mint runtime triples
//   - call any new RPC or read etcd
//   - invent related invariants/failure modes
//
// Cache: patterns are loaded once on first call and held in memory.
// Triggered re-fetch only on store reload (not implemented in v1 —
// briefing returns stale-but-bounded data, which is acceptable for the
// activation-trigger keyword space).

import (
	"context"
	"strings"
	"sync"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// maxPatternsPerBriefing caps how many patterns surface in one response.
// Three is plenty: stronger signal would be one pattern dominating.
const maxPatternsPerBriefing = 3

// loadedPattern is the unpacked form of an ImplementationPattern node —
// flat strings plus the activation_triggers used for matching. Reified
// from store.ImpactFact via classFactsToPatterns.
type loadedPattern struct {
	IRI                string
	ID                 string // bare id, e.g. "globular.pattern.grpc_client_standard"
	Domain             string // resolved domain key (home | repo | "shared") for scope filtering
	Label              string
	Status             string
	Rationale          string
	ActivationTriggers []string
	MustFollow         []string
	RequiredCalls      []string
	ForbiddenCalls     []string
	ReferenceFiles     []string // "role:path" literals
}

// patternCache is a process-level cache of loaded ImplementationPattern nodes.
// Patterns are small in number (a few dozen at most even at scale) and
// activation_trigger matching is read-heavy, so a single load + RW mutex is
// sufficient.
type patternCache struct {
	mu       sync.RWMutex
	loaded   bool
	patterns []loadedPattern
}

var globalPatternCache = &patternCache{}

// loadImplementationPatterns populates the cache from store.ClassFacts. Safe
// to call concurrently; only the first caller hits the store. A backend
// error surfaces as an error so the briefing can degrade gracefully — the
// cache stays unloaded so a later call retries.
func (s *server) loadImplementationPatterns(ctx context.Context) ([]loadedPattern, error) {
	globalPatternCache.mu.RLock()
	if globalPatternCache.loaded {
		out := globalPatternCache.patterns
		globalPatternCache.mu.RUnlock()
		return out, nil
	}
	globalPatternCache.mu.RUnlock()

	globalPatternCache.mu.Lock()
	defer globalPatternCache.mu.Unlock()
	if globalPatternCache.loaded {
		return globalPatternCache.patterns, nil
	}

	if s.store == nil {
		return nil, nil // store unavailable — return empty without erroring
	}
	facts, err := s.store.ClassFacts(ctx, rdf.ClassImplementationPattern, 100)
	if err != nil {
		return nil, err
	}
	patterns := classFactsToPatterns(facts, s.homeDomain)
	globalPatternCache.patterns = patterns
	globalPatternCache.loaded = true
	return patterns, nil
}

// classFactsToPatterns reifies the flat triples from ClassFacts into one
// loadedPattern per node. Per-node fields are accumulated by predicate.
func classFactsToPatterns(facts []store.ImpactFact, homeDomain string) []loadedPattern {
	byNode := map[string]*loadedPattern{}
	for _, f := range facts {
		lp, ok := byNode[f.NodeIRI]
		if !ok {
			// Untagged patterns default to the home domain; aw:repo / aw:domain
			// override below (mirrors collectImpact's node-domain resolution).
			lp = &loadedPattern{IRI: f.NodeIRI, ID: bareIDFromIRI(f.NodeIRI), Domain: homeDomain}
			byNode[f.NodeIRI] = lp
		}
		// f.Predicate is the bare property name, f.Object is the literal value.
		switch f.Predicate {
		case rdf.PropRepo:
			if f.Object != "" {
				lp.Domain = f.Object
			}
		case rdf.PropDomain:
			if f.Object == rdf.DomainShared {
				lp.Domain = rdf.DomainShared
			}
		case rdf.PropLabel:
			lp.Label = f.Object
		case rdf.PropStatus:
			lp.Status = f.Object
		case rdf.PropComment:
			lp.Rationale = f.Object
		case rdf.PropActivationTrigger:
			lp.ActivationTriggers = append(lp.ActivationTriggers, f.Object)
		case rdf.PropMustFollow:
			lp.MustFollow = append(lp.MustFollow, f.Object)
		case rdf.PropRequiresCall:
			lp.RequiredCalls = append(lp.RequiredCalls, f.Object)
		case rdf.PropForbidsCall:
			lp.ForbiddenCalls = append(lp.ForbiddenCalls, f.Object)
		case rdf.PropReferenceFile:
			lp.ReferenceFiles = append(lp.ReferenceFiles, f.Object)
		}
	}
	out := make([]loadedPattern, 0, len(byNode))
	for _, lp := range byNode {
		// Skip drafts/deprecated by status — only `active` patterns surface.
		if lp.Status != "" && lp.Status != "active" {
			continue
		}
		out = append(out, *lp)
	}
	return out
}

// bareIDFromIRI extracts the bare id from a minted IRI like
// "<https://globular.io/awareness#implementationPattern/globular.pattern.foo>".
// Returns the empty string when the IRI is malformed.
func bareIDFromIRI(iri string) string {
	s := strings.TrimPrefix(strings.TrimSuffix(iri, ">"), "<")
	slash := strings.LastIndexByte(s, '/')
	if slash < 0 || slash == len(s)-1 {
		return ""
	}
	return s[slash+1:]
}

// matchPatternsForBriefing returns up to maxPatternsPerBriefing patterns
// that match the task text or the file's structural shape. Strongest
// first. Empty when nothing matches strongly enough — we never invent
// matches to fill space.
func matchPatternsForBriefing(task, file string, patterns []loadedPattern) []*awarenesspb.MatchedImplementationPattern {
	if len(patterns) == 0 {
		return nil
	}
	taskKW := patternKeywordsMap(task)
	fileShape := narrowFileShape(file)

	scoredList := make([]scoredPattern, 0, len(patterns))

	for _, p := range patterns {
		tier, reasons := scorePattern(p, task, taskKW, fileShape)
		if tier == "" {
			continue
		}
		scoredList = append(scoredList, scoredPattern{pattern: p, tier: tier, reasons: reasons})
	}

	sortPatternsByTier(scoredList)

	if len(scoredList) > maxPatternsPerBriefing {
		scoredList = scoredList[:maxPatternsPerBriefing]
	}

	out := make([]*awarenesspb.MatchedImplementationPattern, 0, len(scoredList))
	for _, s := range scoredList {
		out = append(out, &awarenesspb.MatchedImplementationPattern{
			Id:               "implementation_pattern:" + s.pattern.ID,
			Label:            s.pattern.Label,
			MatchStrength:    s.tier,
			MatchReason:      s.reasons,
			ReferenceFiles:   s.pattern.ReferenceFiles,
			MustFollow:       s.pattern.MustFollow,
			RequiredCalls:    s.pattern.RequiredCalls,
			ForbiddenCalls:   s.pattern.ForbiddenCalls,
			RationaleSummary: firstLine(s.pattern.Rationale),
		})
	}
	return out
}

// scorePattern returns the strongest tier the pattern matches at, plus
// human-readable reasons. Empty tier means no match.
//
// Tier rules (only the highest match counts):
//
//	strong: a full activation_trigger is contained in the task text (case-
//	        insensitive), OR ≥4 distinct activation keywords overlap.
//	medium: ≥2 keyword-pair signals overlap (e.g. "grpc" + "client" + "service"
//	        where 2 of those terms are present and they're all from patterns).
//	narrow: file path's last 2 segments share shape with a reference file
//	        (e.g. "_client/_client.go") AND at least one weak signal exists.
//	        Pure shape match without ANY task signal is too weak to surface.
func scorePattern(p loadedPattern, task string, taskKW map[string]bool, fileShape string) (tier string, reasons []string) {
	tier, reasons, bestOverlap, bestTrigger := scoreTriggersWithOverlap(p.ActivationTriggers, task, taskKW)
	if tier != "" {
		return tier, reasons
	}

	// NARROW: file path shape matches a reference file's shape AND ≥1
	// keyword overlap somewhere. Pure shape match alone is too weak.
	// This tier is pattern-only — intents have no reference files.
	if fileShape != "" && bestOverlap >= 1 {
		for _, ref := range p.ReferenceFiles {
			refPath := stripRoleFromReferenceFile(ref)
			if narrowFileShape(refPath) == fileShape {
				return "narrow", []string{
					"file path shape (_client/_client.go) matches reference: " + ref,
					"1 keyword overlap with trigger: " + bestTrigger,
				}
			}
		}
	}
	return "", nil
}

// scoreTriggers scores task text against a trigger list using the shared
// tier engine (strong/medium). Used by both pattern and intent matching so
// the two surfaces cannot drift apart.
func scoreTriggers(triggers []string, task string, taskKW map[string]bool) (tier string, reasons []string) {
	tier, reasons, _, _ = scoreTriggersWithOverlap(triggers, task, taskKW)
	return tier, reasons
}

// scoreTriggersWithOverlap is the full-fidelity scorer: it also returns the
// best keyword-overlap count and trigger so scorePattern can apply its
// pattern-only narrow tier on a miss.
func scoreTriggersWithOverlap(triggers []string, task string, taskKW map[string]bool) (tier string, reasons []string, bestOverlap int, bestTrigger string) {
	if len(triggers) == 0 {
		return "", nil, 0, ""
	}
	taskLower := strings.ToLower(task)

	// STRONG: full activation_trigger phrase contained in task.
	for _, trig := range triggers {
		trigLower := strings.ToLower(strings.TrimSpace(trig))
		if trigLower == "" {
			continue
		}
		if strings.Contains(taskLower, trigLower) {
			return "strong", []string{"activation_trigger phrase matched: " + trig}, bestOverlap, bestTrigger
		}
		// Count distinct keyword overlap.
		count := 0
		for _, kw := range patternKeywords(trig) {
			if taskKW[kw] {
				count++
			}
		}
		if count > bestOverlap {
			bestOverlap = count
			bestTrigger = trig
		}
	}

	// STRONG (token threshold): ≥4 distinct activation keywords overlap.
	if bestOverlap >= 4 {
		return "strong", []string{
			"≥4 activation keywords overlap with trigger: " + bestTrigger,
		}, bestOverlap, bestTrigger
	}

	// MEDIUM: 2–3 keyword overlap with a single trigger.
	if bestOverlap >= 2 {
		return "medium", []string{
			"keyword pair overlap with trigger: " + bestTrigger,
		}, bestOverlap, bestTrigger
	}

	return "", nil, bestOverlap, bestTrigger
}

// patternKeywords lowercases the text and returns the set of distinct
// alphanumeric tokens ≥4 chars. Same shape as the diagnose composer's
// keyword extractor, deliberately permissive — patterns are matched
// against authored activation triggers, not free-form runtime text.
func patternKeywords(text string) []string {
	if text == "" {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	cur := strings.Builder{}
	flush := func() {
		w := cur.String()
		cur.Reset()
		if len(w) < 4 || seen[w] {
			return
		}
		seen[w] = true
		out = append(out, w)
	}
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// patternKeywordsSet is the same but returns a map for O(1) lookup.
func patternKeywordsMap(text string) map[string]bool {
	out := map[string]bool{}
	for _, k := range patternKeywords(text) {
		out[k] = true
	}
	return out
}

// narrowFileShape returns a non-empty token when the path's last two
// segments match the Globular client recipe shape, i.e. parent dir ends
// with "_client" AND filename ends with "_client.go". Otherwise empty.
// Deliberately narrow — we won't fire on every Go file under a service
// directory.
func narrowFileShape(file string) string {
	if file == "" {
		return ""
	}
	segs := strings.Split(file, "/")
	if len(segs) < 2 {
		return ""
	}
	parent := segs[len(segs)-2]
	filename := segs[len(segs)-1]
	if strings.HasSuffix(parent, "_client") && strings.HasSuffix(filename, "_client.go") {
		return "_client/_client.go"
	}
	return ""
}

// stripRoleFromReferenceFile turns "canonical_minimal:path/to/file.go" into
// "path/to/file.go". Returns the original string if no colon is found.
func stripRoleFromReferenceFile(s string) string {
	i := strings.IndexByte(s, ':')
	if i < 0 {
		return s
	}
	return s[i+1:]
}

// scoredPattern is a pattern plus its match metadata, used during ranking.
type scoredPattern struct {
	pattern loadedPattern
	tier    string
	reasons []string
}

// sortPatternsByTier orders the scored slice strongest-first.
func sortPatternsByTier(scored []scoredPattern) {
	rank := func(t string) int {
		switch t {
		case "strong":
			return 3
		case "medium":
			return 2
		case "narrow":
			return 1
		}
		return 0
	}
	// Simple insertion sort — ≤3 entries after capping, list is small.
	for i := 1; i < len(scored); i++ {
		for j := i; j > 0 && rank(scored[j].tier) > rank(scored[j-1].tier); j-- {
			scored[j-1], scored[j] = scored[j], scored[j-1]
		}
	}
}

// firstLine returns the first non-empty line of s, trimmed. Used to surface
// a one-line rationale_summary in the briefing without bloating it.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// invalidateImplementationPatternCacheForTest resets the cache. Used by tests
// that need to switch the underlying store between calls.
func invalidateImplementationPatternCacheForTest() {
	globalPatternCache.mu.Lock()
	defer globalPatternCache.mu.Unlock()
	globalPatternCache.loaded = false
	globalPatternCache.patterns = nil
}
