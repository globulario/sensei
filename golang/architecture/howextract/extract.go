// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

// Result is the completed HOW extraction report containing
// facts, evidence receipts, coverage entries, and limitations.
type Result struct {
	Facts        []architecture.Fact              `json:"facts" yaml:"facts"`
	RawEvidence  []investigation.EvidenceReceipt  `json:"raw_evidence" yaml:"raw_evidence"`
	Coverage     []investigation.CoverageEntry    `json:"coverage" yaml:"coverage"`
	Limitations  []architecture.Limitation        `json:"limitations" yaml:"limitations"`
}

// Extract parses the codebase using the Go AST and semantic extractors
// and returns the composed HOW observations, receipts, and coverage.
func Extract(root string) (Result, error) {
	return extractAll(root)
}
