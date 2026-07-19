// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// initProposeRepo creates a temp git repo with a seeded docs/awareness tree and
// returns its root. The four canonical feedback files are committed so the
// staged diff after a propose is clean and assertable.
func initProposeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "docs/awareness/failure_modes.yaml"),
		"failure_modes:\n  - id: awareness.existing_failure\n    title: An existing failure\n    severity: high\n    related_invariants:\n      - awareness.some_invariant\n")
	mustWrite(t, filepath.Join(root, "docs/awareness/invariants.yaml"),
		"invariants:\n  - id: awareness.some_invariant\n    title: An existing invariant\n    status: active\n")
	mustWrite(t, filepath.Join(root, "docs/awareness/architecture/decisions.yaml"),
		"decisions:\n  - id: decision.existing_architecture\n    title: An existing decision\n    status: accepted\n    rationale: Seed decision for propose tests.\n    related_invariants:\n      - awareness.some_invariant\n")
	// Make the seed path exist so embeddata staging logic has a target.
	mustWrite(t, filepath.Join(root, "golang/server/embeddata/awareness.nt"), "")

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("add", "-A")
	run("commit", "-q", "-m", "seed")
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stubRebuild swaps proposeRebuild for the duration of a test and records the
// args it was called with, so the yaml2nt/loadnt pipeline is exercised as a
// mock (no Oxigraph, no real rebuild).
func stubRebuild(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	prev := proposeRebuild
	proposeRebuild = func(args []string) int {
		calls = append(calls, append([]string(nil), args...))
		return 0
	}
	t.Cleanup(func() { proposeRebuild = prev })
	return &calls
}

func TestApplyProposal_ValidFailureModeWritesYAML(t *testing.T) {
	root := initProposeRepo(t)
	calls := stubRebuild(t)

	req := &ProposeRequest{
		Kind:              "failure_mode",
		Title:             "Reload skips stale embeddata",
		Description:       "Seed reload used a cached path and silently served stale triples.",
		Severity:          "high",
		SourceFiles:       []string{"golang/server/main.go"},
		RelatedInvariants: []string{"awareness.some_invariant"},
		RequiredTests:     []string{"golang/server/reload_test.go:TestReloadFresh"},
		Evidence:          []string{"observed stale node served after rebuild"},
		Domain:            "github.com/globulario/sensei",
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root, oxigraphURL: "http://localhost:7878/store?default"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; errors: %v", code, res.ValidationErrors)
	}
	if res.Status != "created" {
		t.Fatalf("status = %q, want created", res.Status)
	}
	if len(*calls) != 1 {
		t.Fatalf("rebuild calls = %d, want 1", len(*calls))
	}

	// The new entry must be present and parseable under failure_modes.
	data, err := os.ReadFile(filepath.Join(root, "docs/awareness/failure_modes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		FailureModes []map[string]any `yaml:"failure_modes"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written YAML does not parse: %v", err)
	}
	wantID := res.NodeIDs[0]
	var found map[string]any
	for _, fm := range doc.FailureModes {
		if fm["id"] == wantID {
			found = fm
		}
	}
	if found == nil {
		t.Fatalf("new entry %q not found in %d entries", wantID, len(doc.FailureModes))
	}
	if found["title"] != req.Title {
		t.Errorf("title = %v, want %q", found["title"], req.Title)
	}
	// The existing entry must survive untouched.
	if len(doc.FailureModes) != 2 {
		t.Errorf("failure_modes count = %d, want 2 (existing + new)", len(doc.FailureModes))
	}
}

