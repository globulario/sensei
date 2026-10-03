// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"strings"
	"testing"
)

const isoGoMod = "module example.com/fx\n\ngo 1.22\n"

// A function-only adapter: no struct field to confine, no literal to invoke.
// Exactly the shape no earlier family could say anything true about.
const isoAdapter = `package legacy

import "example.com/fx/internal/core"

func Convert(s string) core.Record { return core.Record{Name: s} }
`

const isoAdapterHelper = `package legacy

func normalize(s string) string { return s }
`

const isoCore = `package core

type Record struct{ Name string }
`

// Production code that uses core but stays away from the adapter.
const isoWorkflow = `package workflow

import "example.com/fx/internal/core"

func Run() core.Record { return core.Record{} }
`

func isoTree(extra map[string]string) map[string]string {
	files := map[string]string{
		"go.mod":                       isoGoMod,
		"internal/legacy/convert.go":   isoAdapter,
		"internal/legacy/normalize.go": isoAdapterHelper,
		"internal/core/core.go":        isoCore,
		"internal/workflow/flow.go":    isoWorkflow,
	}
	for k, v := range extra {
		files[k] = v
	}
	return files
}

func isolation(owner string) Proposition {
	return Proposition{Kind: KindPackageImportConfinedTo, Dir: "internal/legacy", Owner: owner, SearchPaths: []string{"."}}
}

// W1. An isolated function-only package DERIVES with zero outside importers,
// and the proposition is ABOUT the package's own files -- the specimen shape the
// earlier families could not state.
func TestAnIsolatedFunctionOnlyPackageIsDerived(t *testing.T) {
	src := pinned(t, isoTree(nil))
	receipt, est := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Derived || est == nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	if receipt.DerivationID != "derive.package_import_confined_to" {
		t.Fatalf("derivation = %s", receipt.DerivationID)
	}
	got := strings.Join(receipt.SubjectFiles(), ",")
	if got != "internal/legacy/convert.go,internal/legacy/normalize.go" {
		t.Fatalf("subjects = %s; the package's own non-test files and nothing else", got)
	}
	// The importers that were checked are INPUTS, not subjects: reading
	// workflow/flow.go to see that it does not import the adapter says nothing
	// about workflow/flow.go.
	for _, s := range receipt.SubjectFiles() {
		if strings.HasPrefix(s, "internal/workflow/") || strings.HasPrefix(s, "internal/core/") {
			t.Fatalf("a file that was only read became a subject: %s", s)
		}
	}
	if !contains(receipt.Inputs, "go.mod") || !contains(receipt.Inputs, "internal/workflow/flow.go") {
		t.Fatalf("inputs = %v; go.mod binds the import path and every searched file must be named", receipt.Inputs)
	}
	if want := "no non-test source under . outside internal/legacy imports the package declared by internal/legacy"; p(receipt) != want {
		t.Fatalf("sentence = %q, want %q", p(receipt), want)
	}
}

// W2. One production import from outside the owner region refutes the
// isolation, and the receipt names the importer.
func TestAProductionImportOutsideTheOwnerRefutesIsolation(t *testing.T) {
	src := pinned(t, isoTree(map[string]string{
		"internal/workflow/replay.go": "package workflow\n\nimport \"example.com/fx/internal/legacy\"\n\nvar _ = legacy.Convert\n",
	}))
	receipt, est := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Refuted || est != nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	if !strings.Contains(receipt.Detail, "internal/workflow/replay.go:3") {
		t.Fatalf("detail does not name the importer: %s", receipt.Detail)
	}
	if !contains(receipt.SubjectFiles(), "internal/workflow/replay.go") {
		t.Fatalf("the violating importer is part of the counterexample: %v", receipt.SubjectFiles())
	}
}

// W2, every spelling of an import edge. An alias, a blank import and a dot
// import all put the package in the importer's program; none may hide.
func TestEveryImportSpellingIsAnImportEdge(t *testing.T) {
	for name, body := range map[string]string{
		"alias": "package workflow\n\nimport l \"example.com/fx/internal/legacy\"\n\nvar _ = l.Convert\n",
		"blank": "package workflow\n\nimport _ \"example.com/fx/internal/legacy\"\n",
		"dot":   "package workflow\n\nimport . \"example.com/fx/internal/legacy\"\n\nvar _ = Convert\n",
		"group": "package workflow\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/fx/internal/legacy\"\n)\n\nvar _ = fmt.Sprint(legacy.Convert)\n",
	} {
		t.Run(name, func(t *testing.T) {
			src := pinned(t, isoTree(map[string]string{"internal/workflow/replay.go": body}))
			receipt, _ := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
			if receipt.Outcome != Refuted {
				t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
			}
		})
	}
}

