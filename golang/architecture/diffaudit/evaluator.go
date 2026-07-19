// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"context"
	"fmt"
	"strings"
)

// SingleFileChecker evaluates single file content and graph impact.
type SingleFileChecker interface {
	CheckFile(ctx context.Context, file string, content string, domain string) ([]AuditFinding, error)
	GetFileImpact(ctx context.Context, file string, domain string) (requiredTests []string, contracts []string, findings []AuditFinding, err error)
}

// AuditOptions configures the diff audit run.
type AuditOptions struct {
	Task         string
	ExpectedHead string
	Domain       string
}

// EvaluateDiff orchestrates single-file checks and cross-file obligation analysis over a ParsedDiff.
func EvaluateDiff(ctx context.Context, parsed *ParsedDiff, checker SingleFileChecker, opts AuditOptions) (*AuditResult, error) {
	if parsed == nil {
		return nil, fmt.Errorf("parsed diff is nil")
	}

	result := &AuditResult{
		Schema:              SchemaV1,
		InputDiffDigest:     parsed.InputDigest,
		InputTrust:          TrustCaller,
		Availability:        AvailabilityAvailable,
		Decision:            DecisionPass,
		ExpectedHead:        opts.ExpectedHead,
		ChangedFiles:        make([]ChangedFileSummary, 0, len(parsed.Files)),
		Findings:            make([]AuditFinding, 0),
		ImplicatedTests:     make([]string, 0),
		ImplicatedContracts: make([]string, 0),
		ReasonCodes:         append([]ReasonCode{}, parsed.ReasonCodes...),
	}

	changedPathSet := make(map[string]bool)
	hasBinary := false

	for _, patch := range parsed.Files {
		changedPathSet[patch.Path] = true

		summary := ChangedFileSummary{
			Path:         patch.Path,
			OldPath:      patch.OldPath,
			Kind:         patch.Kind,
			OldMode:      patch.OldMode,
			NewMode:      patch.NewMode,
			HunkCount:    len(patch.Hunks),
			LinesAdded:   patch.TotalAdded,
			LinesDeleted: patch.TotalDeleted,
		}
		result.ChangedFiles = append(result.ChangedFiles, summary)

		if patch.IsBinary {
			hasBinary = true
			result.Findings = append(result.Findings, AuditFinding{
				RecordID:    "unsupported.binary_patch",
				RecordClass: "unsupported",
				Disposition: "cannot_verify",
				FilePath:    patch.Path,
				Explanation: fmt.Sprintf("binary patch for %s cannot be verified statically", patch.Path),
			})
		}
	}

	if hasBinary {
		result.Availability = AvailabilityCannotVerify
		result.Decision = DecisionCannotVerify
	}

	if checker != nil {
		for _, patch := range parsed.Files {
			if patch.IsBinary {
				continue
			}

			// Gather impact (required tests, contracts, path invariants)
			tests, contracts, impactFindings, err := checker.GetFileImpact(ctx, patch.Path, opts.Domain)
			if err != nil {
				result.Availability = AvailabilityCannotVerify
				result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
			} else {
				result.ImplicatedTests = append(result.ImplicatedTests, tests...)
				result.ImplicatedContracts = append(result.ImplicatedContracts, contracts...)
				result.Findings = append(result.Findings, impactFindings...)
			}

			// Evaluate added/modified content if hunks present
			if len(patch.Hunks) > 0 {
				addedContent := extractAddedContent(patch)
				if addedContent != "" {
					fileFindings, err := checker.CheckFile(ctx, patch.Path, addedContent, opts.Domain)
					if err != nil {
						result.ReasonCodes = append(result.ReasonCodes, ReasonEvaluatorUnavailable)
					} else {
						result.Findings = append(result.Findings, fileFindings...)
					}
				}
			}
		}
	}

	// Compute overall decision based on findings
	for _, f := range result.Findings {
		if f.Disposition == "block" {
			result.Decision = DecisionBlock
			break
		} else if f.Disposition == "review" && result.Decision != DecisionBlock {
			result.Decision = DecisionReview
		} else if f.Disposition == "cannot_verify" && result.Decision == DecisionPass {
			result.Decision = DecisionCannotVerify
		}
	}

	// Calculate final self-excluding digest
	digest, err := result.ComputeDigest()
	if err != nil {
		return nil, fmt.Errorf("failed to compute result digest: %w", err)
	}
	result.Digest = digest

	return result, nil
}

func extractAddedContent(patch ParsedFilePatch) string {
	var sb strings.Builder
	for _, hunk := range patch.Hunks {
		for _, line := range hunk.Lines {
			if strings.HasPrefix(line, "+") {
				sb.WriteString(strings.TrimPrefix(line, "+"))
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}
