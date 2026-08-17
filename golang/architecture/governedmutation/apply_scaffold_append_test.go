// SPDX-License-Identifier: AGPL-3.0-only

package governedmutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

const decisionItem = "  - id: decision.first\n" +
	"    title: The first recorded decision\n" +
	"    status: accepted\n"

func appendTarget(t *testing.T, initial string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "decisions.yaml")
	if initial != "" {
		if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func mustParseList(t *testing.T, path, topKey string) []interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("result does not parse: %v\n---\n%s", err, data)
	}
	list, ok := doc[topKey].([]interface{})
	if !ok {
		t.Fatalf("%q is %T, want a list\n---\n%s", topKey, doc[topKey], data)
	}
	return list
}

// The defect: `sensei init` scaffolds `decisions: []`, and the first append
// produced block items underneath that inline marker — invalid YAML that no
// later read caught, so every decision recorded afterwards was silently absent
// from the graph.
func TestFirstAppendAgainstScaffoldedEmptyMarkerStaysValid(t *testing.T) {
	path := appendTarget(t, "decisions: []\n")

	if err := atomicAppend(path, "decisions", decisionItem); err != nil {
		t.Fatalf("append: %v", err)
	}

	list := mustParseList(t, path, "decisions")
	if len(list) != 1 {
		t.Fatalf("decisions = %d entries, want 1", len(list))
	}
	entry, _ := list[0].(map[string]interface{})
	if id, _ := entry["id"].(string); id != "decision.first" {
		t.Fatalf("id = %q, want decision.first", id)
	}
	// The inline marker must be gone, not merely followed by items.
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "decisions: []") {
		t.Fatalf("the inline empty marker survived:\n%s", data)
	}
}

// A second append must also work — proving the conversion left a real block
// list rather than something that only happens to parse once.
func TestSecondAppendAfterMarkerConversionStillParses(t *testing.T) {
	path := appendTarget(t, "decisions: []\n")
	if err := atomicAppend(path, "decisions", decisionItem); err != nil {
		t.Fatalf("first append: %v", err)
	}
	second := "  - id: decision.second\n    title: Another\n    status: accepted\n"
	if err := atomicAppend(path, "decisions", second); err != nil {
		t.Fatalf("second append: %v", err)
	}
	if list := mustParseList(t, path, "decisions"); len(list) != 2 {
		t.Fatalf("decisions = %d entries, want 2", len(list))
	}
}

// A trailing comment on the marker line is preserved rather than destroyed.
func TestMarkerConversionPreservesTrailingComment(t *testing.T) {
	path := appendTarget(t, "decisions: []  # scaffolded by sensei init\n")
	if err := atomicAppend(path, "decisions", decisionItem); err != nil {
		t.Fatalf("append: %v", err)
	}
	mustParseList(t, path, "decisions")
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# scaffolded by sensei init") {
		t.Fatalf("the trailing comment was dropped:\n%s", data)
	}
}

// Existing block lists and their comments keep working exactly as before.
func TestAppendToExistingBlockListIsUnchanged(t *testing.T) {
	initial := "# Architecture decisions.\ndecisions:\n  - id: decision.zero\n    title: Existing\n    status: accepted\n"
	path := appendTarget(t, initial)
	if err := atomicAppend(path, "decisions", decisionItem); err != nil {
		t.Fatalf("append: %v", err)
	}
	list := mustParseList(t, path, "decisions")
	if len(list) != 2 {
		t.Fatalf("decisions = %d entries, want 2", len(list))
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# Architecture decisions.") {
		t.Fatalf("leading comment lost:\n%s", data)
	}
}

// A missing file and a file with no such key both still bootstrap a block list.
func TestAppendBootstrapsMissingFileAndMissingKey(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nested", "decisions.yaml")
	if err := atomicAppend(missing, "decisions", decisionItem); err != nil {
		t.Fatalf("append to missing file: %v", err)
	}
	if list := mustParseList(t, missing, "decisions"); len(list) != 1 {
		t.Fatalf("bootstrapped %d entries, want 1", len(list))
	}

	other := appendTarget(t, "# only a comment\n")
	if err := atomicAppend(other, "decisions", decisionItem); err != nil {
		t.Fatalf("append with absent key: %v", err)
	}
	if list := mustParseList(t, other, "decisions"); len(list) != 1 {
		t.Fatalf("bootstrapped %d entries, want 1", len(list))
	}
}

// A non-empty inline sequence cannot be appended to textually. Refuse rather
// than corrupt, and leave the file exactly as it was.
func TestNonEmptyFlowSequenceIsRefusedAndFileUntouched(t *testing.T) {
	initial := "decisions: [decision.a, decision.b]\n"
	path := appendTarget(t, initial)

	err := atomicAppend(path, "decisions", decisionItem)
	if err == nil {
		t.Fatal("expected a refusal for an inline non-empty sequence")
	}
	if !strings.Contains(err.Error(), "inline (flow) sequence") {
		t.Fatalf("refusal does not explain itself: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != initial {
		t.Fatalf("the refused append modified the file:\n%s", data)
	}
}

// The general guard: a composed document that would not parse is never written,
// whatever the cause. This is what makes the class closed rather than the one
// known shape patched — the original bug survived because the write was never
// verified, not because the transform was hard to spot.
func TestUnparseableResultIsRefusedBeforeTheRename(t *testing.T) {
	initial := "decisions:\n  - id: decision.zero\n    title: Existing\n"
	path := appendTarget(t, initial)

	// An item that cannot sit in this list: a bad indentation level.
	if err := atomicAppend(path, "decisions", "- id: decision.bad\n   title: [unclosed\n"); err == nil {
		t.Fatal("expected a refusal for a result that does not parse")
	}
	data, _ := os.ReadFile(path)
	if string(data) != initial {
		t.Fatalf("a refused append replaced good content:\n%s", data)
	}
}

// Guard against the fix passing for the wrong reason: confirm the corrupt shape
// really is invalid YAML, so the tests above are asserting something real.
func TestTheCorruptShapeIsGenuinelyInvalidYAML(t *testing.T) {
	corrupt := "decisions: []\n" + decisionItem
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(corrupt), &doc); err == nil {
		t.Fatal("block items under an inline empty sequence parsed cleanly; " +
			"the scaffolded-marker tests are no longer proving anything")
	}
}
