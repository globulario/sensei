// SPDX-License-Identifier: AGPL-3.0-only

package governedmutation

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The defect (#186): renderItem always emits 2-space-indented items, and both
// of YAML's block-sequence styles are legal:
//
//	invariants:            invariants:
//	  - id: a              - id: a
//	    title: ...           title: ...
//
// A corpus written in the second style got a 2-space item appended after a
// 2-space mapping key, which YAML reads as a key inside the previous entry and
// rejects — "did not find expected key". Every proposal against such a corpus
// was refused, and with it the only sanctioned route from a discovered
// architectural law into governed knowledge.
//
// The refusal itself was correct: verifyAppendResult (from the sibling defect
// #173) caught the malformed compose and declined to write. What was wrong was
// producing the malformed compose at all.

// buildCorpus renders a realistic multi-entry corpus in the requested item
// indentation, as the issue's suggested acceptance asks for: the defect never
// reproduced on a small file because a small file is no likelier to use one
// style than the other — it reproduced on a real corpus that happened to use
// the unindented one.
func buildCorpus(topKey, indent string, entries int) string {
	var b strings.Builder
	b.WriteString(topKey + ":\n")
	for i := 0; i < entries; i++ {
		b.WriteString(fmt.Sprintf("%s- id: entry.%03d\n", indent, i))
		b.WriteString(fmt.Sprintf("%s  title: Entry %d\n", indent, i))
		b.WriteString(fmt.Sprintf("%s  status: active\n", indent))
		b.WriteString(fmt.Sprintf("%s  protects:\n", indent))
		b.WriteString(fmt.Sprintf("%s    files:\n", indent))
		b.WriteString(fmt.Sprintf("%s      - golang/some/file_%03d.go\n", indent, i))
	}
	return b.String()
}

// A round-trip per style: propose one entry into a realistic corpus and assert
// the result parses and holds exactly one more entry than before.
func TestAppendMatchesEitherBlockSequenceStyle(t *testing.T) {
	for name, indent := range map[string]string{
		"unindented items (column 0)": "",
		"indented items (2 spaces)":   "  ",
		"deeply indented items":       "    ",
	} {
		t.Run(name, func(t *testing.T) {
			const entries = 250
			path := appendTarget(t, buildCorpus("decisions", indent, entries))

			item := reindentItem(decisionItem, appendItemIndent(path, "decisions"))
			if err := atomicAppend(path, "decisions", item); err != nil {
				t.Fatalf("append into a %s corpus: %v", name, err)
			}

			list := mustParseList(t, path, "decisions")
			if len(list) != entries+1 {
				t.Fatalf("corpus holds %d entries, want %d", len(list), entries+1)
			}
			last, _ := list[len(list)-1].(map[string]interface{})
			if id, _ := last["id"].(string); id != "decision.first" {
				t.Fatalf("last id = %q, want decision.first", id)
			}
			// The first entry must survive untouched: an append that reindents
			// the whole file would also "parse", and would rewrite history.
			first, _ := list[0].(map[string]interface{})
			if id, _ := first["id"].(string); id != "entry.000" {
				t.Fatalf("first id = %q, want entry.000 — existing entries were disturbed", id)
			}
			data, _ := os.ReadFile(path)
			if !strings.Contains(string(data), indent+"- id: decision.first\n") {
				t.Fatalf("the appended item does not use the corpus's own indentation %q:\n%s",
					indent, tailOf(string(data), 6))
			}
		})
	}
}

// Without matching the corpus, the compose is refused — which is what users
// saw. Pinned so a regression reproduces the original symptom rather than
// silently writing something that merely happens to parse.
func TestUnmatchedIndentationIsRefusedRatherThanWritten(t *testing.T) {
	path := appendTarget(t, buildCorpus("decisions", "", 12))
	before, _ := os.ReadFile(path)

	// decisionItem is renderItem's 2-space form, appended without reindenting.
	err := atomicAppend(path, "decisions", decisionItem)
	if err == nil {
		t.Fatal("a 2-space item appended to a column-0 corpus was accepted")
	}
	if !strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("refusal does not name the cause: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("a refused append modified the file")
	}
}

func TestAppendItemIndentReadsTheFirstItem(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"column zero":            {"decisions:\n- id: a\n  title: t\n", ""},
		"two spaces":             {"decisions:\n  - id: a\n    title: t\n", "  "},
		"four spaces":            {"decisions:\n    - id: a\n", "    "},
		"comments and blanks":    {"decisions:\n\n  # note\n\n- id: a\n", ""},
		"empty block marker":     {"decisions:\n", "  "},
		"absent key":             {"other:\n- id: a\n", "  "},
		"another key intervenes": {"decisions:\nother:\n- id: a\n", "  "},
		// A bare indicator is a legal item: `-` alone, content below. It was
		// skipped, and the scan then matched the first NESTED sequence.
		"bare indicator, column zero": {"decisions:\n-\n  id: a\n  protects:\n    files:\n      - x.go\n", ""},
		"bare indicator, indented":    {"decisions:\n  -\n    id: a\n    protects:\n      files:\n        - x.go\n", "  "},
		// The key holds a mapping, not a sequence: there is no outer item
		// whose style could be matched, and a nested one must NOT be borrowed.
		"mapping value with a nested sequence": {"decisions:\n  protects:\n    files:\n      - x.go\n", "  "},
	} {
		t.Run(name, func(t *testing.T) {
			path := appendTarget(t, tc.body)
			if got := appendItemIndent(path, "decisions"); got != tc.want {
				t.Fatalf("indent = %q, want %q", got, tc.want)
			}
		})
	}
}

