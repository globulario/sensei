// SPDX-License-Identifier: AGPL-3.0-only

package ledger_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode"
)

// forbiddenControlPhrases are the CamelCase word phrases that mark a symbol as
// history rewriting or fault/test control rather than ledger capability.
//
// Two properties make this a general rule rather than a name list -- and a name
// list is exactly what let the escape through that this test was written for
// (WithHeadPublicationFault, which no fixed list happened to name):
//
//	word phrases   Matching is on CamelCase WORD boundaries, not substrings, so a
//	               legitimate "Default" is never flagged. A guard that cries wolf
//	               gets deleted, which makes precision a correctness property.
//	word STEMS     Each word matches by prefix, so one entry covers the whole
//	               inflected family: inject / injects / injected / injector.
//	               Naming the exact form is how a list starts rotting again.
var forbiddenControlPhrases = [][]string{
	{"fault"},         // WithHeadPublicationFault(s), HeadFault, FaultSeam, faulty
	{"inject"},        // InjectHeadWriteFaults, InjectedHeadError, Injector
	{"simulat"},       // SimulateHeadFailure, SimulatedWrite, HeadSimulation
	{"rewrit"},        // RewriteLatest, RewritingStore -- history rewriting
	{"for", "test"},   // NewStoreForTest, ...ForTesting escape hatches
	{"test", "only"},  // TestOnlyReset
	{"test", "hook"},  // TestHookBeforeHeadWrite
	{"force", "fail"}, // ForceFailure, ForceFailedHeadWrite
}

// camelWords splits a Go identifier into lowercased CamelCase words.
// "WithHeadPublicationFaults" -> with, head, publication, faults
// "Default"                   -> default   (so "fault" does NOT match it)
// "InjectHeadWriteFaults"     -> inject, head, write, faults
// Digits attach to the word in progress; an acronym run ("HTTPServer") splits
// before the final capital that starts the next word.
func camelWords(name string) []string {
	var (
		words []string
		cur   []rune
	)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '_':
			flush()
		case unicode.IsUpper(r):
			// Start a new word unless we are inside an acronym run that
			// continues (e.g. the "HTTP" of "HTTPServer").
			prevUpper := i > 0 && unicode.IsUpper(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if !prevUpper || nextLower {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}

// forbiddenControlWord reports the control phrase an identifier carries, if any.
// A phrase matches as a contiguous run of CamelCase words, each word matched by
// stem prefix. So "NewStoreForTest" and "NewStoreForTesting" both match
// {"for","test"}, while "Default" matches nothing.
func forbiddenControlWord(name string) (string, bool) {
	words := camelWords(name)
	for _, phrase := range forbiddenControlPhrases {
		for start := 0; start+len(phrase) <= len(words); start++ {
			matched := true
			for i, stem := range phrase {
				if !strings.HasPrefix(words[start+i], stem) {
					matched = false
					break
				}
			}
			if matched {
				return strings.Join(phrase, " "), true
			}
		}
	}
	return "", false
}

// TestNoHistoryRewriteOrFaultToggleAPI proves the immutability law at the API
// boundary: the NORMAL ledger build (no build tags) exports no capability to
// rewrite committed history or toggle HEAD-write failures.
//
// The fault seam lives in a sensei_faultinject-tagged file that ships in no
// ordinary build. This test reads the package's default-build source set and
// fails if any such symbol leaks into it. It inspects the whole declaration
// surface -- functions, methods, types, vars and consts -- because a seam is
// sealed only if the default build exposes no way to reach it, and a guard that
// checks only top-level functions proves less than its name claims.
func TestNoHistoryRewriteOrFaultToggleAPI(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the ledger package directory")
	}
	dir := filepath.Dir(thisFile)

	// build.Default carries no custom build tags, so files gated by
	// //go:build sensei_faultinject are excluded from GoFiles (they land in
	// IgnoredGoFiles). GoFiles is exactly the shipped, non-test source set.
	pkg, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		t.Fatalf("import ledger dir: %v", err)
	}

	// The tagged fault seam must be excluded from the default build.
	for _, f := range pkg.GoFiles {
		if strings.Contains(f, "faultinject") && f != "faultinject_off.go" {
			t.Fatalf("%s leaked into the default (untagged) build", f)
		}
	}
	if len(pkg.IgnoredGoFiles) == 0 {
		t.Error("no build-tagged files at all: the fault seam is expected to live behind sensei_faultinject")
	}

	// No production-shipped file may declare an exported history-rewrite or
	// fault/test-control symbol.
	fset := token.NewFileSet()
	for _, name := range pkg.GoFiles {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			for _, ident := range exportedDeclNames(decl) {
				if bad, found := forbiddenControlWord(ident); found {
					t.Errorf("default ledger build exports %q in %s: the %q control word marks "+
						"test/rewrite machinery, which must live behind the sensei_faultinject build tag",
						ident, name, bad)
				}
			}
		}
	}
}

// exportedDeclNames returns the exported names a declaration introduces:
// functions, methods, types, vars and consts. Methods count -- a caller holding
// a *Store can reach them just as easily as a package-level function.
func exportedDeclNames(decl ast.Decl) []string {
	var names []string
	add := func(id *ast.Ident) {
		if id != nil && id.IsExported() {
			names = append(names, id.Name)
		}
	}
	switch d := decl.(type) {
	case *ast.FuncDecl:
		add(d.Name)
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				add(s.Name)
			case *ast.ValueSpec:
				for _, id := range s.Names {
					add(id)
				}
			}
		}
	}
	return names
}

// TestCamelWordsSplitsWithoutFalsePositives pins the matcher itself. Without it
// the guard above could silently stop matching -- a guard whose matcher is
// untested can pass for the wrong reason, which is exactly the defect it exists
// to catch.
func TestCamelWordsSplitsWithoutFalsePositives(t *testing.T) {
	for _, name := range []string{
		// The pair this guard exists for.
		"WithHeadPublicationFault", "WithHeadPublicationFaults",
		// Inflected forms a fixed name list would miss.
		"InjectHeadWriteFaults", "InjectedHeadError", "HeadWriteInjector",
		"SimulateHeadFailure", "SimulatedHeadWrite", "HeadWriteSimulation",
		"RewriteLatest", "RewritingStore",
		"NewStoreForTest", "NewStoreForTesting",
		"TestOnlyReset", "TestHookBeforeHeadWrite", "ForceFailedHeadWrite",
		"FaultSeam", "FaultyHeadWriter",
	} {
		if _, found := forbiddenControlWord(name); !found {
			t.Errorf("%q was not flagged as fault/test control", name)
		}
	}
	// Legitimate ledger vocabulary must not trip the guard, or it gets deleted.
	for _, name := range []string{
		"Default", "DefaultStore", "Defaulted", "Append", "ReconcileDerivedState",
		"VerifyTaskLedger", "RebuildProjections", "HeadSchemaVersion",
		"ErrEntryDurable", "WithPayloadValidator", "StoreArtifactBytes",
		"ValidateTaskEventPayload", "ParseTaskEventPayload", "ProjectionState",
		"ImportLegacyTask", "VerificationScope", "DigestComputations",
	} {
		if bad, found := forbiddenControlWord(name); found {
			t.Errorf("%q was flagged on word %q: false positive", name, bad)
		}
	}
}
