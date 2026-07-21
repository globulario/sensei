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

func TestValidateTestReconciliation_MissingDiscoveredImplementationForRequiredGoTest(t *testing.T) {
	testIRI := rdf.MintIRI(rdf.ClassTest, "golang/server/main_test.go:TestBriefingStoreNil")
	nt := strings.Join([]string{
		testIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		testIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/required_tests.yaml") + " .",
	}, "\n")
	report, err := ValidateTestReconciliation(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateTestReconciliation: %v", err)
	}
	if len(report.AuthoritativeMissingImplementation) != 1 || report.AuthoritativeMissingImplementation[0] != "golang/server/main_test.go:TestBriefingStoreNil" {
		t.Fatalf("unexpected authoritative missing-implementation report: %+v", report)
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
