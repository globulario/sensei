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

const PlanProposalSchemaFilename = "cognitivecommand-plan-proposal-v1.schema.json"

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	compileOnce        sync.Once
	compileErr         error
	planProposalSchema *jsonschema.Schema
)

func compileSchemas() error {
	compileOnce.Do(func() {
		data, err := PlanProposalSchemaBytes()
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
			compileErr = fmt.Errorf("cognitivecommand: schema %s has no $id", PlanProposalSchemaFilename)
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(header.ID, bytes.NewReader(data)); err != nil {
			compileErr = err
			return
		}
		planProposalSchema, compileErr = compiler.Compile(header.ID)
	})
	return compileErr
}

// PlanProposalSchemaBytes returns an isolated copy of the exact embedded
// canonical schema supplied to the external planning command.
func PlanProposalSchemaBytes() ([]byte, error) {
	data, err := embeddedSchemas.ReadFile("schemas/" + PlanProposalSchemaFilename)
	if err != nil {
		return nil, err
	}
	return append([]byte{}, data...), nil
}

func ValidatePlanProposalSchema(data []byte) error {
	if err := compileSchemas(); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return planProposalSchema.Validate(instance)
}
