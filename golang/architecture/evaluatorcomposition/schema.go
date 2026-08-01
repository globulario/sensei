// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

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
	EvaluationPolicySchemaFilename    = "evaluatorcomposition-evaluationpolicy-v1.schema.json"
	EvaluatorDescriptorSchemaFilename = "evaluatorcomposition-evaluatordescriptor-v1.schema.json"
	EvaluationInputSchemaFilename     = "evaluatorcomposition-evaluationinput-v1.schema.json"
	EvaluatorResultSchemaFilename     = "evaluatorcomposition-evaluatorresult-v1.schema.json"
	EvaluationReceiptSchemaFilename   = "evaluatorcomposition-evaluationreceipt-v1.schema.json"
)

// embeddedSchemas is a vendored, build-time copy of
// docs/schemas/evaluatorcomposition/v1/*.schema.json, embedded directly
// into any binary that imports this package (go:embed cannot reach outside
// its own package directory with a ".." pattern, so the canonical,
// cross-repo-pinned source under docs/schemas/evaluatorcomposition/v1/ is
// mechanically copied here). TestEmbeddedSchemasMatchCanonicalSource
// (schema_test.go) fails the build if the two ever drift.
//
//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce sync.Once
	compileErr  error

	evaluationPolicySchema    *jsonschema.Schema
	evaluatorDescriptorSchema *jsonschema.Schema
	evaluationInputSchema     *jsonschema.Schema
	evaluatorResultSchema     *jsonschema.Schema
	evaluationReceiptSchema   *jsonschema.Schema
)

func compileEmbeddedSchemas() error {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()

		type target struct {
			filename string
			dst      **jsonschema.Schema
		}
		targets := []target{
			{EvaluationPolicySchemaFilename, &evaluationPolicySchema},
			{EvaluatorDescriptorSchemaFilename, &evaluatorDescriptorSchema},
			{EvaluationInputSchemaFilename, &evaluationInputSchema},
			{EvaluatorResultSchemaFilename, &evaluatorResultSchema},
			{EvaluationReceiptSchemaFilename, &evaluationReceiptSchema},
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

// ValidateEvaluationPolicySchema validates raw evaluation-policy JSON bytes
// against the embedded, canonical
// evaluatorcomposition-evaluationpolicy-v1.schema.json using a real Draft
// 2020-12 validator.
func ValidateEvaluationPolicySchema(data []byte) error {
	return validateAgainst(&evaluationPolicySchema, data)
}

// ValidateEvaluatorDescriptorSchema validates raw evaluator-descriptor JSON
// bytes against the embedded, canonical
// evaluatorcomposition-evaluatordescriptor-v1.schema.json.
func ValidateEvaluatorDescriptorSchema(data []byte) error {
	return validateAgainst(&evaluatorDescriptorSchema, data)
}

// ValidateEvaluationInputSchema validates raw evaluation-input JSON bytes
// against the embedded, canonical
// evaluatorcomposition-evaluationinput-v1.schema.json.
func ValidateEvaluationInputSchema(data []byte) error {
	return validateAgainst(&evaluationInputSchema, data)
}

// ValidateEvaluatorResultSchema validates raw evaluator-result JSON bytes
// against the embedded, canonical
// evaluatorcomposition-evaluatorresult-v1.schema.json.
func ValidateEvaluatorResultSchema(data []byte) error {
	return validateAgainst(&evaluatorResultSchema, data)
}

// ValidateEvaluationReceiptSchema validates raw evaluation-receipt JSON
// bytes against the embedded, canonical
// evaluatorcomposition-evaluationreceipt-v1.schema.json.
func ValidateEvaluationReceiptSchema(data []byte) error {
	return validateAgainst(&evaluationReceiptSchema, data)
}
