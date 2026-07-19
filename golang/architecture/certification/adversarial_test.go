// SPDX-License-Identifier: Apache-2.0

package certification

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// collectJSONTags walks a type graph and gathers every json tag name.
func collectJSONTags(t reflect.Type, seen map[reflect.Type]bool, out map[string]bool) {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			out[tag] = true
		}
		collectJSONTags(field.Type, seen, out)
	}
}

// TestAntiForgery_NoAssertableOutcomeFields proves by reflection that the
// typed request surface simply has no field a caller could set to assert an
// outcome. This is the structural half of "never trust caller booleans" — the
// behavioral half is every lane test in this package.
func TestAntiForgery_NoAssertableOutcomeFields(t *testing.T) {
	forbidden := []string{
		"scope_valid",
		"evidence_sufficient",
		"required_paths_satisfied",
		"promotion_allowed",
		"certification_status",
		"score",
		"contract_clean",
		"blocked_by_forbidden_move_ids",
		"human_review_required",
		"correctness_certified",
	}
	tags := map[string]bool{}
	seen := map[reflect.Type]bool{}
	for _, root := range []any{Request{}, Records{}, ScopeVerification{}, ForbiddenMoveFinding{}, Result{}, closureprotocol.CertificationReceipt{}} {
		collectJSONTags(reflect.TypeOf(root), seen, tags)
	}
	for _, name := range forbidden {
		if tags[name] {
			t.Fatalf("forbidden assertable field %q exists on the typed certification surface", name)
		}
	}
}

// TestAntiForgery_RequestCarriesNoBooleans is stronger: the request itself is
// digests, identifiers, and one time — not a single boolean anywhere.
func TestAntiForgery_RequestCarriesNoBooleans(t *testing.T) {
	requestType := reflect.TypeOf(Request{})
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		kind := field.Type.Kind()
		if kind == reflect.Bool {
			t.Fatalf("Request.%s is a bool — no caller-assertable booleans allowed", field.Name)
		}
	}
	verificationType := reflect.TypeOf(ScopeVerification{})
	for i := 0; i < verificationType.NumField(); i++ {
		if verificationType.Field(i).Type.Kind() == reflect.Bool {
			t.Fatalf("ScopeVerification.%s is a bool", verificationType.Field(i).Name)
		}
	}
}

// TestAntiForgery_CleanLookingBundleCannotBuyMissingProof: everything except
// the proof discharge is immaculate; there is no score, note, or flag that can
// compensate.
func TestAntiForgery_CleanLookingBundleCannotBuyMissingProof(t *testing.T) {
	req, rec := greenBundle(t)
	rec.ProofDischarges = nil
	req = rebindGreen(t, rec)
	result := mustEvaluate(t, req, rec, DefaultPolicy())
	if result.Receipt.CertificationVerdict != closureprotocol.CertificationBlocked {
		t.Fatalf("verdict = %s, want blocked", result.Receipt.CertificationVerdict)
	}
	for _, lane := range []Lane{LaneScope, LaneAuthority, LaneEvidence} {
		if laneByName(t, result, lane).Status != closureprotocol.DimensionPass {
			t.Fatalf("test setup: %s lane should be green", lane)
		}
	}
}

func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	// <root>/golang/architecture/certification/adversarial_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
}

// TestOnlyCertificationEngineEstablishesCorrectness scans every Go source file
// in the repository: no code may assign CorrectnessCertified a true value. The
// legacy admission engine's hardcoded `false` writers stay; the only
// establishment of architectural correctness is a CertificationReceipt with a
// certifying verdict emitted by this package.
func TestOnlyCertificationEngineEstablishesCorrectness(t *testing.T) {
	root := repoRootFromTestFile(t)
	assignTrue := regexp.MustCompile(`CorrectnessCertified\s*[:=]=?\s*true`)
	sawConcept := false
	for _, dir := range []string{"golang", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(data), "CorrectnessCertified") {
				sawConcept = true
				if assignTrue.Match(data) {
					t.Errorf("%s assigns CorrectnessCertified = true — only the certification engine may establish correctness", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !sawConcept {
		t.Fatal("scan found no CorrectnessCertified at all — the guard test is miswired")
	}
}

// TestCertificationPackageNeverTouchesCompletion: this package's sources may
// not reference the completed ledger event — completion is Phase 8's
// transaction, structurally out of reach here.
func TestCertificationPackageNeverTouchesCompletion(t *testing.T) {
	root := repoRootFromTestFile(t)
	packageDir := filepath.Join(root, "golang", "architecture", "certification")
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "LedgerEventCompleted") {
			t.Errorf("%s references LedgerEventCompleted", name)
		}
		if strings.Contains(string(data), "TerminalCompleted") {
			t.Errorf("%s references TerminalCompleted", name)
		}
	}
}
