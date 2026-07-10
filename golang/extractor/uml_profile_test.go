// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

// TestUML_InlineAndOverlay covers both authoring paths: an inline `uml:` block
// on a Component, and the `uml_profiles:` overlay attaching a profile to a
// SourceFile node (which is never authored inline).
func TestUML_InlineAndOverlay(t *testing.T) {
	root := makeDir(t, map[string]string{
		"components.yaml": `
components:
  - id: component.uml_demo
    name: Demo
    kind: service
    source_files:
      - golang/server/resolve.go
    uml:
      kind: Component
      stereotype: service
      view: structural
`,
		"uml_profiles.yaml": `
uml_profiles:
  - node: source_file:proto/awareness_graph.proto
    kind: Artifact
    stereotype: proto
    view: structural
    confidence: declared
`,
	})
	out, _ := importDirToString(t, root)
	assertValidNT(t, out)

	compIRI := strings.Trim(rdf.MintIRI(rdf.ClassComponent, "component.uml_demo"), "<>")
	sfIRI := strings.Trim(rdf.MintIRI(rdf.ClassSourceFile, "proto/awareness_graph.proto"), "<>")

	// Inline uml on the component.
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropUmlKind+"> \"Component\"", "component umlKind")
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropUmlStereotype+"> \"service\"", "component umlStereotype")
	wantTriple(t, out, "<"+compIRI+"> <"+rdf.PropUmlView+"> \"structural\"", "component umlView")

	// Overlay uml attached to the SourceFile node (linking, not authoring).
	wantTriple(t, out, "<"+sfIRI+"> <"+rdf.PropUmlKind+"> \"Artifact\"", "source_file umlKind")
	wantTriple(t, out, "<"+sfIRI+"> <"+rdf.PropUmlView+"> \"structural\"", "source_file umlView")
	wantTriple(t, out, "<"+sfIRI+"> <"+rdf.PropUmlConfidence+"> \"declared\"", "source_file umlConfidence")
}

func TestUML_ValidSets(t *testing.T) {
	if !extractor.ValidUMLKinds["Operation"] || !extractor.ValidUMLKinds["Constraint"] {
		t.Error("expected Operation/Constraint in ValidUMLKinds")
	}
	if extractor.ValidUMLKinds["Bogus"] {
		t.Error("Bogus must not be a valid UML kind")
	}
	if !extractor.ValidUMLViews["awareness"] || !extractor.ValidUMLViews["interaction"] {
		t.Error("expected awareness/interaction in ValidUMLViews")
	}
	if extractor.ValidUMLViews["nonsense"] {
		t.Error("nonsense must not be a valid UML view")
	}
}
