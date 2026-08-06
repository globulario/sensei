// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestValidateTestReconciliation_MissingAuthoritativeDefinitionForReferencedDiscoveredTest(t *testing.T) {
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "golang/server/main_test.go:TestBriefingStoreNil")
	codeSymbol := rdf.MintIRI(rdf.ClassCodeSymbol, "ns:code.go.server.Briefing")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "golang/server/main_test.go")
	nt := strings.Join([]string{
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropLabel) + " " + rdf.Lit("TestBriefingStoreNil") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
		codeSymbol + " " + rdf.IRI(rdf.PropTestedBy) + " " + testSymbol + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if len(report.ReferencedDiscoveredMissingSpec) != 1 || report.ReferencedDiscoveredMissingSpec[0] != "golang/server/main_test.go:TestBriefingStoreNil" {
		t.Fatalf("unexpected referenced missing-spec report: %+v", report)
	}
}

// A graph with no discovered test symbols for the anchor's package cannot say
// the test is missing — it never looked. This is the shape the standalone self
// build produces, where the generated/ code-symbol root is excluded: without
// the split it accused the repository of 195 missing tests, none of them real.
func TestValidateTestReconciliation_NoDiscoverySurfaceIsUnavailableNotMissing(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "golang/server/main_test.go:TestBriefingStoreNil")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if len(report.AuthoritativeMissingImplementation) != 0 {
		t.Fatalf("must not accuse without a discovery surface: %+v", report.AuthoritativeMissingImplementation)
	}
	if len(report.AuthoritativeDiscoveryUnavailable) != 1 ||
		report.AuthoritativeDiscoveryUnavailable[0] != "golang/server/main_test.go:TestBriefingStoreNil" {
		t.Fatalf("unexpected discovery-unavailable report: %+v", report)
	}
	if report.HasFindings() {
		t.Fatal("unverified coverage is not a finding")
	}
	if !report.HasUnverified() {
		t.Fatal("unverified coverage must still be visible, not silently clean")
	}
}

// When the package WAS inspected — another test in it was discovered — and the
// anchored test still is not there, that is a claim about the repository and
// must be reported as a genuine missing implementation.
func TestValidateTestReconciliation_MissingDiscoveredImplementationForRequiredGoTest(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "golang/server/main_test.go:TestBriefingStoreNil")
	sibling := rdf.MintIRI(rdf.ClassTestSymbol, "golang/server/main_test.go:TestSomethingThatDoesExist")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "golang/server/main_test.go")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		// A discovered sibling proves the package's tests were inspected.
		sibling + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		sibling + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if len(report.AuthoritativeMissingImplementation) != 1 || report.AuthoritativeMissingImplementation[0] != "golang/server/main_test.go:TestBriefingStoreNil" {
		t.Fatalf("unexpected authoritative missing-implementation report: %+v", report)
	}
	if len(report.AuthoritativeDiscoveryUnavailable) != 0 {
		t.Fatalf("an inspected package must not report as unavailable: %+v", report.AuthoritativeDiscoveryUnavailable)
	}
}

func TestValidateTestReconciliation_AuthoritativeAndDiscoveredAgree(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "golang/server/main_test.go:TestBriefingStoreNil")
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "golang/server/main_test.go:TestBriefingStoreNil")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "golang/server/main_test.go")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropLabel) + " " + rdf.Lit("golang/server/main_test.go:TestBriefingStoreNil") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("expected no findings, got %+v", report)
	}
}

func TestValidateTestReconciliation_DoubleColonRequiredTestMatchesDiscoveredAnchor(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "golang/server/main_test.go::TestBriefingStoreNil")
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "golang/server/main_test.go:TestBriefingStoreNil")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "golang/server/main_test.go")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropLabel) + " " + rdf.Lit("golang/server/main_test.go:TestBriefingStoreNil") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("double-colon required test anchor should match discovered implementation, got %+v", report)
	}
}

func TestValidateTestReconciliation_IgnoresSemanticRequiredTestIDs(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "awareness/debugsession:TestDebugSession_DesiredHashMismatch_FindsInvariant")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("semantic required test IDs should not require discovered implementation, got %+v", report)
	}
}

