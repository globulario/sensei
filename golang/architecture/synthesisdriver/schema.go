// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const RunReceiptSchemaFilename = "synthesisdriver-receipt-v1.schema.json"

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	runReceiptSchemaOnce sync.Once
	runReceiptSchema     *jsonschema.Schema
	runReceiptSchemaErr  error
)

func compileRunReceiptSchema() error {
	runReceiptSchemaOnce.Do(func() {
		data, err := embeddedSchemas.ReadFile("schemas/" + RunReceiptSchemaFilename)
		if err != nil {
			runReceiptSchemaErr = err
			return
		}
		var header struct {
			ID string `json:"$id"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			runReceiptSchemaErr = err
			return
		}
		if header.ID == "" {
			runReceiptSchemaErr = fmt.Errorf("synthesisdriver: receipt schema has no $id")
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(header.ID, bytes.NewReader(data)); err != nil {
			runReceiptSchemaErr = err
			return
		}
		runReceiptSchema, runReceiptSchemaErr = compiler.Compile(header.ID)
	})
	return runReceiptSchemaErr
}

func ValidateRunReceiptSchema(receipt RunReceipt) error {
	if err := compileRunReceiptSchema(); err != nil {
		return err
	}
	data, err := json.Marshal(NormalizeRunReceipt(receipt))
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return runReceiptSchema.Validate(instance)
}
