// SPDX-License-Identifier: AGPL-3.0-only

package cognitivecommand

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	InterpretationProposalSchemaFilename = "cognitivecommand-interpretation-proposal-v1.schema.json"
	PlanProposalSchemaFilename           = "cognitivecommand-plan-proposal-v1.schema.json"
)

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce                  sync.Once
	compileErr                   error
	interpretationProposalSchema *jsonschema.Schema
	planProposalSchema           *jsonschema.Schema
)

func compileSchemas() error {
	compileOnce.Do(func() {
		compiler := jsonschema.NewCompiler()
		targets := []struct {
			filename string
			dst      **jsonschema.Schema
		}{
			{InterpretationProposalSchemaFilename, &interpretationProposalSchema},
			{PlanProposalSchemaFilename, &planProposalSchema},
		}
		for _, target := range targets {
			data, err := embeddedSchemas.ReadFile("schemas/" + target.filename)
			if err != nil {
				compileErr = err
				return
			}
			var header struct {
				ID string `json:"$id"`
			}
			if err := json.Unmarshal(data, &header); err != nil {
				compileErr = err
				return
			}
			if header.ID == "" {
				compileErr = fmt.Errorf("cognitivecommand: schema %s has no $id", target.filename)
				return
			}
			if err := compiler.AddResource(header.ID, bytes.NewReader(data)); err != nil {
				compileErr = err
				return
			}
			schema, err := compiler.Compile(header.ID)
			if err != nil {
				compileErr = err
				return
			}
			*target.dst = schema
		}
	})
	return compileErr
}

func validateProposalSchema(schema **jsonschema.Schema, data []byte) error {
	if err := compileSchemas(); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return (*schema).Validate(instance)
}

func ValidateInterpretationProposalSchema(data []byte) error {
	return validateProposalSchema(&interpretationProposalSchema, data)
}

func ValidatePlanProposalSchema(data []byte) error {
	return validateProposalSchema(&planProposalSchema, data)
}
