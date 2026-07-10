// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// TestValidateReferences_DefinedAndReferenced asserts the well-formed
// case: a forbidden_fix is cited from an invariant AND defined by
// forbidden_fixes.yaml (the importer emits aw:authoredIn). No errors.
func TestValidateReferences_DefinedAndReferenced(t *testing.T) {
	ff := rdf.MintIRI(rdf.ClassForbiddenFix, "abc")
	nt := strings.Join([]string{
		// citation site (ensureNode from importInvariants)
		ff + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassForbiddenFix) + " .",
		// definition site (importForbiddenFixes — Typed + authoredIn)
		ff + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassForbiddenFix) + " .",
		ff + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/forbidden_fixes.yaml") + " .",
	}, "\n")
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors for defined+referenced; got %d: %v", len(errs), errs)
	}
}

// TestValidateReferences_DanglingForbiddenFix is the round-3 case: an
// invariant cites `forbidden.X` but forbidden_fixes.yaml only defines
// `X` (no `forbidden.` prefix). Two distinct IRIs are minted; the
// cited one has no authoredIn and is flagged.
func TestValidateReferences_DanglingForbiddenFix(t *testing.T) {
	citedIRI := rdf.MintIRI(rdf.ClassForbiddenFix, "forbidden.hot_deploy_local_binary_as_break_glass")
	definedIRI := rdf.MintIRI(rdf.ClassForbiddenFix, "hot_deploy_local_binary_as_break_glass")
	nt := strings.Join([]string{
		// invariants.yaml cites the prefixed name
		citedIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassForbiddenFix) + " .",
		// forbidden_fixes.yaml defines the un-prefixed name
		definedIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassForbiddenFix) + " .",
		definedIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " " + rdf.Lit("docs/awareness/forbidden_fixes.yaml") + " .",
	}, "\n")
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 dangling reference; got %d: %v", len(errs), errs)
	}
	if errs[0].ID != "forbidden.hot_deploy_local_binary_as_break_glass" {
		t.Errorf("expected ID 'forbidden.hot_deploy_local_binary_as_break_glass'; got %q", errs[0].ID)
	}
	if errs[0].Class != rdf.ClassForbiddenFix {
		t.Errorf("expected Class ForbiddenFix; got %q", errs[0].Class)
	}
	if !strings.Contains(errs[0].Error(), "dangling ForbiddenFix reference") {
		t.Errorf("error message missing expected prefix: %s", errs[0].Error())
	}
}

// TestValidateReferences_DanglingRequiredTest pins the same pattern for
// Test nodes. invariants.yaml cites a test ID, required_tests.yaml never
// defines it. Validator flags the test as dangling.
func TestValidateReferences_DanglingRequiredTest(t *testing.T) {
	citedIRI := rdf.MintIRI(rdf.ClassTest, "tests/never_defined_test.go::TestSomething")
	nt := strings.Join([]string{
		citedIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
	}, "\n")
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 dangling reference; got %d: %v", len(errs), errs)
	}
	if errs[0].Class != rdf.ClassTest {
		t.Errorf("expected Class Test; got %q", errs[0].Class)
	}
}

// A required_test citation (ClassTest) that points at a REAL, scanned test
// (ClassTestSymbol with aw:authoredIn) is proven to exist by the scan and must
// NOT be flagged dangling — even without a required_tests.yaml entry. The '::'
// citation separator and the ':' scanner separator must compare equal.
func TestValidateReferences_TestSymbolBridgeSatisfiesCitation(t *testing.T) {
	// invariants.yaml cites the test with the '::' convention.
	citedIRI := rdf.MintIRI(rdf.ClassTest, "golang/backup_manager/backup_manager_server/scylla_register_test.go::TestIsRegisteredScyllaCluster")
	// The code scanner emits the same test as a ClassTestSymbol with the ':'
	// convention (and aw:authoredIn to the source file).
	symbolIRI := rdf.MintIRI(rdf.ClassTestSymbol, "golang/backup_manager/backup_manager_server/scylla_register_test.go:TestIsRegisteredScyllaCluster")
	nt := strings.Join([]string{
		citedIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		symbolIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTestSymbol) + " .",
		symbolIRI + " " + rdf.IRI(rdf.PropAuthoredIn) + " \"golang/backup_manager/backup_manager_server/scylla_register_test.go\" .",
	}, "\n")
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("cited test backed by a scanned ClassTestSymbol must not be dangling; got %d: %v", len(errs), errs)
	}
}

// The bridge only satisfies a citation when a REAL test symbol exists. A cited
// test with no matching ClassTestSymbol (typo / nonexistent) still dangles.
func TestValidateReferences_TestSymbolBridge_NoSymbolStillDangles(t *testing.T) {
	citedIRI := rdf.MintIRI(rdf.ClassTest, "tests/typo_test.go::TestDoesNotExist")
	nt := citedIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " ."
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 1 || errs[0].Class != rdf.ClassTest {
		t.Fatalf("cited test with no scanned symbol must still dangle; got %v", errs)
	}
}