func TestValidateTestReconciliation_TypeScriptConcreteAnchorMatchesDiscoveredSymbol(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "typescript/client.spec.ts:SpecTitle_locate_uses_config")
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "typescript/client.spec.ts:SpecTitle_locate_uses_config")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "typescript/client.spec.ts")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("typescript concrete anchor should match discovered implementation, got %+v", report)
	}
}

func TestValidateTestReconciliation_JavaScriptConcreteAnchorMatchesDiscoveredSymbol(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "javascript/client.spec.js:SpecTitle_locate_uses_config")
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "javascript/client.spec.js:SpecTitle_locate_uses_config")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "javascript/client.spec.js")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("javascript concrete anchor should match discovered implementation, got %+v", report)
	}
}

func TestValidateTestReconciliation_PythonConcreteAnchorMatchesDiscoveredSymbol(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "python/test_client.py:test_locate_uses_config")
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "python/test_client.py:test_locate_uses_config")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "python/test_client.py")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("python concrete anchor should match discovered implementation, got %+v", report)
	}
}

func TestValidateTestReconciliation_RustConcreteAnchorMatchesDiscoveredSymbol(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "rust/src/lib.rs:test_locate_uses_config")
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "rust/src/lib.rs:test_locate_uses_config")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "rust/src/lib.rs")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("rust concrete anchor should match discovered implementation, got %+v", report)
	}
}

// The dangling-proof case: an `@awareness tested_by=` annotation names a test
// that no discovered test defines. Before ReferencedMissingImplementation
// existed, the referenced-symbol loop skipped any anchor that was not
// discovered, so this — the one shape that lets the graph assert coverage the
// code does not have — was the single case the validator stayed silent about.
func TestValidateTestReconciliation_ReferencedTestWithNoImplementationIsReported(t *testing.T) {
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "golang/server/resolve_test.go:TestResolveNotFound")
	codeSymbol := rdf.MintIRI(rdf.ClassCodeSymbol, "ns:code.go.server.resolveIRIForClassAndID")
	nt := strings.Join([]string{
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		codeSymbol + " " + rdf.IRI(rdf.PropTestedBy) + " " + testSymbol + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if len(report.ReferencedMissingImplementation) != 1 ||
		report.ReferencedMissingImplementation[0] != "golang/server/resolve_test.go:TestResolveNotFound" {
		t.Fatalf("dangling tested_by reference not reported: %+v", report)
	}
	if !report.HasFindings() {
		t.Fatal("HasFindings must be true for a dangling tested_by reference")
	}
}

// A placeholder like "<test>" appears in documentation examples of the
// annotation syntax. It is not a claim about real coverage, so it must not be
// reported — otherwise the new check cries wolf and gets ignored.
func TestValidateTestReconciliation_ReferencedPlaceholderAnchorIsNotReported(t *testing.T) {
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "<test>")
	codeSymbol := rdf.MintIRI(rdf.ClassCodeSymbol, "ns:code.go.example")
	nt := strings.Join([]string{
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		codeSymbol + " " + rdf.IRI(rdf.PropTestedBy) + " " + testSymbol + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if report.HasFindings() {
		t.Fatalf("placeholder anchor must not be reported, got %+v", report)
	}
}

// A referenced anchor that IS discovered stays on the existing missing-spec
// path — the new check must not swallow or duplicate it.
func TestValidateTestReconciliation_DiscoveredReferenceStillReportsMissingSpecOnly(t *testing.T) {
	testSymbol := rdf.MintIRI(rdf.ClassTestSymbol, "golang/server/main_test.go:TestBriefing_UnavailableWhenStoreNil")
	codeSymbol := rdf.MintIRI(rdf.ClassCodeSymbol, "ns:code.go.server.Briefing")
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, "golang/server/main_test.go")
	nt := strings.Join([]string{
		testSymbol + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		testSymbol + " " + rdf.IRI(rdf.PropDefinedInFile) + " " + fileIRI + " .",
		codeSymbol + " " + rdf.IRI(rdf.PropTestedBy) + " " + testSymbol + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if len(report.ReferencedMissingImplementation) != 0 {
		t.Fatalf("discovered reference must not be reported as dangling: %+v", report)
	}
	if len(report.ReferencedDiscoveredMissingSpec) != 1 {
		t.Fatalf("discovered reference should still report missing spec: %+v", report)
	}
}
