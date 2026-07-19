// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

import (
	"context"
	"fmt"
	"strings"
)

// Requirement represents an obligated contract or test target from the graph.
type Requirement struct {
	ID   string `json:"id"`
	Path string `json:"path,omitempty"`
}

// SingleFileChecker evaluates single file content and graph impact.
type SingleFileChecker interface {
	CheckFile(ctx context.Context, file string, content string, domain string) ([]AuditFinding, error)
	GetFileImpact(ctx context.Context, file string, domain string) (requiredTests []Requirement, contracts []Requirement, RelevantRules []string, err error)
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

	if checker == nil {
		result.Availability = AvailabilityCannotVerify
		result.Decision = DecisionCannotVerify
		result.ReasonCodes = append(result.ReasonCodes, ReasonEvaluatorUnavailable)
		digest, _ := result.ComputeDigest()
		result.Digest = digest
		_ = result.Validate()
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

		readPath := patch.Path
		if patch.OldPath != "" {
			readPath = patch.OldPath
		}

		// 1. Gather file impact (required tests, contracts)
		tests, contracts, _, err := checker.GetFileImpact(ctx, readPath, opts.Domain)
		if err != nil {
			result.Availability = AvailabilityCannotVerify
			result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
		} else {
			for _, t := range tests {
				result.ImplicatedTests = append(result.ImplicatedTests, t.ID)
			}
			for _, c := range contracts {
				result.ImplicatedContracts = append(result.ImplicatedContracts, c.ID)
			}

			// Cross-file obligation check: required test file omitted from changed path set
			for _, reqTest := range tests {
				if reqTest.Path != "" && !changedPathSet[reqTest.Path] {
					result.Findings = append(result.Findings, AuditFinding{
						RecordID:    "obligation.omitted_required_test",
						RecordClass: "required_test",
						Disposition: "review",
						FilePath:    patch.Path,
						Explanation: fmt.Sprintf("file %s requires test %s which is omitted from the supplied diff", patch.Path, reqTest.Path),
					})
				}
			}
		}

		// 2. Content evaluation
		if len(patch.Hunks) > 0 {
			var proposedContent string
			var contentLoaded bool

			if baseReader != nil {
				baseContent, ok, err := baseReader.ReadBaseFile(ctx, readPath)
				if err == nil && ok {
					reconstructed, err := applyHunks(baseContent, patch.Hunks, patch.Kind == ChangeAdd)
					if err == nil {
						proposedContent = reconstructed
						contentLoaded = true
					} else {
						result.Availability = AvailabilityCannotVerify
						result.ReasonCodes = append(result.ReasonCodes, ReasonMalformedDiff)
						result.Limitations = append(result.Limitations, err.Error())
					}
				}
			}

			if !contentLoaded && patch.Kind == ChangeAdd {
				reconstructed, err := applyHunks("", patch.Hunks, true)
				if err == nil {
					proposedContent = reconstructed
					contentLoaded = true
				} else {
					result.Availability = AvailabilityCannotVerify
					result.ReasonCodes = append(result.ReasonCodes, ReasonMalformedDiff)
					result.Limitations = append(result.Limitations, err.Error())
				}
			}

			if !contentLoaded {
				result.Availability = AvailabilityCannotVerify
				if len(result.ReasonCodes) == 0 {
					result.ReasonCodes = append(result.ReasonCodes, ReasonRepoContextUnavailable)
				}
			} else if proposedContent != "" {
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

	digest, err := result.ComputeDigest()
	if err != nil {
		return nil, fmt.Errorf("failed to compute result digest: %w", err)
	}
	result.Digest = digest

	// Enforce Validate() checks before returning
	if err := result.Validate(); err != nil {
		result.Decision = DecisionCannotVerify
		result.ReasonCodes = append(result.ReasonCodes, ReasonResultValidationFail)
		result.Limitations = append(result.Limitations, fmt.Sprintf("validation failed: %v", err))
		digest, _ := result.ComputeDigest()
		result.Digest = digest
	}

	return result, nil
}

func deduplicateFindings(in []AuditFinding) []AuditFinding {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool)
	out := make([]AuditFinding, 0, len(in))
	for _, f := range in {
		key := fmt.Sprintf("%s|%s|%s|%d", f.FilePath, f.RecordID, f.RecordClass, f.HunkIndex)
		if !seen[key] {
			seen[key] = true
			out = append(out, f)
		}
	}
	return out
}

func applyHunks(base string, hunks []DiffHunk, isAdd bool) (string, error) {
	if len(hunks) == 0 {
		return base, nil
	}

	if isAdd {
		var out []string
		for _, hunk := range hunks {
			if hunk.OldLines != 0 {
				return "", fmt.Errorf("invalid add hunk header: old lines count must be 0, got %d", hunk.OldLines)
			}
			for _, line := range hunk.Lines {
				if !strings.HasPrefix(line, "+") {
					return "", fmt.Errorf("invalid line in add hunk: new files can only contain additions (+), got %q", line)
				}
				out = append(out, strings.TrimPrefix(line, "+"))
			}
		}
		return strings.Join(out, "\n"), nil
	}

	baseLines := strings.Split(base, "\n")
	var out []string
	baseIdx := 0
	lastOldEnd := 0

	for _, hunk := range hunks {
		if hunk.OldStart <= lastOldEnd {
			return "", fmt.Errorf("out-of-order or overlapping hunk: start line %d <= last old end %d", hunk.OldStart, lastOldEnd)
		}
		if hunk.OldStart-1 > len(baseLines) {
			return "", fmt.Errorf("hunk starts beyond base EOF: start line %d > base lines %d", hunk.OldStart, len(baseLines))
		}

		targetIdx := hunk.OldStart - 1
		for baseIdx < targetIdx && baseIdx < len(baseLines) {
			out = append(out, baseLines[baseIdx])
			baseIdx++
		}

		for _, line := range hunk.Lines {
			if strings.HasPrefix(line, "+") {
				out = append(out, strings.TrimPrefix(line, "+"))
			} else if strings.HasPrefix(line, "-") {
				expectedDel := strings.TrimPrefix(line, "-")
				if baseIdx >= len(baseLines) {
					return "", fmt.Errorf("deletion beyond base EOF at base line %d", baseIdx+1)
				}
				if baseLines[baseIdx] != expectedDel {
					return "", fmt.Errorf("hunk line mismatch on deleted line at base line %d: base %q != patch %q", baseIdx+1, baseLines[baseIdx], expectedDel)
				}
				baseIdx++
			} else {
				contextLine := strings.TrimPrefix(line, " ")
				if baseIdx >= len(baseLines) {
					return "", fmt.Errorf("context match beyond base EOF at base line %d", baseIdx+1)
				}
				if baseLines[baseIdx] != contextLine {
					return "", fmt.Errorf("hunk line mismatch on context line at base line %d: base %q != patch %q", baseIdx+1, baseLines[baseIdx], contextLine)
				}
				out = append(out, baseLines[baseIdx])
				baseIdx++
			}
		}

		lastOldEnd = hunk.OldStart + hunk.OldLines - 1
	}

	for baseIdx < len(baseLines) {
		out = append(out, baseLines[baseIdx])
		baseIdx++
	}

	return strings.Join(out, "\n"), nil
}
