#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if old not in text:
        raise SystemExit(f"{label}: expected source text not found")
    return text.replace(old, new, 1)


compose = Path("golang/architecture/evaluatorcomposition/compose.go")
text = compose.read_text()
text = replace_once(
    text,
    "\treferencedEvidence := make(map[string]bool)\n",
    "\treferencedEvidence := make(map[string]bool)\n\trequiredUnavailableDetail := \"\"\n",
    "required unavailable accumulator",
)
text = replace_once(
    text,
    '''\t\tif execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted && spec.Required {
\t\t\tunavailable := Composition{
\t\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,
\t\t\t\tEvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),
\t\t\t\tFailureDetail:     fmt.Sprintf("required evaluator %q ended with terminal outcome %q", id, execution.Result.TerminalOutcome),
\t\t\t}
\t\t\treturn finalizeCompositionCleanup(unavailable, ordered)
\t\t}
''',
    '''\t\tif execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted && spec.Required {
\t\t\tif requiredUnavailableDetail == "" {
\t\t\t\trequiredUnavailableDetail = fmt.Sprintf("required evaluator %q ended with terminal outcome %q", id, execution.Result.TerminalOutcome)
\t\t\t}
\t\t\tcontinue
\t\t}
''',
    "required unavailable early return",
)
text = replace_once(
    text,
    '''\t\tif spec.Required {
\t\t\tunavailable := Composition{
\t\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,
\t\t\t\tEvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),
\t\t\t\tFailureDetail:     fmt.Sprintf("required evaluator %q produced no finalized result", spec.EvaluatorID),
\t\t\t}
\t\t\treturn finalizeCompositionCleanup(unavailable, ordered)
\t\t}
''',
    '''\t\tif spec.Required {
\t\t\tif requiredUnavailableDetail == "" {
\t\t\t\trequiredUnavailableDetail = fmt.Sprintf("required evaluator %q produced no finalized result", spec.EvaluatorID)
\t\t\t}
\t\t\tcontinue
\t\t}
''',
    "missing required evaluator early return",
)
text = replace_once(
    text,
    '''\tchecks = canonicalChecks(checks)
''',
    '''\tif requiredUnavailableDetail != "" {
\t\tunavailable := Composition{
\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,
\t\t\tEvaluatorBindings: canonicalBindings(bindings),
\t\t\tFailureDetail:     requiredUnavailableDetail,
\t\t}
\t\treturn finalizeCompositionCleanup(unavailable, ordered)
\t}

\tchecks = canonicalChecks(checks)
''',
    "required unavailable finalization",
)
compose.write_text(text)

complete = Path("golang/architecture/evaluatorcomposition/complete.go")
text = complete.read_text()
text = replace_once(
    text,
    '''\tcase DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:
\t\treturn TerminateEvaluationUnavailable(checkpoint, handoff, policy, composition.Disposition,
\t\t\tcomposition.FailureDetail, executions, now)
\tdefault:
\t\treturn TerminateEvaluationUnavailable(checkpoint, handoff, policy, DispositionCompositionFailure,
\t\t\tfmt.Sprintf("composer returned illegal checkpoint-5 disposition %q", composition.Disposition), executions, now)
''',
    '''\tcase DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:
\t\treturn terminateEvaluationUnavailable(checkpoint, handoff, policy, composition.Disposition,
\t\t\tcomposition.FailureDetail, composition.EvaluatorBindings, executions, now)
\tdefault:
\t\treturn terminateEvaluationUnavailable(checkpoint, handoff, policy, DispositionCompositionFailure,
\t\t\tfmt.Sprintf("composer returned illegal checkpoint-5 disposition %q", composition.Disposition), composition.EvaluatorBindings, executions, now)
''',
    "composition terminalization bindings",
)
text = replace_once(
    text,
    '''func TerminateEvaluationUnavailable(
\tcheckpoint Result,
\thandoff runnercomposition.VerifiedGenerationHandoff,
\tpolicy EvaluationPolicy,
\tdisposition Disposition,
\tfailureDetail string,
\texecutions []EvaluatorExecution,
\tnow func() time.Time,
) (Result, error) {
''',
    '''func TerminateEvaluationUnavailable(
\tcheckpoint Result,
\thandoff runnercomposition.VerifiedGenerationHandoff,
\tpolicy EvaluationPolicy,
\tdisposition Disposition,
\tfailureDetail string,
\texecutions []EvaluatorExecution,
\tnow func() time.Time,
) (Result, error) {
\treturn terminateEvaluationUnavailable(checkpoint, handoff, policy, disposition, failureDetail,
\t\tbindingsFromExecutions(executions), executions, now)
}

func terminateEvaluationUnavailable(
\tcheckpoint Result,
\thandoff runnercomposition.VerifiedGenerationHandoff,
\tpolicy EvaluationPolicy,
\tdisposition Disposition,
\tfailureDetail string,
\tacceptedBindings []EvaluatorResultBinding,
\texecutions []EvaluatorExecution,
\tnow func() time.Time,
) (Result, error) {
''',
    "terminalization helper",
)
text = replace_once(
    text,
    "\treceipt.EvaluatorResultBindings = bindingsFromExecutions(executions)\n",
    "\treceipt.EvaluatorResultBindings = canonicalBindings(acceptedBindings)\n",
    "accepted receipt bindings",
)
complete.write_text(text)

complete_test = Path("golang/architecture/evaluatorcomposition/complete_test.go")
text = complete_test.read_text()
marker = '''\tt.Run("composition failure", func(t *testing.T) {
'''
new_test = '''\tt.Run("required unavailable excludes itself and preserves later completed evidence", func(t *testing.T) {
\t\thandoff, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
\t\t\tpolicy.Evaluators = []EvaluatorSpec{
\t\t\t\t{EvaluatorID: "a.unavailable", Required: true},
\t\t\t\t{EvaluatorID: "z.completed", Required: true},
\t\t\t}
\t\t\tpolicy.RequiredCheckIDs = []string{}
\t\t})
\t\tcompleted := checkpoint5Execution(t, checkpoint, policy, "z.completed", EvaluatorOutcomeCompleted,
\t\t\t[]synthesis.CheckObservation{checkpoint5Check("completed-check", synthesis.CheckPassed)}, nil, nil, true)
\t\tunavailable := checkpoint5Execution(t, checkpoint, policy, "a.unavailable", EvaluatorOutcomeUnavailable,
\t\t\t[]synthesis.CheckObservation{}, nil, nil, true)
\t\tresult, err := CompleteEvaluation(context.Background(), checkpoint, handoff, policy,
\t\t\t[]EvaluatorExecution{completed, unavailable}, nil, runFixedNow)
\t\tif err != nil {
\t\t\tt.Fatal(err)
\t\t}
\t\tassertEvaluatorUnavailableCompletion(t, result, DispositionRequiredEvaluatorUnavailable)
\t\tif len(result.Receipt.EvaluatorResultBindings) != 1 || result.Receipt.EvaluatorResultBindings[0].EvaluatorID != "z.completed" {
\t\t\tt.Fatalf("required-unavailable receipt bindings = %+v, want only completed evaluator evidence", result.Receipt.EvaluatorResultBindings)
\t\t}
\t})

'''
text = replace_once(text, marker, new_test + marker, "end-to-end required unavailable regression")
complete_test.write_text(text)
