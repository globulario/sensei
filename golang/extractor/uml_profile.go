// SPDX-License-Identifier: Apache-2.0

// Optional UML profile — a lightweight UML vocabulary layer over AWG nodes.
//
// UML is OPTIONAL classification metadata, never authority: a node's AWG class
// (Component, Invariant, …) and AWG relations stay canonical. The profile lets
// architects and agents share a standard architectural language and lets
// dashboards group/filter a focus graph by UML view/kind.
//
// Two authoring paths, both thin:
//   - inline `uml:` block on an authored node (Component/Boundary/Contract/
//     Decision/Evidence/Invariant/ForbiddenFix/FailureMode/Test).
//   - the `uml_profiles:` overlay file, which attaches a profile to ANY node by
//     class-qualified id (used for non-authored nodes like SourceFiles, and by
//     proto-scan). Linking is not authoring: the overlay never types the node.
package extractor

import (
	"fmt"
	"os"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// umlProfile is the inline `uml:` block. All fields optional; an empty profile
// emits nothing.
type umlProfile struct {
	Kind       string `yaml:"kind"`
	Stereotype string `yaml:"stereotype"`
	View       string `yaml:"view"`
	Notes      string `yaml:"notes"`
	Confidence string `yaml:"confidence"`
}

// ValidUMLKinds is the v1 closed set of uml.kind values. Exported so the
// validator (cmd/awg) shares one source of truth.
var ValidUMLKinds = map[string]bool{
	"Component": true, "Package": true, "Interface": true, "Operation": true,
	"Class": true, "DataType": true, "Artifact": true, "Node": true,
	"Deployment": true, "Dependency": true, "Realization": true, "Usage": true,
	"Association": true, "Signal": true, "Event": true, "StateMachine": true,
	"State": true, "Activity": true, "Constraint": true, "UseCase": true,
	"Actor": true,
}

// ValidUMLViews is the v1 closed set of uml.view values. "awareness" is the
// AWG-specific view for the principle/invariant/failure/evidence layer.
var ValidUMLViews = map[string]bool{
	"structural": true, "behavioral": true, "interaction": true,
	"deployment": true, "awareness": true,
}

// emitUML emits the UML profile literals for subj. No-op for empty fields, so a
// node without a uml block emits nothing. Invalid enum values are emitted as-is
// (validation is the validator's job, not the importer's) — but the importer
// never invents UML metadata.
func emitUML(e *rdf.Emitter, subj string, u umlProfile) {
	emitOptLit(e, subj, rdf.PropUmlKind, u.Kind)
	emitOptLit(e, subj, rdf.PropUmlStereotype, u.Stereotype)
	emitOptLit(e, subj, rdf.PropUmlView, u.View)
	emitOptLit(e, subj, rdf.PropUmlNotes, u.Notes)
	emitOptLit(e, subj, rdf.PropUmlConfidence, u.Confidence)
}

// ── uml_profiles overlay importer ──────────────────────────────────────────

type yamlUMLProfileEntry struct {
	Node       string `yaml:"node"` // class-qualified id, e.g. "source_file:proto/awareness_graph.proto"
	umlProfile `yaml:",inline"`
}

type umlProfilesFile struct {
	UMLProfiles []yamlUMLProfileEntry `yaml:"uml_profiles"`
}

// importUMLProfiles attaches UML metadata to existing nodes by class-qualified
// id. It never types the target (the defining file owns the node); it only adds
// the optional uml literals.
func importUMLProfiles(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f umlProfilesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, p := range f.UMLProfiles {
		iri, ok := knowledgeRefToIRI(p.Node)
		if !ok {
			continue // unknown class prefix — skip rather than mint a bogus node
		}
		emitUML(e, iri, p.umlProfile)
	}
	return nil
}
