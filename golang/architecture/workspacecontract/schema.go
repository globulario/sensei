// SPDX-License-Identifier: AGPL-3.0-only

package workspacecontract

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Vendored schema filenames.
const (
	IdentitySchemaFilename  = "workspace-identity-v1.schema.json"
	AdmissionSchemaFilename = "workspace-admission-v1.schema.json"
)

// embeddedSchemas is a vendored, build-time copy of
// docs/schemas/workspace/v1/*.schema.json, embedded directly into any
// binary that imports this package (go:embed cannot reach outside its own
// package directory with a ".." pattern, so the canonical, cross-repo-pinned
// source under docs/schemas/workspace/v1/ is mechanically copied here).
// This is required because sensei_workspace_status and friends run inside
// cmd/awareness-mcp — a binary distributed standalone (release tarball,
// winget, Homebrew) with no docs/ directory anywhere near it at runtime,
// unlike the repo-internal dashboard-projection CLI command this package's
// schema-validation approach was modeled on. TestEmbeddedSchemasMatchCanonicalSource
// (schema_test.go) fails the build if the two ever drift.
//
//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce     sync.Once
	compileErr      error
	identitySchema  *jsonschema.Schema
	admissionSchema *jsonschema.Schema
)

func compileEmbeddedSchemas() error {
	compileOnce.Do(func() {
		identitySchema, compileErr = compileEmbedded(IdentitySchemaFilename)
		if compileErr != nil {
			return
		}
		admissionSchema, compileErr = compileEmbedded(AdmissionSchemaFilename)
	})
	return compileErr
}

func compileEmbedded(filename string) (*jsonschema.Schema, error) {
	data, err := embeddedSchemas.ReadFile("schemas/" + filename)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", filename, err)
	}
	id, err := schemaID(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(id, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("add resource %s: %w", id, err)
	}
	schema, err := c.Compile(id)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", filename, err)
	}
	return schema, nil
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

// ValidateIdentitySchema validates raw workspace-identity JSON bytes against
// the embedded, canonical workspace-identity-v1.schema.json using a real
// Draft 2020-12 validator — required fields, enums, nullability, patterns,
// additionalProperties:false, and every other constraint the schema
// expresses. Requires no filesystem access beyond what is already compiled
// into the binary.
func ValidateIdentitySchema(data []byte) error {
	if err := compileEmbeddedSchemas(); err != nil {
		return err
	}
	instance, err := decodeInstance(data)
	if err != nil {
		return err
	}
	return identitySchema.Validate(instance)
}

// ValidateAdmissionSchema validates raw workspace-admission JSON bytes
// against the embedded, canonical workspace-admission-v1.schema.json using a
// real Draft 2020-12 validator.
func ValidateAdmissionSchema(data []byte) error {
	if err := compileEmbeddedSchemas(); err != nil {
		return err
	}
	instance, err := decodeInstance(data)
	if err != nil {
		return err
	}
	return admissionSchema.Validate(instance)
}
