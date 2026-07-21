// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"fmt"
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"time"
)

// Options binds deterministic extraction inputs supplied by the orchestrator.
type Options struct{ CapturedAt string }

// Result is the completed HOW extraction report containing
// facts, evidence receipts, coverage entries, and limitations.
type Result struct {
	Facts       []architecture.Fact             `json:"facts" yaml:"facts"`
	RawEvidence []investigation.EvidenceReceipt `json:"raw_evidence" yaml:"raw_evidence"`
	Coverage    []investigation.CoverageEntry   `json:"coverage" yaml:"coverage"`
	Limitations []architecture.Limitation       `json:"limitations" yaml:"limitations"`
}

// Extract parses the codebase using the Go AST and semantic extractors
// and returns the composed HOW observations, receipts, and coverage.
func Extract(root string) (Result, error) {
	return ExtractWithOptions(root, Options{})
}

func ExtractWithOptions(root string, opts Options) (Result, error) {
	if _, err := time.Parse(time.RFC3339, opts.CapturedAt); err != nil {
		return Result{}, fmt.Errorf("captured_at must be an explicit RFC3339 input: %w", err)
	}
	return extractAll(root, opts)
}
