// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

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
	CandidateArtifactSchemaFilename = "runnercomposition-candidateartifact-v1.schema.json"
	RunnerReceiptSchemaFilename     = "runnercomposition-runnerreceipt-v1.schema.json"
)

// embeddedSchemas is a vendored, build-time copy of
// docs/schemas/runnercomposition/v1/*.schema.json, embedded directly into
// any binary that imports this package (go:embed cannot reach outside its
// own package directory with a ".." pattern, so the canonical,
// cross-repo-pinned source under docs/schemas/runnercomposition/v1/ is
// mechanically copied here). TestEmbeddedSchemasMatchCanonicalSource
// (schema_test.go) fails the build if the two ever drift.
//
//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce sync.Once
	compileErr  error

	candidateArtifactSchema *jsonschema.Schema
	runnerReceiptSchema     *jsonschema.Schema
)

func compileEmbeddedSchemas() error {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()

		type target struct {
			filename string
			dst      **jsonschema.Schema
		}
		targets := []target{
			{CandidateArtifactSchemaFilename, &candidateArtifactSchema},
			{RunnerReceiptSchemaFilename, &runnerReceiptSchema},
		}
		for _, t := range targets {
			schema, err := compileEmbedded(c, t.filename)
			if err != nil {
				compileErr = err
				return
			}
			*t.dst = schema
		}
	})
	return compileErr
}

func compileEmbedded(c *jsonschema.Compiler, filename string) (*jsonschema.Schema, error) {
	data, err := embeddedSchemas.ReadFile("schemas/" + filename)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", filename, err)
	}
	id, err := schemaID(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
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

func validateAgainst(schema **jsonschema.Schema, data []byte) error {
	if err := compileEmbeddedSchemas(); err != nil {
		return err
	}
	instance, err := decodeInstance(data)
	if err != nil {
		return err
	}
	return (*schema).Validate(instance)
}

// ValidateCandidateArtifactSchema validates raw candidate-artifact JSON
// bytes against the embedded, canonical
// runnercomposition-candidateartifact-v1.schema.json using a real Draft
// 2020-12 validator.
func ValidateCandidateArtifactSchema(data []byte) error {
	return validateAgainst(&candidateArtifactSchema, data)
}

// ValidateRunnerReceiptSchema validates raw runner-receipt JSON bytes
// against the embedded, canonical runnercomposition-runnerreceipt-v1.schema.json.
func ValidateRunnerReceiptSchema(data []byte) error {
	return validateAgainst(&runnerReceiptSchema, data)
}
