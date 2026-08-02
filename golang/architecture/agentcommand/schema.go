// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const MutationPlanSchemaFilename = "agentcommand-mutation-plan-v1.schema.json"

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	mutationPlanSchemaOnce sync.Once
	mutationPlanSchema     *jsonschema.Schema
	mutationPlanSchemaErr  error
)

func compileMutationPlanSchema() error {
	mutationPlanSchemaOnce.Do(func() {
		data, err := embeddedSchemas.ReadFile("schemas/" + MutationPlanSchemaFilename)
		if err != nil {
			mutationPlanSchemaErr = err
			return
		}
		var header struct {
			ID string `json:"$id"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			mutationPlanSchemaErr = err
			return
		}
		if header.ID == "" {
			mutationPlanSchemaErr = fmt.Errorf("agentcommand: mutation plan schema has no $id")
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(header.ID, bytes.NewReader(data)); err != nil {
			mutationPlanSchemaErr = err
			return
		}
		mutationPlanSchema, mutationPlanSchemaErr = compiler.Compile(header.ID)
	})
	return mutationPlanSchemaErr
}

// ValidateMutationPlanSchema validates one canonical plan with the embedded
// Draft 2020-12 schema. Semantic laws such as unique operation IDs and the
// self-referential digest remain enforced by ValidateMutationPlan.
func ValidateMutationPlanSchema(plan MutationPlan) error {
	if err := compileMutationPlanSchema(); err != nil {
		return err
	}
	data, err := json.Marshal(NormalizeMutationPlan(plan))
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return mutationPlanSchema.Validate(instance)
}
