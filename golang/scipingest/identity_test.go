// SPDX-License-Identifier: AGPL-3.0-only

package scipingest

import (
	"reflect"
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
)

// A SCIP symbol carries its full scope. The code-symbol id must not throw that
// scope away, because two differently-scoped declarations in one file are two
// different things -- `type Mode string` and the `Mode` field of TaskMode both
// live in internal/workflow/mode.go, and a graph that keeps only one of them
// answers questions about the wrong declaration.

// scopedIndex models mode.go: a top-level type Mode, the TaskMode struct that
// has a field of that type, and a top-level constant. Order is the caller's, so
// a test can present the same declarations in either order.
func scopedIndex(reversed bool) *scip.Index {
	const (
		modeType  = "scip-go gomod repo . `workflow`/Mode#"
		taskMode  = "scip-go gomod repo . `workflow`/TaskMode#"
		modeField = "scip-go gomod repo . `workflow`/TaskMode#Mode."
		assisted  = "scip-go gomod repo . `workflow`/Assisted."
		// A field of an anonymous struct nested inside TaskMode: its scope is
		// two types deep, and only the whole chain identifies it.
		nested = "scip-go gomod repo . `workflow`/TaskMode#$anon_7f3#Mode."
	)
	syms := []*scip.SymbolInformation{
		{Symbol: modeType, DisplayName: "Mode", Kind: scip.SymbolInformation_Type},
		{Symbol: taskMode, DisplayName: "TaskMode", Kind: scip.SymbolInformation_Struct},
		{Symbol: modeField, DisplayName: "Mode", Kind: scip.SymbolInformation_Field},
		{Symbol: assisted, DisplayName: "Assisted", Kind: scip.SymbolInformation_Constant},
		{Symbol: nested, DisplayName: "Mode", Kind: scip.SymbolInformation_Field},
	}
	if reversed {
		for i, j := 0, len(syms)-1; i < j; i, j = i+1, j-1 {
			syms[i], syms[j] = syms[j], syms[i]
		}
	}
	return &scip.Index{Documents: []*scip.Document{{
		RelativePath: "internal/workflow/mode.go",
		Language:     "go",
		Symbols:      syms,
	}}}
}

func TestASymbolScopedByATypeKeepsThatScopeInItsIdentity(t *testing.T) {
	res := Ingest(scopedIndex(false), Options{})
	got := map[string]string{}
	for _, s := range res.Symbols {
		got[s.ID] = s.Kind
	}
	want := map[string]string{
		"internal/workflow/mode.go:Mode":                    "type",
		"internal/workflow/mode.go:TaskMode":                "type",
		"internal/workflow/mode.go:TaskMode.Mode":           "var",
		"internal/workflow/mode.go:Assisted":                "const",
		"internal/workflow/mode.go:TaskMode.$anon_7f3.Mode": "var",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the field lost its scope and collided with the type it is named after\n got: %v\nwant: %v", got, want)
	}
}

// The census must be a function of the declarations, not of the order the
// indexer happened to emit them in. scip-go visits packages concurrently, so
// two runs over one unchanged tree can present the same symbols in a different
// order; if identity collapses, whichever arrives first wins and unchanged
// source appears to change kind between runs.
func TestTheSymbolCensusDoesNotDependOnIndexOrder(t *testing.T) {
	forward := Ingest(scopedIndex(false), Options{})
	reverse := Ingest(scopedIndex(true), Options{})
	if !reflect.DeepEqual(forward.Symbols, reverse.Symbols) {
		t.Fatalf("the same declarations produced two different censuses\nforward: %+v\nreverse: %+v", forward.Symbols, reverse.Symbols)
	}
	// Order-independence alone is not the property this test is named for: a
	// census that loses the same declarations every run is also stable. The
	// census must be the WHOLE census, so nothing may be discarded.
	if forward.Dropped != 0 || len(forward.Symbols) != len(scopedIndex(false).GetDocuments()[0].GetSymbols()) {
		t.Fatalf("the census is stable but incomplete: %d of %d declarations survived, %d dropped",
			len(forward.Symbols), len(scopedIndex(false).GetDocuments()[0].GetSymbols()), forward.Dropped)
	}
}

// collidingIndex models the residue: two declarations that reach one id even
// with their scope kept -- both are package-level and named Handler in one
// file, so there is no enclosing type to tell them apart. They differ in kind,
// which is what makes the surviving one observable. Scope cannot separate them,
// so the tie must at least be broken the same way every run, and the loss must
// be counted.
func collidingIndex(reversed bool) *scip.Index {
	syms := []*scip.SymbolInformation{
		{Symbol: "scip-go gomod repo . `tools`/Handler#", DisplayName: "Handler", Kind: scip.SymbolInformation_Type},
		{Symbol: "scip-go gomod repo . `tools`/Handler().", DisplayName: "Handler", Kind: scip.SymbolInformation_Function},
	}
	if reversed {
		syms[0], syms[1] = syms[1], syms[0]
	}
	return &scip.Index{Documents: []*scip.Document{{
		RelativePath: "tools/handler.go",
		Language:     "go",
		Symbols:      syms,
	}}}
}

func TestAnUnseparableCollisionResolvesTheSameWayEveryRunAndIsCounted(t *testing.T) {
	forward := Ingest(collidingIndex(false), Options{})
	reverse := Ingest(collidingIndex(true), Options{})
	if !reflect.DeepEqual(forward.Symbols, reverse.Symbols) {
		t.Fatalf("the surviving symbol depends on the order the indexer emitted it\nforward: %+v\nreverse: %+v", forward.Symbols, reverse.Symbols)
	}
	if forward.Dropped != 1 || reverse.Dropped != 1 {
		t.Fatalf("a discarded definition was not counted: forward=%d reverse=%d, want 1 each", forward.Dropped, reverse.Dropped)
	}
}
