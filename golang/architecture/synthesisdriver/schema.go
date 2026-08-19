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

const (
	RunReceiptSchemaFilename       = "synthesisdriver-receipt-v1.schema.json"
	CheckpointSchemaFilename       = "synthesisdriver-checkpoint-v1.schema.json"
	ResumeAssessmentSchemaFilename = "synthesisdriver-resume-assessment-v1.schema.json"
)

//go:embed schemas/*.json
var embeddedSchemas embed.FS

var (
	runReceiptSchemaOnce sync.Once
	runReceiptSchema     *jsonschema.Schema
	runReceiptSchemaErr  error

	checkpointSchemaOnce sync.Once
	checkpointSchema     *jsonschema.Schema
	checkpointSchemaErr  error

	resumeAssessmentSchemaOnce sync.Once
	resumeAssessmentSchema     *jsonschema.Schema
	resumeAssessmentSchemaErr  error
)

// compileEmbeddedSchema compiles one embedded schema under its own declared
// $id. A schema with no $id is a compile error rather than a silently
// unvalidated document.
func compileEmbeddedSchema(filename string) (*jsonschema.Schema, error) {
	data, err := embeddedSchemas.ReadFile("schemas/" + filename)
	if err != nil {
		return nil, err
	}
	var header struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	if header.ID == "" {
		return nil, fmt.Errorf("synthesisdriver: schema %s has no $id", filename)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(header.ID, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return compiler.Compile(header.ID)
}

func compileRunReceiptSchema() error {
	runReceiptSchemaOnce.Do(func() {
		runReceiptSchema, runReceiptSchemaErr = compileEmbeddedSchema(RunReceiptSchemaFilename)
	})
	return runReceiptSchemaErr
}

func compileCheckpointSchema() error {
	checkpointSchemaOnce.Do(func() {
		checkpointSchema, checkpointSchemaErr = compileEmbeddedSchema(CheckpointSchemaFilename)
	})
	return checkpointSchemaErr
}

func ValidateRunReceiptSchema(receipt RunReceipt) error {
	if err := compileRunReceiptSchema(); err != nil {
		return err
	}
	return validateInstance(runReceiptSchema, NormalizeRunReceipt(receipt))
}

func compileResumeAssessmentSchema() error {
	resumeAssessmentSchemaOnce.Do(func() {
		resumeAssessmentSchema, resumeAssessmentSchemaErr = compileEmbeddedSchema(ResumeAssessmentSchemaFilename)
	})
	return resumeAssessmentSchemaErr
}

func ValidateResumeAssessmentSchema(assessment ResumeAssessment) error {
	if err := compileResumeAssessmentSchema(); err != nil {
		return err
	}
	return validateInstance(resumeAssessmentSchema, NormalizeResumeAssessment(assessment))
}

func ValidateCheckpointSchema(checkpoint Checkpoint) error {
	if err := compileCheckpointSchema(); err != nil {
		return err
	}
	return validateInstance(checkpointSchema, NormalizeCheckpoint(checkpoint))
}

// ValidateCheckpointDocument validates raw bytes before they are decoded into
// a Checkpoint. Decoding first would silently drop an unknown field, so a
// document that does not belong to this closed schema has to be refused while
// it is still bytes.
func ValidateCheckpointDocument(data []byte) error {
	if err := compileCheckpointSchema(); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return checkpointSchema.Validate(instance)
}

// validateInstance marshals a document and validates it with numbers decoded
// as json.Number, so an integer field is never widened to a float and then
// accepted against an "integer" constraint it does not satisfy.
func validateInstance(schema *jsonschema.Schema, document any) error {
	data, err := json.Marshal(document)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return err
	}
	return schema.Validate(instance)
}
