// SPDX-License-Identifier: Apache-2.0

package knowledgeadoption_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/knowledgeadoption"
	"github.com/globulario/sensei/golang/rdf"
)

func adoptionFixture(t *testing.T, files map[string]string, graph string) knowledgeadoption.Result {
	t.Helper()
	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := knowledgeadoption.Run(knowledgeadoption.Options{
		RepositoryRoot: root, RepositoryDomain: "example.com/repo",
		Revision: strings.Repeat("1", 40), GraphDigest: strings.Repeat("a", 64),
		DecisionTimestamp: "2026-07-14T09:00:00Z", ProvisionalGraph: []byte(graph),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func decisionFor(t *testing.T, result knowledgeadoption.Result, id string) knowledgeadoption.CandidateDecision {
	t.Helper()
	for _, decision := range result.Report.Decisions {
		if decision.CandidateID == id {
			return decision
		}
	}
	t.Fatalf("candidate %s missing from report", id)
	return knowledgeadoption.CandidateDecision{}
}

func componentGraph(ids ...string) string {
	var out strings.Builder
	for _, id := range ids {
		subj := rdf.MintIRI(rdf.ClassComponent, id)
		out.WriteString(subj + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassComponent) + " .\n")
	}
	return out.String()
}

func componentFileGraph(component, file string) string {
	subj := rdf.MintIRI(rdf.ClassComponent, component)
	fileIRI := rdf.MintIRI(rdf.ClassSourceFile, file)
	return subj + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassComponent) + " .\n" +
		subj + " " + rdf.IRI(rdf.PropAnchoredIn) + " " + fileIRI + " .\n" +
		fileIRI + " " + rdf.IRI(rdf.PropType) + " " + rdf.IRI(rdf.ClassSourceFile) + " .\n"
}

const completeContractReceipt = `
status: machine_adopted
promotion_status: machine_adopted
assertion_origin: model_inferred
epistemic_status: supported
architectural_plane: intended
decision_actor: sensei.intent_mine
decision_context: project_reconstruction
decision_policy: adoption.intent.strong_grounding.v1
decision_timestamp: "2026-07-14T09:00:00Z"
valid_for_revision: "1111111111111111111111111111111111111111"
valid_for_graph_digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
review_status: not_human_reviewed
`

func TestStrongCorroboratedInvariantIsMachineAdopted(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n", "a_test.go": "package a\n",
		"docs/awareness/candidates/invariant/rule.yaml": `
id: candidate.writer_monotonic
class: InvariantCandidate
statement: Writer state never moves from written back to unwritten.
confidence: high
source_paths: [file:a.go, file:a_test.go]
invalidation_conditions: [writer state representation changes]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.writer_monotonic"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("decision=%+v", got)
	}
}

func TestWeakInvariantRemainsStaged(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n",
		"docs/awareness/candidates/invariant/rule.yaml": `
id: candidate.writer_guess
class: InvariantCandidate
statement: Writer behavior might matter.
confidence: medium
source_paths: [file:a.go]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.writer_guess"); got.Outcome != knowledgeadoption.OutcomeStaged {
		t.Fatalf("decision=%+v", got)
	}
}

func TestRepeatedConcreteFailureModeIsMachineAdopted(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"context.go": "package a\n",
		"docs/awareness/candidates/failure_mode/context.yaml": `
id: candidate.context_stream
class: FailureModeCandidate
theme: context stream
reason: Two independent regressions panic when a synthetic context has no request.
confidence: high
source_paths: [file:context.go, "pr:4217:1", "pr:4662:2"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.context_stream"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("decision=%+v", got)
	}
}

func TestVagueBugThemeRemainsStaged(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"route.go": "package a\n",
		"docs/awareness/candidates/failure_mode/route.yaml": `
id: candidate.route_theme
class: FailureModeCandidate
theme: routes
reason: Several changes touched routing and may be related.
confidence: medium
source_paths: [file:route.go, "pr:1:1", "pr:2:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.route_theme"); got.Outcome != knowledgeadoption.OutcomeStaged {
		t.Fatalf("decision=%+v", got)
	}
}

