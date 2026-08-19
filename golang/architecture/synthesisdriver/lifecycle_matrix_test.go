// SPDX-License-Identifier: AGPL-3.0-only

// lifecycle_matrix_test.go is issue #149's completion proof matrix.
//
// PROOF BOUNDARY. Hermetic proof of synthesis lifecycle COMPOSITION. Real
// infrastructure and process integration is witnessed separately by
// scripts/synthesis-run-smoke.sh.
//
// Neither layer impersonates the other. This file answers "does the governed
// composition behave as contracted, on every PR"; the smoke answers "does that
// composition still work against a real Oxigraph, a real graph server, and a
// real vendor subprocess", once, deliberately. A matrix that only existed in a
// ten-minute manual script would stop being true without anyone noticing; a
// smoke that duplicated all of these rows would become a second matrix that
// eventually disagrees with this one.
//
// Every row here enters through the public Run/Resume dispatcher -- the same
// surface cmd/awg enters -- with fakes only at the owner boundaries. Calling
// owners directly would prove N components rather than N lifecycle
// compositions, which is the thing the matrix exists to protect.
package synthesisdriver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// alwaysFails is the evaluation predicate for the exhaustion rows: every
// attempt fails, so the budget is the only thing that can stop the session.
func alwaysFails(evaluatorcomposition.EvaluationInput) bool { return true }

// Retry exhaustion, proved across the whole budget range rather than at one
// point. Asserting "2 attempts" for a budget of 1 would pass just as well
// against a constant; asserting attempts == budget+1 for several budgets is
// what makes the number track the grant.
//
// The session must END when the budget is spent. An exhausted budget that
// silently allowed one more attempt would make every budget advisory.
func TestRetryExhaustionEndsTheSessionWithoutAnotherAttempt(t *testing.T) {
	for _, budget := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			state, config := lifecycleHarness(t, budget, 0,
				string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
				synthesis.RecommendRetryGeneration, alwaysFails)

			result, err := Run(context.Background(), state, config)
			if err != nil {
				t.Fatal(err)
			}
			if result.Receipt.Disposition != DispositionTerminalFailure {
				t.Fatalf("disposition = %q, want %q once the retry budget is spent", result.Receipt.Disposition, DispositionTerminalFailure)
			}
			if consumed := result.SessionState.ConsumedRetryBudget(); consumed != budget {
				t.Fatalf("consumed retry budget = %d, want exactly the %d granted", consumed, budget)
			}
			if attempts := len(result.Trace.GenerationHandoffs); attempts != budget+1 {
				t.Fatalf("generation attempts = %d, want %d (the original plus its %d retries)", attempts, budget+1, budget)
			}
			if result.SessionState.PlanGeneration != 1 {
				t.Fatalf("retry exhaustion changed the plan generation to %d; that is a replan, not a retry", result.SessionState.PlanGeneration)
			}
		})
	}
}

// Replan exhaustion, the same law one level up: a spent replan budget ends the
// session instead of reserving another plan generation.
func TestReplanExhaustionEndsTheSessionWithoutAnotherPlan(t *testing.T) {
	for _, budget := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			state, config := lifecycleHarness(t, 0, budget,
				string(evaluatorcomposition.FailureClassProofPlanStructural),
				synthesis.RecommendReplan, alwaysFails)

			result, err := Run(context.Background(), state, config)
			if err != nil {
				t.Fatal(err)
			}
			if result.Receipt.Disposition != DispositionTerminalFailure {
				t.Fatalf("disposition = %q, want %q once the replan budget is spent", result.Receipt.Disposition, DispositionTerminalFailure)
			}
			if consumed := result.SessionState.ConsumedReplanBudget(); consumed != budget {
				t.Fatalf("consumed replan budget = %d, want exactly the %d granted", consumed, budget)
			}
			if result.SessionState.PlanGeneration != budget+1 {
				t.Fatalf("plan generation = %d, want %d (the original plus its %d replans)", result.SessionState.PlanGeneration, budget+1, budget)
			}
		})
	}
}

