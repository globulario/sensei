// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=server.intent_triggers
// @awareness file_role=briefing_intent_trigger_matcher
// @awareness implements=globular.awareness_graph:intent.awg.graph_is_compiled_context_not_authority
// @awareness risk=low
package main

// intent_triggers.go — surfaces Intent nodes in briefing/preflight by
// matching task text against authored aw:activationTrigger literals.
//
// Before this file, only ImplementationPatterns consumed activation
// triggers; intent triggers were stored in the graph but never matched, so
// a contract-level intent (e.g. the ObjectStore topology screen contract)
// could not surface from a task phrase — the agent had to already know its
// exact ID. A safety contract that requires prior knowledge of its own ID
// is too passive; this matcher makes task phrases like "build the
// ObjectStore topology page" pull the law of the page unprompted.
//
// The scorer is the same tier engine patterns use (scoreTriggers): strong
// on full-phrase containment or ≥4 keyword overlap, medium on ≥2. There is
// no narrow/file-shape tier for intents — intents are task-scoped, not
// file-shaped. Matching never invents: no triggers, no match.

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// maxIntentsPerTaskMatch caps how many trigger-matched intents surface in
// one response. Mirrors maxPatternsPerBriefing.
const maxIntentsPerTaskMatch = 3

// loadedIntent is the unpacked form of an Intent node — label, level, and
// the activation triggers used for matching.
type loadedIntent struct {
	IRI                string
	ID                 string
	Domain             string // resolved domain key (home | repo | "shared") for scope filtering
	Label              string
	Level              string
	Status             string
	ActivationTriggers []string
}

type intentTriggerCache struct {
	mu      sync.RWMutex
	loaded  bool
	intents []loadedIntent
}

var globalIntentTriggerCache = &intentTriggerCache{}

// loadIntentTriggers populates the cache from store.ClassFacts. Same
// lifecycle as loadImplementationPatterns: first caller hits the store,
// errors leave the cache unloaded so a later call retries.
func (s *server) loadIntentTriggers(ctx context.Context) ([]loadedIntent, error) {
	globalIntentTriggerCache.mu.RLock()
	if globalIntentTriggerCache.loaded {
		out := globalIntentTriggerCache.intents
		globalIntentTriggerCache.mu.RUnlock()
		return out, nil
	}
	globalIntentTriggerCache.mu.RUnlock()

	globalIntentTriggerCache.mu.Lock()
	defer globalIntentTriggerCache.mu.Unlock()
	if globalIntentTriggerCache.loaded {
		return globalIntentTriggerCache.intents, nil
	}

	if s.store == nil {
		return nil, nil
	}
	// Intents are more numerous than patterns (200+); the limit bounds the
	// fact rows, and only intents that author activation_triggers are kept.
	facts, err := s.store.ClassFacts(ctx, rdf.ClassIntent, 5000)
	if err != nil {
		return nil, err
	}
	intents := classFactsToIntents(facts, s.homeDomain)
	globalIntentTriggerCache.intents = intents
	globalIntentTriggerCache.loaded = true
	return intents, nil
}

// classFactsToIntents reifies flat triples into loadedIntent values,
// keeping only matchable intents: at least one activation trigger and a
// live status (deprecated/superseded/rejected never surface).
func classFactsToIntents(facts []store.ImpactFact, homeDomain string) []loadedIntent {
	byNode := map[string]*loadedIntent{}
	for _, f := range facts {
		li, ok := byNode[f.NodeIRI]
		if !ok {
			// Untagged intents default to the home domain; aw:repo / aw:domain
			// override below (mirrors collectImpact's node-domain resolution).
			li = &loadedIntent{IRI: f.NodeIRI, ID: bareIDFromIRI(f.NodeIRI), Domain: homeDomain}
			byNode[f.NodeIRI] = li
		}
		switch f.Predicate {
		case rdf.PropRepo:
			if f.Object != "" {
				li.Domain = f.Object
			}
		case rdf.PropDomain:
			if f.Object == rdf.DomainShared {
				li.Domain = rdf.DomainShared
			}
		case rdf.PropLabel:
			li.Label = f.Object
		case rdf.PropStatus:
			li.Status = f.Object
		case rdf.PropLevel:
			li.Level = f.Object
		case rdf.PropActivationTrigger:
			li.ActivationTriggers = append(li.ActivationTriggers, f.Object)
		}
	}
	out := make([]loadedIntent, 0, len(byNode))
	for _, li := range byNode {
		if len(li.ActivationTriggers) == 0 {
			continue
		}
		switch li.Status {
		case "deprecated", "superseded", "rejected":
			continue
		}
		out = append(out, *li)
	}
	return out
}