func TestApplyProposal_DecisionWritesArchitectureDecisionYAML(t *testing.T) {
	root := initProposeRepo(t)
	calls := stubRebuild(t)

	req := &ProposeRequest{
		Kind:               "decision",
		Title:              "Recognize enforced awareness mutations",
		Description:        "Canonical awareness sources may satisfy the behavior plane conditionally through deterministic validation and graph compilation.",
		Context:            "Closure needs an explicit enforcement path for canonical awareness-source mutations.",
		Consequences:       "Behavioral support stays conditional and non-authoritative.",
		ArchitecturalPlane: "desired",
		RelatedInvariants:  []string{"awareness.some_invariant"},
		RelatedFailures:    []string{"awareness.existing_failure"},
		AffectsComponents:  []string{"component.cli_propose"},
		SourceFiles:        []string{"cmd/awg/cmd_propose.go"},
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root, oxigraphURL: "http://localhost:7878/store?default"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; errors: %v", code, res.ValidationErrors)
	}
	if res.Status != "created" {
		t.Fatalf("status = %q, want created", res.Status)
	}
	if len(*calls) != 1 {
		t.Fatalf("rebuild calls = %d, want 1", len(*calls))
	}

	data, err := os.ReadFile(filepath.Join(root, "docs/awareness/architecture/decisions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Decisions []map[string]any `yaml:"decisions"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("written YAML does not parse: %v", err)
	}
	wantID := res.NodeIDs[0]
	var found map[string]any
	for _, decision := range doc.Decisions {
		if decision["id"] == wantID {
			found = decision
		}
	}
	if found == nil {
		t.Fatalf("new decision %q not found in %d entries", wantID, len(doc.Decisions))
	}
	if found["status"] != "accepted" {
		t.Fatalf("status = %v, want accepted", found["status"])
	}
	if found["architectural_plane"] != "desired" {
		t.Fatalf("architectural_plane = %v, want desired", found["architectural_plane"])
	}
	if found["rationale"] != req.Description {
		t.Fatalf("rationale = %v, want %q", found["rationale"], req.Description)
	}
	if found["context"] != req.Context {
		t.Fatalf("context = %v, want %q", found["context"], req.Context)
	}
	if found["consequences"] != req.Consequences {
		t.Fatalf("consequences = %v, want %q", found["consequences"], req.Consequences)
	}
	if len(doc.Decisions) != 2 {
		t.Fatalf("decisions count = %d, want 2 (existing + new)", len(doc.Decisions))
	}
}

func TestApplyProposal_InvalidRejectedNoMutation(t *testing.T) {
	root := initProposeRepo(t)
	stubRebuild(t)
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	before, _ := os.ReadFile(path)

	// Missing contract link AND missing evidence/test → must be rejected.
	req := &ProposeRequest{
		Kind:  "failure_mode",
		Title: "A vague note with no contract",
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root})
	if code == 0 {
		t.Fatalf("expected non-zero exit for invalid proposal, got 0")
	}
	if res.Status != "validation_failed" {
		t.Fatalf("status = %q, want validation_failed", res.Status)
	}
	if len(res.ValidationErrors) == 0 {
		t.Fatal("expected validation errors")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid proposal mutated the YAML file")
	}
}

// TestApplyProposal_ContradictoryIDRefusedNoMutation: reusing an existing
// canonical ID with a DIFFERENT body is a typed contradiction — the governed
// mutation owner refuses it and mutates nothing (never overwrites). This is the
// Slice-8.1b contract that supersedes the old "any duplicate id is a silent
// no-op" behavior.
func TestApplyProposal_ContradictoryIDRefusedNoMutation(t *testing.T) {
	root := initProposeRepo(t)
	stubRebuild(t)
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")
	before, _ := os.ReadFile(path)

	req := &ProposeRequest{
		Kind:              "failure_mode",
		ID:                "awareness.existing_failure", // already in the seed, different body
		Title:             "Trying to re-add an existing failure",
		RelatedInvariants: []string{"awareness.some_invariant"},
		Evidence:          []string{"dup"},
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root})
	if code != 1 {
		t.Fatalf("contradictory id should be refused (exit 1), got %d", code)
	}
	if res.Status != "validation_failed" {
		t.Fatalf("status = %q, want validation_failed", res.Status)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("contradictory proposal mutated the YAML file")
	}
}

// TestApplyProposal_ExactReplayIsDuplicateNoMutation: re-proposing the exact same
// canonical id + equivalent body is a replay — deterministic, exit 0, no second
// record.
func TestApplyProposal_ExactReplayIsDuplicateNoMutation(t *testing.T) {
	root := initProposeRepo(t)
	stubRebuild(t)
	path := filepath.Join(root, "docs/awareness/failure_modes.yaml")

	req := &ProposeRequest{
		Kind:              "failure_mode",
		Title:             "Reload serves a stale seed after a failed rebuild",
		Description:       "A failed rebuild left the previous seed served as current.",
		Severity:          "high",
		SourceFiles:       []string{"golang/server/reload.go"},
		RelatedInvariants: []string{"awareness.some_invariant"},
		RequiredTests:     []string{"golang/server/reload_test.go:TestReloadFresh"},
		Evidence:          []string{"observed stale seed served"},
		Domain:            "github.com/globulario/sensei",
	}
	first, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root})
	if code != 0 {
		t.Fatalf("first apply exit = %d, want 0; errors: %v", code, first.ValidationErrors)
	}
	afterFirst, _ := os.ReadFile(path)

	// Same request again → replay, no mutation.
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root})
	if code != 0 {
		t.Fatalf("replay should exit 0, got %d", code)
	}
	if res.Status != "duplicate" {
		t.Fatalf("status = %q, want duplicate", res.Status)
	}
	afterSecond, _ := os.ReadFile(path)
	if string(afterFirst) != string(afterSecond) {
		t.Fatal("replay mutated the YAML file")
	}
}

