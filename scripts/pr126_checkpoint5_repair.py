#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if old not in text:
        raise SystemExit(f"{label}: expected source text not found")
    return text.replace(old, new, 1)


compose = Path("golang/architecture/evaluatorcomposition/compose.go")
text = compose.read_text()

marker = "\texecutionByID := make(map[string]EvaluatorExecution, len(ordered))\n"
if "\tcheckOwner := make(map[string]string)" not in text:
    text = replace_once(text, marker, marker + "\tcheckOwner := make(map[string]string)\n", "check-owner marker")

start = text.index("\t\texecutionByID[id] = execution")
end = text.index("\t\tresultChecks := append([]synthesis.CheckObservation(nil), execution.Result.Checks...)", start)
replacement = '''\t\texecutionByID[id] = execution
\t\tif execution.Result.CleanupSucceeded == nil {
\t\t\tbase.FailureDetail = fmt.Sprintf("evaluator %q result has no O4-owned cleanup truth", id)
\t\t\treturn withCompositionEvidence(base, bindings, ordered)
\t\t}

\t\tif execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted && spec.Required {
\t\t\tunavailable := Composition{
\t\t\t\tDisposition:       DispositionRequiredEvaluatorUnavailable,
\t\t\t\tEvaluatorBindings: append([]EvaluatorResultBinding(nil), bindings...),
\t\t\t\tFailureDetail:     fmt.Sprintf("required evaluator %q ended with terminal outcome %q", id, execution.Result.TerminalOutcome),
\t\t\t}
\t\t\treturn finalizeCompositionCleanup(unavailable, ordered)
\t\t}

\t\tbindings = append(bindings, EvaluatorResultBinding{
\t\t\tEvaluatorID:            id,
\t\t\tDescriptorDigestSHA256: execution.Descriptor.DescriptorDigestSHA256,
\t\t\tResultDigestSHA256:     execution.Result.ResultDigestSHA256,
\t\t})
\t\tif execution.Result.TerminalOutcome != EvaluatorOutcomeCompleted {
\t\t\tfailureClasses = append(failureClasses, FailureClassOptionalEvaluatorUnavailable)
\t\t\tlimitations = append(limitations, synthesis.Limitation{
\t\t\t\tSource: "evaluatorcomposition", Scope: id,
\t\t\t\tReason:   fmt.Sprintf("optional evaluator ended with terminal outcome %q", execution.Result.TerminalOutcome),
\t\t\t\tBlocking: false,
\t\t\t})
\t\t}

\t\tfor _, check := range execution.Result.Checks {
\t\t\tif owner, duplicate := checkOwner[check.CheckID]; duplicate {
\t\t\t\tbase.FailureDetail = fmt.Sprintf("duplicate check_id %q is reported by evaluators %q and %q", check.CheckID, owner, id)
\t\t\t\treturn withCompositionEvidence(base, bindings, ordered)
\t\t\t}
\t\t\tcheckOwner[check.CheckID] = id
\t\t}
'''
text = text[:start] + replacement + text[end:]
text = text.replace(
    'Reason: "optional evaluator produced no finalized result", Blocking: true,',
    'Reason: "optional evaluator produced no finalized result", Blocking: false,',
)
text = text.replace(
    'Source: "evaluatorcomposition", Scope: "required-checks", Reason: reason, Blocking: true,',
    'Source: "evaluatorcomposition", Scope: "required-checks", Reason: reason, Blocking: false,',
)
compose.write_text(text)

