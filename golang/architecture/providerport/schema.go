// SPDX-License-Identifier: AGPL-3.0-only

package providerport

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
	CapabilitiesSchemaFilename     = "providerport-capabilities-v1.schema.json"
	RequestSchemaFilename          = "providerport-request-v1.schema.json"
	ResultSchemaFilename           = "providerport-result-v1.schema.json"
	ObservationBatchSchemaFilename = "providerport-observationbatch-v1.schema.json"
	ReceiptSchemaFilename          = "providerport-receipt-v1.schema.json"
)

// embeddedSchemas is a vendored, build-time copy of
// docs/schemas/providerport/v1/*.schema.json, embedded directly into any
// binary that imports this package (go:embed cannot reach outside its own
// package directory with a ".." pattern, so the canonical, cross-repo-pinned
// source under docs/schemas/providerport/v1/ is mechanically copied here).
// TestEmbeddedSchemasMatchCanonicalSource (schema_test.go) fails the build
// if the two ever drift.
//
//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce sync.Once
	compileErr  error

	capabilitiesSchema     *jsonschema.Schema
	requestSchema          *jsonschema.Schema
	resultSchema           *jsonschema.Schema
	observationBatchSchema *jsonschema.Schema
	receiptSchema          *jsonschema.Schema
)

func compileEmbeddedSchemas() error {
	compileOnce.Do(func() {
		type target struct {
			filename string
			dst      **jsonschema.Schema
		}
		targets := []target{
			{CapabilitiesSchemaFilename, &capabilitiesSchema},
			{RequestSchemaFilename, &requestSchema},
			{ResultSchemaFilename, &resultSchema},
			{ObservationBatchSchemaFilename, &observationBatchSchema},
			{ReceiptSchemaFilename, &receiptSchema},
		}
		for _, t := range targets {
			schema, err := compileEmbedded(t.filename)
			if err != nil {
				compileErr = err
				return
			}
			*t.dst = schema
		}
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

// ValidateCapabilitiesSchema validates raw capabilities JSON bytes against
// the embedded, canonical providerport-capabilities-v1.schema.json using a
// real Draft 2020-12 validator.
func ValidateCapabilitiesSchema(data []byte) error {
	return validateAgainst(&capabilitiesSchema, data)
}

// ValidateRequestSchema validates raw request JSON bytes against the
// embedded, canonical providerport-request-v1.schema.json.
func ValidateRequestSchema(data []byte) error { return validateAgainst(&requestSchema, data) }

// ValidateResultSchema validates raw result JSON bytes against the
// embedded, canonical providerport-result-v1.schema.json.
func ValidateResultSchema(data []byte) error { return validateAgainst(&resultSchema, data) }

// ValidateObservationBatchSchema validates raw observation-batch JSON bytes
// against the embedded, canonical
// providerport-observationbatch-v1.schema.json.
func ValidateObservationBatchSchema(data []byte) error {
	return validateAgainst(&observationBatchSchema, data)
}

// ValidateReceiptSchema validates raw receipt JSON bytes against the
// embedded, canonical providerport-receipt-v1.schema.json.
func ValidateReceiptSchema(data []byte) error { return validateAgainst(&receiptSchema, data) }