func TestApplyProposal_RebuildInvokedWithExpectedArgs(t *testing.T) {
	root := initProposeRepo(t)
	calls := stubRebuild(t)

	req := &ProposeRequest{
		Kind:            "invariant",
		Title:           "Reload must validate before serving",
		SourceFiles:     []string{"golang/server/reload.go"},
		RelatedFailures: []string{"awareness.existing_failure"},
		RequiredTests:   []string{"golang/server/reload_test.go:TestReloadValidates"},
	}
	_, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root, oxigraphURL: "http://localhost:9999/store?default"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(*calls) != 1 {
		t.Fatalf("rebuild calls = %d, want 1", len(*calls))
	}
	got := strings.Join((*calls)[0], " ")
	for _, want := range []string{"--oxigraph-url", "http://localhost:9999/store?default", "--ag-repo", root} {
		if !strings.Contains(got, want) {
			t.Errorf("rebuild args %q missing %q", got, want)
		}
	}
}

func TestApplyProposal_NoRebuildSkipsPipeline(t *testing.T) {
	root := initProposeRepo(t)
	calls := stubRebuild(t)

	req := &ProposeRequest{
		Kind:              "forbidden_fix",
		Title:             "Do not cache reload paths",
		Description:       "Caching the reload path is what caused the stale-serve failure.",
		RelatedInvariants: []string{"awareness.some_invariant"},
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root, noRebuild: true})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; %v", code, res.ValidationErrors)
	}
	if len(*calls) != 0 {
		t.Fatalf("rebuild should be skipped with noRebuild, got %d calls", len(*calls))
	}
	if res.Reload != "skipped" {
		t.Errorf("reload = %q, want skipped", res.Reload)
	}
}

func TestApplyProposal_FailsBeforeMutationWhenCombinedRebuildCannotBeProven(t *testing.T) {
	agRepo, svcRepo := setupSeedStatusRepos(t)
	if code := runRebuild([]string{"--combined", "--ag-repo", agRepo, "--services-repo", svcRepo, "--no-runtime-reload"}); code != 0 {
		t.Fatalf("runRebuild code=%d, want 0", code)
	}
	calls := stubRebuild(t)
	path := filepath.Join(agRepo, "docs/awareness/invariants.yaml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	req := &ProposeRequest{
		Kind:          "invariant",
		Title:         "Runtime reload must certify combined graph state",
		Contract:      "served graph authority must match the current validated combined artifact",
		SourceFiles:   []string{"golang/server/main.go"},
		RequiredTests: []string{"golang/server/reload_test.go:TestReloadValidates"},
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: agRepo, agRepo: agRepo, oxigraphURL: "http://localhost:7878/store?default"})
	if code == 0 {
		t.Fatalf("expected non-zero exit when services repo is unavailable, got 0")
	}
	if len(res.ValidationErrors) == 0 || !strings.Contains(res.ValidationErrors[0], "paired services repo") {
		t.Fatalf("validation errors=%v, want paired services repo failure", res.ValidationErrors)
	}
	if len(*calls) != 0 {
		t.Fatalf("rebuild should not run when atomic rebuild prerequisites fail, got %d calls", len(*calls))
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("proposal mutated YAML before cross-repo rebuild prerequisites were satisfied")
	}
}

