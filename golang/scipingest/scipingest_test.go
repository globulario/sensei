// SPDX-License-Identifier: AGPL-3.0-only

package scipingest

import (
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"

	"github.com/globulario/sensei/golang/scanner"
)

// buildIndex constructs a minimal SCIP index modeling command/issue.go with two
// top-level functions: issueClose (lines 10-20) and issueReopen (lines 30-40).
// issueClose references an external symbol fmt.Fprintf; issueReopen references
// the internal issueClose. This mirrors the cli/cli #1337 shape.
func buildIndex() *scip.Index {
	const (
		closeSym  = "scip-go gomod repo . `command`/issueClose()."
		reopenSym = "scip-go gomod repo . `command`/issueReopen()."
		fprintf   = "scip-go gomod std . `fmt`/Fprintf()."
	)
	doc := &scip.Document{
		RelativePath: "command/issue.go",
		Language:     "go",
		Symbols: []*scip.SymbolInformation{
			{Symbol: closeSym, DisplayName: "issueClose", Kind: scip.SymbolInformation_Function},
			{Symbol: reopenSym, DisplayName: "issueReopen", Kind: scip.SymbolInformation_Function},
		},
		Occurrences: []*scip.Occurrence{
			// issueClose definition, body spans lines 10-20.
			{Symbol: closeSym, SymbolRoles: int32(scip.SymbolRole_Definition), Range: []int32{10, 5, 15}, EnclosingRange: []int32{10, 0, 20, 0}},
			// issueReopen definition, body spans lines 30-40.
			{Symbol: reopenSym, SymbolRoles: int32(scip.SymbolRole_Definition), Range: []int32{30, 5, 16}, EnclosingRange: []int32{30, 0, 40, 0}},
			// A reference to fmt.Fprintf inside issueClose (line 12).
			{Symbol: fprintf, Range: []int32{12, 8, 15}},
			// A reference to issueClose inside issueReopen (line 33).
			{Symbol: closeSym, Range: []int32{33, 8, 18}},
		},
	}
	return &scip.Index{Documents: []*scip.Document{doc}}
}

func TestIngest_Symbols(t *testing.T) {
	res := Ingest(buildIndex(), Options{})
	if len(res.Symbols) != 2 {
		t.Fatalf("want 2 symbols, got %d: %+v", len(res.Symbols), res.Symbols)
	}
	byID := map[string]bool{}
	for _, s := range res.Symbols {
		byID[s.ID] = true
		if s.File != "command/issue.go" {
			t.Errorf("symbol %s: file = %q, want command/issue.go", s.ID, s.File)
		}
		if s.Language != "go" {
			t.Errorf("symbol %s: language = %q, want go", s.ID, s.Language)
		}
		if s.Kind != "function" {
			t.Errorf("symbol %s: kind = %q, want function", s.ID, s.Kind)
		}
	}
	for _, want := range []string{"command/issue.go:issueClose", "command/issue.go:issueReopen"} {
		if !byID[want] {
			t.Errorf("missing expected symbol id %q", want)
		}
	}

	t.Run("method identity keeps receiver despite bare display name", func(t *testing.T) {
		si := &scip.SymbolInformation{
			Symbol:      "scip-go gomod repo . `render`/JSON#Render().",
			DisplayName: "Render",
			Kind:        scip.SymbolInformation_Method,
		}
		if got := symbolDisplayName(si); got != "JSON.Render" {
			t.Fatalf("symbolDisplayName() = %q, want receiver-qualified JSON.Render", got)
		}
	})
}