func TestExplicitRevertForbiddenFixIsMachineAdopted(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"query.go": "package a\n",
		"docs/awareness/candidates/forbidden_fix/query.yaml": `
id: candidate.query_binding
class: ForbiddenFixCandidate
theme: query binding
reason: An explicit revert rejects reintroducing the broken query binding change.
confidence: medium
source_paths: [file:query.go, "pr:3899:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.query_binding"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("decision=%+v", got)
	}
}

func TestCompilerEnforcedInternalBoundaryIsMachineAdopted(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"internal/x/x.go": "package x\n",
		"docs/awareness/candidates/boundary_candidates.yaml": `
boundaries:
  - id: boundary.visibility.internal.x
    name: x internal boundary
    kind: visibility
    description: "Go internal/ package: compiler-enforced module visibility."
    separates: [component.internal.x]
    source_files: [internal/x/x.go]
`,
	}, componentGraph("component.internal.x"))
	if got := decisionFor(t, result, "boundary.visibility.internal.x"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("decision=%+v", got)
	}
}

func TestDanglingBoundaryComponentIsRejected(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"internal/x/x.go": "package x\n",
		"docs/awareness/candidates/boundary_candidates.yaml": `
boundaries:
  - id: boundary.visibility.internal.x
    name: x internal boundary
    kind: visibility
    description: "Go internal/ package: compiler-enforced module visibility."
    separates: [component.missing]
    source_files: [internal/x/x.go]
`,
	}, componentGraph("component.internal.x"))
	if got := decisionFor(t, result, "boundary.visibility.internal.x"); got.Outcome != knowledgeadoption.OutcomeRejected {
		t.Fatalf("decision=%+v", got)
	}
}

func TestConflictingCandidateRemainsContestedOrStaged(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n", "a_test.go": "package a\n",
		"docs/awareness/candidates/invariant/rule.yaml": `
id: candidate.writer_monotonic
class: InvariantCandidate
statement: Writer state never regresses.
confidence: high
source_paths: [file:a.go, file:a_test.go]
invalidation_conditions: [writer state changes]
contradictions: [another source disagrees]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.writer_monotonic"); got.Outcome != knowledgeadoption.OutcomeStaged {
		t.Fatalf("decision=%+v", got)
	}
	for _, summary := range result.Report.Classes {
		if summary.Class == knowledgeadoption.ClassInvariant && summary.Contested != 1 {
			t.Fatalf("invariant summary=%+v", summary)
		}
	}
}

func TestNoCandidateBecomesGoverned(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"query.go": "package a\n",
		"docs/awareness/candidates/forbidden_fix/query.yaml": `
id: candidate.query_binding
class: ForbiddenFixCandidate
reason: An explicit revert rejects the broken query change.
source_paths: [file:query.go, "pr:2:1"]
`,
	}, "")
	for name, path := range result.Paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "status: governed") || strings.Contains(string(raw), "promotion_status: governed") {
			t.Fatalf("%s emitted governed knowledge:\n%s", name, raw)
		}
	}
	for _, summary := range result.Report.Classes {
		if summary.Governed != 0 {
			t.Fatalf("class summary reported automatic governance: %+v", summary)
		}
	}
}

