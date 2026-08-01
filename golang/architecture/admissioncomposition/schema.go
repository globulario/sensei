// SPDX-License-Identifier: AGPL-3.0-only

package admissioncomposition

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	RequestSchemaFilename = "admissioncomposition-request-v1.schema.json"
	ReceiptSchemaFilename = "admissioncomposition-receipt-v1.schema.json"
)

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce   sync.Once
	compileErr    error
	requestSchema *jsonschema.Schema
	receiptSchema *jsonschema.Schema
)

func compileEmbeddedSchemas() error {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		var err error
		requestSchema, err = compileEmbedded(compiler, RequestSchemaFilename)
		if err != nil {
			compileErr = err
			return
		}
		receiptSchema, err = compileEmbedded(compiler, ReceiptSchemaFilename)
		if err != nil {
			compileErr = err
		}
	})
	return compileErr
}

func compileEmbedded(compiler *jsonschema.Compiler, filename string) (*jsonschema.Schema, error) {
	data, err := embeddedSchemas.ReadFile("schemas/" + filename)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", filename, err)
	}
	var header struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, fmt.Errorf("decode %s $id: %w", filename, err)
	}
	if header.ID == "" {
		return nil, fmt.Errorf("%s has no $id", filename)
	}
	if err := compiler.AddResource(header.ID, bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("add %s: %w", filename, err)
	}
	schema, err := compiler.Compile(header.ID)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", filename, err)
	}
	return schema, nil
}

func validateSchema(schema **jsonschema.Schema, data []byte) error {
	if err := compileEmbeddedSchemas(); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return (*schema).Validate(instance)
}

func ValidateRequestSchema(data []byte) error {
	return validateSchema(&requestSchema, data)
}

func ValidateReceiptSchema(data []byte) error {
	return validateSchema(&receiptSchema, data)
}
