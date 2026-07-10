// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
)

func TestFrozenContractSet_ImportedAndAnchored(t *testing.T) {
	out, report := outcomeDir(t, map[string]string{
		"contract.yaml": `
contract_set_version: 1
task_id: cli__cli-1388
repo: github.com/cli/cli
base_commit: 71bebd3f54f1c4006fa57a272382e8a285c9100c
contracts:
  - id: contract.repo_fork_and_view_nontty_scriptability
    kind: invariant
    confidence: explicit
    statement: Repo commands must stay scriptable in non-tty mode.
    required_scope:
      files:
        - command/repo.go
      behavior:
        - repo fork non-tty emits no informational output
    allowed_related_scope:
      files:
        - command/repo_test.go
      behavior:
        - benchmark fail-to-pass test updates
    out_of_scope:
      files:
        - docs/**
    required_paths:
      - id: repo_fork_non_tty
        description: repo fork non-tty path
        severity: high
    proof_required: true
    required_test_paths:
      - command/repo_test.go
    required_test_symbols:
      - TestRepoForkNonTTY
    no_new_tests_means: test_proof_incomplete
    invariants:
      - repo.non_tty_scriptability
    failure_modes:
      - repo.interactive_prompt_when_nontty
    intents:
      - repo.scriptable_cli_contract
    required_tests:
      - repo:TestRepoForkNonTTY
    components:
      - component.command.repo
    awg_anchors:
      - invariant:meta.signal_over_noise
      - failure_mode:repo.unscoped_output_noise
      - intent:repo.cli_tty_behavior
      - test:repo:TestRepoViewNonTTY
      - component:component.command.repo
    scope_confidence:
      scope_precision: high
      required_paths_coverage: high
    governs:
      files:
        - command/*.go
      symbols:
        - repoFork
      invariants:
        - repo.output_must_be_stream_appropriate
      failure_modes:
        - repo.tty_branch_regression
      intents:
        - repo.terminal_mode_is_explicit
      required_tests:
        - repo:TestRepoForkTTY
      components:
        - component.command.output
    detect:
      type: regex_forbidden
      pattern: 'fmt\\.Fprintf'
      message: No informational chatter in non-tty mode.
`,
	})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if got := report.Imported()[0].Schema; got != "frozen_contract_set" {
		t.Fatalf("schema: want frozen_contract_set, got %q", got)
	}

	subj := rdf.MintIRI(rdf.ClassContract, "contract.repo_fork_and_view_nontty_scriptability")
	file := rdf.MintIRI(rdf.ClassSourceFile, "command/repo.go")
	testFile := rdf.MintIRI(rdf.ClassSourceFile, "command/repo_test.go")

	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassContract)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropKind)+` "invariant"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropConfidence)+` "explicit"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForTask)+` "cli__cli-1388"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRepo)+` "github.com/cli/cli"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDomain)+` "repo"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropProofRequired)+` "true"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiredTestPath)+` "command/repo_test.go"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiredTestSymbol)+` "TestRepoForkNonTTY"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropNoNewTestsMeans)+` "test_proof_incomplete"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresVerification)+` "repo fork non-tty emits no informational output"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresVerification)+` "repo fork non-tty path"`)
	requiredPathSlot := rdf.MintIRI(rdf.ClassProofSlot, "slot.contract.contract.repo_fork_and_view_nontty_scriptability.repo_fork_non_tty")
	mustContain(t, out, requiredPathSlot+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassProofSlot)+" .")
	mustContain(t, out, requiredPathSlot+" "+rdf.IRI(rdf.PropSlotKind)+` "required_path"`)
	mustContain(t, out, requiredPathSlot+" "+rdf.IRI(rdf.PropSeverity)+` "high"`)
	mustContain(t, out, requiredPathSlot+" "+rdf.IRI(rdf.PropRequired)+` "true"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresProofSlot)+" "+requiredPathSlot+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCoversPath)+` "docs/**"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCoversPath)+` "command/*.go"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropExpressedBy)+" "+file+" .")
	mustContain(t, out, file+" "+rdf.IRI(rdf.PropImplements)+" "+subj+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropExpressedBy)+" "+testFile+" .")
	mustContain(t, out, testFile+" "+rdf.IRI(rdf.PropImplements)+" "+subj+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDetectForbiddenPattern)+` "fmt\\\\.Fprintf"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDetectMessage)+` "No informational chatter in non-tty mode."`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropConstrainedByInvariant)+" "+rdf.MintIRI(rdf.ClassInvariant, "repo.non_tty_scriptability")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropConstrainedByInvariant)+" "+rdf.MintIRI(rdf.ClassInvariant, "repo.output_must_be_stream_appropriate")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropConstrainedByInvariant)+" "+rdf.MintIRI(rdf.ClassInvariant, "meta.signal_over_noise")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAffects)+" "+rdf.MintIRI(rdf.ClassFailureMode, "repo.interactive_prompt_when_nontty")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAffects)+" "+rdf.MintIRI(rdf.ClassFailureMode, "repo.tty_branch_regression")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropAffects)+" "+rdf.MintIRI(rdf.ClassFailureMode, "repo.unscoped_output_noise")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRelatedTo)+" "+rdf.MintIRI(rdf.ClassIntent, "repo.scriptable_cli_contract")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRelatedTo)+" "+rdf.MintIRI(rdf.ClassIntent, "repo.terminal_mode_is_explicit")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRelatedTo)+" "+rdf.MintIRI(rdf.ClassIntent, "repo.cli_tty_behavior")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresTest)+" "+rdf.MintIRI(rdf.ClassTest, "repo:TestRepoForkNonTTY")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresTest)+" "+rdf.MintIRI(rdf.ClassTest, "repo:TestRepoForkTTY")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRequiresTest)+" "+rdf.MintIRI(rdf.ClassTest, "repo:TestRepoViewNonTTY")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRelatedTo)+" "+rdf.MintIRI(rdf.ClassComponent, "component.command.repo")+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropRelatedTo)+" "+rdf.MintIRI(rdf.ClassComponent, "component.command.output")+" .")
}
