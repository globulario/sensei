#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


compose_path = Path("golang/architecture/evaluatorcomposition/compose.go")
compose = compose_path.read_text()
compose = replace_once(
    compose,
    "\treferencedEvidence := make(map[string]bool)\n",
    "\treferencedEvidence := make(map[string]bool)\n\trequiredUnavailableDetails := make([]string, 0)\n",
    "required-unavailable accumulator",
)
compose = replace_once(
    compose,
    '''\t\tif execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted && spec.Required {\n\t\t\tunavailable := Composition{\n\t\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,\n\t\t\t\tEvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),\n\t\t\t\tFailureDetail:     fmt.Sprintf("required evaluator %q ended with terminal outcome %q", id, execution.Result.TerminalOutcome),\n\t\t\t}\n\t\t\treturn finalizeCompositionCleanup(unavailable, ordered)\n\t\t}\n''',
    '''\t\tif execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted && spec.Required {\n\t\t\trequiredUnavailableDetails = append(requiredUnavailableDetails,\n\t\t\t\tfmt.Sprintf("required evaluator %q ended with terminal outcome %q", id, execution.Result.TerminalOutcome))\n\t\t\tcontinue\n\t\t}\n''',
    "required evaluator early return",
)
compose = replace_once(
    compose,
    '''\t\tif spec.Required {\n\t\t\tunavailable := Composition{\n\t\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,\n\t\t\t\tEvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),\n\t\t\t\tFailureDetail:     fmt.Sprintf("required evaluator %q produced no finalized result", spec.EvaluatorID),\n\t\t\t}\n\t\t\treturn finalizeCompositionCleanup(unavailable, ordered)\n\t\t}\n''',
    '''\t\tif spec.Required {\n\t\t\trequiredUnavailableDetails = append(requiredUnavailableDetails,\n\t\t\t\tfmt.Sprintf("required evaluator %q produced no finalized result", spec.EvaluatorID))\n\t\t\tcontinue\n\t\t}\n''',
    "missing required evaluator early return",
)
compose = replace_once(
    compose,
    '''\tchecks = canonicalChecks(checks)\n\tlimitations = canonicalLimitations(limitations)\n\tfailureClasses = canonicalStrings(failureClasses)\n\tbindings = canonicalBindings(bindings)\n''',
    '''\tbindings = canonicalBindings(bindings)\n\tif len(requiredUnavailableDetails) > 0 {\n\t\tsort.Strings(requiredUnavailableDetails)\n\t\tunavailable := Composition{\n\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,\n\t\t\tEvaluatorBindings: bindings,\n\t\t\tFailureDetail:     strings.Join(requiredUnavailableDetails, "; "),\n\t\t}\n\t\treturn finalizeCompositionCleanup(unavailable, ordered)\n\t}\n\n\tchecks = canonicalChecks(checks)\n\tlimitations = canonicalLimitations(limitations)\n\tfailureClasses = canonicalStrings(failureClasses)\n''',
    "required unavailable finalization",
)
compose_path.write_text(compose)

complete_path = Path("golang/architecture/evaluatorcomposition/complete.go")
complete = complete_path.read_text()
complete = replace_once(
    complete,
    '''\t\t\treturn TerminateEvaluationUnavailable(checkpoint, handoff, policy, DispositionCompositionFailure,\n\t\t\t\t"composer returned evaluated disposition with no Evaluation", executions, now)\n''',
    '''\t\t\treturn terminateEvaluationUnavailableWithBindings(checkpoint, handoff, policy, DispositionCompositionFailure,\n\t\t\t\t"composer returned evaluated disposition with no Evaluation", composition.EvaluatorBindings, executions, now)\n''',
    "nil evaluation terminal path",
)
complete = replace_once(
    complete,
    '''\tcase DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:\n\t\treturn TerminateEvaluationUnavailable(checkpoint, handoff, policy, composition.Disposition,\n\t\t\tcomposition.FailureDetail, executions, now)\n\tdefault:\n\t\treturn TerminateEvaluationUnavailable(checkpoint, handoff, policy, DispositionCompositionFailure,\n\t\t\tfmt.Sprintf("composer returned illegal checkpoint-5 disposition %q", composition.Disposition), executions, now)\n''',
    '''\tcase DispositionRequiredEvaluatorUnavailable, DispositionCompositionFailure:\n\t\treturn terminateEvaluationUnavailableWithBindings(checkpoint, handoff, policy, composition.Disposition,\n\t\t\tcomposition.FailureDetail, composition.EvaluatorBindings, executions, now)\n\tdefault:\n\t\treturn terminateEvaluationUnavailableWithBindings(checkpoint, handoff, policy, DispositionCompositionFailure,\n\t\t\tfmt.Sprintf("composer returned illegal checkpoint-5 disposition %q", composition.Disposition), composition.EvaluatorBindings, executions, now)\n''',
    "composer terminal paths",
)
complete = replace_once(
    complete,
    '''func TerminateEvaluationUnavailable(\n\tcheckpoint Result,\n\thandoff runnercomposition.VerifiedGenerationHandoff,\n\tpolicy EvaluationPolicy,\n\tdisposition Disposition,\n\tfailureDetail string,\n\texecutions []EvaluatorExecution,\n\tnow func() time.Time,\n) (Result, error) {\n''',
    '''func TerminateEvaluationUnavailable(\n\tcheckpoint Result,\n\thandoff runnercomposition.VerifiedGenerationHandoff,\n\tpolicy EvaluationPolicy,\n\tdisposition Disposition,\n\tfailureDetail string,\n\texecutions []EvaluatorExecution,\n\tnow func() time.Time,\n) (Result, error) {\n\treturn terminateEvaluationUnavailableWithBindings(checkpoint, handoff, policy, disposition, failureDetail,\n\t\tbindingsFromExecutions(executions), executions, now)\n}\n\nfunc terminateEvaluationUnavailableWithBindings(\n\tcheckpoint Result,\n\thandoff runnercomposition.VerifiedGenerationHandoff,\n\tpolicy EvaluationPolicy,\n\tdisposition Disposition,\n\tfailureDetail string,\n\tbindings []EvaluatorResultBinding,\n\texecutions []EvaluatorExecution,\n\tnow func() time.Time,\n) (Result, error) {\n''',
    "terminal helper split",
)
complete = replace_once(
    complete,
    "\treceipt.EvaluatorResultBindings = bindingsFromExecutions(executions)\n",
    "\treceipt.EvaluatorResultBindings = append([]EvaluatorResultBinding{}, canonicalBindings(bindings)...)\n",
    "terminal accepted bindings",
)
complete_path.write_text(complete)