complete = Path("golang/architecture/evaluatorcomposition/complete.go")
text = complete.read_text()
insertion_marker = '''\tif *handoff.RunnerReceipt.CandidateArtifactDigestSHA256 != checkpoint.Candidate.CandidateArtifactDigestSHA256 {
\t\treturn fmt.Errorf("completion handoff candidate digest does not match checkpoint candidate")
\t}
'''
if "completion handoff O2 documents" not in text:
    insertion = insertion_marker + '''\trequestDigest, resultDigest, o2ReceiptDigest, err := validateHandoffO2Documents(handoff)
\tif err != nil {
\t\treturn fmt.Errorf("completion handoff O2 documents: %w", err)
\t}
\tif handoff.RunnerReceipt.RequestDigestSHA256 != requestDigest || handoff.RunnerReceipt.ResultDigestSHA256 == nil || *handoff.RunnerReceipt.ResultDigestSHA256 != resultDigest || handoff.RunnerReceipt.O2ReceiptDigestSHA256 == nil || *handoff.RunnerReceipt.O2ReceiptDigestSHA256 != o2ReceiptDigest {
\t\treturn fmt.Errorf("completion handoff O2 document digests do not match the accepted RunnerReceipt")
\t}
\tif handoff.Result.GenerationPayload == nil {
\t\treturn fmt.Errorf("completion handoff carries no generation payload")
\t}
\tattempt := *handoff.Result.GenerationPayload
\tattemptDigest, err := synthesis.AttemptDigest(attempt)
\tif err != nil {
\t\treturn fmt.Errorf("completion handoff attempt digest: %w", err)
\t}
\tif attemptDigest != checkpoint.SessionState.LatestAttemptDigestSHA256 {
\t\treturn fmt.Errorf("completion handoff attempt digest %q does not match checkpoint %q", attemptDigest, checkpoint.SessionState.LatestAttemptDigestSHA256)
\t}
\tif err := crossBindCandidate(*checkpoint.Candidate, checkpoint.SessionState, attempt); err != nil {
\t\treturn fmt.Errorf("completion checkpoint candidate binding: %w", err)
\t}
'''
    text = replace_once(text, insertion_marker, insertion, "completion handoff marker")
complete.write_text(text)

proof_test = Path("golang/architecture/evaluatorcomposition/compose_proof_test.go")
text = proof_test.read_text()
start = text.index("func proofExecution(")
end = text.index("func TestComposeEvaluationValidatesExactClosureProofDischargeBytes")
replacement = '''func proofExecution(t *testing.T, checkpoint Result, policy EvaluationPolicy, reference EvidenceReference) EvaluatorExecution {
\tt.Helper()
\tsurface := &recordingEvaluatorSurface{ref: "surface://checkpoint5/proof.evaluator/plain", root: t.TempDir(), mode: SurfaceModePlain}
\tinput, err := BuildEvaluationInput(checkpoint.SessionState, *checkpoint.Candidate, policy, surface)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tdescriptor := evaluatorDescriptorForExecution(t, "proof.evaluator")
\tdescriptor.SupportedCheckIDs = []string{"proof-check"}
\tdescriptor.Limitations = []synthesis.Limitation{}
\tdescriptor = NormalizeEvaluatorDescriptor(descriptor)
\tdescriptorDigest, err := EvaluatorDescriptorDigest(descriptor)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tdescriptor.DescriptorDigestSHA256 = descriptorDigest
\tcleanupSucceeded := true
\tresult := EvaluatorResult{
\t\tSchemaVersion:                   EvaluatorResultSchemaVersion,
\t\tEvaluatorID:                     descriptor.EvaluatorID,
\t\tEvaluatorDescriptorDigestSHA256: descriptor.DescriptorDigestSHA256,
\t\tEvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
\t\tTerminalOutcome:                 EvaluatorOutcomeCompleted,
\t\tChecks: []synthesis.CheckObservation{{
\t\t\tCheckID:            "proof-check",
\t\t\tStatus:             synthesis.CheckPassed,
\t\t\tDetail:             "closure proof discharged",
\t\t\tEvidenceReferences: []string{reference.Reference},
\t\t}},
\t\tEvidenceReferences:       []EvidenceReference{reference},
\t\tClassifiedFailureReasons: []string{},
\t\tLimitations:              []synthesis.Limitation{},
\t\tCleanupSucceeded:         &cleanupSucceeded,
\t}
\tresult = NormalizeEvaluatorResult(result)
\tresultDigest, err := EvaluatorResultDigest(result)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tresult.ResultDigestSHA256 = resultDigest
\tif err := ValidateEvaluatorResult(result); err != nil {
\t\tt.Fatalf("proof execution result invalid: %v", err)
\t}
\treturn EvaluatorExecution{Descriptor: descriptor, Input: input, Result: result}
}

'''
text = text[:start] + replacement + text[end:]
proof_test.write_text(text)