// Deterministic replay of a FRESH run. The existing replay proof covers resume;
// this covers the other half, and it is the property that makes every other row
// in this file meaningful: if two identical runs could differ, a passing row
// would only describe the run that happened to be observed.
//
// What is claimed is precise, because two earlier drafts of this row claimed
// more than was true and each looked like a defect in the code:
//
//  1. Building a SECOND harness for the second run. CI rejected it and was
//     right to: the fixture commits with the wall clock, so two harnesses
//     produce repositories with different base revisions whenever the runs
//     straddle a second boundary. Locally they did not, and it passed. The
//     receipt was correctly binding the base revision; the test was calling
//     two runs against two different repositories "identical".
//
//  2. Reusing the harness but not the clock. lifecycleHarness's clock ADVANCES
//     one second per call, so the replay stamped later times and those reach
//     the receipt. Two runs of one session at two different times really are
//     two different runs — the receipt doing its job, not nondeterminism.
//
// With the clock held and everything else identical, what O7 decides is
// reproducible: the disposition, the accepted Interpretation and Plan
// identities, the sealed candidate that is the thing actually admitted and
// applied, and the whole transition chain.
//
// The run RECEIPT digest is asserted too, and getting there is what this row
// was for. It was NOT replay-stable when the matrix first ran, and the cause
// was a real divergence rather than a property of replay: synthesis.Receipt was
// the only member of the receipt chain whose identity included its completion
// timestamp, while runnercomposition and evaluatorcomposition both zeroed
// theirs — and the latter's comment asserted that synthesis.Receipt agreed.
// A terminal receipt's identity is what the session concluded, not when the
// conclusion was stamped, so O1 now follows the convention its siblings already
// documented for it.
func TestReplayingOneSessionReproducesEveryO7Decision(t *testing.T) {
	state, config := lifecycleHarness(t, 1, 1,
		string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
		synthesis.RecommendRetryGeneration,
		func(input evaluatorcomposition.EvaluationInput) bool { return input.AttemptNumber == 1 })
	frozen := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	config.Now = func() time.Time { return frozen }

	first, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	// The same starting state and the same capabilities, a second time. Run
	// takes the state by value and seals content-addressed artifacts, so a
	// replay is legitimate rather than a second, conflicting session.
	second, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatalf("replaying the same session failed: %v", err)
	}

	if first.Receipt.Disposition != DispositionCandidateReady {
		t.Fatalf("the replay fixture did not reach a candidate: %q", first.Receipt.Disposition)
	}
	if first.Receipt.Disposition != second.Receipt.Disposition {
		t.Fatalf("replay reached %q, first run reached %q", second.Receipt.Disposition, first.Receipt.Disposition)
	}

	// The sealed candidate is the load-bearing one: it is what admission
	// decides about and what application materializes. Two replays that
	// produced different candidates would make every downstream digest binding
	// meaningless.
	if first.Candidate == nil || second.Candidate == nil {
		t.Fatal("a candidate-ready run produced no candidate artifact")
	}
	if first.Candidate.CandidateArtifactDigestSHA256 != second.Candidate.CandidateArtifactDigestSHA256 {
		t.Fatalf("replay sealed a different candidate:\n1: %s\n2: %s",
			first.Candidate.CandidateArtifactDigestSHA256, second.Candidate.CandidateArtifactDigestSHA256)
	}
	if first.Interpretation == nil || second.Interpretation == nil ||
		first.Interpretation.InterpretationDigestSHA256 != second.Interpretation.InterpretationDigestSHA256 {
		t.Fatal("replay accepted a different interpretation identity")
	}
	if first.Plan == nil || second.Plan == nil ||
		first.Plan.PlanDigestSHA256 != second.Plan.PlanDigestSHA256 {
		t.Fatal("replay accepted a different plan identity")
	}

	if first.Receipt.ReceiptDigestSHA256 != second.Receipt.ReceiptDigestSHA256 {
		t.Fatalf("replay produced a different run receipt identity:\n1: %s\n2: %s",
			first.Receipt.ReceiptDigestSHA256, second.Receipt.ReceiptDigestSHA256)
	}
	if len(first.Trace.Events) != len(second.Trace.Events) {
		t.Fatalf("replay recorded %d events against %d", len(second.Trace.Events), len(first.Trace.Events))
	}
	// Events are a closed interface set, so the chain is compared by the
	// sequence of event types: that IS the transition chain.
	for i := range first.Trace.Events {
		a, b := fmt.Sprintf("%T", first.Trace.Events[i]), fmt.Sprintf("%T", second.Trace.Events[i])
		if a != b {
			t.Fatalf("transition chains diverged at %d: %s vs %s", i, a, b)
		}
	}
}

