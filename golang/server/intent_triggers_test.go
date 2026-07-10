// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
	"github.com/globulario/awareness-graph/golang/store"
)

// topologyContractFacts mirrors the live intent
// admin_console.objectstore_topology_screen_contract — the law of the page
// this matcher exists to surface. Triggers match the authored YAML.
func topologyContractFacts() []store.ImpactFact {
	const subj = "<https://globular.io/awareness#intent/admin_console.objectstore_topology_screen_contract>"
	mk := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: subj, TypeIRI: rdf.ClassIntent, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		mk(rdf.PropLabel, "Law of the page: ObjectStore topology screen — defined before any component exists"),
		mk(rdf.PropStatus, "seed"),
		mk(rdf.PropLevel, "contract"),
		mk(rdf.PropActivationTrigger, "ObjectStoreTopologyPage"),
		mk(rdf.PropActivationTrigger, "objectstore topology screen"),
		mk(rdf.PropActivationTrigger, "/admin/objectstore/topology"),
		mk(rdf.PropActivationTrigger, "admin console topology"),
		mk(rdf.PropActivationTrigger, "topology apply UI"),
		mk(rdf.PropActivationTrigger, "minio topology screen"),
	}
}

// broadPrincipleFacts is a lower-level intent sharing one trigger keyword,
// used to prove contract-level intents rank first on equal tier.
func broadPrincipleFacts() []store.ImpactFact {
	const subj = "<https://globular.io/awareness#intent/some.broad_topology_principle>"
	mk := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: subj, TypeIRI: rdf.ClassIntent, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		mk(rdf.PropLabel, "Broad topology principle"),
		mk(rdf.PropStatus, "seed"),
		mk(rdf.PropLevel, "principle"),
		mk(rdf.PropActivationTrigger, "objectstore topology screen"),
	}
}

// unrelatedContractFacts is a screen contract whose triggers must NOT match
// topology tasks.
func unrelatedContractFacts() []store.ImpactFact {
	const subj = "<https://globular.io/awareness#intent/admin_console.release_status_screen_contract>"
	mk := func(p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: subj, TypeIRI: rdf.ClassIntent, Predicate: p, Object: o}
	}
	return []store.ImpactFact{
		mk(rdf.PropLabel, "Law of the page: release status screen"),
		mk(rdf.PropStatus, "seed"),
		mk(rdf.PropLevel, "contract"),
		mk(rdf.PropActivationTrigger, "ReleaseStatusPage"),
		mk(rdf.PropActivationTrigger, "release status screen"),
	}
}

// newIntentTriggerTestServer wires a fakeStore serving both intent facts and
// the canonical implementation pattern, resetting both caches.
func newIntentTriggerTestServer(t *testing.T) *server {
	t.Helper()
	invalidateImplementationPatternCacheForTest()
	invalidateIntentTriggerCacheForTest()
	return newServer(fakeStore{
		classFacts: func(_ context.Context, classIRI string, _ int) ([]store.ImpactFact, error) {
			switch classIRI {
			case rdf.ClassImplementationPattern:
				return grpcClientStandardFacts(), nil
			case rdf.ClassIntent:
				var out []store.ImpactFact
				out = append(out, topologyContractFacts()...)
				out = append(out, broadPrincipleFacts()...)
				out = append(out, unrelatedContractFacts()...)
				return out, nil
			}
			return nil, nil
		},
		impactForFile: func(_ context.Context, _ string) ([]store.ImpactFact, error) {
			return nil, nil
		},
	})
}

func briefingReferencedIDs(t *testing.T, s *server, task string) []string {
	t.Helper()
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{Task: task})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	return resp.GetReferencedIds()
}

// Required test 1 + 5: a task phrase surfaces the contract intent in both
// briefing (task-only) and preflight, without knowing its ID.
func TestIntentTrigger_TopologyTaskPhrase_SurfacesLawOfThePage(t *testing.T) {
	s := newIntentTriggerTestServer(t)

	for _, task := range []string{
		"build the ObjectStoreTopologyPage for the admin console",
		"implement /admin/objectstore/topology",
		"create the objectstore topology screen with an apply button",
	} {
		// Briefing, task-only.
		refs := briefingReferencedIDs(t, s, task)
		found := false
		for _, r := range refs {
			if r == "intent:admin_console.objectstore_topology_screen_contract" {
				found = true
			}
		}
		if !found {
			t.Fatalf("task %q: briefing did not surface the topology contract; refs=%v", task, refs)
		}

		// Preflight.
		pre, err := s.Preflight(context.Background(), &awarenesspb.PreflightRequest{Task: task})
		if err != nil {
			t.Fatalf("Preflight(%q): %v", task, err)
		}
		found = false
		for _, in := range pre.GetDirectIntents() {
			if in.GetId() == "admin_console.objectstore_topology_screen_contract" {
				found = true
			}
		}
		if !found {
			t.Fatalf("task %q: preflight DirectIntents missing topology contract", task)
		}
		if pre.GetStatus() == awarenesspb.PreflightStatus_PREFLIGHT_STATUS_EMPTY {
			t.Fatalf("task %q: preflight EMPTY despite matched contract", task)
		}
	}
}

