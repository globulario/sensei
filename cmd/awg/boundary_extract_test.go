// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/extractor/importgraph"
)

func TestExtractBoundaryCandidates(t *testing.T) {
	comps := []importgraph.Component{
		// A root-level internal/ package → module-private visibility boundary.
		{ID: "component.internal.bytesconv", Name: "bytesconv", SourceFiles: []string{"internal/bytesconv/bytesconv.go"}},
		// A nested internal/ package → scoped visibility boundary.
		{ID: "component.svc.internal.state", Name: "state", SourceFiles: []string{"svc/internal/state/state.go"}},
		// A contract-exposing component → API boundary.
		{ID: "component.api", Name: "api", ExposesContracts: []string{"contract.orders"}, SourceFiles: []string{"api/api.go"}},
		// A hub depended on by 3 components → stability boundary.
		{ID: "component.core", Name: "core", SourceFiles: []string{"core/core.go"}},
		{ID: "component.a", DependsOn: []string{"component.core"}},
		{ID: "component.b", DependsOn: []string{"component.core"}},
		{ID: "component.c", DependsOn: []string{"component.core"}},
		// A component with only 1 consumer → NOT a hub.
		{ID: "component.util", Name: "util", SourceFiles: []string{"util/util.go"}},
		{ID: "component.d", DependsOn: []string{"component.util"}},
	}

	got := map[string]boundaryCandidate{}
	for _, b := range extractBoundaryCandidates(comps) {
		got[b.ID] = b
	}

	// Root-level internal/: module-private wording.
	if b, ok := got["boundary.visibility.internal.bytesconv"]; !ok {
		t.Error("missing root-level internal visibility boundary")
	} else if !strings.Contains(b.Description, "module-private") || b.Kind != "visibility" {
		t.Errorf("root internal boundary wrong: kind=%q desc=%q", b.Kind, b.Description)
	}

	// Nested internal/: scoped to svc/.
	if b, ok := got["boundary.visibility.svc.internal.state"]; !ok {
		t.Error("missing nested internal visibility boundary")
	} else if !strings.Contains(b.Description, "within svc/") {
		t.Errorf("nested internal boundary not scoped to svc/: %q", b.Description)
	}

	// Contract exposure → API boundary.
	if b, ok := got["boundary.api.api"]; !ok {
		t.Error("missing API boundary for contract-exposing component")
	} else if b.Kind != "api" || len(b.ExposesContracts) != 1 {
		t.Errorf("API boundary wrong: %+v", b)
	}

	// Hub with 3 consumers → stability boundary.
	if b, ok := got["boundary.hub.core"]; !ok {
		t.Error("missing hub boundary for component.core (3 consumers)")
	} else if b.Kind != "stability" || !strings.Contains(b.Description, "3 components") {
		t.Errorf("hub boundary wrong: kind=%q desc=%q", b.Kind, b.Description)
	}

	// Util with 1 consumer → must NOT be a hub (below threshold).
	if _, ok := got["boundary.hub.util"]; ok {
		t.Error("component.util has 1 consumer but was flagged as a hub boundary")
	}
}

func TestBoundaryObservationFactsFromComponents(t *testing.T) {
	comps := []importgraph.Component{
		{ID: "component.internal.bytesconv", Name: "bytesconv", SourceFiles: []string{"internal/bytesconv/bytesconv.go"}},
		{ID: "component.api", Name: "api", ExposesContracts: []string{"contract.orders"}, SourceFiles: []string{"api/api.go"}},
		{ID: "component.a", DependsOn: []string{"component.api"}, SourceFiles: []string{"a/a.go"}},
	}
	facts := boundaryObservationFacts("github.com/example/repo", comps)
	assertBoundaryFact(t, facts, "component.internal.bytesconv", "is_under_go_internal_boundary", "")
	assertBoundaryFact(t, facts, "component.api", "exposes_contract", "contract.orders")
	assertBoundaryFact(t, facts, "component.a", "depends_on", "component.api")
	assertBoundaryFact(t, facts, "component.api", "has_observed_consumer", "component.a")
	for _, f := range facts {
		if f.Kind != "structural_observation" {
			t.Fatalf("boundary adapter emitted non-observation fact: %#v", f)
		}
	}
}

func TestInternalParent(t *testing.T) {
	if p, ok := internalParent([]string{"internal/x/x.go"}); !ok || p != "" {
		t.Errorf("root internal: got (%q,%v), want (\"\",true)", p, ok)
	}
	if p, ok := internalParent([]string{"svc/internal/x/x.go"}); !ok || p != "svc" {
		t.Errorf("nested internal: got (%q,%v), want (\"svc\",true)", p, ok)
	}
	if _, ok := internalParent([]string{"pkg/x/x.go"}); ok {
		t.Error("non-internal path reported as internal")
	}
}

func assertBoundaryFact(t *testing.T, facts []architecture.Fact, subject, predicate, object string) {
	t.Helper()
	for _, f := range facts {
		if f.Subject == subject && f.Predicate == predicate && f.Object == object {
			return
		}
	}
	t.Fatalf("missing boundary fact %s %s %s in %#v", subject, predicate, object, facts)
}
