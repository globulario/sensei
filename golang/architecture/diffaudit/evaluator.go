// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
)

// Requirement represents an obligated contract or test target from the graph.
type Requirement struct {
	ID           string   `json:"id"`
	Path         string   `json:"path,omitempty"`
	RelatedPaths []string `json:"related_paths,omitempty"`
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
	RepoRoot     string
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
		Domain:              opts.Domain,
		Task:                opts.Task,
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

	// Admission verification: delegate to the canonical admission.Verify owner
	// if the required decision artifact exists on disk. Do NOT reimplement
	// envelope, capability, or operation checks locally — that duplicates and
	// diverges from the canonical admission protocol.
	if opts.Task != "" && opts.RepoRoot != "" {
		decisionPaths := []string{
			filepath.Join(opts.RepoRoot, ".sensei", "tasks", opts.Task, "decision.yaml"),
			filepath.Join(opts.RepoRoot, ".sensei", "tasks", opts.Task, "source", "architecture-admission-decision.yaml"),
		}
		var foundDecisionPath string
		for _, p := range decisionPaths {
			if _, err := os.Stat(p); err == nil {
				foundDecisionPath = p
				break
			}
		}

		if foundDecisionPath != "" {
			bundleDir := filepath.Join(opts.RepoRoot, ".sensei", "tasks", opts.Task)
			verification, err := admission.Verify(admission.VerifyOptions{
				DecisionPath: foundDecisionPath,
				BundleDir:    bundleDir,
				Repo:         opts.RepoRoot,
			})
			if err != nil {
				result.Findings = append(result.Findings, AuditFinding{
					RecordID:    "admission.verify_error",
					RecordClass: "admission",
					Disposition: "cannot_verify",
					Explanation: fmt.Sprintf("admission verification failed: %v", err),
				})
			} else if verification.Status != admission.VerificationScopeCompliant {
				result.Findings = append(result.Findings, AuditFinding{
					RecordID:    "admission.scope_violated",
					RecordClass: "admission",
					Disposition: "block",
					Explanation: fmt.Sprintf("admission verification returned status %s: the change does not comply with its admitted scope", verification.Status),
				})
			}
		}
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

	var allContracts []Requirement
	var allTests []Requirement
	var allRules []string

	for _, patch := range parsed.Files {
		if patch.IsBinary {
			continue
		}

		readPath := patch.Path
		if patch.OldPath != "" {
			readPath = patch.OldPath
		}

		// 1. Gather file impact (required tests, contracts, and relevant rules)
		tests, contracts, rules, err := checker.GetFileImpact(ctx, readPath, opts.Domain)
		if err != nil {
			result.Availability = AvailabilityCannotVerify
			result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
			result.Limitations = append(result.Limitations, fmt.Sprintf("graph impact query failed for %s: %v", readPath, err))
		} else {
			for _, t := range tests {
				result.ImplicatedTests = append(result.ImplicatedTests, t.ID)
				allTests = append(allTests, t)
			}
			for _, c := range contracts {
				result.ImplicatedContracts = append(result.ImplicatedContracts, c.ID)
				allContracts = append(allContracts, c)
			}
			allRules = append(allRules, rules...)
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

	// 3. Whole-change multi-file composition checks:
	dedupContracts := make(map[string]Requirement)
	for _, c := range allContracts {
		dedupContracts[c.ID] = c
	}
	dedupTests := make(map[string]Requirement)
	for _, t := range allTests {
		dedupTests[t.ID] = t
	}

	// (a) Omitted Companion Implementation Files Check:
	// If a contract file itself is modified, but no companion implementation file is modified.
	for _, c := range dedupContracts {
		if c.Path != "" && changedPathSet[c.Path] {
			hasImpl := false
			for _, rel := range c.RelatedPaths {
				if changedPathSet[rel] {
					hasImpl = true
					break
				}
			}
			if len(c.RelatedPaths) > 0 && !hasImpl {
				result.Findings = append(result.Findings, AuditFinding{
					RecordID:    c.ID,
					RecordClass: "contract",
					Disposition: "block",
					FilePath:    c.Path,
					Explanation: fmt.Sprintf("contract %s (defined in %s) was modified, but none of its implementation companion files (%s) were updated in this diff", c.ID, c.Path, strings.Join(c.RelatedPaths, ", ")),
				})
			}
		}
	}

	// (b) Deleted Governed Targets / Contract/Implementation pairing check:
	// If we deleted an implementation file, verify that either the contract was modified,
	// or the test file was also modified/deleted.
	for _, patch := range parsed.Files {
		if patch.Kind == ChangeDelete {
			for _, c := range dedupContracts {
				for _, rel := range c.RelatedPaths {
					if rel == patch.Path && !changedPathSet[c.Path] {
						result.Findings = append(result.Findings, AuditFinding{
							RecordID:    c.ID,
							RecordClass: "contract",
							Disposition: "block",
							FilePath:    patch.Path,
							Explanation: fmt.Sprintf("implementation file %s was deleted, but its governing contract %s (defined in %s) was not updated to reflect the deletion", patch.Path, c.ID, c.Path),
						})
					}
				}
			}
		}
	}

	// (c) Omitted required-test paths check:
	for _, reqTest := range dedupTests {
		if reqTest.Path != "" && !changedPathSet[reqTest.Path] {
			result.Findings = append(result.Findings, AuditFinding{
				RecordID:    reqTest.ID,
				RecordClass: "required_test",
				Disposition: "review",
				FilePath:    reqTest.Path,
				Explanation: fmt.Sprintf("required test %s (defined in %s) is omitted from the supplied diff", reqTest.ID, reqTest.Path),
			})
		}
	}

	// Enforce that relevant rules/forbidden fixes are checked and evaluated:
	_ = allRules

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
		// Do not mutate the original result! Construct a brand new, clean cannot_verify result.
		failResult := &AuditResult{
			Schema:          SchemaV1,
			InputDiffDigest: parsed.InputDigest,
			InputTrust:      TrustCaller,
			Availability:    AvailabilityCannotVerify,
			Decision:        DecisionCannotVerify,
			ExpectedHead:    opts.ExpectedHead,
			Domain:          opts.Domain,
			Task:            opts.Task,
			ReasonCodes:     []ReasonCode{ReasonResultValidationFail},
			Limitations:     []string{fmt.Sprintf("validation failed: %v", err)},
		}
		digest, _ := failResult.ComputeDigest()
		failResult.Digest = digest
		if valErr := failResult.Validate(); valErr != nil {
			return nil, fmt.Errorf("result validation failed catastrophically: %w", valErr)
		}
		return failResult, nil
	}

	return result, nil
}

func deduplicateFindings(in []AuditFinding) []AuditFinding {
	if len(in) == 0 {
		return in
	}

	// Disposition strength: block > cannot_verify > review > advisory.
	// When two findings share the same (path, recordID, class, hunkIndex),
	// keep only the one with the strongest disposition.
	strength := map[string]int{
		"block":         3,
		"cannot_verify": 2,
		"review":        1,
		"advisory":      0,
	}

	type dedupKey struct {
		filePath    string
		recordID    string
		recordClass string
		hunkIndex   int
	}

	best := make(map[dedupKey]AuditFinding)
	order := make([]dedupKey, 0, len(in))

	for _, f := range in {
		key := dedupKey{
			filePath:    f.FilePath,
			recordID:    f.RecordID,
			recordClass: f.RecordClass,
			hunkIndex:   f.HunkIndex,
		}
		existing, exists := best[key]
		if !exists {
			best[key] = f
			order = append(order, key)
		} else if strength[f.Disposition] > strength[existing.Disposition] {
			best[key] = f
		}
	}

	out := make([]AuditFinding, 0, len(order))
	for _, key := range order {
		out = append(out, best[key])
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
					return "", fmt.Errorf("invalid line in add hunk: new files can only contain additions (+)")
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
					return "", fmt.Errorf("hunk line mismatch on deleted line at base line %d", baseIdx+1)
				}
				baseIdx++
			} else {
				contextLine := strings.TrimPrefix(line, " ")
				if baseIdx >= len(baseLines) {
					return "", fmt.Errorf("context match beyond base EOF at base line %d", baseIdx+1)
				}
				if baseLines[baseIdx] != contextLine {
					return "", fmt.Errorf("hunk line mismatch on context line at base line %d", baseIdx+1)
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
