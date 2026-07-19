// SPDX-License-Identifier: Apache-2.0

package main

// Frozen contract-set support for `sensei gate` (Phase-2, PR1).
//
// This is a SEPARATE, self-contained code path from the default `sensei gate`
// (EditCheck/gRPC) flow: it activates only when --contracts is passed, never
// touches the Sensei server, and leaves default behavior unchanged. It proves the
// mechanical core of Phase-2 — a frozen, human-authored contract can be checked
// against a diff WITHOUT reading the gold patch — by evaluating each contract's
// detect rule over the diff's added/changed lines only.
//
// Scope (PR1): the only detect type implemented is `regex_forbidden`. Other
// detect types load fine but evaluate to `not_applicable` with a note, so a
// contract we cannot yet check is never silently reported as respected.
// See docs/design/contract-first-phase2-experiment.md.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Frozen contract-set schema (subset; PR1) ────────────────────────────────

type contractGoverns struct {
	Files   []string `yaml:"files"`
	Symbols []string `yaml:"symbols"`
}

type contractScope struct {
	Files    []string `yaml:"files"`
	Behavior []string `yaml:"behavior"`
}

type contractDetect struct {
	Type    string `yaml:"type"`
	Pattern string `yaml:"pattern"`
	Message string `yaml:"message"`
}

type contractConfidence struct {
	ScopePrecision        string `yaml:"scope_precision"`
	RequiredPathsCoverage string `yaml:"required_paths_coverage"`
}

type contractProof struct {
	ProofRequired       bool     `yaml:"proof_required"`
	RequiredTestPaths   []string `yaml:"required_test_paths"`
	RequiredTestSymbols []string `yaml:"required_test_symbols"`
	NoNewTestsMeans     string   `yaml:"no_new_tests_means"`
}

type requiredPath struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Severity    string `yaml:"severity,omitempty"`
}

func (p *requiredPath) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var description string
		if err := node.Decode(&description); err != nil {
			return err
		}
		p.Description = description
		return nil
	case yaml.MappingNode:
		type raw requiredPath
		var decoded raw
		if err := node.Decode(&decoded); err != nil {
			return err
		}
		*p = requiredPath(decoded)
		return nil
	default:
		return fmt.Errorf("required_paths entry must be string or mapping")
	}
}

type frozenContract struct {
	ID                  string             `yaml:"id"`
	Kind                string             `yaml:"kind"`
	Confidence          string             `yaml:"confidence"`
	Statement           string             `yaml:"statement"`
	Governs             contractGoverns    `yaml:"governs"`
	Detect              contractDetect     `yaml:"detect"`
	RequiredScope       contractScope      `yaml:"required_scope"`
	AllowedRelatedScope contractScope      `yaml:"allowed_related_scope"`
	OutOfScope          contractScope      `yaml:"out_of_scope"`
	RequiredPaths       []requiredPath     `yaml:"required_paths"`
	MustNotChange       []string           `yaml:"must_not_change"`
	ScopeConfidence     contractConfidence `yaml:"scope_confidence"`
	Proof               contractProof      `yaml:",inline"`
}

type contractSet struct {
	Version   int              `yaml:"contract_set_version"`
	TaskID    string           `yaml:"task_id"`
	Repo      string           `yaml:"repo"`
	Contracts []frozenContract `yaml:"contracts"`
}

// ── Verdicts + report (stable JSON for the future scoring harness) ──────────

type contractEvidence struct {
	File    string `json:"file"`
	Line    int    `json:"line"` // 1-based index within the file's added/changed lines
	Matched string `json:"matched"`
	Message string `json:"message,omitempty"`
}