func TestValidateReferences_DanglingContractMention(t *testing.T) {
	missingContract := rdf.MintIRI(rdf.ClassContract, "contract.missing_authority")
	fm := rdf.MintIRI(rdf.ClassFailureMode, "failure.missing_contract")
	nt := strings.Join([]string{
		fm + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassFailureMode) + " .",
		fm + " " + rdf.IRI(rdf.PropViolatesContract) + " " + missingContract + " .",
		missingContract + " " + rdf.IRI(rdf.PropViolatedBy) + " " + fm + " .",
	}, "\n")
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 dangling contract reference; got %d: %v", len(errs), errs)
	}
	if errs[0].Class != rdf.ClassContract {
		t.Fatalf("expected Contract class; got %q", errs[0].Class)
	}
	if errs[0].ID != "contract.missing_authority" {
		t.Fatalf("expected missing contract id, got %q", errs[0].ID)
	}
}

// TestValidateReferences_StableOrder ensures error output is sorted by
// (class, id). yaml2nt prints these to stderr in CI; deterministic
// output is required for diff-based reviewers.
func TestValidateReferences_StableOrder(t *testing.T) {
	a := rdf.MintIRI(rdf.ClassForbiddenFix, "aaa")
	b := rdf.MintIRI(rdf.ClassForbiddenFix, "bbb")
	c := rdf.MintIRI(rdf.ClassTest, "ccc")
	// Emit in reverse-sorted order to force a real sort.
	nt := strings.Join([]string{
		c + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " .",
		b + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassForbiddenFix) + " .",
		a + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassForbiddenFix) + " .",
	}, "\n")
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 3 {
		t.Fatalf("expected 3 dangling references; got %d", len(errs))
	}
	wantIDs := []string{"aaa", "bbb", "ccc"}
	for i, want := range wantIDs {
		if errs[i].ID != want {
			t.Errorf("errs[%d].ID = %q; want %q", i, errs[i].ID, want)
		}
	}
}

// TestValidateReferences_IgnoresUntrackedClasses confirms that nodes
// typed as classes outside defaultPolicies (e.g. Invariant, FailureMode)
// are not flagged even when they lack aw:authoredIn — those classes
// have their own drift queries / authoring models.
func TestValidateReferences_IgnoresUntrackedClasses(t *testing.T) {
	inv := rdf.MintIRI(rdf.ClassInvariant, "some_invariant_without_authoredIn")
	nt := inv + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassInvariant) + " ."
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected 0 errors (Invariant class not in defaultPolicies); got %d: %v", len(errs), errs)
	}
}

// TestFilterAllowed_RatchetSplitsNewFromKnown asserts that entries in
// the baseline allowlist are returned as "known" (CI-passing) and
// entries not in the allowlist are returned as "new" (CI-failing).
// The ratchet pattern is the whole point of the baseline mechanism.
func TestFilterAllowed_RatchetSplitsNewFromKnown(t *testing.T) {
	old := ReferenceError{Class: rdf.ClassForbiddenFix, ID: "old_known_undefined"}
	fresh := ReferenceError{Class: rdf.ClassForbiddenFix, ID: "fresh_undefined"}
	allowed := map[string]bool{
		old.Class + "\t" + old.ID: true,
	}
	newOnes, known := FilterAllowed([]ReferenceError{old, fresh}, allowed)
	if len(newOnes) != 1 || newOnes[0].ID != "fresh_undefined" {
		t.Errorf("expected one new error 'fresh_undefined'; got %v", newOnes)
	}
	if len(known) != 1 || known[0].ID != "old_known_undefined" {
		t.Errorf("expected one known error 'old_known_undefined'; got %v", known)
	}
}

// TestLoadAllowedRefs_RoundTrip pins serialize→load equivalence so the
// baseline file format is stable. Comments and blank lines are
// tolerated; malformed entries return an error.
func TestLoadAllowedRefs_RoundTrip(t *testing.T) {
	errs := []ReferenceError{
		{Class: rdf.ClassForbiddenFix, ID: "alpha"},
		{Class: rdf.ClassTest, ID: "tests/foo_test.go::Bar"},
	}
	var buf strings.Builder
	if err := SerializeAllowedRefs(&buf, errs); err != nil {
		t.Fatalf("SerializeAllowedRefs: %v", err)
	}
	allowed, err := LoadAllowedRefs(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("LoadAllowedRefs: %v", err)
	}
	for _, e := range errs {
		if !allowed[e.Class+"\t"+e.ID] {
			t.Errorf("missing round-tripped entry: %s\\t%s", e.Class, e.ID)
		}
	}
}

func TestLoadAllowedRefs_RejectsMalformedLine(t *testing.T) {
	input := "# header\nClass_with_no_tab_separator\n"
	_, err := LoadAllowedRefs(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for malformed line, got nil")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should reference line 2; got %v", err)
	}
}

// TestValidateReferences_HandlesPercentEncodedIDs pins the round-trip:
// MintIRI percent-encodes punctuation in IDs (e.g. file-path test IDs
// like `tests/foo_test.go`). The error's ID field must report the
// original author-readable form, not the percent-encoded IRI segment.
func TestValidateReferences_HandlesPercentEncodedIDs(t *testing.T) {
	rawID := "tests/foo_test.go::TestSomething"
	cited := rdf.MintIRI(rdf.ClassTest, rawID)
	nt := cited + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassTest) + " ."
	errs, err := ValidateReferences(strings.NewReader(nt))
	if err != nil {
		t.Fatalf("ValidateReferences: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 dangling reference; got %d", len(errs))
	}
	if errs[0].ID != rawID {
		t.Errorf("expected decoded ID %q; got %q", rawID, errs[0].ID)
	}
}