func TestIngest_References(t *testing.T) {
	res := Ingest(buildIndex(), Options{})
	// Expect: issueClose -> Fprintf (external), issueReopen -> issueClose (internal).
	var gotExternal, gotInternal bool
	for _, r := range res.Refs {
		switch {
		case r.FromID == "command/issue.go:issueClose" && r.ToName == "Fprintf":
			gotExternal = true
			if r.ToID != "" {
				t.Errorf("Fprintf is external, want empty ToID, got %q", r.ToID)
			}
		case r.FromID == "command/issue.go:issueReopen" && r.ToName == "issueClose":
			gotInternal = true
			if r.ToID != "command/issue.go:issueClose" {
				t.Errorf("internal ref ToID = %q, want command/issue.go:issueClose", r.ToID)
			}
		}
	}
	if !gotExternal {
		t.Errorf("missing attributed reference issueClose -> Fprintf; refs=%+v", res.Refs)
	}
	if !gotInternal {
		t.Errorf("missing attributed reference issueReopen -> issueClose; refs=%+v", res.Refs)
	}
}

// buildIndexWithTest adds a *_test.go document alongside the production one, so
// the ExcludeTestFiles option can be exercised.
func buildIndexWithTest() *scip.Index {
	idx := buildIndex()
	const testSym = "scip-go gomod repo . `command`/TestIssueClose()."
	idx.Documents = append(idx.Documents, &scip.Document{
		RelativePath: "command/issue_test.go",
		Language:     "go",
		Symbols: []*scip.SymbolInformation{
			{Symbol: testSym, DisplayName: "TestIssueClose", Kind: scip.SymbolInformation_Function},
		},
		Occurrences: []*scip.Occurrence{
			{Symbol: testSym, SymbolRoles: int32(scip.SymbolRole_Definition), Range: []int32{5, 5, 19}, EnclosingRange: []int32{5, 0, 25, 0}},
		},
	})
	return idx
}

func TestIngest_ExcludeTestFiles(t *testing.T) {
	// Default: the test symbol is ingested (backward-compatible).
	withTests := Ingest(buildIndexWithTest(), Options{})
	if !hasSymbolID(withTests.Symbols, "command/issue_test.go:TestIssueClose") {
		t.Fatal("default ingest should include the test symbol")
	}
	// ExcludeTestFiles: the test symbol is dropped, production symbols remain.
	noTests := Ingest(buildIndexWithTest(), Options{ExcludeTestFiles: true})
	if hasSymbolID(noTests.Symbols, "command/issue_test.go:TestIssueClose") {
		t.Error("ExcludeTestFiles should drop *_test.go symbols")
	}
	for _, want := range []string{"command/issue.go:issueClose", "command/issue.go:issueReopen"} {
		if !hasSymbolID(noTests.Symbols, want) {
			t.Errorf("ExcludeTestFiles dropped a production symbol %q", want)
		}
	}
}

func TestIsTestFile(t *testing.T) {
	tests := map[string]bool{
		"pkg/foo_test.go": true, "pkg/foo.go": false,
		"src/a.test.ts": true, "src/a.spec.tsx": true, "src/a.ts": false,
		"t/test_x.py": true, "t/x_test.py": true, "t/x.py": false,
	}
	for path, want := range tests {
		if got := isTestFile(path); got != want {
			t.Errorf("isTestFile(%q) = %v, want %v", path, got, want)
		}
	}
}

func hasSymbolID(syms []scanner.CodeSymbol, id string) bool {
	for _, s := range syms {
		if s.ID == id {
			return true
		}
	}
	return false
}

// TestIngest_SiblingConventionQuery is the 1337 motivating case: several
// functions that all reference the same symbol should be recoverable, so a
// caller can ask "which of these did a patch miss?".
func TestIngest_SiblingConventionQuery(t *testing.T) {
	res := Ingest(buildIndex(), Options{})
	callersOfFprintf := map[string]bool{}
	for _, r := range res.Refs {
		if r.ToName == "Fprintf" {
			callersOfFprintf[r.FromID] = true
		}
	}
	if !callersOfFprintf["command/issue.go:issueClose"] {
		t.Errorf("expected issueClose among Fprintf callers, got %v", callersOfFprintf)
	}
}
