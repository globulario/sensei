// SPDX-License-Identifier: AGPL-3.0-only

package extractor_test

import (
	"testing"

	"github.com/globulario/sensei/golang/rdf"
)

func TestLearningEvent_ImportedAndLinkedToCertificationContext(t *testing.T) {
	out, report := outcomeDir(t, map[string]string{
		"learning.yaml": `
learning_event:
  id: learning.mode_d.cli__cli-1388.20260618T120600Z
  task: cli__cli-1388
  mode: D
  model: claude-opus-4-8
  run_signature: 7a205aab80610bf2324ecc169af3c62a208d5fad
  learning_evidence: usable
  learning_allowed: true
  promotion_allowed: true
  certification_status: certified_clean_repair
  certifiable: true
  governing_contract_id: contract.repo_fork_and_view_nontty_scriptability
  human_review_required: false
  promoted_lesson_candidates:
    - invariant:convergence.no_infinite_retry
  diagnosis:
    primary_failure_mode: clean_contract_repair
  decision:
    action: stop_and_report
  certification:
    reason: Explicit frozen-contract evidence was sufficient.
    missing_evidence: []
  current:
    score: 97
  previous:
    score: 90
  lesson: Contract precision changed repair quality.
`,
	})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d", len(report.Imported()))
	}
	if got := report.Imported()[0].Schema; got != "learning_event" {
		t.Fatalf("schema: want learning_event, got %q", got)
	}

	subj := rdf.MintIRI(rdf.ClassLearningEvent, "learning.mode_d.cli__cli-1388.20260618T120600Z")
	contract := rdf.MintIRI(rdf.ClassContract, "contract.repo_fork_and_view_nontty_scriptability")
	invariant := rdf.MintIRI(rdf.ClassInvariant, "convergence.no_infinite_retry")

	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassLearningEvent)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassOutcomeFeedback)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropMode)+` "D"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropModelName)+` "claude-opus-4-8"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropLearningEvidence)+` "usable"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCertificationStatus)+` "certified_clean_repair"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropPrimaryFailureMode)+` "clean_contract_repair"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropCurrentScore)+` "97"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropPreviousScore)+` "90"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropGovernedByContract)+" "+contract+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropUsedKnowledgeNode)+" "+contract+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropUsedKnowledgeNode)+" "+invariant+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropComment)+` "Contract precision changed repair quality.\n\nExplicit frozen-contract evidence was sufficient."`)
}
