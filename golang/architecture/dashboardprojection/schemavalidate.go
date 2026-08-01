// SPDX-License-Identifier: AGPL-3.0-only

package dashboardprojection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Vendored schema filenames. The agent-handoff schema's cross-schema $ref
// ("dashboard-projection-v1.schema.json#/...") is a literal relative
// filename, not a URL matching either schema's declared $id — so schemas are
// registered with the compiler under these exact literal names rather than
// their $id, which is how the $ref actually resolves in this repository and
// in the sensei-dashboard consumer.
const (
	ProjectionSchemaFilename = "dashboard-projection-v1.schema.json"
	HandoffSchemaFilename    = "agent-handoff-v1.schema.json"
)

// crossRefAlias is the URL the agent-handoff schema's relative $ref
// ("dashboard-projection-v1.schema.json#/...") actually resolves to once
// standard $id-relative resolution runs against the handoff schema's own
// declared $id (".../schema/agent-handoff-v1.json"): the sibling path
// ".../schema/dashboard-projection-v1.schema.json". That does not match the
// projection schema's own declared $id (".../schema/dashboard-projection-v1
// .json", no ".schema"), which is the same filename/$id mismatch already
// documented on the projection schema's top-level $comment. Registering the
// projection schema under this extra alias, in addition to its own $id, is
// what makes the cross-schema $ref resolve without editing either vendored
// schema file.
const crossRefAlias = "https://globulario.github.io/sensei-dashboard/schema/dashboard-projection-v1.schema.json"

// compileSchemas loads and compiles both vendored, canonical JSON Schemas
// from schemaDir (docs/schemas/dashboard-projection/v1 in this repository)
// using a real Draft 2020-12 validator, not this package's own hand-written
// structural checks. Both schemas are added — the projection schema under
// both its own $id and crossRefAlias — before either is compiled, so the
// agent-handoff schema's cross-schema $ref into the projection schema
// resolves.
func compileSchemas(schemaDir string) (proj, handoff *jsonschema.Schema, err error) {
	c := jsonschema.NewCompiler()

	projData, err := os.ReadFile(filepath.Join(schemaDir, ProjectionSchemaFilename))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", ProjectionSchemaFilename, err)
	}
	projID, err := schemaID(projData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", ProjectionSchemaFilename, err)
	}
	if err := c.AddResource(projID, bytes.NewReader(projData)); err != nil {
		return nil, nil, fmt.Errorf("add resource %s: %w", projID, err)
	}
	if err := c.AddResource(crossRefAlias, bytes.NewReader(projData)); err != nil {
		return nil, nil, fmt.Errorf("add resource %s: %w", crossRefAlias, err)
	}

	handoffData, err := os.ReadFile(filepath.Join(schemaDir, HandoffSchemaFilename))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", HandoffSchemaFilename, err)
	}
	handoffID, err := schemaID(handoffData)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", HandoffSchemaFilename, err)
	}
	if err := c.AddResource(handoffID, bytes.NewReader(handoffData)); err != nil {
		return nil, nil, fmt.Errorf("add resource %s: %w", handoffID, err)
	}

	proj, err = c.Compile(projID)
	if err != nil {
		return nil, nil, fmt.Errorf("compile %s: %w", ProjectionSchemaFilename, err)
	}
	handoff, err = c.Compile(handoffID)
	if err != nil {
		return nil, nil, fmt.Errorf("compile %s: %w", HandoffSchemaFilename, err)
	}
	return proj, handoff, nil
}

func schemaID(data []byte) (string, error) {
	var doc struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("decode $id: %w", err)
	}
	if doc.ID == "" {
		return "", fmt.Errorf("schema has no $id")
	}
	return doc.ID, nil
}

func decodeInstance(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return v, nil
}

// ValidateProjectionSchema validates raw projection JSON bytes against the
// canonical, vendored dashboard-projection-v1.schema.json using a real
// Draft 2020-12 validator — required fields, enums, formats, patterns,
// additionalProperties:false, and every other constraint the schema
// expresses, not just this package's hand-written cross-record Validate().
func ValidateProjectionSchema(schemaDir string, data []byte) error {
	proj, _, err := compileSchemas(schemaDir)
	if err != nil {
		return err
	}
	instance, err := decodeInstance(data)
	if err != nil {
		return err
	}
	return proj.Validate(instance)
}

// ValidateHandoffSchema validates raw agent-handoff JSON bytes against the
// canonical, vendored agent-handoff-v1.schema.json, resolving its
// cross-schema $ref into the projection schema.
func ValidateHandoffSchema(schemaDir string, data []byte) error {
	_, handoff, err := compileSchemas(schemaDir)
	if err != nil {
		return err
	}
	instance, err := decodeInstance(data)
	if err != nil {
		return err
	}
	return handoff.Validate(instance)
}