// --- the matrix itself ------------------------------------------------------
//
// Section 13 lists the rows #149 needs proven. Written as prose in a design
// document, such a list rots silently: a row can lose its proof to a rename or
// a deletion and the document goes on asserting coverage that no longer exists.
//
// So the matrix is data, and a test checks it. Each row names the test that
// proves it and the file that test lives in; the census verifies every named
// proof is actually present. A row whose proof is deleted fails here, which is
// the only way a coverage claim stays honest without someone re-reading the
// design doc.
//
// Rows proven in another package name that package's file. The census does not
// re-run them -- duplicating a proof into a second place is how two versions of
// one claim start to disagree -- it only asserts they still exist.
type matrixRow struct {
	requirement string // the section 13 row, in its own words
	provenBy    string // the test that proves it, or "" when the row is open
	file        string // repository-relative file that test lives in
	openBecause string // why the row is not proven, when provenBy is empty
}

func section13Matrix() []matrixRow {
	const (
		driver   = "golang/architecture/synthesisdriver/lifecycle_test.go"
		matrix   = "golang/architecture/synthesisdriver/lifecycle_matrix_test.go"
		resume   = "golang/architecture/synthesisdriver/resume_driver_test.go"
		refusal  = "golang/architecture/synthesisdriver/resume_refusal_matrix_test.go"
		o8       = "golang/architecture/synthesisdriver/cognitive_o8_integration_test.go"
		applyCLI = "cmd/awg/cmd_synthesis_apply_test.go"
		recordCL = "cmd/awg/cmd_synthesis_record_verification_test.go"
		record   = "golang/architecture/candidateapply/record_test.go"
		apply    = "golang/architecture/candidateapply/apply_test.go"
	)
	return []matrixRow{
		{requirement: "fresh successful run", provenBy: "TestO7CompletesWithGroundedInterpretationAndO8Planning", file: o8},
		{requirement: "valid resume from an interrupted session", provenBy: "TestResumeContinuesAnInterruptedSession", file: resume},
		{requirement: "resume drift refusal for every identity class", provenBy: "TestEveryDriftClassRefusesBeforeAnyOwnerCall", file: refusal},
		{requirement: "a refused resume calls no owner", provenBy: "TestEveryDriftClassRefusesBeforeAnyOwnerCall", file: refusal},
		{requirement: "provider stop preserves the O1 phase", provenBy: "TestRunProviderStopPreservesO1Phase", file: driver},
		{requirement: "required evaluator unavailable", provenBy: "TestRunRequiredEvaluatorUnavailableUsesO4TerminalPath", file: driver},
		{requirement: "candidate rejected before O4", provenBy: "TestRunO3NonVerifiedStopsBeforeO4", file: driver},
		{requirement: "retry then success", provenBy: "TestRunConsumesExactlyOneRetry", file: driver},
		{requirement: "replan then success", provenBy: "TestRunConsumesExactlyOneReplan", file: driver},
		{requirement: "retry exhaustion", provenBy: "TestRetryExhaustionEndsTheSessionWithoutAnotherAttempt", file: matrix},
		{requirement: "replan exhaustion", provenBy: "TestReplanExhaustionEndsTheSessionWithoutAnotherPlan", file: matrix},
		{requirement: "deterministic replay, fresh (every O7 decision)", provenBy: "TestReplayingOneSessionReproducesEveryO7Decision", file: matrix},
		{requirement: "run receipt digest is replay-stable", provenBy: "TestReplayingOneSessionReproducesEveryO7Decision", file: matrix},
		{requirement: "terminal receipt identity excludes its completion time", provenBy: "TestReceiptIdentityIsWhatConcludedNotWhenItWasStamped", file: "golang/architecture/synthesis/digest_identity_test.go"},
		{requirement: "deterministic replay, resumed", provenBy: "TestResumeIsDeterministicGivenDeterministicOwners", file: resume},
		{requirement: "budgets survive restart unchanged", provenBy: "TestResumeCannotEnlargeTheStepBudget", file: resume},
		{requirement: "candidate artifact tampering", provenBy: "TestCheckpointRefusesTamperedAcceptedArtifacts", file: "golang/architecture/synthesisdriver/checkpoint_test.go"},
		{requirement: "successful apply, receipt persisted", provenBy: "TestSynthesisApplyPersistsItsReceipt", file: applyCLI},
		{requirement: "apply refuses a dirty target", provenBy: "TestSynthesisApplyRefusesADirtyTarget", file: applyCLI},
		{requirement: "apply refuses the wrong base", provenBy: "TestSynthesisApplyRefusesATargetOnTheWrongBase", file: applyCLI},
		{requirement: "apply refuses a previously consumed candidate", provenBy: "TestSynthesisApplyRefusesAPreviouslyConsumedCandidate", file: applyCLI},
		{requirement: "apply + record failure reports applied-but-unrecorded", provenBy: "TestApplyReportsAppliedButUnrecordedRatherThanReapplying", file: recordCL},
		{requirement: "retry after record failure does not re-apply", provenBy: "TestApplyReportsAppliedButUnrecordedRatherThanReapplying", file: recordCL},
		{requirement: "application receipt immutable after recording", provenBy: "TestRecordingLeavesTheApplicationReceiptByteIdentical", file: record},
		{requirement: "verification compliant, recorded", provenBy: "TestRecordVerificationBindsToTheApplicationWithoutRewritingIt", file: recordCL},
		{requirement: "verification scope violation after real application", provenBy: "TestAScopeViolationIsRecordedAndReportedWithoutReapplying", file: recordCL},
		{requirement: "wrong-lineage verification refused", provenBy: "TestRecordRefusesBrokenLineage", file: record},
		{requirement: "historical receipt compatibility", provenBy: "TestAttachVerificationBindsDecisionAndPatch", file: apply},

		// Open rows. Named, with the reason, rather than quietly missing:
		// section 13 is explicit that a proof which cannot be run honestly is
		// left open and its blocker stated.
		{requirement: "O4 accept followed by admission refusal", openBecause: "needs a real admit-change evaluation refusing a derived request; the smoke reaches admitted only"},
		{requirement: "admitted-with-conditions with exact acknowledgement binding", openBecause: "no fixture constructs a conditionally-closed convergence session"},
		{requirement: "waiting admission", openBecause: "reachable only from an unconverged task; observed but never deliberately constructed"},
		{requirement: "oversized provider output", openBecause: "belongs to the agentcommand/commandprovider boundary, not the driver composition"},
		{requirement: "provider timeout process-group cleanup", openBecause: "requires a real subprocess; witnessed by scripts/synthesis-run-smoke.sh, not hermetically"},
		{requirement: "unsupported candidate operation refusal", openBecause: "reachable through synthesis-admit; no hermetic row constructs an add/delete candidate yet"},
		{requirement: "apply digest mismatch", openBecause: "candidateapply refuses it structurally; no row drives it through the CLI dispatcher"},
	}
}