complete_test_path = Path("golang/architecture/evaluatorcomposition/complete_test.go")
complete_test = complete_test_path.read_text()
complete_test = replace_once(
    complete_test,
    '''import (\n\t"context"\n\t"strings"\n\t"testing"\n''',
    '''import (\n\t"context"\n\t"reflect"\n\t"strings"\n\t"testing"\n''',
    "test reflect import",
)
new_test = r'''
func TestCompleteEvaluationRequiredUnavailableReceiptUsesAcceptedBindingsIndependentOfOrder(t *testing.T) {
	handoff, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
		policy.Evaluators = []EvaluatorSpec{
			{EvaluatorID: "a.unavailable", Required: true},
			{EvaluatorID: "z.completed", Required: true},
		}
		policy.RequiredCheckIDs = []string{}
	})
	unavailable := checkpoint5Execution(t, checkpoint, policy, "a.unavailable", EvaluatorOutcomeUnavailable,
		[]synthesis.CheckObservation{}, nil, nil, true)
	completed := checkpoint5Execution(t, checkpoint, policy, "z.completed", EvaluatorOutcomeCompleted,
		[]synthesis.CheckObservation{checkpoint5Check("z.completed-check", synthesis.CheckPassed)}, nil, nil, true)

	orders := []struct {
		name       string
		executions []EvaluatorExecution
	}{
		{name: "unavailable first", executions: []EvaluatorExecution{unavailable, completed}},
		{name: "completed first", executions: []EvaluatorExecution{completed, unavailable}},
	}

	var firstBindings []EvaluatorResultBinding
	var firstReceiptDigest string
	for i, order := range orders {
		t.Run(order.name, func(t *testing.T) {
			result, err := CompleteEvaluation(context.Background(), checkpoint, handoff, policy,
				order.executions, nil, runFixedNow)
			if err != nil {
				t.Fatal(err)
			}
			assertEvaluatorUnavailableCompletion(t, result, DispositionRequiredEvaluatorUnavailable)
			bindings := result.Receipt.EvaluatorResultBindings
			if len(bindings) != 1 || bindings[0].EvaluatorID != "z.completed" {
				t.Fatalf("required-unavailable receipt bindings = %+v, want only z.completed", bindings)
			}
			if i == 0 {
				firstBindings = append([]EvaluatorResultBinding(nil), bindings...)
				firstReceiptDigest = result.Receipt.ReceiptDigestSHA256
				return
			}
			if !reflect.DeepEqual(bindings, firstBindings) {
				t.Fatalf("receipt bindings depend on execution order: first=%+v second=%+v", firstBindings, bindings)
			}
			if result.Receipt.ReceiptDigestSHA256 != firstReceiptDigest {
				t.Fatalf("receipt digest depends on execution order: first=%q second=%q", firstReceiptDigest, result.Receipt.ReceiptDigestSHA256)
			}
		})
	}
}

'''
marker = "func TestTerminateEvaluationUnavailableRecordsMaterializationFailure(t *testing.T) {\n"
complete_test = replace_once(complete_test, marker, new_test + marker, "end-to-end required unavailable test")
complete_test_path.write_text(complete_test)
