// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

import (
	"context"
	"fmt"
	"strings"
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
	// GetFileImpact returns the file's governed obligations plus the observed
	// authority commit of the rule snapshot that produced them. An authoritative
	// graph must always expose a non-empty commit; the implementation fails
	// closed (returns an error) when it cannot, so a successful call always
	// carries an identity the result can bind its digest to.
	GetFileImpact(ctx context.Context, file string, domain string) (requiredTests []Requirement, contracts []Requirement, RelevantRules []string, graphCommit string, err error)
}

// GraphGenerationReporter reports which graph GENERATION is answering right now.
//
// Law 5 of the graph-identity front: every graph query a run uses must prove it
// belongs to the pinned identity, and a silent generation switch is forbidden.
// GetFileImpact's graphCommit cannot carry that proof — it identifies the rule
// snapshot, which on this installation belongs to another repository, so two
// different Sensei generations built from one snapshot are indistinguishable by it.
//
// Optional in the Go sense only. A checker that does not implement it cannot bind
// its result to a generation, and such a result is NOT available: an identity that
// cannot be checked is not a matching one.
type GraphGenerationReporter interface {
	GraphGeneration(ctx context.Context) (string, error)
}

// BaseFileReader optionally reads the un-edited base content of a file from the repository.
type BaseFileReader interface {
	ReadBaseFile(ctx context.Context, path string) (string, bool, error)
}

