// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"fmt"
	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
	"time"
)

// Options binds deterministic extraction inputs supplied by the orchestrator.
type Options struct {
	CapturedAt     string
	Repository     architecture.ClaimDocumentBinding
	ResourceLimits map[string]string
}

// Extract parses the codebase using explicit deterministic inputs and returns
// a complete normalized Phase 10 investigation Document.
func Extract(root string, opts Options) (investigation.Document, error) {
	if _, err := time.Parse(time.RFC3339, opts.CapturedAt); err != nil {
		return investigation.Document{}, fmt.Errorf("captured_at must be an explicit RFC3339 input: %w", err)
	}
	return extractAll(root, opts)
}
