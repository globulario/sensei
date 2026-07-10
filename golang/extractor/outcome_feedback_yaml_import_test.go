// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
	"github.com/globulario/sensei/golang/rdf"
)

// outcomeDir builds a temp directory with the given files and runs
// ImportAwarenessDir on it, returning the emitted N-Triples and the report.
func outcomeDir(t *testing.T, files map[string]string) (string, *extractor.ImportReport) {
	t.Helper()
	var buf bytes.Buffer
	root := makeDir(t, files)
	_, report, err := extractor.ImportAwarenessDir(root, &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessDir: %v", err)
	}
	return buf.String(), report
}

func mustNotContain(t *testing.T, body, needle string) {
	t.Helper()
	if strings.Contains(body, needle) {
		t.Errorf("output unexpectedly contains:\n  %s", needle)
	}
}

// Detection: id + class:OutcomeFeedback routes to the outcome_feedback importer
// (and NOT to the intent importer, which fires on id+level).
func TestOutcomeFeedback_DetectedByIDAndClass(t *testing.T) {
	out, report := outcomeDir(t, map[string]string{
		"o.yaml": `
id: outcome.example
class: OutcomeFeedback
label: Example outcome
outcome_status: success
`,
	})
	assertValidNT(t, out)

	if len(report.Imported()) != 1 {
		t.Fatalf("expected 1 imported file, got %d (skipped=%d)", len(report.Imported()), len(report.Skipped()))
	}
	if got := report.Imported()[0].Schema; got != "outcome_feedback" {
		t.Errorf("schema: want outcome_feedback, got %q", got)
	}
}

// Typed node + label + scalar context literals.
func TestOutcomeFeedback_CoreLiteralsAndTypedNode(t *testing.T) {
	out, _ := outcomeDir(t, map[string]string{
		"o.yaml": `
id: outcome.example
class: OutcomeFeedback
label: Example outcome
status: active
decision: applied
outcome_status: blocked
failure_class: hard_blocked_action
reason_code: needs_approval
observed_at: "2026-06"
for_task: do the risky thing
for_finding: finding.abc
for_workflow_run: run-123
for_step: step-7
used_preflight_status: OK
used_risk_class: DATA_LOSS_RISK
promoted_from_incident: INC-2026-0017
notes: |
  Some context.
`,
	})

	subj := rdf.MintIRI(rdf.ClassOutcomeFeedback, "outcome.example")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassOutcomeFeedback)+" .")
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropLabel)+` "Example outcome"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropDecision)+` "applied"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropOutcomeStatus)+` "blocked"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropFailureClass)+` "hard_blocked_action"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropReasonCode)+` "needs_approval"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForFinding)+` "finding.abc"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForWorkflowRun)+` "run-123"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForStep)+` "step-7"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropUsedPreflightStatus)+` "OK"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropPromotedFromIncident)+` "INC-2026-0017"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropComment)+` "Some context."`)
}

// usedKnowledgeNode links to invariant, failure_mode, implementation_pattern,
// and test nodes resolve to the same minted IRIs those importers produce. The
// finding and workflow-run links are literals, not node edges.
func TestOutcomeFeedback_LinksToKnowledgeNodes(t *testing.T) {
	out, _ := outcomeDir(t, map[string]string{
		"o.yaml": `
id: outcome.example
class: OutcomeFeedback
label: Example
for_finding: critical_state.stale_key/n1/k1
for_workflow_run: run-123
used_knowledge_nodes:
  - invariant:convergence.no_infinite_retry
  - failure_mode:hidden_workflow.controller_remove_node_inline_preflight_and_drain
  - implementation_pattern:globular.pattern.workflow_durable_step_receipt
  - required_test:TestReleaseApplyWorkflows_DispatchOneRunPerReleaseWithPerNodeForeach
`,
	})

	subj := rdf.MintIRI(rdf.ClassOutcomeFeedback, "outcome.example")
	link := func(target string) string {
		return subj + " " + rdf.IRI(rdf.PropUsedKnowledgeNode) + " " + target + " ."
	}
	mustContain(t, out, link(rdf.MintIRI(rdf.ClassInvariant, "convergence.no_infinite_retry")))
	mustContain(t, out, link(rdf.MintIRI(rdf.ClassFailureMode, "hidden_workflow.controller_remove_node_inline_preflight_and_drain")))
	mustContain(t, out, link(rdf.MintIRI(rdf.ClassImplementationPattern, "globular.pattern.workflow_durable_step_receipt")))
	mustContain(t, out, link(rdf.MintIRI(rdf.ClassTest, "TestReleaseApplyWorkflows_DispatchOneRunPerReleaseWithPerNodeForeach")))

	// Finding + workflow run are literal context, not node edges.
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForFinding)+` "critical_state.stale_key/n1/k1"`)
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropForWorkflowRun)+` "run-123"`)
}

// The safety property: an OutcomeFeedback that names invariant:foo links to it
// but must NEVER type foo as an Invariant. Likewise sparse/missing metadata must
// not emit empty literals or fabricate any authority node. Outcome feedback is
// indexed knowledge that points AT authority, it does not create authority.
func TestOutcomeFeedback_DoesNotMintAuthorityOrEmptyLiterals(t *testing.T) {
	out, _ := outcomeDir(t, map[string]string{
		"o.yaml": `
id: outcome.sparse
class: OutcomeFeedback
label: Sparse outcome
used_knowledge_nodes:
  - invariant:totally.made.up.invariant
`,
	})

	subj := rdf.MintIRI(rdf.ClassOutcomeFeedback, "outcome.sparse")
	target := rdf.MintIRI(rdf.ClassInvariant, "totally.made.up.invariant")

	// The link exists...
	mustContain(t, out, subj+" "+rdf.IRI(rdf.PropUsedKnowledgeNode)+" "+target+" .")
	// ...but the referenced invariant is NOT typed as an Invariant by this importer.
	mustNotContain(t, out, target+" "+rdf.IRI(rdf.PropType)+" "+rdf.IRI(rdf.ClassInvariant))
	// ...and it carries no status that would make it look like an active rule.
	mustNotContain(t, out, target+" "+rdf.IRI(rdf.PropStatus))

	// Sparse record: fields that were omitted must produce no literal triples.
	mustNotContain(t, out, subj+" "+rdf.IRI(rdf.PropDecision))
	mustNotContain(t, out, subj+" "+rdf.IRI(rdf.PropOutcomeStatus))
	mustNotContain(t, out, subj+" "+rdf.IRI(rdf.PropFailureClass))
	// Empty-string literal must never appear for any emitted predicate.
	mustNotContain(t, out, `"" .`)
}

// Empty id is a soft skip — no triples, no error, not counted as imported.
func TestOutcomeFeedback_EmptyIDSoftSkip(t *testing.T) {
	out, report := outcomeDir(t, map[string]string{
		"o.yaml": `
class: OutcomeFeedback
label: nameless
`,
	})
	if len(report.Imported()) != 0 {
		t.Errorf("expected 0 imported, got %d", len(report.Imported()))
	}
	mustNotContain(t, out, "OutcomeFeedback")
}