// TestSection13MatrixNamesProofsThatExist is the census.
//
// It reads the named files and checks each named test is declared. That is a
// deliberately shallow check -- it proves the proof EXISTS, not that it proves
// what the row claims -- because the alternative, re-asserting each row's
// content here, would be the duplicate matrix this file exists to avoid.
func TestSection13MatrixNamesProofsThatExist(t *testing.T) {
	sources := map[string]string{}
	read := func(file string) string {
		if body, ok := sources[file]; ok {
			return body
		}
		// The matrix cites files across packages, so paths are resolved from
		// the repository root rather than the package directory.
		data, err := os.ReadFile(filepath.Join("..", "..", "..", file))
		if err != nil {
			t.Fatalf("matrix cites %s, which cannot be read: %v", file, err)
		}
		sources[file] = string(data)
		return sources[file]
	}

	var proven, open int
	for _, row := range section13Matrix() {
		if row.provenBy == "" {
			if strings.TrimSpace(row.openBecause) == "" {
				t.Errorf("row %q is open but states no reason; an unexplained gap reads as an oversight", row.requirement)
			}
			open++
			continue
		}
		if strings.TrimSpace(row.file) == "" {
			t.Errorf("row %q names a proof but no file", row.requirement)
			continue
		}
		if !strings.Contains(read(row.file), "func "+row.provenBy+"(") {
			t.Errorf("row %q cites %s in %s, which no longer declares it", row.requirement, row.provenBy, row.file)
		}
		proven++
	}

	// A matrix that silently became all-open would still pass every check
	// above. State the shape so a collapse is visible.
	if proven < 20 {
		t.Errorf("only %d rows are proven; the matrix has lost coverage", proven)
	}
	t.Logf("section 13: %d rows proven, %d open", proven, open)
}