// Required test 2: implementation-pattern trigger matching must not regress.
func TestIntentTrigger_PatternMatchingUnchanged(t *testing.T) {
	s := newIntentTriggerTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	pats := resp.GetImplementationPatterns()
	if len(pats) != 1 || !strings.Contains(pats[0].GetId(), "grpc_client_standard") {
		t.Fatalf("pattern matching regressed: %v", pats)
	}
	if pats[0].GetMatchStrength() != "strong" {
		t.Fatalf("want strong pattern match, got %s", pats[0].GetMatchStrength())
	}
}

// Required test 3: explicit ID lookup still resolves (matcher is additive).
func TestIntentTrigger_ExplicitIDLookupStillResolves(t *testing.T) {
	s := newServer(fakeStore{
		describe: func(_ context.Context, iri string) ([]store.Triple, error) {
			if !strings.Contains(iri, "intent/admin_console.objectstore_topology_screen_contract") {
				t.Fatalf("unexpected iri %s", iri)
			}
			return []store.Triple{
				{Predicate: rdf.RdfsNS + "label", Object: "Law of the page"},
				{Predicate: rdf.RdfNS + "type", Object: rdf.ClassIntent, ObjectIsIRI: true},
			}, nil
		},
	})
	resp, err := s.Resolve(context.Background(), &awarenesspb.ResolveRequest{
		Class: "intent", Id: "admin_console.objectstore_topology_screen_contract",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resp.GetFound() {
		t.Fatal("explicit ID lookup no longer resolves")
	}
}

// Required test 4: a non-matching task must not pull unrelated screen
// contracts.
func TestIntentTrigger_NonMatchingTaskPullsNothing(t *testing.T) {
	s := newIntentTriggerTestServer(t)
	refs := briefingReferencedIDs(t, s, "fix a typo in the etcd watcher log message")
	for _, r := range refs {
		if strings.HasPrefix(r, "intent:admin_console.") {
			t.Fatalf("unrelated task pulled screen contract: %v", refs)
		}
	}
	// And the topology task must not pull the RELEASE screen contract.
	refs = briefingReferencedIDs(t, s, "build the ObjectStoreTopologyPage for the admin console")
	for _, r := range refs {
		if r == "intent:admin_console.release_status_screen_contract" {
			t.Fatalf("topology task pulled the release contract: %v", refs)
		}
	}
}

// Ranking: when a contract and a broad principle both match at the same
// tier, the contract ranks first.
func TestIntentTrigger_ContractOutranksPrincipleOnEqualTier(t *testing.T) {
	invalidateIntentTriggerCacheForTest()
	intents := classFactsToIntents(append(topologyContractFacts(), broadPrincipleFacts()...), "home")
	matched := matchIntentsForTask("work on the objectstore topology screen layout", intents)
	if len(matched) < 2 {
		t.Fatalf("want both intents matched, got %d", len(matched))
	}
	if matched[0].GetId() != "admin_console.objectstore_topology_screen_contract" {
		t.Fatalf("contract did not rank first: %v", matched[0].GetId())
	}
}

// Lifecycle: deprecated/superseded intents never surface, and intents
// without triggers are not loaded.
func TestIntentTrigger_DeprecatedAndTriggerlessExcluded(t *testing.T) {
	mk := func(subj, p, o string) store.ImpactFact {
		return store.ImpactFact{NodeIRI: subj, TypeIRI: rdf.ClassIntent, Predicate: p, Object: o}
	}
	facts := []store.ImpactFact{
		mk("<https://x#intent/dead.contract>", rdf.PropLabel, "Dead"),
		mk("<https://x#intent/dead.contract>", rdf.PropStatus, "deprecated"),
		mk("<https://x#intent/dead.contract>", rdf.PropActivationTrigger, "objectstore topology screen"),
		mk("<https://x#intent/no.triggers>", rdf.PropLabel, "No triggers"),
		mk("<https://x#intent/no.triggers>", rdf.PropStatus, "seed"),
	}
	intents := classFactsToIntents(facts, "home")
	if len(intents) != 0 {
		t.Fatalf("want 0 loadable intents, got %d: %v", len(intents), intents)
	}
}