func TestApplyProposal_DiffIncludesNewEntry(t *testing.T) {
	root := initProposeRepo(t)
	stubRebuild(t)

	req := &ProposeRequest{
		Kind:              "failure_mode",
		Title:             "Diff visibility scar",
		Description:       "Captured to assert the returned diff carries the entry.",
		RelatedInvariants: []string{"awareness.some_invariant"},
		Evidence:          []string{"observed"},
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; %v", code, res.ValidationErrors)
	}
	id := res.NodeIDs[0]
	if !strings.Contains(res.DiffSummary, id) {
		t.Fatalf("returned diff does not include new entry id %q:\n%s", id, res.DiffSummary)
	}
	if res.NextCommand == "" || !strings.Contains(res.NextCommand, "git -C "+root) {
		t.Errorf("next command should be a concrete git commit for the repo, got %q", res.NextCommand)
	}
}

func TestApplyProposal_ContractUnknownGoesToCandidatesNoRebuild(t *testing.T) {
	root := initProposeRepo(t)
	calls := stubRebuild(t)

	req := &ProposeRequest{
		Kind:             "contract_unknown",
		Title:            "Unclear ownership of reload path",
		Description:      "Observed a failure but the owning contract is unknown.",
		ProposedContract: "Reload path ownership belongs to the store layer.",
		Evidence:         []string{"observed stale serve"},
	}
	res, code := applyProposal(req, proposeOptions{targetRepo: root, agRepo: root})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; %v", code, res.ValidationErrors)
	}
	if len(*calls) != 0 {
		t.Fatalf("contract_unknown must not rebuild, got %d calls", len(*calls))
	}
	if res.Reload != "skipped" {
		t.Errorf("reload = %q, want skipped", res.Reload)
	}
	// It must land under candidates/ (skipped by the strict build).
	matches, _ := filepath.Glob(filepath.Join(root, "docs/awareness/candidates/contract_unknown_*.yaml"))
	if len(matches) != 1 {
		t.Fatalf("expected one candidate file, got %d", len(matches))
	}
}

func TestValidateProposal_ContractUnknownRequiresProposalOrRevision(t *testing.T) {
	req := &ProposeRequest{Kind: "contract_unknown", Title: "x", Description: "y", Evidence: []string{"z"}}
	normalizeProposeRequest(req)
	errs := validateProposal(req)
	if len(errs) == 0 {
		t.Fatal("contract_unknown without proposed_contract/revision_request must be rejected")
	}
}

func TestValidateProposal_DecisionRequiresRationaleAndArchitecturalLink(t *testing.T) {
	req := &ProposeRequest{
		Kind:               "decision",
		Title:              "Incomplete decision",
		ArchitecturalPlane: "desired",
	}
	normalizeProposeRequest(req)
	errs := validateProposal(req)
	if len(errs) == 0 {
		t.Fatal("decision without rationale and links must be rejected")
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{
		"decision: description is required and becomes rationale",
		"decision: connect the record to at least one invariant, failure, forbidden fix, source file, boundary, contract, component, or supporting evidence",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("decision validation errors missing %q:\n%s", want, joined)
		}
	}
}

func TestValidateProposal_DecisionRejectsUnsupportedFields(t *testing.T) {
	req := &ProposeRequest{
		Kind:               "decision",
		Title:              "Invalid decision",
		Description:        "Still a decision.",
		Severity:           "high",
		RequiredTests:      []string{"golang/server/reload_test.go:TestReloadValidates"},
		ArchitecturalPlane: "future",
		SourceFiles:        []string{"cmd/awg/cmd_propose.go"},
	}
	normalizeProposeRequest(req)
	errs := validateProposal(req)
	if len(errs) == 0 {
		t.Fatal("decision with unsupported fields must be rejected")
	}
	joined := strings.Join(errs, "\n")
	for _, want := range []string{
		"decision: severity is not supported",
		"decision: required_test links are not supported directly; define evidence or separate required_test records",
		`decision: architectural_plane "future" is not one of desired|intended|historical`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("decision validation errors missing %q:\n%s", want, joined)
		}
	}
}

func TestDeriveProposalID_DeterministicSlug(t *testing.T) {
	req := &ProposeRequest{Kind: "failure_mode", Title: "Reload Skips Stale Embeddata!", Domain: "github.com/globulario/sensei"}
	got := deriveProposalID(req)
	want := "failure.sensei.reload_skips_stale_embeddata"
	if got != want {
		t.Fatalf("derived id = %q, want %q", got, want)
	}
	// Idempotent.
	if deriveProposalID(req) != got {
		t.Fatal("id derivation is not deterministic")
	}
}