type contractVerdict struct {
	TaskID                     string                 `json:"task_id,omitempty"`
	ID                         string                 `json:"id"`
	Kind                       string                 `json:"kind,omitempty"`
	Verdict                    string                 `json:"verdict"` // respected | violated | not_applicable
	ApplicableFiles            []string               `json:"applicable_files,omitempty"`
	Evidence                   *contractEvidence      `json:"evidence,omitempty"`
	Note                       string                 `json:"note,omitempty"`
	ContractStatus             string                 `json:"contract_status,omitempty"`
	ScopeStatus                string                 `json:"scope_status,omitempty"`
	Confidence                 string                 `json:"confidence,omitempty"`
	ScopeBroadeningDetected    bool                   `json:"scope_broadening_detected,omitempty"`
	ContractClean              bool                   `json:"contract_clean"`
	ContractFailureReason      string                 `json:"contract_failure_reason"`
	AllowedChangedFiles        []string               `json:"allowed_changed_files,omitempty"`
	ActualChangedFiles         []string               `json:"actual_changed_files,omitempty"`
	LeakedFiles                []string               `json:"leaked_files,omitempty"`
	ProofRequired              bool                   `json:"proof_required,omitempty"`
	ProofStatus                string                 `json:"proof_status,omitempty"`
	RequiredTestPaths          []string               `json:"required_test_paths,omitempty"`
	MissingRequiredTestPaths   []string               `json:"missing_required_test_paths,omitempty"`
	RequiredTestSymbols        []string               `json:"required_test_symbols,omitempty"`
	MissingRequiredTestSymbols []string               `json:"missing_required_test_symbols,omitempty"`
	EditedFileClassification   map[string]string      `json:"edited_file_classification,omitempty"`
	BlindSpots                 []string               `json:"blind_spots,omitempty"`
	Warnings                   []contractScopeWarning `json:"warnings,omitempty"`
}

type contractSummary struct {
	Contracts     int `json:"contracts"`
	Respected     int `json:"respected"`
	Violated      int `json:"violated"`
	NotApplicable int `json:"not_applicable"`
	ScopeWarnings int `json:"scope_warnings,omitempty"`
}

type gateContractsReport struct {
	Mode     string            `json:"mode"`
	Diff     string            `json:"diff"`
	Enforce  bool              `json:"enforce"`
	Verdicts []contractVerdict `json:"contracts"`
	Summary  contractSummary   `json:"summary"`
}

const (
	verdictRespected     = "respected"
	verdictViolated      = "violated"
	verdictNotApplicable = "not_applicable"
)

type contractScopeWarning struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Files   []string `json:"files,omitempty"`
}

type scopeAssessment struct {
	Warnings                   []contractScopeWarning
	BlindSpots                 []string
	ScopeBroadening            bool
	AllowedChangedFiles        []string
	ActualChangedFiles         []string
	LeakedFiles                []string
	EditedFileClassification   map[string]string
	ContractFailureReason      string
	ContractClean              bool
	ProofRequired              bool
	ProofStatus                string
	RequiredTestPaths          []string
	MissingRequiredTestPaths   []string
	RequiredTestSymbols        []string
	MissingRequiredTestSymbols []string
}

// ── Glob matching (minimal `**`-aware; no external dependency) ──────────────

// globToRegexp converts a path glob to an anchored regexp. `*` matches within a
// single path segment; `**` crosses segments; `/**/` matches zero or more
// segments (so "a/**/b" matches both "a/b" and "a/x/y/b").
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); {
		switch {
		case strings.HasPrefix(glob[i:], "/**/"):
			b.WriteString("/(?:.*/)?")
			i += 4
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 3
		case strings.HasPrefix(glob[i:], "/**"):
			b.WriteString("(?:/.*)?")
			i += 3
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i += 2
		case glob[i] == '*':
			b.WriteString("[^/]*")
			i++
		case glob[i] == '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// matchesAnyGlob reports whether path matches any of the globs.
func matchesAnyGlob(path string, globs []string) (bool, error) {
	for _, g := range globs {
		re, err := globToRegexp(g)
		if err != nil {
			return false, fmt.Errorf("bad governs glob %q: %w", g, err)
		}
		if re.MatchString(path) {
			return true, nil
		}
	}
	return false, nil
}

// ── Loading ─────────────────────────────────────────────────────────────────

// loadContractSets reads a single YAML file or every *.yaml/*.yml in a directory.
func loadContractSets(path string) ([]contractSet, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".yaml" || ext == ".yml" {
				files = append(files, filepath.Join(path, e.Name()))
			}
		}
		sort.Strings(files)
	} else {
		files = []string{path}
	}
	sets := make([]contractSet, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var cs contractSet
		if err := yaml.Unmarshal(data, &cs); err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		sets = append(sets, cs)
	}
	return sets, nil
}