func TestExplicitMaintainerRationaleCreatesDecisionCandidate(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"binding.go": "package binding\n",
		"docs/awareness/candidates/forbidden_fix/defaults.yaml": `
id: candidate.binding_defaults
class: ForbiddenFixCandidate
theme: binding defaults
reason: A maintainer explicitly rejects adding array support, reasoning that complexity outweighs value. This is a deliberate authority decision to keep defaults scalar rather than an accidental omission.
source_paths: [file:binding.go, "pr:1797:1"]
`,
	}, "")
	decision := decisionFor(t, result, "candidate.decision_choice.binding_defaults")
	if decision.Outcome != knowledgeadoption.OutcomeMachineAdopted || !strings.HasPrefix(decision.KnowledgeID, "decision.history.") {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestDecisionRequiresChoiceAndRationale(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n",
		"docs/awareness/candidates/decision/incomplete.yaml": `
id: candidate.incomplete_decision
class: DecisionCandidate
choice: Keep one path.
source_paths: [file:a.go, "pr:1:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.incomplete_decision"); got.Outcome != knowledgeadoption.OutcomeStaged {
		t.Fatalf("decision=%+v", got)
	}
}

func TestHistoricalDecisionUsesHistoricalPlane(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n",
		"docs/awareness/candidates/decision/historical.yaml": `
id: candidate.one_path
class: DecisionCandidate
choice: Keep one parse path.
rationale: Duplicated parsing produced divergent behavior.
alternatives_rejected: [duplicate parsing in each entry point]
architectural_plane: historical
source_paths: [file:a.go, "pr:2:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.one_path"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("decision=%+v", got)
	}
	raw, err := os.ReadFile(result.Paths["decisions"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "architectural_plane: historical") {
		t.Fatalf("historical plane missing:\n%s", raw)
	}
}

func TestCurrentDecisionRequiresCurrentApplicabilityEvidence(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n",
		"docs/awareness/candidates/decision/current.yaml": `
id: candidate.current_path
class: DecisionCandidate
choice: Keep one parse path.
rationale: Duplicated parsing produced divergent behavior.
alternatives_rejected: [duplicate parsing]
architectural_plane: intended
source_paths: [file:a.go, "pr:2:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.current_path"); got.Outcome != knowledgeadoption.OutcomeStaged {
		t.Fatalf("decision=%+v", got)
	}
}

func TestSupersededDecisionIsNotCurrent(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"a.go": "package a\n",
		"docs/awareness/candidates/decision/old.yaml": `
id: candidate.old_path
class: DecisionCandidate
choice: Keep the old parse path.
rationale: It once avoided duplication.
alternatives_rejected: [new parser]
architectural_plane: historical
superseded_by: decision.new_path
source_paths: [file:a.go, "pr:2:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.old_path"); got.Outcome != knowledgeadoption.OutcomeStaged {
		t.Fatalf("decision=%+v", got)
	}
}

func TestDecisionAndForbiddenFixAreNotDuplicatedWithoutDistinctPropositions(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"binding.go": "package binding\n",
		"docs/awareness/candidates/forbidden_fix/defaults.yaml": `
id: candidate.binding_defaults
class: ForbiddenFixCandidate
theme: binding defaults
reason: A maintainer explicitly rejects adding array support, reasoning that complexity outweighs value. This is a deliberate authority decision to keep defaults scalar rather than an accidental omission.
source_paths: [file:binding.go, "pr:1797:1"]
`,
	}, "")
	fix := decisionFor(t, result, "candidate.binding_defaults")
	decision := decisionFor(t, result, "candidate.decision_choice.binding_defaults")
	if fix.KnowledgeID == decision.KnowledgeID || fix.Outcome != knowledgeadoption.OutcomeMachineAdopted || decision.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("fix=%+v decision=%+v", fix, decision)
	}
}

