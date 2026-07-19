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
	GetFileImpact(ctx context.Context, file string, domain string) (requiredTests []string, contracts []string, RelevantRules []string, err error)
}

// BaseFileReader optionally reads the un-edited base content of a file from the repository.
type BaseFileReader interface {
	ReadBaseFile(ctx context.Context, path string) (string, bool, error)
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

	// Rule: nil checker cannot prove verification -> cannot_verify
	if checker == nil {
		result.Availability = AvailabilityCannotVerify
		result.Decision = DecisionCannotVerify
		result.ReasonCodes = append(result.ReasonCodes, ReasonEvaluatorUnavailable)
		digest, _ := result.ComputeDigest()
		result.Digest = digest
		return result, nil
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

	baseReader, _ := checker.(BaseFileReader)

	for _, patch := range parsed.Files {
		if patch.IsBinary {
			continue
		}

		// 1. Gather file impact (required tests, contracts)
		tests, contracts, _, err := checker.GetFileImpact(ctx, patch.Path, opts.Domain)
		if err != nil {
			result.Availability = AvailabilityCannotVerify
			result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
		} else {
			result.ImplicatedTests = append(result.ImplicatedTests, tests...)
			result.ImplicatedContracts = append(result.ImplicatedContracts, contracts...)

			// Cross-file obligation check: required test file omitted from changed path set
			for _, testPath := range tests {
				if strings.Contains(testPath, "/") && !changedPathSet[testPath] {
					result.Findings = append(result.Findings, AuditFinding{
						RecordID:    "obligation.omitted_required_test",
						RecordClass: "required_test",
						Disposition: "review",
						FilePath:    patch.Path,
						Explanation: fmt.Sprintf("file %s requires test %s which is omitted from the supplied diff", patch.Path, testPath),
					})
				}
			}
		}

		// 2. Content evaluation
		if len(patch.Hunks) > 0 {
			var proposedContent string
			var contentLoaded bool

			if baseReader != nil {
				baseContent, ok, err := baseReader.ReadBaseFile(ctx, patch.Path)
				if err == nil && ok {
					reconstructed, err := applyHunks(baseContent, patch.Hunks)
					if err == nil {
						proposedContent = reconstructed
						contentLoaded = true
					}
				}
			}

			if !contentLoaded {
				// Fallback to hunk added-line evaluation
				var addedLines []string
				for _, hunk := range patch.Hunks {
					for _, line := range hunk.Lines {
						if strings.HasPrefix(line, "+") {
							addedLines = append(addedLines, strings.TrimPrefix(line, "+"))
						}
					}
				}
				proposedContent = strings.Join(addedLines, "\n")
			}

			if proposedContent != "" {
				fileFindings, err := checker.CheckFile(ctx, patch.Path, proposedContent, opts.Domain)
				if err != nil {
					result.ReasonCodes = append(result.ReasonCodes, ReasonEvaluatorUnavailable)
					result.Availability = AvailabilityCannotVerify
				} else {
					result.Findings = append(result.Findings, fileFindings...)
				}
			}
		}
	}

	// Deduplicate findings monotonically
	result.Findings = deduplicateFindings(result.Findings)

	// Compute overall decision
	if result.Availability != AvailabilityAvailable || len(result.ReasonCodes) > 0 {
		result.Decision = DecisionCannotVerify
	} else {
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
	}

	// Calculate self-excluding digest
	digest, err := result.ComputeDigest()
	if err != nil {
		return nil, fmt.Errorf("failed to compute result digest: %w", err)
	}
	result.Digest = digest

	return result, nil
}

func deduplicateFindings(in []AuditFinding) []AuditFinding {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool)
	out := make([]AuditFinding, 0, len(in))
	for _, f := range in {
		key := fmt.Sprintf("%s|%s|%s|%s", f.FilePath, f.RecordID, f.RecordClass, f.Explanation)
		if !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

func applyHunks(base string, hunks []DiffHunk) (string, error) {
	baseLines := strings.Split(base, "\n")
	var out []string
	baseIdx := 0

	for _, hunk := range hunks {
		targetIdx := hunk.OldStart - 1
		if targetIdx < 0 {
			targetIdx = 0
		}
		for baseIdx < targetIdx && baseIdx < len(baseLines) {
			out = append(out, baseLines[baseIdx])
			baseIdx++
		}

		for _, line := range hunk.Lines {
			if strings.HasPrefix(line, "+") {
				out = append(out, strings.TrimPrefix(line, "+"))
			} else if strings.HasPrefix(line, "-") {
				baseIdx++
			} else {
				if baseIdx < len(baseLines) {
					out = append(out, baseLines[baseIdx])
					baseIdx++
				}
			}
		}
	}

	for baseIdx < len(baseLines) {
		out = append(out, baseLines[baseIdx])
		baseIdx++
	}

	return strings.Join(out, "\n"), nil
}
