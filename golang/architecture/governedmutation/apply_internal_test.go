// SPDX-License-Identifier: AGPL-3.0-only

package governedmutation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAtomicAppendRejectsResultingInvalidYAML is the regression ratchet for
// the write-time validation gap: atomicAppend used to write whatever text it
// concatenated with no check that the RESULT was still valid YAML. A prior
// live incident had `sensei propose` report status:created on exactly such a
// write — the file it "successfully" wrote no longer parsed, breaking every
// other entry in it. atomicAppend must now refuse to write (and return an
// error) rather than produce a corrupt file, regardless of what caused the
// corruption.
func TestAtomicAppendRejectsResultingInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "failure_modes.yaml")

	// A deliberately malformed pre-existing file: an unclosed flow mapping.
	// Appending anything after this can never produce valid YAML.
	malformed := "failure_modes:\n  - id: broken\n    title: [unterminated\n"
	if err := os.WriteFile(path, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	itemText, err := renderItem(map[string]any{"id": "failure.new", "title": "x"}, 2)
	if err != nil {
		t.Fatal(err)
	}

	err = atomicAppend(path, "failure_modes", itemText)
	if err == nil {
		t.Fatal("expected atomicAppend to reject a result that doesn't parse as YAML, got nil error")
	}
	if !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("error = %v, want it to explain the YAML validation failure", err)
	}

	// The refusal must leave the original (already-broken, but unchanged)
	// file exactly as it was — no partial/temp write left behind.
	after, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(after) != malformed {
		t.Fatalf("file was modified despite the rejected append:\nbefore=%q\nafter=%q", malformed, after)
	}
}

// TestDetectListIndentMeasuresExistingEntries locks the indent-detection
// contract directly: 4-space files detect as 4, 2-space files detect as 2,
// and anything with no existing list (new file, empty topKey) falls back to
// defaultListIndent.
func TestDetectListIndentMeasuresExistingEntries(t *testing.T) {
	cases := []struct {
		name string
		file string
		want int
	}{
		{
			name: "four_space",
			file: "failure_modes:\n    - id: a\n      title: x\n",
			want: 4,
		},
		{
			name: "two_space",
			file: "failure_modes:\n  - id: a\n    title: x\n",
			want: 2,
		},
		{
			name: "no_items_yet",
			file: "failure_modes:\n",
			want: defaultListIndent,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "failure_modes.yaml")
			if err := os.WriteFile(path, []byte(c.file), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := detectListIndent(path, "failure_modes"); got != c.want {
				t.Errorf("detectListIndent(%q) = %d, want %d", c.name, got, c.want)
			}
		})
	}

	t.Run("missing_file", func(t *testing.T) {
		if got := detectListIndent(filepath.Join(t.TempDir(), "nope.yaml"), "failure_modes"); got != defaultListIndent {
			t.Errorf("missing file: detectListIndent = %d, want default %d", got, defaultListIndent)
		}
	})
}
