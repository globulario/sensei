// SPDX-License-Identifier: AGPL-3.0-only

package gosemantics

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A field is not necessarily declared where its struct is. An embedded field
// lives in another file, and anchoring it to the struct's file while taking the
// line from the field produces an anchor that can cite a line past the end of
// the file it names.
//
// Found by measurement rather than by reading: the #131 evidence-resolvability
// pass reported 74 observations from this extractor citing lines beyond EOF,
// including golang/architecture/authority/model.go — a 118-line file — cited at
// lines 126 to 140.
func TestStructFieldObservationsAnchorToTheirOwnFile(t *testing.T) {
	root := t.TempDir()
	// A type ALIAS is the reproducer. The alias declaration sits at the top of a
	// short file, while the aliased struct's fields are declared in another,
	// longer file — so taking the file from the alias and the line from the
	// field cites a line that does not exist.
	var aliased strings.Builder
	aliased.WriteString("package other\n")
	for i := 0; i < 60; i++ {
		aliased.WriteString("\n// padding to push the struct well past the alias file's length\n")
	}
	aliased.WriteString("\ntype Thing struct {\n\tThingField string `json:\"thing_field\"`\n}\n")
	files := map[string]string{
		"go.mod": "module example.com/fixture\n\ngo 1.25.0\n",
		"holder.go": `package fixture

import "example.com/fixture/other"

type Alias = other.Thing

type Holder struct {
	Own string ` + "`json:\"own\"`" + `
}
`,
		"other/other.go": aliased.String(),
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Extract(root)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	lineCounts := map[string]int{}
	for name := range files {
		f, err := os.Open(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			lineCounts[name]++
		}
		f.Close()
	}

	seenBaseField := false
	for _, o := range result.Observations {
		if o.File == "" || o.Line == 0 {
			continue
		}
		if n, ok := lineCounts[o.File]; ok && o.Line > n {
			t.Fatalf("observation cites line %d of %s, which has %d lines: %+v", o.Line, o.File, n, o)
		}
		if strings.HasSuffix(o.Subject, ".ThingField") {
			seenBaseField = true
			if o.File != "other/other.go" {
				t.Fatalf("ThingField is declared in other/other.go but anchored to %s: %+v", o.File, o)
			}
		}
	}
	if !seenBaseField {
		t.Fatal("no observation for the aliased struct's field: the fixture no longer reproduces the case it was written for")
	}
}