func TestExplicitProductionRegressionMayCreateIncidentCandidate(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"writer.go": "package a\n",
		"docs/awareness/candidates/incident/regression.yaml": `
id: candidate.writer_release_regression
class: IncidentCandidate
title: Writer release regression
statement: A production regression caused a compatibility break for callers.
severity: high
event_time_or_revision_range: v1.2.0..v1.2.1
observed_consequence: callers received invalid response state
linked_failure_mode: failure_mode.writer_state
resolution: resolved by release rollback
source_paths: [file:writer.go, "pr:20:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.writer_release_regression"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("decision=%+v", got)
	}
}

func TestOrdinaryBugFixDoesNotCreateIncident(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"writer.go": "package a\n",
		"docs/awareness/candidates/failure_mode/bug.yaml": `
id: candidate.writer_bug
class: FailureModeCandidate
reason: A bug caused an incorrect writer value in two reviews.
severity: medium
source_paths: [file:writer.go, "pr:1:1", "pr:2:1"]
`,
	}, "")
	for _, decision := range result.Report.Decisions {
		if decision.CandidateClass == "IncidentCandidate" {
			t.Fatalf("ordinary bug created incident: %+v", decision)
		}
	}
}

func TestFailureSurfaceWithoutEventCreatesFailureModeOnly(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"writer.go": "package a\n",
		"docs/awareness/candidates/failure_mode/bug.yaml": `
id: candidate.writer_bug
class: FailureModeCandidate
reason: A recurring regression returns an incorrect writer state.
severity: medium
source_paths: [file:writer.go, "pr:1:1", "pr:2:1"]
`,
	}, "")
	if got := decisionFor(t, result, "candidate.writer_bug"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("failure mode=%+v", got)
	}
	raw, err := os.ReadFile(result.Paths["incidents"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "incident_id:") {
		t.Fatalf("failure surface invented incident:\n%s", raw)
	}
}

func TestContractIntentAloneDoesNotCreateContract(t *testing.T) {
	result := adoptionFixture(t, map[string]string{
		"binding.go": "package binding\n", "binding_test.go": "package binding\n",
		"docs/intent/binding.yaml": `
id: binding.intent_alone
level: contract
title: Binding contract
intent: Binding reads a request and populates a target.
expressed_by: [binding.go]
required_tests: [binding_test.go]
`,
	}, componentFileGraph("component.binding", "binding.go"))
	if got := decisionFor(t, result, "candidate.binding.intent_alone"); got.Outcome != knowledgeadoption.OutcomeStaged || !containsString(got.MissingFields, "machine_adopted_intent_receipt") {
		t.Fatalf("contract decision=%+v", got)
	}
}

func TestCompleteContractShapeMayBeMachineAdopted(t *testing.T) {
	result := completeContractFixture(t, "")
	if got := decisionFor(t, result, "candidate.binding.complete_contract"); got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("contract decision=%+v", got)
	}
	raw, err := os.ReadFile(result.Paths["contracts"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "public_consumer_category: external Go caller") || !strings.Contains(string(raw), "exposed_by:") {
		t.Fatalf("materialized contract incomplete:\n%s", raw)
	}
}

func TestMissingProviderStagesContractCandidate(t *testing.T) {
	result := completeContractFixture(t, "missing_provider")
	got := decisionFor(t, result, "candidate.binding.complete_contract")
	if got.Outcome != knowledgeadoption.OutcomeStaged || !containsString(got.MissingFields, "provider_component") {
		t.Fatalf("contract decision=%+v", got)
	}
}

func TestMissingConsumerStagesContractCandidate(t *testing.T) {
	result := contractFixture(t, `
id: mystery.complete_contract
level: contract
title: Mystery behavior
intent: The implementation reads input.
public_consumer_category: ""
interaction: public_go_interface
read_or_write: read
stability: stable
expressed_by: [binding.go]
required_tests: [binding_test.go]
`+completeContractReceipt, componentFileGraph("component.binding", "binding.go"))
	got := decisionFor(t, result, "candidate.mystery.complete_contract")
	if got.Outcome != knowledgeadoption.OutcomeStaged || !containsString(got.MissingFields, "consumer_component_or_public_category") {
		t.Fatalf("contract decision=%+v", got)
	}
}

func TestExternalLibraryConsumerCategoryIsSupported(t *testing.T) {
	result := completeContractFixture(t, "")
	got := decisionFor(t, result, "candidate.binding.complete_contract")
	if got.Outcome != knowledgeadoption.OutcomeMachineAdopted {
		t.Fatalf("contract decision=%+v", got)
	}
}

func TestMissingReadWriteSemanticsStagesCandidate(t *testing.T) {
	result := contractFixture(t, `
id: binding.missing_semantics
level: contract
title: Binding boundary
intent: A stable behavior exists.
public_consumer_category: external Go caller
interaction: public_go_interface
stability: stable
expressed_by: [binding.go]
required_tests: [binding_test.go]
`+completeContractReceipt, componentFileGraph("component.binding", "binding.go"))
	got := decisionFor(t, result, "candidate.binding.missing_semantics")
	if got.Outcome != knowledgeadoption.OutcomeStaged || !containsString(got.MissingFields, "read_write_semantics") {
		t.Fatalf("contract decision=%+v", got)
	}
}

func TestMissingRequiredTestOrEvidenceStagesCandidate(t *testing.T) {
	result := contractFixture(t, `
id: binding.missing_test
level: contract
title: Binding boundary
intent: Binding reads request input.
public_consumer_category: external Go caller
interaction: public_go_interface
read_or_write: read
stability: stable
expressed_by: [binding.go]
`+completeContractReceipt, componentFileGraph("component.binding", "binding.go"))
	got := decisionFor(t, result, "candidate.binding.missing_test")
	if got.Outcome != knowledgeadoption.OutcomeStaged || !containsString(got.MissingFields, "required_test_or_evidence") {
		t.Fatalf("contract decision=%+v", got)
	}
}

func TestContractMaterializationPreservesIntentProvenance(t *testing.T) {
	result := completeContractFixture(t, "")
	raw, err := os.ReadFile(result.Paths["contracts"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "documentation:docs/intent/contract.yaml") || !strings.Contains(string(raw), "file:binding.go") {
		t.Fatalf("intent provenance missing:\n%s", raw)
	}
}

func completeContractFixture(t *testing.T, mode string) knowledgeadoption.Result {
	graph := componentFileGraph("component.binding", "binding.go")
	if mode == "missing_provider" {
		graph = componentGraph("component.binding")
	}
	return contractFixture(t, `
id: binding.complete_contract
level: contract
title: Binding contract
intent: Binding reads the request and populates the caller target.
public_consumer_category: external Go caller
interaction: public_go_interface
read_or_write: read_write
stability: stable
expressed_by: [binding.go]
required_tests: [binding_test.go]
`+completeContractReceipt, graph)
}

func contractFixture(t *testing.T, intent, graph string) knowledgeadoption.Result {
	t.Helper()
	return adoptionFixture(t, map[string]string{
		"binding.go": "package binding\n", "binding_test.go": "package binding\n",
		"docs/intent/contract.yaml": intent,
	}, graph)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
