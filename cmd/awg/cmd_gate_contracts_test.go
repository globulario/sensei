// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sampleSets returns one frozen contract governing runtime_proof.go with a
// regex_forbidden detect rule.
func sampleSets() []contractSet {
	return []contractSet{{
		Version: 1,
		TaskID:  "t1",
		Contracts: []frozenContract{{
			ID:        "contract.state.runtime_not_desired",
			Kind:      "invariant",
			Statement: "runtime health is not desired state",
			Governs:   contractGoverns{Files: []string{"golang/node_agent/**/runtime_proof.go"}},
			Detect: contractDetect{
				Type:    "regex_forbidden",
				Pattern: `status\s*=\s*Available`,
				Message: "marks Available without runtime proof",
			},
		}},
	}}
}

// Test 1: a diff that changes a governed file and matches regex_forbidden must
// report `violated`.
func TestEvaluateContracts_GovernedMatch_Violated(t *testing.T) {
	changes := map[string]string{
		"golang/node_agent/node_agent_server/runtime_proof.go": "ok line\n\tstatus = Available // bad\nmore",
	}
	verdicts, err := evaluateContracts(changes, sampleSets())
	if err != nil {
		t.Fatalf("evaluateContracts: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(verdicts))
	}
	v := verdicts[0]
	if v.Verdict != verdictViolated {
		t.Fatalf("want violated, got %q", v.Verdict)
	}
	if v.Evidence == nil || v.Evidence.Line != 2 {
		t.Fatalf("want evidence on added-line 2, got %+v", v.Evidence)
	}
	if v.Evidence.File != "golang/node_agent/node_agent_server/runtime_proof.go" {
		t.Fatalf("evidence file wrong: %q", v.Evidence.File)
	}
}

// Test 2: a diff that changes a governed file but does not match must report
// `respected`.
func TestEvaluateContracts_GovernedNoMatch_Respected(t *testing.T) {
	changes := map[string]string{
		"golang/node_agent/node_agent_server/runtime_proof.go": "status = Installed\nstatus = Running",
	}
	verdicts, err := evaluateContracts(changes, sampleSets())
	if err != nil {
		t.Fatalf("evaluateContracts: %v", err)
	}
	if len(verdicts) != 1 || verdicts[0].Verdict != verdictRespected {
		t.Fatalf("want one respected verdict, got %+v", verdicts)
	}
	if verdicts[0].Evidence != nil {
		t.Fatalf("respected verdict must carry no evidence, got %+v", verdicts[0].Evidence)
	}
}

// Test 3: a diff that does not touch governed files must report `not_applicable`.
func TestEvaluateContracts_UngovernedFile_NotApplicable(t *testing.T) {
	changes := map[string]string{
		"golang/other/place.go": "status = Available", // would match, but file not governed
	}
	verdicts, err := evaluateContracts(changes, sampleSets())
	if err != nil {
		t.Fatalf("evaluateContracts: %v", err)
	}
	if len(verdicts) != 1 || verdicts[0].Verdict != verdictNotApplicable {
		t.Fatalf("want one not_applicable verdict, got %+v", verdicts)
	}
	if len(verdicts[0].ApplicableFiles) != 0 {
		t.Fatalf("not_applicable must have no applicable files, got %+v", verdicts[0].ApplicableFiles)
	}
}

// An unsupported detect type over a governed file must be not_applicable WITH a
// note — never a silent "respected".
func TestEvaluateContracts_UnsupportedDetect_NotApplicableWithNote(t *testing.T) {
	sets := sampleSets()
	sets[0].Contracts[0].Detect.Type = "ruleguard"
	changes := map[string]string{
		"golang/node_agent/node_agent_server/runtime_proof.go": "status = Available",
	}
	verdicts, err := evaluateContracts(changes, sets)
	if err != nil {
		t.Fatalf("evaluateContracts: %v", err)
	}
	if verdicts[0].Verdict != verdictNotApplicable || verdicts[0].Note == "" {
		t.Fatalf("want not_applicable with a note, got %+v", verdicts[0])
	}
}

// Test 4 & 5: --enforce fails only on a violation; without --enforce, violations
// are report-only (exit 0).
func TestGateContractsExitCode(t *testing.T) {
	violated := []contractVerdict{{ID: "c", Verdict: verdictViolated}}
	respected := []contractVerdict{{ID: "c", Verdict: verdictRespected}}
	notApplic := []contractVerdict{{ID: "c", Verdict: verdictNotApplicable}}

	if got := gateContractsExitCode(violated, true); got != 1 {
		t.Fatalf("enforce + violated: want exit 1, got %d", got)
	}
	if got := gateContractsExitCode(respected, true); got != 0 {
		t.Fatalf("enforce + respected: want exit 0, got %d", got)
	}
	if got := gateContractsExitCode(notApplic, true); got != 0 {
		t.Fatalf("enforce + not_applicable: want exit 0, got %d", got)
	}
	if got := gateContractsExitCode(violated, false); got != 0 {
		t.Fatalf("no enforce + violated: want report-only exit 0, got %d", got)
	}
}

// Test 6: JSON output is stable (deterministic field order + values) for the
// future scoring harness.
func TestBuildContractReport_StableJSON(t *testing.T) {
	verdicts := []contractVerdict{{
		TaskID:          "t1",
		ID:              "contract.x",
		Kind:            "invariant",
		Verdict:         verdictViolated,
		ApplicableFiles: []string{"a/runtime_proof.go"},
		Evidence: &contractEvidence{
			File:    "a/runtime_proof.go",
			Line:    2,
			Matched: "status = Available",
			Message: "no available from local",
		},
	}}
	report := buildContractReport("HEAD", true, verdicts)

	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "mode": "contract-gate",
  "diff": "HEAD",
  "enforce": true,
  "contracts": [
    {
      "task_id": "t1",
      "id": "contract.x",
      "kind": "invariant",
      "verdict": "violated",
      "applicable_files": [
        "a/runtime_proof.go"
      ],
      "evidence": {
        "file": "a/runtime_proof.go",
        "line": 2,
        "matched": "status = Available",
        "message": "no available from local"
      },
      "contract_clean": false,
      "contract_failure_reason": ""
    }
  ],
  "summary": {
    "contracts": 1,
    "respected": 0,
    "violated": 1,
    "not_applicable": 0
  }
}`
	if string(got) != want {
		t.Fatalf("unstable JSON.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}

	// Determinism: marshaling twice yields identical bytes.
	got2, _ := json.MarshalIndent(report, "", "  ")
	if string(got) != string(got2) {
		t.Fatalf("non-deterministic JSON across marshals")
	}
}

func TestEvaluateContracts_UnderconstrainedContractScopeWarning(t *testing.T) {
	sets := []contractSet{{
		Version: 1,
		TaskID:  "cli__cli-1337",
		Contracts: []frozenContract{{
			ID:         "cli.issue_close_status_includes_title",
			Kind:       "invariant",
			Confidence: "explicit",
			Statement:  "Issue close output must include the issue title.",
			Governs: contractGoverns{
				Files: []string{"command/*.go"},
			},
			Detect: contractDetect{
				Type:    "regex_forbidden",
				Pattern: `__never_matches__`,
				Message: "fixture",
			},
		}},
	}}
	changes := map[string]string{
		"command/issue.go": `fmt.Println("issue closed")`,
	}
	verdicts, err := evaluateContracts(changes, sets)
	if err != nil {
		t.Fatalf("evaluateContracts: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(verdicts))
	}
	v := verdicts[0]
	if v.Verdict != verdictRespected {
		t.Fatalf("want respected verdict, got %+v", v)
	}
	if v.ScopeStatus != "underconstrained" {
		t.Fatalf("want scope_status underconstrained, got %q", v.ScopeStatus)
	}
	if len(v.Warnings) == 0 || v.Warnings[0].Code != "contract_scope_underconstrained" {
		t.Fatalf("want underconstrained warning, got %+v", v.Warnings)
	}
	wantBlindSpots := map[string]bool{
		"required_scope_missing":    false,
		"out_of_scope_missing":      false,
		"required_paths_incomplete": false,
	}
	for _, spot := range v.BlindSpots {
		if _, ok := wantBlindSpots[spot]; ok {
			wantBlindSpots[spot] = true
		}
	}
	for spot, seen := range wantBlindSpots {
		if !seen {
			t.Fatalf("missing blind spot %q in %+v", spot, v.BlindSpots)
		}
	}
}

func TestEvaluateContracts_ScopedContractDetectsNeighboringEdit(t *testing.T) {
	sets := []contractSet{{
		Version: 1,
		TaskID:  "cli__cli-1337",
		Contracts: []frozenContract{{
			ID:         "cli.issue_close_status_includes_title",
			Kind:       "invariant",
			Confidence: "explicit",
			Statement:  "Issue close output must include the issue title.",
			Governs: contractGoverns{
				Files: []string{"command/*.go"},
			},
			RequiredScope: contractScope{
				Files: []string{"command/issue.go"},
			},
			AllowedRelatedScope: contractScope{
				Files: []string{"command/shared_format.go"},
			},
			OutOfScope: contractScope{
				Files: []string{"command/pr.go"},
			},
			RequiredPaths: []requiredPath{
				{Description: "issue close success message"},
				{Description: "issue close merged/conflicting message"},
			},
			ScopeConfidence: contractConfidence{
				ScopePrecision:        "high",
				RequiredPathsCoverage: "high",
			},
			Detect: contractDetect{
				Type:    "regex_forbidden",
				Pattern: `__never_matches__`,
				Message: "fixture",
			},
		}},
	}}
	changes := map[string]string{
		"command/issue.go": `fmt.Println("issue closed")`,
		"command/pr.go":    `fmt.Println("pr closed")`,
	}
	verdicts, err := evaluateContracts(changes, sets)
	if err != nil {
		t.Fatalf("evaluateContracts: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(verdicts))
	}
	v := verdicts[0]
	if !v.ScopeBroadeningDetected {
		t.Fatalf("want scope broadening detected, got %+v", v)
	}
	foundOutOfScope := false
	for _, w := range v.Warnings {
		if w.Code == "scope_broadening_detected" && len(w.Files) == 1 && w.Files[0] == "command/pr.go" {
			foundOutOfScope = true
		}
	}
	if !foundOutOfScope {
		t.Fatalf("want out-of-scope warning for command/pr.go, got %+v", v.Warnings)
	}
	if v.ContractFailureReason != "out_of_scope_edit" {
		t.Fatalf("want out_of_scope_edit failure reason, got %+v", v)
	}
	if v.ContractClean {
		t.Fatalf("out-of-scope edit should not be contract clean: %+v", v)
	}
}

func TestLoadContractSets_StructuredRequiredPaths(t *testing.T) {
	path := filepath.Join("..", "..", "eval", "multi-swe-bench", "contracts", "cli__cli-1337.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("benchmark fixture not present (eval/ is excluded from the standalone build)")
	}
	sets, err := loadContractSets(path)
	if err != nil {
		t.Fatalf("loadContractSets: %v", err)
	}
	if len(sets) != 1 || len(sets[0].Contracts) != 1 {
		t.Fatalf("unexpected loaded contracts: %+v", sets)
	}
	paths := sets[0].Contracts[0].RequiredPaths
	if len(paths) != 14 {
		t.Fatalf("want 14 structured required paths, got %d", len(paths))
	}
	if paths[0].ID != "issue_close_success_title" || paths[0].Description == "" {
		t.Fatalf("structured required path not decoded: %+v", paths[0])
	}
}

func TestEvaluateContractsWithFiles_ArtifactLeakDetected(t *testing.T) {
	sets := []contractSet{{
		Version: 1,
		TaskID:  "cli__cli-1337",
		Contracts: []frozenContract{{
			ID:         "cli.issue_close_status_includes_title",
			Kind:       "invariant",
			Confidence: "explicit",
			Statement:  "Issue close output must include the issue title.",
			Governs: contractGoverns{
				Files: []string{"command/*.go"},
			},
			RequiredScope: contractScope{
				Files: []string{"command/issue.go"},
			},
			AllowedRelatedScope: contractScope{
				Files: []string{"command/issue_test.go"},
			},
			OutOfScope: contractScope{
				Files: []string{"docs/**"},
			},
			RequiredPaths: []requiredPath{
				{ID: "issue_close_success_title", Description: "issue close success message"},
			},
			ScopeConfidence: contractConfidence{
				ScopePrecision:        "high",
				RequiredPathsCoverage: "high",
			},
			Detect: contractDetect{
				Type:    "regex_forbidden",
				Pattern: `__never_matches__`,
				Message: "fixture",
			},
		}},
	}}
	changes := map[string]string{
		"command/issue.go": `fmt.Println("issue closed")`,
	}
	actualChanged := []string{"command/issue.go", "gate.err"}
	verdicts, err := evaluateContractsWithFiles(changes, actualChanged, sets)
	if err != nil {
		t.Fatalf("evaluateContractsWithFiles: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(verdicts))
	}
	v := verdicts[0]
	if v.ContractClean {
		t.Fatalf("artifact leak should not be contract clean: %+v", v)
	}
	if v.ContractFailureReason != "repo_artifact_leak" {
		t.Fatalf("want repo_artifact_leak failure reason, got %+v", v)
	}
	if len(v.LeakedFiles) != 1 || v.LeakedFiles[0] != "gate.err" {
		t.Fatalf("want leaked gate.err, got %+v", v.LeakedFiles)
	}
	if got := v.EditedFileClassification["gate.err"]; got != "repo_artifact_leak" {
		t.Fatalf("want repo_artifact_leak classification, got %q", got)
	}
}

func TestEvaluateContractsWithFiles_ProofRequiredMissingTests(t *testing.T) {
	sets := []contractSet{{
		Version: 1,
		TaskID:  "cli__cli-1337",
		Contracts: []frozenContract{{
			ID:         "cli.issue_close_status_includes_title",
			Kind:       "invariant",
			Confidence: "explicit",
			Statement:  "Issue close output must include the issue title.",
			Governs: contractGoverns{
				Files: []string{"command/*.go"},
			},
			RequiredScope: contractScope{
				Files: []string{"command/issue.go"},
			},
			AllowedRelatedScope: contractScope{
				Files: []string{"command/issue_test.go"},
			},
			OutOfScope: contractScope{
				Files: []string{"docs/**"},
			},
			RequiredPaths: []requiredPath{
				{ID: "issue_close_success_title", Description: "issue close success message"},
			},
			Proof: contractProof{
				ProofRequired:     true,
				RequiredTestPaths: []string{"command/issue_test.go"},
				NoNewTestsMeans:   "test_proof_incomplete",
			},
			Detect: contractDetect{
				Type:    "regex_forbidden",
				Pattern: `__never_matches__`,
				Message: "fixture",
			},
		}},
	}}
	changes := map[string]string{
		"command/issue.go": `fmt.Println("issue closed")`,
	}
	actualChanged := []string{"command/issue.go"}
	verdicts, err := evaluateContractsWithFiles(changes, actualChanged, sets)
	if err != nil {
		t.Fatalf("evaluateContractsWithFiles: %v", err)
	}
	v := verdicts[0]
	if v.ProofRequired != true || v.ProofStatus != "incomplete" {
		t.Fatalf("want incomplete proof, got %+v", v)
	}
	if v.ContractFailureReason != "test_proof_incomplete" {
		t.Fatalf("want test_proof_incomplete, got %+v", v)
	}
	if v.ContractClean {
		t.Fatalf("missing required tests should not be contract clean: %+v", v)
	}
	if len(v.MissingRequiredTestPaths) != 1 || v.MissingRequiredTestPaths[0] != "command/issue_test.go" {
		t.Fatalf("missing required test paths wrong: %+v", v.MissingRequiredTestPaths)
	}
}

func TestEvaluateContractsWithFiles_ProofRequiredSatisfied(t *testing.T) {
	sets := []contractSet{{
		Version: 1,
		TaskID:  "cli__cli-1337",
		Contracts: []frozenContract{{
			ID:         "cli.issue_close_status_includes_title",
			Kind:       "invariant",
			Confidence: "explicit",
			Statement:  "Issue close output must include the issue title.",
			Governs: contractGoverns{
				Files: []string{"command/*.go"},
			},
			RequiredScope: contractScope{
				Files: []string{"command/issue.go"},
			},
			AllowedRelatedScope: contractScope{
				Files: []string{"command/issue_test.go"},
			},
			OutOfScope: contractScope{
				Files: []string{"docs/**"},
			},
			RequiredPaths: []requiredPath{
				{ID: "issue_close_success_title", Description: "issue close success message"},
			},
			Proof: contractProof{
				ProofRequired:     true,
				RequiredTestPaths: []string{"command/issue_test.go"},
				NoNewTestsMeans:   "test_proof_incomplete",
			},
			Detect: contractDetect{
				Type:    "regex_forbidden",
				Pattern: `__never_matches__`,
				Message: "fixture",
			},
		}},
	}}
	changes := map[string]string{
		"command/issue.go":      `fmt.Println("issue closed")`,
		"command/issue_test.go": `func TestIssueClose(t *testing.T) {}`,
	}
	actualChanged := []string{"command/issue.go", "command/issue_test.go"}
	verdicts, err := evaluateContractsWithFiles(changes, actualChanged, sets)
	if err != nil {
		t.Fatalf("evaluateContractsWithFiles: %v", err)
	}
	v := verdicts[0]
	if v.ProofRequired != true || v.ProofStatus != "complete" {
		t.Fatalf("want complete proof, got %+v", v)
	}
	if v.ContractFailureReason != "" {
		t.Fatalf("proof-complete contract should have no failure reason: %+v", v)
	}
	if !v.ContractClean {
		t.Fatalf("proof-complete contract should stay clean: %+v", v)
	}
}

func TestLoadContractSets_ProposedProofFieldsDoNotAffectGateAuthority(t *testing.T) {
	dir := t.TempDir()
	yaml := `contract_set_version: 1
task_id: t1
repo: github.com/example/repo
contracts:
  - id: contract.x
    kind: invariant
    confidence: inferred
    statement: source behavior must stay stable
    governs:
      files: ["command/*.go"]
    required_scope:
      files: ["command/issue.go"]
    detect:
      type: regex_forbidden
      pattern: '__never_matches__'
      message: "fixture"
    contract_status: proposed
    proof_status: proposed
    proof_required_proposed: true
    required_test_paths_proposed:
      - command/issue_test.go
    required_test_symbols_proposed:
      - TestIssueClose
    promotion_required: true
`
	fp := filepath.Join(dir, "task.yaml")
	if err := os.WriteFile(fp, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	sets, err := loadContractSets(fp)
	if err != nil {
		t.Fatalf("loadContractSets: %v", err)
	}
	changes := map[string]string{
		"command/issue.go": `fmt.Println("issue closed")`,
	}
	actualChanged := []string{"command/issue.go"}
	verdicts, err := evaluateContractsWithFiles(changes, actualChanged, sets)
	if err != nil {
		t.Fatalf("evaluateContractsWithFiles: %v", err)
	}
	if len(verdicts) != 1 {
		t.Fatalf("want 1 verdict, got %d", len(verdicts))
	}
	v := verdicts[0]
	if v.ProofRequired {
		t.Fatalf("proposed proof fields must not become authoritative: %+v", v)
	}
	if v.ProofStatus != "not_required" {
		t.Fatalf("proposed proof fields must not affect proof status, got %+v", v)
	}
	if !v.ContractClean {
		t.Fatalf("proposed proof fields must not affect contract_clean: %+v", v)
	}
}

func TestGlobToRegexp(t *testing.T) {
	cases := []struct {
		glob, path string
		want       bool
	}{
		{"golang/node_agent/**/runtime_proof.go", "golang/node_agent/node_agent_server/runtime_proof.go", true},
		{"golang/node_agent/**/runtime_proof.go", "golang/node_agent/runtime_proof.go", true}, // zero dirs
		{"golang/node_agent/**/runtime_proof.go", "golang/other/runtime_proof.go", false},
		{"golang/*/x.go", "golang/a/x.go", true},
		{"golang/*/x.go", "golang/a/b/x.go", false}, // * is single-segment
		{"docs/**", "docs/a/b/c.md", true},
		{"exact/path.go", "exact/path.go", true},
		{"exact/path.go", "exact/path_other.go", false},
	}
	for _, c := range cases {
		re, err := globToRegexp(c.glob)
		if err != nil {
			t.Fatalf("globToRegexp(%q): %v", c.glob, err)
		}
		if got := re.MatchString(c.path); got != c.want {
			t.Errorf("glob %q vs %q: got %v want %v (re=%s)", c.glob, c.path, got, c.want, re.String())
		}
	}
}

func TestLoadContractSets_FileAndDir(t *testing.T) {
	dir := t.TempDir()
	yaml := `contract_set_version: 1
task_id: t1
repo: github.com/globulario/services
contracts:
  - id: contract.x
    kind: invariant
    confidence: explicit
    governs:
      files: ["golang/**/runtime_proof.go"]
    detect:
      type: regex_forbidden
      pattern: 'status = Available'
      message: "bad"
`
	fp := filepath.Join(dir, "task.yaml")
	if err := os.WriteFile(fp, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	// load by file
	sets, err := loadContractSets(fp)
	if err != nil {
		t.Fatalf("load file: %v", err)
	}
	if len(sets) != 1 || len(sets[0].Contracts) != 1 || sets[0].Contracts[0].ID != "contract.x" {
		t.Fatalf("file load parsed wrong: %+v", sets)
	}
	if sets[0].Contracts[0].Detect.Type != "regex_forbidden" {
		t.Fatalf("detect not parsed: %+v", sets[0].Contracts[0].Detect)
	}

	// load by directory
	setsDir, err := loadContractSets(dir)
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if len(setsDir) != 1 || setsDir[0].TaskID != "t1" {
		t.Fatalf("dir load parsed wrong: %+v", setsDir)
	}
}
