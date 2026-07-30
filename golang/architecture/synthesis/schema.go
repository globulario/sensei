// SPDX-License-Identifier: AGPL-3.0-only

package synthesis

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
	SessionSchemaFilename        = "synthesis-session-v1.schema.json"
	InterpretationSchemaFilename = "synthesis-interpretation-v1.schema.json"
	PlanSchemaFilename           = "synthesis-plan-v1.schema.json"
	AttemptSchemaFilename        = "synthesis-attempt-v1.schema.json"
	EvaluationSchemaFilename     = "synthesis-evaluation-v1.schema.json"
	ReceiptSchemaFilename        = "synthesis-receipt-v1.schema.json"
)

// embeddedSchemas is a vendored, build-time copy of
// docs/schemas/synthesis/v1/*.schema.json, embedded directly into any binary
// that imports this package (go:embed cannot reach outside its own package
// directory with a ".." pattern, so the canonical, cross-repo-pinned source
// under docs/schemas/synthesis/v1/ is mechanically copied here).
// TestEmbeddedSchemasMatchCanonicalSource (schema_test.go) fails the build if
// the two ever drift.
//
//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce sync.Once
	compileErr  error

	sessionSchema        *jsonschema.Schema
	interpretationSchema *jsonschema.Schema
	planSchema           *jsonschema.Schema
	attemptSchema        *jsonschema.Schema
	evaluationSchema     *jsonschema.Schema
	receiptSchema        *jsonschema.Schema
)

func compileEmbeddedSchemas() error {
	compileOnce.Do(func() {
		type target struct {
			filename string
			dst      **jsonschema.Schema
		}
		targets := []target{
			{SessionSchemaFilename, &sessionSchema},
			{InterpretationSchemaFilename, &interpretationSchema},
			{PlanSchemaFilename, &planSchema},
			{AttemptSchemaFilename, &attemptSchema},
			{EvaluationSchemaFilename, &evaluationSchema},
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

// ValidateSessionSchema validates raw session JSON bytes against the
// embedded, canonical synthesis-session-v1.schema.json using a real Draft
// 2020-12 validator.
func ValidateSessionSchema(data []byte) error { return validateAgainst(&sessionSchema, data) }

// ValidateInterpretationSchema validates raw interpretation JSON bytes
// against the embedded, canonical synthesis-interpretation-v1.schema.json.
func ValidateInterpretationSchema(data []byte) error {
	return validateAgainst(&interpretationSchema, data)
}

// ValidatePlanSchema validates raw plan JSON bytes against the embedded,
// canonical synthesis-plan-v1.schema.json.
func ValidatePlanSchema(data []byte) error { return validateAgainst(&planSchema, data) }

// ValidateAttemptSchema validates raw attempt JSON bytes against the
// embedded, canonical synthesis-attempt-v1.schema.json.
func ValidateAttemptSchema(data []byte) error { return validateAgainst(&attemptSchema, data) }

// ValidateEvaluationSchema validates raw evaluation JSON bytes against the
// embedded, canonical synthesis-evaluation-v1.schema.json.
func ValidateEvaluationSchema(data []byte) error { return validateAgainst(&evaluationSchema, data) }

// ValidateReceiptSchema validates raw receipt JSON bytes against the
// embedded, canonical synthesis-receipt-v1.schema.json.
func ValidateReceiptSchema(data []byte) error { return validateAgainst(&receiptSchema, data) }
