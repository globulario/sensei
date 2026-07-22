// SPDX-License-Identifier: AGPL-3.0-only

package investigator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

type defaultPostProcessor struct{}

// NewPostProcessor returns the default PostProcessor implementation.
func NewPostProcessor() PostProcessor {
	return &defaultPostProcessor{}
}

func (p *defaultPostProcessor) ValidateGrounding(ctx context.Context, doc investigation.Document, root string) error {
	evidenceMap := make(map[string]investigation.EvidenceReceipt)
	for _, rec := range doc.RawEvidence {
		evidenceMap[rec.ID] = rec
	}

	for _, claim := range doc.CandidateClaims {
		// 1. Human review required and promotion status checks
		if !claim.HumanReviewRequired {
			return fmt.Errorf("candidate claim %s must require human review", claim.ID)
		}
		if claim.PromotionStatus != architecture.PromotionCandidate {
			return fmt.Errorf("candidate claim %s must have promotion status %q", claim.ID, architecture.PromotionCandidate)
		}

		// 2. Confidence range check
		if claim.Confidence < 0.0 || claim.Confidence > 1.0 {
			return fmt.Errorf("candidate claim %s confidence must be between 0.0 and 1.0, got %f", claim.ID, claim.Confidence)
		}

		// 3. Supporting and refuting evidence must be disjoint
		supportSet := make(map[string]bool)
		for _, s := range claim.SupportingEvidence {
			supportSet[s] = true
		}
		for _, r := range claim.RefutingEvidence {
			if supportSet[r] {
				return fmt.Errorf("candidate claim %s has overlapping supporting and refuting evidence ID %q", claim.ID, r)
			}
		}

		// 4. Verification that referenced files exist in codebase root
		if root != "" {
			for _, file := range claim.Scope.Files {
				full := filepath.Join(root, file)
				if _, err := os.Stat(full); err != nil {
					return fmt.Errorf("candidate claim %s references non-existent file %q under root %q: %w", claim.ID, file, root, err)
				}
			}
		}

		// 5. Evidence reference resolution and scope boundaries verification
		// If a claim has supporting/refuting evidence, its scope must be bounded by the evidence scopes.
		for _, ref := range claim.SupportingEvidence {
			rec, ok := evidenceMap[ref]
			if !ok {
				return fmt.Errorf("candidate claim %s references missing supporting evidence %q", claim.ID, ref)
			}

			// Validate scope bounding: claim scope must not exceed evidence scope
			if err := validateScopeSubset(claim.Scope, rec.Scope); err != nil {
				return fmt.Errorf("candidate claim %s scope exceeds supporting evidence %q scope: %w", claim.ID, ref, err)
			}
		}

		for _, ref := range claim.RefutingEvidence {
			rec, ok := evidenceMap[ref]
			if !ok {
				return fmt.Errorf("candidate claim %s references missing refuting evidence %q", claim.ID, ref)
			}

			// Validate scope bounding: claim scope must not exceed evidence scope
			if err := validateScopeSubset(claim.Scope, rec.Scope); err != nil {
				return fmt.Errorf("candidate claim %s scope exceeds refuting evidence %q scope: %w", claim.ID, ref, err)
			}
		}
	}

	return nil
}

// validateScopeSubset checks if sub is a subset of parent.
func validateScopeSubset(sub, parent architecture.ClaimScope) error {
	// Check files
	parentFiles := make(map[string]bool)
	for _, f := range parent.Files {
		parentFiles[f] = true
	}
	for _, f := range sub.Files {
		// If parent is empty, it means unbounded/global. Otherwise, child must be contained.
		if len(parent.Files) > 0 && !parentFiles[f] {
			return fmt.Errorf("file %q not found in evidence scope", f)
		}
	}

	// Check symbols
	parentSymbols := make(map[string]bool)
	for _, s := range parent.Symbols {
		parentSymbols[s] = true
	}
	for _, s := range sub.Symbols {
		if len(parent.Symbols) > 0 && !parentSymbols[s] {
			return fmt.Errorf("symbol %q not found in evidence scope", s)
		}
	}

	// Check components
	parentComponents := make(map[string]bool)
	for _, c := range parent.Components {
		parentComponents[c] = true
	}
	for _, c := range sub.Components {
		if len(parent.Components) > 0 && !parentComponents[c] {
			return fmt.Errorf("component %q not found in evidence scope", c)
		}
	}

	return nil
}
