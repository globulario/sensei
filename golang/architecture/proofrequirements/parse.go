// SPDX-License-Identifier: AGPL-3.0-only

package proofrequirements

import (
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// ParseObligations decodes a generated proof_obligations.yaml body back into an
// ObligationsDoc, so a consumer (e.g. the result pipeline's proof stage) reuses
// the exact obligations the generated-artifact producer computed rather than
// extracting authority surfaces a second time.
func ParseObligations(data []byte) (ObligationsDoc, error) {
	var doc proofObligationsDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ObligationsDoc{}, fmt.Errorf("proofrequirements: parse obligations: %w", err)
	}
	return doc, nil
}

// ValidateObligations checks a parsed obligations document for structural
// soundness: every obligation carries a stable id, and every required slot has an
// id and kind.
func ValidateObligations(doc ObligationsDoc) error {
	seen := map[string]bool{}
	for _, o := range doc.ProofObligations {
		id := strings.TrimSpace(o.ID)
		if id == "" {
			return fmt.Errorf("proofrequirements: proof obligation without an id")
		}
		if seen[id] {
			return fmt.Errorf("proofrequirements: duplicate proof obligation id %q", id)
		}
		seen[id] = true
		for _, s := range o.RequiredSlots {
			if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Kind) == "" {
				return fmt.Errorf("proofrequirements: obligation %q has a slot without id or kind", id)
			}
		}
	}
	return nil
}