// ── Evaluation (pure: no I/O, exhaustively testable) ────────────────────────

// evaluateContracts checks every contract in every set against the diff's
// added/changed lines (changes: repo-relative file -> "\n"-joined added lines).
// It is the testable core of the contract gate.
func evaluateContracts(changes map[string]string, sets []contractSet) ([]contractVerdict, error) {
	return evaluateContractsWithFiles(changes, nil, sets)
}

func evaluateContractsWithFiles(changes map[string]string, actualChangedFiles []string, sets []contractSet) ([]contractVerdict, error) {
	changedFiles := make([]string, 0, len(changes))
	for f := range changes {
		changedFiles = append(changedFiles, f)
	}
	sort.Strings(changedFiles)
	if len(actualChangedFiles) == 0 {
		actualChangedFiles = append([]string{}, changedFiles...)
	} else {
		actualChangedFiles = append([]string{}, actualChangedFiles...)
		sort.Strings(actualChangedFiles)
	}

	var verdicts []contractVerdict
	for _, set := range sets {
		for _, c := range set.Contracts {
			v := contractVerdict{
				TaskID:         set.TaskID,
				ID:             c.ID,
				Kind:           c.Kind,
				ContractStatus: "found",
				Confidence:     c.Confidence,
				ScopeStatus:    "high",
			}
			scope := assessContractScope(c, actualChangedFiles)
			if len(scope.Warnings) > 0 {
				v.Warnings = scope.Warnings
				v.ScopeStatus = "underconstrained"
			}
			if len(scope.BlindSpots) > 0 {
				v.BlindSpots = scope.BlindSpots
			}
			v.ScopeBroadeningDetected = scope.ScopeBroadening
			v.ContractClean = scope.ContractClean
			v.ContractFailureReason = scope.ContractFailureReason
			v.AllowedChangedFiles = scope.AllowedChangedFiles
			v.ActualChangedFiles = scope.ActualChangedFiles
			v.LeakedFiles = scope.LeakedFiles
			v.ProofRequired = scope.ProofRequired
			v.ProofStatus = scope.ProofStatus
			v.RequiredTestPaths = scope.RequiredTestPaths
			v.MissingRequiredTestPaths = scope.MissingRequiredTestPaths
			v.RequiredTestSymbols = scope.RequiredTestSymbols
			v.MissingRequiredTestSymbols = scope.MissingRequiredTestSymbols
			v.EditedFileClassification = scope.EditedFileClassification
			if c.ScopeConfidence.ScopePrecision == "low" || c.ScopeConfidence.RequiredPathsCoverage == "low" {
				if len(v.Warnings) == 0 {
					v.Warnings = append(v.Warnings, contractScopeWarning{
						Code:    "contract_scope_underconstrained",
						Message: "Contract found, but scope is underconstrained. The repair may broaden beyond the reported defect.",
					})
				}
				v.ScopeStatus = "underconstrained"
			} else if c.ScopeConfidence.ScopePrecision == "medium" || c.ScopeConfidence.RequiredPathsCoverage == "medium" {
				if v.ScopeStatus == "high" {
					v.ScopeStatus = "medium"
				}
			}

			// Which changed files does this contract govern?
			var applicable []string
			for _, f := range changedFiles {
				ok, err := matchesAnyGlob(f, c.Governs.Files)
				if err != nil {
					return nil, fmt.Errorf("contract %q: %w", c.ID, err)
				}
				if ok {
					applicable = append(applicable, f)
				}
			}
			if len(applicable) == 0 {
				v.Verdict = verdictNotApplicable
				verdicts = append(verdicts, v)
				continue
			}
			v.ApplicableFiles = applicable

			// PR1 supports only regex_forbidden. Anything else is reported as
			// not_applicable WITH a note — never a silent "respected".
			if c.Detect.Type != "regex_forbidden" {
				v.Verdict = verdictNotApplicable
				v.Note = fmt.Sprintf("detect.type %q not supported in this build", c.Detect.Type)
				verdicts = append(verdicts, v)
				continue
			}
			re, err := regexp.Compile(c.Detect.Pattern)
			if err != nil {
				return nil, fmt.Errorf("contract %q: bad detect.pattern: %w", c.ID, err)
			}

			// regex_forbidden: a match in any governed file's added lines is a
			// violation. The detect rule reads the DIFF, never the gold patch.
			matched := false
			for _, f := range applicable {
				for i, line := range strings.Split(changes[f], "\n") {
					if re.MatchString(line) {
						v.Verdict = verdictViolated
						v.Evidence = &contractEvidence{
							File:    f,
							Line:    i + 1,
							Matched: line,
							Message: c.Detect.Message,
						}
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				v.Verdict = verdictRespected
			}
			verdicts = append(verdicts, v)
		}
	}

	sort.SliceStable(verdicts, func(i, j int) bool {
		if verdicts[i].TaskID != verdicts[j].TaskID {
			return verdicts[i].TaskID < verdicts[j].TaskID
		}
		return verdicts[i].ID < verdicts[j].ID
	})
	return verdicts, nil
}

// buildContractReport assembles the stable report (used for JSON output + tests).
func buildContractReport(diff string, enforce bool, verdicts []contractVerdict) gateContractsReport {
	s := contractSummary{Contracts: len(verdicts)}
	for _, v := range verdicts {
		switch v.Verdict {
		case verdictRespected:
			s.Respected++
		case verdictViolated:
			s.Violated++
		default:
			s.NotApplicable++
		}
		s.ScopeWarnings += len(v.Warnings)
	}
	return gateContractsReport{
		Mode:     "contract-gate",
		Diff:     diff,
		Enforce:  enforce,
		Verdicts: verdicts,
		Summary:  s,
	}
}

func assessContractScope(c frozenContract, changedFiles []string) scopeAssessment {
	result := scopeAssessment{
		AllowedChangedFiles:      append(append([]string{}, c.RequiredScope.Files...), c.AllowedRelatedScope.Files...),
		ActualChangedFiles:       append([]string{}, changedFiles...),
		EditedFileClassification: map[string]string{},
		ContractClean:            true,
		ProofRequired:            c.Proof.ProofRequired,
		ProofStatus:              "not_required",
		RequiredTestPaths:        append([]string{}, c.Proof.RequiredTestPaths...),
		RequiredTestSymbols:      append([]string{}, c.Proof.RequiredTestSymbols...),
	}

	if len(c.RequiredScope.Files) == 0 {
		result.BlindSpots = append(result.BlindSpots, "required_scope_missing")
	}
	if len(c.OutOfScope.Files) == 0 {
		result.BlindSpots = append(result.BlindSpots, "out_of_scope_missing")
	}
	if len(c.RequiredPaths) == 0 {
		result.BlindSpots = append(result.BlindSpots, "required_paths_incomplete")
	}
	if len(result.BlindSpots) > 0 {
		result.Warnings = append(result.Warnings, contractScopeWarning{
			Code:    "contract_scope_underconstrained",
			Message: "Contract found, but scope is underconstrained. The repair may broaden beyond the reported defect.",
		})
	}

	declaredAllowed := append([]string{}, c.RequiredScope.Files...)
	declaredAllowed = append(declaredAllowed, c.AllowedRelatedScope.Files...)
	seenWarnings := map[string]bool{}
	for _, f := range changedFiles {
		if isArtifactLeak(f) {
			result.LeakedFiles = append(result.LeakedFiles, f)
			result.EditedFileClassification[f] = "repo_artifact_leak"
			addScopeWarning(&result.Warnings, seenWarnings, contractScopeWarning{
				Code:    "repo_artifact_leak",
				Message: "Changed path looks like a contract/log artifact rather than source repair content.",
				Files:   []string{f},
			})
			continue
		}
		inAllowed := false
		if len(declaredAllowed) > 0 {
			ok, err := matchesAnyGlob(f, declaredAllowed)
			if err == nil {
				inAllowed = ok
			}
		}

		inOutOfScope := false
		if len(c.OutOfScope.Files) > 0 {
			ok, err := matchesAnyGlob(f, c.OutOfScope.Files)
			if err == nil {
				inOutOfScope = ok
			}
		}
		if inOutOfScope {
			result.ScopeBroadening = true
			result.EditedFileClassification[f] = "out_of_scope_drift"
			addScopeWarning(&result.Warnings, seenWarnings, contractScopeWarning{
				Code:    "scope_broadening_detected",
				Message: "Edited file is explicitly out of scope for this contract.",
				Files:   []string{f},
			})
			continue
		}
		if len(declaredAllowed) > 0 && !inAllowed {
			result.ScopeBroadening = true
			result.EditedFileClassification[f] = "unknown_needs_review"
			addScopeWarning(&result.Warnings, seenWarnings, contractScopeWarning{
				Code:    "edited_files_outside_declared_scope",
				Message: "Edited file is outside required_scope and allowed_related_scope.",
				Files:   []string{f},
			})
			continue
		}
		if matches, _ := matchesAnyGlob(f, c.RequiredScope.Files); matches {
			result.EditedFileClassification[f] = "required_scope"
		} else {
			result.EditedFileClassification[f] = "allowed_related_scope"
		}
	}

	switch {
	case len(result.LeakedFiles) > 0:
		result.ContractFailureReason = "repo_artifact_leak"
		result.ContractClean = false
	case result.ScopeBroadening:
		result.ContractFailureReason = "out_of_scope_edit"
		result.ContractClean = false
	}
	assessContractProof(c, changedFiles, &result, seenWarnings)
	sort.Strings(result.AllowedChangedFiles)
	sort.Strings(result.LeakedFiles)
	sort.Strings(result.RequiredTestPaths)
	sort.Strings(result.RequiredTestSymbols)
	sort.Strings(result.MissingRequiredTestPaths)
	sort.Strings(result.MissingRequiredTestSymbols)
	return result
}

func assessContractProof(c frozenContract, changedFiles []string, result *scopeAssessment, seenWarnings map[string]bool) {
	if !c.Proof.ProofRequired {
		return
	}
	result.ProofStatus = "complete"
	for _, path := range c.Proof.RequiredTestPaths {
		matched := false
		for _, changed := range changedFiles {
			ok, err := matchesAnyGlob(changed, []string{path})
			if err == nil && ok {
				matched = true
				break
			}
		}
		if !matched {
			result.MissingRequiredTestPaths = append(result.MissingRequiredTestPaths, path)
		}
	}
	for _, symbol := range c.Proof.RequiredTestSymbols {
		// Symbol grounding is not available in the frozen gate yet; report it as
		// missing proof so higher layers can keep certification partial.
		result.MissingRequiredTestSymbols = append(result.MissingRequiredTestSymbols, symbol)
	}
	if len(result.MissingRequiredTestPaths) == 0 && len(result.MissingRequiredTestSymbols) == 0 {
		return
	}
	result.ProofStatus = "incomplete"
	addScopeWarning(&result.Warnings, seenWarnings, contractScopeWarning{
		Code:    "required_test_proof_missing",
		Message: "Behavior-changing repair requires explicit test proof, but the required test edits are missing.",
		Files:   append([]string{}, result.MissingRequiredTestPaths...),
	})
	if c.Proof.NoNewTestsMeans == "test_proof_incomplete" && result.ContractFailureReason == "" {
		result.ContractFailureReason = "test_proof_incomplete"
		result.ContractClean = false
	}
}

func isArtifactLeak(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "contract_block.json", "verification.md", "revision_request.md", "gate.json", "gate.err", "gate.out", "agent_gate.json":
		return true
	}
	if strings.HasPrefix(path, "artifacts/") || strings.HasPrefix(path, ".awg-artifacts/") {
		return true
	}
	return strings.HasSuffix(base, ".log") || strings.HasSuffix(base, ".err") || strings.HasSuffix(base, ".out")
}

func addScopeWarning(dst *[]contractScopeWarning, seen map[string]bool, warning contractScopeWarning) {
	key := warning.Code + "|" + strings.Join(warning.Files, ",")
	if seen[key] {
		return
	}
	seen[key] = true
	*dst = append(*dst, warning)
}

// gateContractsExitCode: non-zero ONLY when --enforce is set and a contract is
// violated. Without --enforce the gate is report-only (always 0).
func gateContractsExitCode(verdicts []contractVerdict, enforce bool) int {
	if !enforce {
		return 0
	}
	for _, v := range verdicts {
		if v.Verdict == verdictViolated {
			return 1
		}
	}
	return 0
}

// ── Command entry (wired from runGate when --contracts is set) ──────────────

func runGateContracts(repoRoot, diff, contractsPath string, enforce, asJSON bool) int {
	sets, err := loadContractSets(contractsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei gate: load contracts: %v\n", err)
		return 2
	}
	changes, err := gitAddedLinesByFile(repoRoot, diff)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei gate: %v\n", err)
		return 2
	}
	verdicts, err := evaluateContractsWithFiles(changes, gitChangedFiles(repoRoot), sets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei gate: %v\n", err)
		return 2
	}
	report := buildContractReport(diff, enforce, verdicts)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printContractReport(report)
	}
	return gateContractsExitCode(verdicts, enforce)
}

