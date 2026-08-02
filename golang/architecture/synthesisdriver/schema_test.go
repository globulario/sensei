// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

func TestEmbeddedRunReceiptSchemaMatchesCanonicalSource(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	canonical := filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "docs", "schemas", "synthesisdriver", "v1", RunReceiptSchemaFilename)
	canonicalBytes, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	embeddedBytes, err := embeddedSchemas.ReadFile("schemas/" + RunReceiptSchemaFilename)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalBytes) != string(embeddedBytes) {
		t.Fatal("embedded O7 receipt schema drifted from canonical source")
	}
}

func TestRunReceiptSchemaRejectsInventedMergeAuthority(t *testing.T) {
	receipt := RunReceipt{
		SchemaVersion:                  RunReceiptSchemaVersion,
		ReceiptID:                      "o7.schema.test",
		GeneratedBy:                    GeneratedBy,
		SessionDigestSHA256:            strings.Repeat("a", 64),
		FinalPhase:                     string(synthesis.PhasePlanning),
		Disposition:                    DispositionStepLimitReached,
		StepCount:                      1,
		O2ReceiptDigestsSHA256:         []string{},
		RunnerReceiptDigestsSHA256:     []string{},
		EvaluationReceiptDigestsSHA256: []string{},
		SynthesisReceiptDigestSHA256:   nil,
		CandidateArtifactDigestSHA256:  nil,
		Detail:                         "bounded stop",
		StartedAt:                      "2026-08-02T00:00:00Z",
		CompletedAt:                    "2026-08-02T00:00:01Z",
		ReceiptDigestSHA256:            strings.Repeat("b", 64),
	}
	if err := ValidateRunReceiptSchema(receipt); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["merge_pull_request"] = true
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := compileRunReceiptSchema(); err != nil {
		t.Fatal(err)
	}
	var instance any
	if err := json.Unmarshal(mutated, &instance); err != nil {
		t.Fatal(err)
	}
	if err := runReceiptSchema.Validate(instance); err == nil {
		t.Fatal("O7 receipt schema accepted invented merge authority")
	}
}