compose_test = Path("golang/architecture/evaluatorcomposition/compose_test.go")
text = compose_test.read_text()
old = '''\t\tif !hasBlockingLimitation(composition.Evaluation.Limitations) {
\t\t\tt.Fatal("optional missing evidence did not preserve a blocking limitation")
\t\t}
'''
new = '''\t\tif len(composition.Evaluation.Limitations) == 0 {
\t\t\tt.Fatal("optional missing evidence did not preserve a limitation")
\t\t}
\t\tif hasBlockingLimitation(composition.Evaluation.Limitations) {
\t\t\tt.Fatal("optional missing evidence was incorrectly promoted to a blocking limitation")
\t\t}
'''
if old in text:
    text = text.replace(old, new, 1)
if "TestComposeEvaluationRejectsDuplicateCheckIDsAcrossEvaluators" not in text:
    text += '''
func TestComposeEvaluationRejectsDuplicateCheckIDsAcrossEvaluators(t *testing.T) {
\t_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
\t\tpolicy.Evaluators = []EvaluatorSpec{{EvaluatorID: "first", Required: true}, {EvaluatorID: "second", Required: true}}
\t\tpolicy.RequiredCheckIDs = []string{}
\t})
\tfirst := checkpoint5Execution(t, checkpoint, policy, "first", EvaluatorOutcomeCompleted,
\t\t[]synthesis.CheckObservation{checkpoint5Check("shared-check", synthesis.CheckPassed)}, nil, nil, true)
\tsecond := checkpoint5Execution(t, checkpoint, policy, "second", EvaluatorOutcomeCompleted,
\t\t[]synthesis.CheckObservation{checkpoint5Check("shared-check", synthesis.CheckPassed)}, nil, nil, true)
\tcomposition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
\t\t[]EvaluatorExecution{second, first}, nil)
\tif composition.Disposition != DispositionCompositionFailure || !strings.Contains(composition.FailureDetail, "duplicate check_id") {
\t\tt.Fatalf("duplicate cross-evaluator check composition = %+v", composition)
\t}
}

func TestComposeEvaluationExcludesUnavailableRequiredEvaluatorFromAcceptedBindings(t *testing.T) {
\t_, checkpoint, policy := checkpoint5ReadyFixture(t, func(policy *EvaluationPolicy) {
\t\tpolicy.Evaluators = []EvaluatorSpec{{EvaluatorID: "complete", Required: true}, {EvaluatorID: "unavailable", Required: true}}
\t\tpolicy.RequiredCheckIDs = []string{}
\t})
\tcomplete := checkpoint5Execution(t, checkpoint, policy, "complete", EvaluatorOutcomeCompleted,
\t\t[]synthesis.CheckObservation{checkpoint5Check("complete-check", synthesis.CheckPassed)}, nil, nil, true)
\tunavailable := checkpoint5Execution(t, checkpoint, policy, "unavailable", EvaluatorOutcomeUnavailable,
\t\t[]synthesis.CheckObservation{}, nil, nil, true)
\tcomposition := ComposeEvaluation(context.Background(), checkpoint.SessionState, *checkpoint.Candidate, policy,
\t\t[]EvaluatorExecution{unavailable, complete}, nil)
\tif composition.Disposition != DispositionRequiredEvaluatorUnavailable {
\t\tt.Fatalf("required unavailable composition = %+v", composition)
\t}
\tif len(composition.EvaluatorBindings) != 1 || composition.EvaluatorBindings[0].EvaluatorID != "complete" {
\t\tt.Fatalf("required unavailable bindings include unaccepted evaluator: %+v", composition.EvaluatorBindings)
\t}
}
'''
compose_test.write_text(text)