func printContractReport(r gateContractsReport) {
	mode := "report-only"
	if r.Enforce {
		mode = "enforce"
	}
	fmt.Printf("Sensei gate (frozen contracts, %s) — diff %s\n", mode, r.Diff)
	fmt.Printf("  contracts: %d   respected: %d   violated: %d   not_applicable: %d   scope_warnings: %d\n\n",
		r.Summary.Contracts, r.Summary.Respected, r.Summary.Violated, r.Summary.NotApplicable, r.Summary.ScopeWarnings)
	for _, v := range r.Verdicts {
		label := v.ID
		if v.TaskID != "" {
			label = v.TaskID + " / " + v.ID
		}
		fmt.Printf("  [%s] %s\n", strings.ToUpper(v.Verdict), label)
		if v.Note != "" {
			fmt.Printf("      note: %s\n", v.Note)
		}
		if v.ScopeStatus != "" {
			fmt.Printf("      scope_status: %s\n", v.ScopeStatus)
		}
		for _, w := range v.Warnings {
			fmt.Printf("      warning[%s]: %s\n", w.Code, w.Message)
			if len(w.Files) > 0 {
				fmt.Printf("      files: %s\n", strings.Join(w.Files, ", "))
			}
		}
		if v.Evidence != nil {
			fmt.Printf("      %s:+%d  %s\n", v.Evidence.File, v.Evidence.Line, strings.TrimSpace(v.Evidence.Matched))
			if v.Evidence.Message != "" {
				fmt.Printf("      %s\n", v.Evidence.Message)
			}
		}
	}
	if r.Enforce && r.Summary.Violated > 0 {
		fmt.Printf("\n%d contract violation(s) — gate FAILED (--enforce).\n", r.Summary.Violated)
	} else {
		fmt.Printf("\n%d violation(s) (report-only — exit 0).\n", r.Summary.Violated)
	}
}
