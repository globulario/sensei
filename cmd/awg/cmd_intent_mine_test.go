// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/extractor/coldsource"
)

func TestMachineAdoptedContractCategoryUsesContractLevel(t *testing.T) {
	if got := machineAdoptedIntentLevel("API-contract"); got != "contract" {
		t.Fatalf("level=%q", got)
	}
	if got := machineAdoptedIntentLevel("operational_deployment"); got != "constraint" {
		t.Fatalf("level=%q", got)
	}
}

// TestApplyIntentGroundings_MachineAdoptsStrongOnly proves the adoption contract
// end to end: strong grounded intent may enter the model as machine_adopted,
// while weaker material stays in candidates/ and is skipped by graph import.
func TestApplyIntentGroundings_MachineAdoptsStrongOnly(t *testing.T) {
	repo := t.TempDir()

	cands := []coldsource.IntentCandidate{
		{
			IntentID: "build-shell-once",
			Claim:    "The DOM shell must be built exactly once and never rebuilt on refresh.",
			Category: "ui-truth",
			Evidence: coldsource.Evidence{Code: []string{"file:apps/web/src/pages/cluster_nodes.ts"}},
		},
		{
			IntentID: "maybe-poll-faster",
			Claim:    "Polling might be faster somewhere.",
			Category: "performance",
			Evidence: coldsource.Evidence{Code: []string{"file:apps/web/src/pages/cluster_nodes.ts"}},
		},
	}
	groundings := []coldsource.IntentGrounding{
		{IntentID: "build-shell-once", OutputClass: coldsource.StrongIntent, Certainty: 0.91},
		{IntentID: "maybe-poll-faster", OutputClass: coldsource.StaleIntent, Certainty: 0.40},
	}

	res, err := applyIntentGroundings(repo, cands, groundings, intentAdoptionPolicy{AllowMachineAdoption: true})
	if err != nil {
		t.Fatalf("applyIntentGroundings: %v", err)
	}
	if res.MachineAdopted != 1 || res.Staged != 1 || res.ParkedInvalid != 0 || res.Skipped != 0 || res.GroundedStrong != 1 {
		t.Fatalf("apply result=%+v, want adopted=1 staged=1 invalid=0 skipped=0 strong=1", res)
	}

	awDir := filepath.Join(repo, "docs", "awareness")
	intentFile := filepath.Join(awDir, "intent_build_shell_once.yaml")
	raw, err := os.ReadFile(intentFile)
	if err != nil {
		t.Fatalf("expected machine-adopted intent file: %v", err)
	}
	for _, want := range []string{"id: intent.build_shell_once", "status: machine_adopted", "promotion_status: machine_adopted", "assertion_origin: model_inferred", "epistemic_status: supported", "architectural_plane: intended", "review_status: not_human_reviewed", "decision_actor: sensei.intent_mine", "decision_context: delegated_machine_adoption", "decision_policy: adoption.intent.strong_grounding.v1"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("machine-adopted intent file missing %q:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), "status: active") {
		t.Fatalf("machine-adopted intent was marked governed active:\n%s", raw)
	}

	stagedRaw, err := os.ReadFile(filepath.Join(awDir, "candidates", "intents.yaml"))
	if err != nil {
		t.Fatalf("expected staged candidate file: %v", err)
	}
	for _, want := range []string{"intent.maybe_poll_faster", "status: candidate", "review_state: staged", "grounding_class: stale_intent"} {
		if !strings.Contains(string(stagedRaw), want) {
			t.Errorf("staged candidate file missing %q:\n%s", want, stagedRaw)
		}
	}

	// Round-trip: machine-adopted intent imports as an Intent with explicit
	// machine provenance. The weaker staged candidate remains excluded.
	var buf bytes.Buffer
	e, _, err := extractor.ImportAwarenessDir(awDir, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "intent/intent.build_shell_once") || !strings.Contains(out, "machine_adopted") || !strings.Contains(out, "model_inferred") || !strings.Contains(out, "sensei.intent_mine") || !strings.Contains(out, "delegated_machine_adoption") {
		t.Errorf("machine-adopted intent did not import with provenance:\n%s", out)
	}
	if strings.Contains(out, "maybe_poll_faster") {
		t.Errorf("staged weak candidate leaked into graph compilation:\n%s", out)
	}
}

func TestApplyIntentGroundings_StageOnlyDoesNotAdopt(t *testing.T) {
	repo := t.TempDir()
	cands := []coldsource.IntentCandidate{{
		IntentID: "x-rule",
		Claim:    "X must hold.",
		Category: "api-contract",
		Evidence: coldsource.Evidence{Code: []string{"file:x.go"}},
	}}
	gs := []coldsource.IntentGrounding{{IntentID: "x-rule", OutputClass: coldsource.StrongIntent, Certainty: 0.85}}

	res, err := applyIntentGroundings(repo, cands, gs, intentAdoptionPolicy{})
	if err != nil {
		t.Fatalf("applyIntentGroundings: %v", err)
	}
	if res.MachineAdopted != 0 || res.Staged != 1 {
		t.Fatalf("stage-only result=%+v, want adopted=0 staged=1", res)
	}
	if matches, _ := filepath.Glob(filepath.Join(repo, "docs", "awareness", "intent_*.yaml")); len(matches) != 0 {
		t.Fatalf("stage-only wrote machine-adopted intent files: %v", matches)
	}
}

// TestApplyIntentGroundings_Idempotent: a second machine adoption of the same
// intent reuses the same identity and does not rewrite it.
func TestApplyIntentGroundings_Idempotent(t *testing.T) {
	repo := t.TempDir()
	cands := []coldsource.IntentCandidate{{
		IntentID: "x-rule",
		Claim:    "X must hold.",
		Category: "api-contract",
		Evidence: coldsource.Evidence{Code: []string{"file:x.go"}},
	}}
	gs := []coldsource.IntentGrounding{{IntentID: "x-rule", OutputClass: coldsource.StrongIntent, Certainty: 0.85}}

	if res, err := applyIntentGroundings(repo, cands, gs, intentAdoptionPolicy{AllowMachineAdoption: true}); err != nil || res.MachineAdopted != 1 {
		t.Fatalf("first adopt: res=%+v err=%v, want adopted=1/nil", res, err)
	}
	res, err := applyIntentGroundings(repo, cands, gs, intentAdoptionPolicy{AllowMachineAdoption: true})
	if err != nil || res.Skipped != 1 {
		t.Fatalf("second adopt: res=%+v err=%v, want skipped=1/nil", res, err)
	}
}

func TestApplyIntentGroundings_InvalidStrongCandidateParked(t *testing.T) {
	repo := t.TempDir()
	cands := []coldsource.IntentCandidate{{
		IntentID: "intent.",
		Claim:    "Route trees must stay consistent.",
		Category: "api-contract",
		Evidence: coldsource.Evidence{Code: []string{"file:tree.go"}},
	}}
	gs := []coldsource.IntentGrounding{{IntentID: "intent.", OutputClass: coldsource.StrongIntent, Certainty: 0.99}}

	res, err := applyIntentGroundings(repo, cands, gs, intentAdoptionPolicy{AllowMachineAdoption: true})
	if err != nil {
		t.Fatalf("applyIntentGroundings: %v", err)
	}
	if res.MachineAdopted != 0 || res.Staged != 0 || res.ParkedInvalid != 1 || res.Skipped != 0 {
		t.Fatalf("apply result=%+v, want adopted=0 staged=0 invalid=1 skipped=0", res)
	}
	if matches, _ := filepath.Glob(filepath.Join(repo, "docs", "awareness", "intent_*.yaml")); len(matches) != 0 {
		t.Fatalf("invalid candidate was applied: %v", matches)
	}
	raw, err := os.ReadFile(filepath.Join(repo, "docs", "awareness", "candidates", "intents.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "candidate.identity.invalid") || !strings.Contains(string(raw), "review_state: parked_invalid") {
		t.Fatalf("parked candidate missing machine reason:\n%s", raw)
	}
}

func TestApplyIntentGroundings_ExtractedCandidateRequiresTitleAndStatement(t *testing.T) {
	repo := t.TempDir()
	cands := []coldsource.IntentCandidate{{
		IntentID:       coldsource.MintIntentID("", "Route trees must stay consistent.", []string{"tree.go"}),
		Title:          "",
		Claim:          "Route trees must stay consistent.",
		Category:       "api-contract",
		Evidence:       coldsource.Evidence{Code: []string{"file:tree.go"}},
		ExtractedByLLM: true,
	}, {
		IntentID:       coldsource.MintIntentID("Route tree consistency", "", []string{"tree.go"}),
		Title:          "Route tree consistency",
		Claim:          "   ",
		Category:       "api-contract",
		Evidence:       coldsource.Evidence{Code: []string{"file:tree.go"}},
		ExtractedByLLM: true,
	}}
	gs := []coldsource.IntentGrounding{
		{IntentID: cands[0].IntentID, OutputClass: coldsource.StrongIntent, Certainty: 0.99},
		{IntentID: cands[1].IntentID, OutputClass: coldsource.StrongIntent, Certainty: 0.99},
	}

	res, err := applyIntentGroundings(repo, cands, gs, intentAdoptionPolicy{AllowMachineAdoption: true})
	if err != nil {
		t.Fatalf("applyIntentGroundings: %v", err)
	}
	if res.MachineAdopted != 0 || res.Staged != 0 || res.ParkedInvalid != 2 {
		t.Fatalf("apply result=%+v, want adopted=0 staged=0 invalid=2", res)
	}
	raw, err := os.ReadFile(filepath.Join(repo, "docs", "awareness", "candidates", "intents.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"candidate.title.empty", "candidate.statement.empty"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("parked candidates missing %q:\n%s", want, raw)
		}
	}
}

func TestApplyIntentGroundings_DuplicateManualIDCollisionParked(t *testing.T) {
	repo := t.TempDir()
	cands := []coldsource.IntentCandidate{{
		IntentID: "router-contract",
		Claim:    "Router contract one.",
		Category: "api-contract",
		Evidence: coldsource.Evidence{Code: []string{"file:gin.go"}},
	}, {
		IntentID: "router-contract",
		Claim:    "Router contract two.",
		Category: "api-contract",
		Evidence: coldsource.Evidence{Code: []string{"file:tree.go"}},
	}}
	gs := []coldsource.IntentGrounding{
		{IntentID: "router-contract", OutputClass: coldsource.StrongIntent, Certainty: 0.99},
		{IntentID: "router-contract", OutputClass: coldsource.StrongIntent, Certainty: 0.99},
	}

	res, err := applyIntentGroundings(repo, cands, gs, intentAdoptionPolicy{AllowMachineAdoption: true})
	if err != nil {
		t.Fatalf("applyIntentGroundings: %v", err)
	}
	if res.MachineAdopted != 1 || res.Staged != 0 || res.ParkedInvalid != 1 || res.Skipped != 0 {
		t.Fatalf("apply result=%+v, want adopted=1 staged=0 invalid=1 skipped=0", res)
	}
	raw, err := os.ReadFile(filepath.Join(repo, "docs", "awareness", "candidates", "intents.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "candidate.identity.collision") || !strings.Contains(string(raw), "conflicting_id: intent.router_contract") {
		t.Fatalf("collision was not parked with reason:\n%s", raw)
	}
}

// TestCanonicalIntentID normalizes mined ids to the canonical dotted form.
func TestCanonicalIntentID(t *testing.T) {
	for in, want := range map[string]string{
		"build-shell-once":        "intent.build_shell_once",
		"intent.build-shell-once": "intent.build_shell_once",
		"Preserve Cached Data":    "intent.preserve_cached_data",
		"session_storage_auth":    "intent.session_storage_auth",
	} {
		if got := canonicalIntentID(in); got != want {
			t.Errorf("canonicalIntentID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIntentMineAcceptsClaudeCLIThroughCage(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("The router must preserve middleware order.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeDir := t.TempDir()
	fakeClaude := filepath.Join(fakeDir, "claude")
	reply := `{"type":"result","is_error":false,"result":"{\"candidates\":[{\"intent_id\":\"middleware-order\",\"title\":\"Router middleware order\",\"claim\":\"Router middleware order must be preserved.\",\"category\":\"api-contract\",\"source_citations\":[\"file:README.md:1\"],\"code_anchors\":[\"file:README.md\"]}]}"}`
	script := "#!/bin/sh\n" +
		"if [ -n \"$ANTHROPIC_API_KEY\" ] || [ -n \"$ANTHROPIC_AUTH_TOKEN\" ]; then\n" +
		"  printf '%s' '{\"is_error\":true,\"result\":\"ENV_LEAK\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"cat >/dev/null\n" +
		"printf '%s' '" + reply + "'\n"
	if err := os.WriteFile(fakeClaude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-poison")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "bad-token")

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runIntentMine([]string{"--path", repo, "--sources", "docs", "--drafter", "claude-cli", "--max", "1", "--adopt"})
	})
	if code != 0 {
		t.Fatalf("runIntentMine code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "intent drafter: claude-cli") || !strings.Contains(stderr, "direct_api_environment_ignored=true") {
		t.Fatalf("stderr missing claude-cli receipt:\n%s", stderr)
	}
	if !strings.Contains(stdout, "intent.router_m") {
		t.Fatalf("stdout missing Sensei-minted drafted intent:\n%s", stdout)
	}
	if strings.Contains(stdout, "middleware-order") {
		t.Fatalf("stdout leaked model-owned intent id:\n%s", stdout)
	}
	matches, _ := filepath.Glob(filepath.Join(repo, "docs", "awareness", "intent_*.yaml"))
	if len(matches) != 1 {
		t.Fatalf("claude-cli adopt wrote %d machine-adopted intent files, want 1: %v", len(matches), matches)
	}
	adopted, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Machine-adopted by `sensei intent-mine --adopt`", "intent.router_m", "status: machine_adopted", "decision_actor: sensei.intent_mine", "decision_context: delegated_machine_adoption", "Machine adopted: 1", "Governed: 0"} {
		haystack := string(adopted)
		if want == "Machine adopted: 1" || want == "Governed: 0" {
			haystack = stdout
		}
		if !strings.Contains(haystack, want) {
			t.Fatalf("claude-cli adopt missing %q\nstdout:\n%s\nintent:\n%s", want, stdout, adopted)
		}
	}
}

func TestIntentMineAcceptsCodexCLIThroughCage(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("The router must preserve middleware order.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeDir := t.TempDir()
	fakeCodex := filepath.Join(fakeDir, "codex")
	reply := `{"candidates":[{"intent_id":"middleware-order","title":"Router middleware order","claim":"Router middleware order must be preserved.","category":"api-contract","source_citations":["file:README.md:1"],"code_anchors":["file:README.md"]}]}`
	script := "#!/bin/sh\n" +
		"out=\n" +
		"prev=\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--output-last-message\" ]; then out=\"$arg\"; fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"if [ -n \"$OPENAI_API_KEY\" ]; then\n" +
		"  echo ENV_LEAK >&2\n" +
		"  exit 18\n" +
		"fi\n" +
		"cat >/dev/null\n" +
		"printf '%s' '" + reply + "' >\"$out\"\n"
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENAI_API_KEY", "sk-poison")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-poison")

	code, stdout, stderr := captureStdoutStderr(t, func() int {
		return runIntentMine([]string{"--path", repo, "--sources", "docs", "--drafter", "codex-cli", "--max", "1", "--adopt"})
	})
	if code != 0 {
		t.Fatalf("runIntentMine code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stderr, "intent drafter: codex-cli") || !strings.Contains(stderr, "direct_api_environment_ignored=true") {
		t.Fatalf("stderr missing codex-cli receipt:\n%s", stderr)
	}
	if !strings.Contains(stdout, "intent.router_m") {
		t.Fatalf("stdout missing Sensei-minted drafted intent:\n%s", stdout)
	}
	if strings.Contains(stdout, "middleware-order") {
		t.Fatalf("stdout leaked model-owned intent id:\n%s", stdout)
	}
	matches, _ := filepath.Glob(filepath.Join(repo, "docs", "awareness", "intent_*.yaml"))
	if len(matches) != 1 {
		t.Fatalf("codex-cli adopt wrote %d machine-adopted intent files, want 1: %v", len(matches), matches)
	}
	adopted, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Machine-adopted by `sensei intent-mine --adopt`", "intent.router_m", "status: machine_adopted", "decision_actor: sensei.intent_mine", "decision_context: delegated_machine_adoption", "Machine adopted: 1", "Governed: 0"} {
		haystack := string(adopted)
		if want == "Machine adopted: 1" || want == "Governed: 0" {
			haystack = stdout
		}
		if !strings.Contains(haystack, want) {
			t.Fatalf("codex-cli adopt missing %q\nstdout:\n%s\nintent:\n%s", want, stdout, adopted)
		}
	}
}