// A missing file has no style to match, so the rendered default stands — the
// scaffolding path must keep working.
func TestAppendItemIndentDefaultsWhenFileIsAbsent(t *testing.T) {
	path := appendTarget(t, "")
	if got := appendItemIndent(path, "decisions"); got != "  " {
		t.Fatalf("indent = %q, want the rendered default", got)
	}
}

// Reindenting must preserve the relative nesting renderItem produced, or a
// nested mapping would silently change which key it belongs to.
func TestReindentItemPreservesRelativeNesting(t *testing.T) {
	got := reindentItem(decisionItem, "")
	want := "- id: decision.first\n" +
		"  title: The first recorded decision\n" +
		"  status: accepted\n"
	if got != want {
		t.Fatalf("reindent to column 0:\n%q\nwant:\n%q", got, want)
	}
	if reindentItem(decisionItem, "  ") != decisionItem {
		t.Fatal("reindenting to the rendered indentation changed the text")
	}
	deep := reindentItem(decisionItem, "    ")
	for _, line := range strings.Split(strings.TrimRight(deep, "\n"), "\n") {
		if !strings.HasPrefix(line, "    ") {
			t.Fatalf("line lost its indentation: %q", line)
		}
	}
}

func tailOf(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

// The record must land in the GOVERNED sequence, never in a nested one.
//
// With a bare `-` outer indicator the detector previously skipped the real
// item and matched `protects.files` six columns in. The record was reindented
// into that nested list, and because verifyAppendResult only checks that the
// top-level key is still a list, the append could report success while the
// record was not an entry of the governed sequence at all — the worst
// available outcome, since a caller would have no reason to look again.
func TestAppendNeverLandsInANestedSequence(t *testing.T) {
	corpus := "decisions:\n" +
		"-\n" +
		"  id: entry.000\n" +
		"  title: Entry 0\n" +
		"  protects:\n" +
		"    files:\n" +
		"      - golang/some/file.go\n"
	path := appendTarget(t, corpus)

	item := reindentItem(decisionItem, appendItemIndent(path, "decisions"))
	if err := atomicAppend(path, "decisions", item); err != nil {
		t.Fatalf("append: %v", err)
	}

	list := mustParseList(t, path, "decisions")
	if len(list) != 2 {
		t.Fatalf("decisions holds %d entries, want 2 — the record did not join the governed sequence", len(list))
	}
	last, _ := list[1].(map[string]interface{})
	if id, _ := last["id"].(string); id != "decision.first" {
		t.Fatalf("last entry id = %q, want decision.first", id)
	}
	// And the original entry's nested list must be untouched.
	first, _ := list[0].(map[string]interface{})
	protects, _ := first["protects"].(map[string]interface{})
	files, _ := protects["files"].([]interface{})
	if len(files) != 1 {
		t.Fatalf("the nested files list holds %d entries, want 1 — the record was appended into it", len(files))
	}
}