// A subdirectory of the package is a DIFFERENT package. Its import is an edge
// from outside, not the package importing itself.
func TestASubpackageImportIsFromOutside(t *testing.T) {
	src := pinned(t, isoTree(map[string]string{
		"internal/legacy/sub/sub.go": "package sub\n\nimport \"example.com/fx/internal/legacy\"\n\nvar _ = legacy.Convert\n",
	}))
	receipt, _ := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Refuted || !strings.Contains(receipt.Detail, "internal/legacy/sub/sub.go") {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
}

// A path that merely SHARES A PREFIX with the package's import path is another
// package; matching by prefix would refute an isolation that holds.
func TestAPrefixSharingPathIsNotTheSamePackage(t *testing.T) {
	src := pinned(t, isoTree(map[string]string{
		"internal/legacyx/x.go":     "package legacyx\n\nfunc X() {}\n",
		"internal/workflow/uses.go": "package workflow\n\nimport \"example.com/fx/internal/legacyx\"\n\nvar _ = legacyx.X\n",
	}))
	receipt, _ := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Derived {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
}

// W4a TEST-ONLY IMPORT CONTROL. A test outside the package that imports it is a
// test dependency, not the production graph reaching the adapter: the isolation
// still DERIVES, and neither the test nor a testdata fixture becomes a subject.
func TestATestOnlyImporterDoesNotRefuteIsolation(t *testing.T) {
	src := pinned(t, isoTree(map[string]string{
		"internal/workflow/flow_test.go":    "package workflow\n\nimport \"example.com/fx/internal/legacy\"\n\nvar _ = legacy.Convert\n",
		"internal/workflow/ext_test.go":     "package workflow_test\n\nimport \"example.com/fx/internal/legacy\"\n\nvar _ = legacy.Convert\n",
		"internal/workflow/testdata/bad.go": "package bad\n\nimport \"example.com/fx/internal/legacy\"\n\nvar _ = legacy.Convert\n",
	}))
	receipt, est := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Derived || est == nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	for _, s := range receipt.SubjectFiles() {
		if strings.HasSuffix(s, "_test.go") || strings.Contains(s, "/testdata/") {
			t.Fatalf("a test file became a subject: %s", s)
		}
	}
	for _, in := range receipt.Inputs {
		if strings.HasSuffix(in, "_test.go") || strings.Contains(in, "/testdata/") {
			t.Fatalf("a test file was read as production source: %s", in)
		}
	}
}

// The package's OWN test files are not part of what the proposition is about
// either: only its non-test files are subjects.
func TestThePackagesOwnTestsAreNotSubjects(t *testing.T) {
	src := pinned(t, isoTree(map[string]string{
		"internal/legacy/convert_test.go": "package legacy\n\nimport \"testing\"\n\nfunc TestConvert(t *testing.T) { _ = Convert(\"x\") }\n",
	}))
	receipt, _ := Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Derived || contains(receipt.SubjectFiles(), "internal/legacy/convert_test.go") {
		t.Fatalf("%s %v: %s", receipt.Outcome, receipt.SubjectFiles(), receipt.Detail)
	}
}

// A named owner is the one outside directory allowed to import the package; the
// same tree without it is refuted. The owner's own import is not a subject.
func TestTheNamedOwnerMayImportThePackage(t *testing.T) {
	src := pinned(t, isoTree(map[string]string{
		"internal/report/report.go": "package report\n\nimport \"example.com/fx/internal/legacy\"\n\nvar _ = legacy.Convert\n",
	}))
	receipt, _ := Derive(src, isolation("internal/report"), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Derived {
		t.Fatalf("with owner: %s: %s", receipt.Outcome, receipt.Detail)
	}
	if contains(receipt.SubjectFiles(), "internal/report/report.go") {
		t.Fatalf("an allowed importer became a subject: %v", receipt.SubjectFiles())
	}
	if want := "every observable non-test import of the package declared by internal/legacy under . originates from internal/legacy or internal/report"; p(receipt) != want {
		t.Fatalf("sentence = %q", p(receipt))
	}
	receipt, _ = Derive(src, isolation(""), at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Refuted {
		t.Fatalf("without owner: %s: %s", receipt.Outcome, receipt.Detail)
	}
}

// W3. An import path the pinned tree does not let this bind is UNRESOLVED,
// never DERIVED: finding no imports of a path nobody could have used proves
// nothing.
func TestAnUnboundablePackageIdentityIsUnresolved(t *testing.T) {
	t.Run("no go.mod", func(t *testing.T) {
		files := isoTree(nil)
		delete(files, "go.mod")
		receipt, est := Derive(pinned(t, files), isolation(""), at("2026-10-03T19:30:00Z"))
		if receipt.Outcome != Unresolved || est != nil {
			t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
		}
	})
	t.Run("go.mod without a module line", func(t *testing.T) {
		receipt, _ := Derive(pinned(t, isoTree(map[string]string{"go.mod": "go 1.22\n"})), isolation(""), at("2026-10-03T19:30:00Z"))
		if receipt.Outcome != Unresolved {
			t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
		}
	})
	t.Run("nested module", func(t *testing.T) {
		receipt, _ := Derive(pinned(t, isoTree(map[string]string{
			"internal/legacy/go.mod": "module example.com/other\n",
		})), isolation(""), at("2026-10-03T19:30:00Z"))
		if receipt.Outcome != Unresolved || !strings.Contains(receipt.Detail, "internal/legacy/go.mod") {
			t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
		}
	})
	t.Run("two package clauses", func(t *testing.T) {
		receipt, _ := Derive(pinned(t, isoTree(map[string]string{
			"internal/legacy/other.go": "package other\n",
		})), isolation(""), at("2026-10-03T19:30:00Z"))
		if receipt.Outcome != Unresolved {
			t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
		}
	})
}

// No package, no proposition: a directory with only tests, or none at all, is
// UNKNOWN rather than an isolation of nothing.
func TestNoPackageIsUnknownNotIsolated(t *testing.T) {
	for name, extra := range map[string]map[string]string{
		"only tests": {"internal/empty/e_test.go": "package empty\n"},
		"absent":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			p := isolation("")
			p.Dir = "internal/empty"
			receipt, est := Derive(pinned(t, isoTree(extra)), p, at("2026-10-03T19:30:00Z"))
			if receipt.Outcome != Unknown || est != nil {
				t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
			}
		})
	}
}

// W4 GENERICITY, in fixture form: a different seam -- a root-level package whose
// import path IS the module path, imported by a command that is its owner.
func TestARootPackageWithACommandOwnerIsDerived(t *testing.T) {
	src := pinned(t, map[string]string{
		"go.mod":         isoGoMod,
		"fx.go":          "package fx\n\nfunc Version() string { return \"v\" }\n",
		"cmd/fx/main.go": "package main\n\nimport \"example.com/fx\"\n\nfunc main() { _ = fx.Version() }\n",
	})
	prop := Proposition{Kind: KindPackageImportConfinedTo, Dir: ".", Owner: "cmd/fx", SearchPaths: []string{"."}}
	receipt, _ := Derive(src, prop, at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Derived || strings.Join(receipt.SubjectFiles(), ",") != "fx.go" {
		t.Fatalf("%s %v: %s", receipt.Outcome, receipt.SubjectFiles(), receipt.Detail)
	}
	prop.Owner = ""
	receipt, _ = Derive(src, prop, at("2026-10-03T19:30:00Z"))
	if receipt.Outcome != Refuted || !strings.Contains(receipt.Detail, "cmd/fx/main.go") {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
}

// A proposition missing its package or its search scope is not one this family
// can attempt; nothing else may answer it in its place.
func TestAnIncompleteIsolationPropositionIsUnknown(t *testing.T) {
	src := pinned(t, isoTree(nil))
	for name, prop := range map[string]Proposition{
		"no dir":    {Kind: KindPackageImportConfinedTo, SearchPaths: []string{"."}},
		"no search": {Kind: KindPackageImportConfinedTo, Dir: "internal/legacy"},
	} {
		t.Run(name, func(t *testing.T) {
			receipt, est := Derive(src, prop, at("2026-10-03T19:30:00Z"))
			if receipt.Outcome != Unknown || est != nil {
				t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
			}
		})
	}
}

func p(r Receipt) string { return r.Proposition.String() }

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