// intentLevelRank orders intent levels by operational specificity: a
// contract for a specific screen outranks a broad principle when both
// match the same task.
func intentLevelRank(level string) int {
	switch level {
	case "contract":
		return 7
	case "safety_rule":
		return 6
	case "invariant":
		return 5
	case "constraint":
		return 4
	case "mechanism":
		return 3
	case "operator_model", "pattern", "operational", "implementation":
		return 2
	case "principle":
		return 1
	default: // vision and unknown levels rank last
		return 0
	}
}

// matchIntentsForTask returns up to maxIntentsPerTaskMatch intents whose
// activation triggers match the task text, strongest tier first, then by
// level specificity (contract > safety_rule > … > vision), then by ID for
// determinism. Returns nil when the task is empty or nothing matches.
func matchIntentsForTask(task string, intents []loadedIntent) []*awarenesspb.KnowledgeNode {
	if strings.TrimSpace(task) == "" || len(intents) == 0 {
		return nil
	}
	taskKW := patternKeywordsMap(task)

	type scoredIntent struct {
		intent loadedIntent
		tier   string
	}
	var scored []scoredIntent
	for _, in := range intents {
		tier, _ := scoreTriggers(in.ActivationTriggers, task, taskKW)
		if tier == "" {
			continue
		}
		scored = append(scored, scoredIntent{intent: in, tier: tier})
	}
	if len(scored) == 0 {
		return nil
	}

	tierRank := func(t string) int {
		if t == "strong" {
			return 2
		}
		return 1 // medium
	}
	sort.Slice(scored, func(i, j int) bool {
		if tierRank(scored[i].tier) != tierRank(scored[j].tier) {
			return tierRank(scored[i].tier) > tierRank(scored[j].tier)
		}
		if intentLevelRank(scored[i].intent.Level) != intentLevelRank(scored[j].intent.Level) {
			return intentLevelRank(scored[i].intent.Level) > intentLevelRank(scored[j].intent.Level)
		}
		return scored[i].intent.ID < scored[j].intent.ID
	})

	if len(scored) > maxIntentsPerTaskMatch {
		scored = scored[:maxIntentsPerTaskMatch]
	}

	out := make([]*awarenesspb.KnowledgeNode, 0, len(scored))
	for _, sc := range scored {
		out = append(out, &awarenesspb.KnowledgeNode{
			Iri:    sc.intent.IRI,
			Id:     sc.intent.ID,
			Class:  "Intent",
			Label:  sc.intent.Label,
			Status: sc.intent.Status,
		})
	}
	return out
}

// appendMatchedIntentsSection renders trigger-matched intents into briefing
// prose. Empty input renders nothing.
func appendMatchedIntentsSection(b *strings.Builder, intents []*awarenesspb.KnowledgeNode) {
	if len(intents) == 0 {
		return
	}
	b.WriteString("\nTask-matched intents (activation triggers):\n")
	for _, in := range intents {
		b.WriteString("- intent:" + in.GetId() + " — " + in.GetLabel() + "\n")
	}
}

// invalidateIntentTriggerCacheForTest resets the cache, mirroring
// invalidateImplementationPatternCacheForTest.
func invalidateIntentTriggerCacheForTest() {
	globalIntentTriggerCache.mu.Lock()
	defer globalIntentTriggerCache.mu.Unlock()
	globalIntentTriggerCache.loaded = false
	globalIntentTriggerCache.intents = nil
}
