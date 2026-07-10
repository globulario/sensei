// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"sort"
	"strings"
	"testing"
)

func TestReconcileSubjects_StoreOnlyAreOrphans(t *testing.T) {
	seed := []string{
		"https://globular.io/awareness#invariant/a",
		"https://globular.io/awareness#invariant/b",
		"https://globular.io/awareness#failureMode/c",
	}
	live := map[string]bool{
		"https://globular.io/awareness#invariant/a":   true,
		"https://globular.io/awareness#failureMode/c": true,
		// runtime-only additive load — not in the authored corpus:
		"https://globular.io/awareness#invariant/ghost": true,
	}

	storeOnly, seedOnly := reconcileSubjects(seed, live)

	if got := strings.Join(storeOnly, ","); got != "https://globular.io/awareness#invariant/ghost" {
		t.Errorf("storeOnly = %q, want the ghost node only", got)
	}
	if got := strings.Join(seedOnly, ","); got != "https://globular.io/awareness#invariant/b" {
		t.Errorf("seedOnly = %q, want invariant/b (live store lagging)", got)
	}
}

func TestReconcileSubjects_CleanWhenIdentical(t *testing.T) {
	subjects := []string{
		"https://globular.io/awareness#invariant/a",
		"https://globular.io/awareness#invariant/b",
	}
	live := map[string]bool{subjects[0]: true, subjects[1]: true}

	storeOnly, seedOnly := reconcileSubjects(subjects, live)
	if len(storeOnly) != 0 || len(seedOnly) != 0 {
		t.Fatalf("expected clean reconcile, got storeOnly=%v seedOnly=%v", storeOnly, seedOnly)
	}
}

func TestReconcileSubjects_SortedAndDeterministic(t *testing.T) {
	seed := []string{"https://globular.io/awareness#x/1"}
	live := map[string]bool{
		"https://globular.io/awareness#x/1": true,
		"https://globular.io/awareness#z/9": true,
		"https://globular.io/awareness#a/0": true,
	}
	storeOnly, _ := reconcileSubjects(seed, live)
	if !sort.StringsAreSorted(storeOnly) {
		t.Errorf("storeOnly not sorted: %v", storeOnly)
	}
	if len(storeOnly) != 2 {
		t.Fatalf("want 2 orphans, got %d: %v", len(storeOnly), storeOnly)
	}
}

func TestAwarenessSubjectsFromNT_FiltersNamespaceAndStripsBrackets(t *testing.T) {
	nt := strings.Join([]string{
		`<https://globular.io/awareness#invariant/a> <https://globular.io/awareness#authoredIn> "docs/x.yaml" .`,
		`<https://globular.io/awareness#invariant/a> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Invariant> .`,
		`<http://example.com/foreign> <https://globular.io/awareness#authoredIn> "y" .`, // foreign ns — excluded
		`_:bnode <https://globular.io/awareness#authoredIn> "z" .`,                      // blank node — excluded
	}, "\n")

	got := awarenessSubjectsFromNT([]byte(nt))
	if len(got) != 1 || got[0] != "https://globular.io/awareness#invariant/a" {
		t.Fatalf("got %v, want exactly the single awareness subject (brackets stripped, distinct)", got)
	}
}

func TestReconcileExitCode(t *testing.T) {
	cases := []struct {
		name string
		res  reconcileResult
		want int
	}{
		{"orphans always fail", reconcileResult{StoreOnly: []string{"x"}, StoreState: "orphans"}, 1},
		{"clean passes", reconcileResult{StoreState: "reconciled"}, 0},
		{"down without require-clean passes", reconcileResult{StoreState: "down"}, 0},
		{"down with require-clean fails closed", reconcileResult{StoreState: "down", RequireClean: true}, 1},
		{"empty with require-clean fails closed", reconcileResult{StoreState: "empty", RequireClean: true}, 1},
	}
	for _, c := range cases {
		if got := reconcileExitCode(c.res); got != c.want {
			t.Errorf("%s: exit = %d, want %d", c.name, got, c.want)
		}
	}
}