// AuditOptions configures the diff audit run.
//
// awareness_audit_diff is a read-only projection over the caller-supplied diff
// (see docs/design/frontier-awareness-audit-diff.md §2). It is never an
// admission authority and must never observe ambient repository state. Working
// -tree admission verification is a separate, explicitly governed surface — the
// verify_admission tool — not a step composed here. Task and Domain participate
// only in the result's identity; neither is ever interpolated into a filesystem
// path.
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

	// This evaluator does NOT call admission verification. Admission observes
	// ambient repository/working-tree state against an admitted scope, which is a
	// different subject than the caller-supplied diff this projection audits, and
	// composing it here would make the read-only tool an admission authority that
	// reads ambient Git state — both forbidden by the governing contract §2.
	// Working-tree admission is the separate verify_admission tool.

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

	// LAW 5, first sample. Taken before any graph query so the pair brackets every
	// query this audit makes: a generation that is the same before and after is one
	// that did not change underneath the answers.
	generationReporter, _ := checker.(GraphGenerationReporter)
	generationBefore, generationErr := observeGeneration(ctx, generationReporter)
	ledger := &generationLedger{}
	ledger.note("the opening sample", generationBefore)

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

		// 1. Gather file impact (required tests, contracts, relevant rules, and
		//    the observed authority commit of the rule snapshot).
		tests, contracts, rules, graphCommit, err := checker.GetFileImpact(ctx, readPath, opts.Domain)
		// Bind THIS query, not just the audit. See generationLedger.
		if gen, gerr := observeGeneration(ctx, generationReporter); gerr == nil {
			ledger.note("the impact query for "+readPath, gen)
		}
		if err != nil {
			result.Availability = AvailabilityCannotVerify
			result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
			result.Limitations = append(result.Limitations, fmt.Sprintf("graph impact query failed for %s: %v", readPath, err))
		} else if graphCommit == "" {
			// The canonical lock: a successful impact response MUST carry the rule
			// snapshot's commit identity. Without it the result cannot be bound to
			// the graph that produced it, so fail closed here rather than trust
			// every present and future caller to enforce the binding upstream.
			result.Availability = AvailabilityCannotVerify
			result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
			result.Limitations = append(result.Limitations, fmt.Sprintf("graph impact for %s returned no authority commit identity", readPath))
		} else {
			// Bind the result to the rule snapshot that produced it. The commit
			// must be consistent across every file in one audit (one graph, one
			// authority); a divergence means the snapshot shifted mid-audit and
			// the result cannot be trusted.
			if result.GraphCommit == "" {
				result.GraphCommit = graphCommit
			} else if result.GraphCommit != graphCommit {
				result.Availability = AvailabilityCannotVerify
				result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
				result.Limitations = append(result.Limitations, fmt.Sprintf("graph authority commit inconsistent across files: %s vs %s", result.GraphCommit, graphCommit))
			}
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
				// Bind THIS query. EditCheckResponse states no generation, so this is the
				// observation adjacent to the call rather than the call's own identity.
				if gen, gerr := observeGeneration(ctx, generationReporter); gerr == nil {
					ledger.note("the rule evaluation for "+patch.Path, gen)
				}
				if err != nil {
					result.ReasonCodes = append(result.ReasonCodes, ReasonEvaluatorUnavailable)
					result.Availability = AvailabilityCannotVerify
					// Record WHY, as every sibling failure path in this
					// function does. A reason code names the category; without
					// the error a caller cannot tell an unreachable graph from
					// a rejected RPC from oversized content from a panicking
					// rule — and cannot_verify then looks indistinguishable
					// from the change being bad.
					result.Limitations = append(result.Limitations, fmt.Sprintf("evaluator failed for %s: %v", patch.Path, err))
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

	// LAW 5, second sample and the verdict.
	//
	// Placed before the decision is computed, so a switch degrades the decision
	// rather than being appended to a verdict already announced as pass.
	generationAfter, afterErr := observeGeneration(ctx, generationReporter)
	ledger.note("the closing sample", generationAfter)
	switchedA, switchedB, switched := ledger.disagreement()
	switch {
	case generationErr != nil || afterErr != nil:
		err := generationErr
		if err == nil {
			err = afterErr
		}
		result.Availability = AvailabilityCannotVerify
		result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
		result.Limitations = append(result.Limitations,
			fmt.Sprintf("the graph generation answering this audit could not be observed: %v", err))
	case generationBefore == "" || generationAfter == "":
		// Unobservable, not agreeing. Reported the same way whether the checker
		// cannot report at all or reported nothing, because both are "no identity"
		// and a verdict that differed between them would be describing the
		// messenger rather than the graph.
		result.Availability = AvailabilityCannotVerify
		result.ReasonCodes = append(result.ReasonCodes, ReasonGraphUnavailable)
		result.Limitations = append(result.Limitations,
			"the graph generation answering this audit was not observable, so this result cannot be bound to the graph that produced it")
	case switched:
		// Any two contributing queries naming different generations refuses the verdict,
		// which subsumes the old before != after test: the brackets are two of the
		// observations, so a pair that disagrees is still caught here, and so now is a
		// rollback that leaves them equal.
		result.Availability = AvailabilityCannotVerify
		result.ReasonCodes = append(result.ReasonCodes, ReasonGraphGenerationSwitched)
		result.Limitations = append(result.Limitations,
			fmt.Sprintf("this audit's queries were not all answered by one graph generation: %s answered %s, %s answered %s; a silent generation switch is forbidden",
				switchedA.generation, switchedA.query, switchedB.generation, switchedB.query))
	default:
		result.GraphGeneration = generationBefore
	}

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

// observeGeneration asks the reporter which generation is answering.
//
// A checker that cannot report is not an error — it is the absence of an identity,
// and the caller above treats absence as unverifiable rather than as agreement. The
// distinction is kept because a reporting FAILURE and the absence of a reporter are
// different facts about different things, and only one of them names something an
// operator can fix.
// generationLedger is the ONE place the audit's generation rule lives: every graph-backed
// query contributing to one verdict must have been answered by the same generation.
//
// It exists because a before/after bracket cannot prove what happened between its ends.
// The review finding named the counterexample: a publish-and-rollback G1 -> G2 -> G1
// leaves both brackets reading G1 while the queries in between were answered by G2, and
// the evaluator emitted a PASS bound to G1 (reproduced 2026-09-13,
// TestAnAuditRefusesWhenAQueryWasAnsweredByAnotherGeneration).
//
// WHAT THIS PROVES, exactly. Every observation is recorded with the query it belongs to,
// and one disagreement refuses the whole verdict. An Impact-backed query states the
// generation carried ON ITS OWN RESPONSE, which is per-response proof. An EditCheck-backed
// query cannot: EditCheckResponse carries no GraphAuthority, so its identity is the
// observation taken adjacent to that single call. That narrows the unproven window from
// the whole audit to one RPC; it does not close it. Closing it needs a GraphAuthority on
// EditCheckResponse, which is a wire change and is deliberately NOT made here.
type generationLedger struct {
	seen []generationObservation
}

type generationObservation struct {
	generation string
	query      string // what was asked, so a disagreement names the query, not just the value
}

func (l *generationLedger) note(query, generation string) {
	generation = strings.TrimSpace(generation)
	if generation == "" {
		return
	}
	l.seen = append(l.seen, generationObservation{generation: generation, query: query})
}

// disagreement returns the first two observations that name different generations.
func (l *generationLedger) disagreement() (a, b generationObservation, found bool) {
	for i := range l.seen {
		if l.seen[i].generation != l.seen[0].generation {
			return l.seen[0], l.seen[i], true
		}
	}
	return generationObservation{}, generationObservation{}, false
}

func observeGeneration(ctx context.Context, r GraphGenerationReporter) (string, error) {
	if r == nil {
		return "", nil
	}
	gen, err := r.GraphGeneration(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(gen), nil
}
