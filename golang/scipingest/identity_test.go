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
		"internal/workflow/mode.go:Mode~type":               "type",
		"internal/workflow/mode.go:TaskMode~type":           "type",
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

// SCIP already distinguishes a type from a function of the same name:
// `Handler#` is the type, `Handler().` the function. An id that drops that
// distinction lets two declarations claim one identity, and no tie-break can
// repair it -- one of them simply stops existing. Both must survive.
func sameNameIndex(reversed bool) *scip.Index {
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

func TestATypeAndAFunctionOfOneNameAreTwoIdentities(t *testing.T) {
	forward := Ingest(sameNameIndex(false), Options{})
	reverse := Ingest(sameNameIndex(true), Options{})
	if !reflect.DeepEqual(forward.Symbols, reverse.Symbols) {
		t.Fatalf("the census depends on the order the indexer emitted the declarations\nforward: %+v\nreverse: %+v", forward.Symbols, reverse.Symbols)
	}
	got := map[string]string{}
	for _, s := range forward.Symbols {
		got[s.ID] = s.Kind
	}
	want := map[string]string{
		"tools/handler.go:Handler~type": "type",
		"tools/handler.go:Handler":      "function",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("a type and a function of one name did not survive as two identities\n got: %v\nwant: %v", got, want)
	}
	if forward.Dropped != 0 {
		t.Fatalf("a declaration was discarded although SCIP distinguishes the two: Dropped = %d, want 0", forward.Dropped)
	}
}

// A package is not declared in any one file -- scip-go emits one symbol per
// document -- so minting it as a CodeSymbol of the file that names it invents a
// declaration and collides with a real function of the same name. `package
// main` and `func main()` in one file were the last collision in the real
// corpus.
func TestAPackageIsNotADeclarationOfTheFileThatNamesIt(t *testing.T) {
	idx := &scip.Index{Documents: []*scip.Document{{
		RelativePath: "tools/lockscan.go",
		Language:     "go",
		Symbols: []*scip.SymbolInformation{
			{Symbol: "scip-go gomod repo . `tools`/", DisplayName: "main", Kind: scip.SymbolInformation_Package},
			{Symbol: "scip-go gomod repo . `tools`/main().", DisplayName: "main", Kind: scip.SymbolInformation_Function},
		},
	}}}
	res := Ingest(idx, Options{})
	got := map[string]string{}
	for _, s := range res.Symbols {
		got[s.ID] = s.Kind
	}
	want := map[string]string{"tools/lockscan.go:main": "function"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the package was minted as a declaration of this file\n got: %v\nwant: %v", got, want)
	}
	if res.Dropped != 0 {
		t.Fatalf("a real declaration was discarded to make room for a package: Dropped = %d, want 0", res.Dropped)
	}
}

// `_` is Go's deliberate anonymity: it cannot be referenced, so it can never be
// the subject of a question the graph is asked. Two of them in one file are not
// two declarations of anything, and counting the second as a discarded
// definition would make the loss counter report a loss that did not happen.
func TestTheBlankIdentifierIsNotADeclaration(t *testing.T) {
	idx := &scip.Index{Documents: []*scip.Document{{
		RelativePath: "internal/workflow/review_test.go",
		Language:     "go",
		Symbols: []*scip.SymbolInformation{
			{Symbol: "scip-go gomod repo . `workflow`/_.", DisplayName: "_", Kind: scip.SymbolInformation_Variable},
			{Symbol: "scip-go gomod repo . `workflow`/Reviewer#_.", DisplayName: "_", Kind: scip.SymbolInformation_Field},
			{Symbol: "scip-go gomod repo . `workflow`/Review().", DisplayName: "Review", Kind: scip.SymbolInformation_Function},
		},
	}}}
	res := Ingest(idx, Options{})
	if len(res.Symbols) != 1 || res.Symbols[0].ID != "internal/workflow/review_test.go:Review" {
		t.Fatalf("the blank identifier became a subject: %+v", res.Symbols)
	}
	if res.Dropped != 0 {
		t.Fatalf("discarding an anonymous non-declaration was reported as a loss: Dropped = %d, want 0", res.Dropped)
	}
}

// An indexer may report one declaration twice. That is one declaration: the
// repeat must not be counted as a discarded definition, or the loss counter --
// whose only value is that it is believed -- cries loss where there is none.
func TestOneDeclarationReportedTwiceIsNotALoss(t *testing.T) {
	sym := &scip.SymbolInformation{
		Symbol: "scip-go gomod repo . `workflow`/Review().", DisplayName: "Review", Kind: scip.SymbolInformation_Function,
	}
	idx := &scip.Index{Documents: []*scip.Document{{
		RelativePath: "internal/workflow/review.go",
		Language:     "go",
		Symbols:      []*scip.SymbolInformation{sym, sym},
	}}}
	res := Ingest(idx, Options{})
	if len(res.Symbols) != 1 {
		t.Fatalf("one declaration became %d symbols: %+v", len(res.Symbols), res.Symbols)
	}
	if res.Dropped != 0 {
		t.Fatalf("a repeat of one declaration was counted as a discarded definition: Dropped = %d, want 0", res.Dropped)
	}
}
