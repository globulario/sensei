// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// countingProvider records how many times an owner was actually called, which
// is how the tests prove a resumed run does not replay work that is already
// completed history.
type countingProvider struct {
	inner providerport.Provider
	calls int
}

func (p *countingProvider) Describe(ctx context.Context) (providerport.Capabilities, error) {
	return p.inner.Describe(ctx)
}

func (p *countingProvider) Execute(ctx context.Context, request providerport.Request, observer providerport.Observer) (providerport.Result, error) {
	p.calls++
	return p.inner.Execute(ctx, request, observer)
}

// errProcessDied is the sentinel a crashing store raises. It is deliberately
// NOT a governed outcome: the point is to model the machine disappearing, not
// a provider declining.
var errProcessDied = errors.New("test: the process died at this boundary")

// crashingStore persists boundaries normally and then kills the process at a
// chosen one.
//
// This is the faithful simulation of section 4: the checkpoint precedes the
// owner call, so a process that dies here leaves a durable boundary and an
// owner call that never became completed history. Cancelling the context
// inside a provider instead would NOT model a crash — providerport.Run
// converts that into a governed provider-stopped result and the run ends
// normally with a receipt.
type crashingStore struct {
	inner          CheckpointStore
	crashAfterSave int // 0 = never crash
	saves          int
}

func (s *crashingStore) Save(ctx context.Context, checkpoint Checkpoint) error {
	if err := s.inner.Save(ctx, checkpoint); err != nil {
		return err
	}
	s.saves++
	if s.crashAfterSave > 0 && s.saves >= s.crashAfterSave {
		return errProcessDied
	}
	return nil
}

func (s *crashingStore) Load(ctx context.Context, digest string) (Checkpoint, error) {
	return s.inner.Load(ctx, digest)
}

func (s *crashingStore) Latest(ctx context.Context) (Checkpoint, error) {
	return s.inner.Latest(ctx)
}

// checkpointHarness is the lifecycle harness with durable checkpointing wired
// in, plus the store so a test can inspect the boundaries that were written.
func checkpointHarness(t *testing.T, retryBudget, replanBudget int) (synthesis.SessionState, Config, *MemoryCheckpointStore) {
	t.Helper()
	state, config := lifecycleHarness(
		t,
		retryBudget,
		replanBudget,
		string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
		synthesis.RecommendRetryGeneration,
		func(input evaluatorcomposition.EvaluationInput) bool { return false },
	)
	store := NewMemoryCheckpointStore()
	config.CheckpointStore = store
	config.CheckpointBinding = CheckpointBinding{
		TaskID:                       "task.o7.durable",
		TaskControlStateDigestSHA256: strings.Repeat("4", 64),
		TaskControlGeneration:        2,
	}
	return state, config, store
}

// bindingFor composes the current observation a CLI would build. Here it
// matches the checkpoint, which is the no-drift case.
func bindingFor(checkpoint Checkpoint) ResumeBinding {
	return ResumeBinding{Current: checkpointIdentitySet(checkpoint)}
}

// A run with a store injected records a boundary before each owner call, and
// those boundaries form one verifiable chain.
func TestRunPersistsDurableBoundaries(t *testing.T) {
	state, config, store := checkpointHarness(t, 0, 0)

	result, err := Run(context.Background(), state, config)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.Disposition != DispositionCandidateReady {
		t.Fatalf("harness run did not reach candidate-ready: %q", result.Receipt.Disposition)
	}

	latest, err := store.Latest(context.Background())
	if err != nil {
		t.Fatalf("a run with a store must leave at least one boundary: %v", err)
	}
	if latest.Sequence < 2 {
		t.Fatalf("expected several boundaries, got sequence %d", latest.Sequence)
	}

	// Walk the chain back to its origin: every link must resolve, and step
	// accounting must be monotone.
	seen := 0
	current := latest
	for {
		seen++
		if current.MaxSteps != config.MaxSteps {
			t.Fatalf("boundary %d recorded max_steps=%d, want %d", current.Sequence, current.MaxSteps, config.MaxSteps)
		}
		if current.PreviousCheckpointDigestSHA256 == nil {
			if current.Sequence != 1 {
				t.Fatalf("the chain origin has sequence %d", current.Sequence)
			}
			break
		}
		previous, err := store.Load(context.Background(), *current.PreviousCheckpointDigestSHA256)
		if err != nil {
			t.Fatalf("chain link from sequence %d does not resolve: %v", current.Sequence, err)
		}
		if previous.Sequence != current.Sequence-1 {
			t.Fatalf("sequence %d links to %d", current.Sequence, previous.Sequence)
		}
		if previous.StepsConsumed > current.StepsConsumed {
			t.Fatalf("steps consumed went backwards: %d then %d", previous.StepsConsumed, current.StepsConsumed)
		}
		current = previous
	}
	if seen != latest.Sequence {
		t.Fatalf("walked %d boundaries for a chain of length %d", seen, latest.Sequence)
	}
}

// The core capability: a session interrupted mid-flight continues in a later
// process from its last durable boundary and still reaches its governed
// outcome, without replaying the owners that already completed.
func TestResumeContinuesAnInterruptedSession(t *testing.T) {
	// Crashing at successive boundaries resumes from successive O1 phases:
	// the first boundary is the fresh session, later ones are reached only
	// after real owner calls completed.
	for name, crashAfter := range map[string]int{
		"at the first boundary":  1,
		"at the second boundary": 2,
		"at the third boundary":  3,
	} {
		t.Run(name, func(t *testing.T) {
			state, config, store := checkpointHarness(t, 0, 0)
			config.CheckpointStore = &crashingStore{inner: store, crashAfterSave: crashAfter}

			if _, err := Run(context.Background(), state, config); !errors.Is(err, errProcessDied) {
				t.Fatalf("the crashing run should surface the crash, got %v", err)
			}

			checkpoint, err := store.Latest(context.Background())
			if err != nil {
				t.Fatalf("an interrupted run must leave a resumable boundary: %v", err)
			}
			if !ResumablePhase(synthesis.Phase(checkpoint.SessionState.Phase)) {
				t.Fatalf("boundary phase %q is not resumable", checkpoint.SessionState.Phase)
			}

			// A second process: fresh capabilities, same immutable budget, the
			// checkpoint as the only carrier of what already happened.
			_, resumedConfig, _ := checkpointHarness(t, 0, 0)
			resumedConfig.CheckpointStore = store
			resumedConfig.CheckpointBinding = config.CheckpointBinding
			resumedConfig.WorkspaceIdentity = config.WorkspaceIdentity
			resumedConfig.RepositoryRoot = config.RepositoryRoot
			resumedConfig.CandidateStore = config.CandidateStore
			resumedConfig.EvaluationEngine = config.EvaluationEngine

			interpretation := &countingProvider{inner: resumedConfig.InterpretationProvider}
			resumedConfig.InterpretationProvider = interpretation

			result, assessment, err := Resume(context.Background(), checkpoint, bindingFor(checkpoint), resumedConfig)
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if !assessment.Allowed() {
				t.Fatalf("resume refused an unchanged boundary: %v", assessment.Detail)
			}
			if result.Receipt.Disposition != DispositionCandidateReady {
				t.Fatalf("resumed run ended %q: %s", result.Receipt.Disposition, result.Receipt.Detail)
			}

			// Completed history is never reinterpreted. Past the created
			// phase O1 has already accepted an interpretation, so the resumed
			// process must take it from the checkpoint rather than asking a
			// provider for a new one.
			if checkpoint.SessionState.Phase != string(synthesis.PhaseCreated) {
				if interpretation.calls != 0 {
					t.Fatalf("resume re-ran the interpretation provider %d times from phase %q",
						interpretation.calls, checkpoint.SessionState.Phase)
				}
				if result.SessionState.InterpretationDigestSHA256 != checkpoint.SessionState.InterpretationDigestSHA256 {
					t.Fatal("resume accepted a different interpretation than the one O1 had recorded")
				}
			}

			// The receipt describes the whole session, not just this process:
			// evidence recorded before the restart is still bound.
			if len(result.Receipt.O2ReceiptDigestsSHA256) < len(checkpoint.Trace.O2ReceiptDigestsSHA256) {
				t.Fatal("resumed receipt dropped evidence recorded before the restart")
			}
			if len(result.Receipt.InterpretationClosureReceiptDigestsSHA256) != 1 {
				t.Fatalf("resumed receipt carries %d closure receipts, want exactly the one promotion this session had",
					len(result.Receipt.InterpretationClosureReceiptDigestsSHA256))
			}
		})
	}
}

// A refused resume performs no owner call at all. This is the property that
// makes drift refusal meaningful: a refusal that had already called a provider
// would have changed the world it refused to enter.
func TestResumeRefusalMakesNoOwnerCall(t *testing.T) {
	state, config, store := checkpointHarness(t, 0, 0)

	config.CheckpointStore = &crashingStore{inner: store, crashAfterSave: 2}
	if _, err := Run(context.Background(), state, config); !errors.Is(err, errProcessDied) {
		t.Fatalf("expected the crash, got %v", err)
	}
	checkpoint, err := store.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, resumedConfig, _ := checkpointHarness(t, 0, 0)
	resumedConfig.CheckpointStore = store
	resumedConfig.CheckpointBinding = config.CheckpointBinding
	resumedConfig.WorkspaceIdentity = config.WorkspaceIdentity
	resumedConfig.RepositoryRoot = config.RepositoryRoot

	counted := map[string]*countingProvider{
		"interpretation": {inner: resumedConfig.InterpretationProvider},
		"planning":       {inner: resumedConfig.PlanningProvider},
	}
	resumedConfig.InterpretationProvider = counted["interpretation"]
	resumedConfig.PlanningProvider = counted["planning"]

	drifted := bindingFor(checkpoint)
	drifted.Current.GraphAuthorityDigestSHA256 = strings.Repeat("8", 64)

	result, assessment, err := Resume(context.Background(), checkpoint, drifted, resumedConfig)
	if err != nil {
		t.Fatalf("a refusal is not an error: %v", err)
	}
	if assessment.Allowed() {
		t.Fatal("drifted graph authority was allowed to resume")
	}
	if *assessment.RefusalReason != RefusalGraphAuthorityDrift {
		t.Fatalf("refused with %q", *assessment.RefusalReason)
	}
	if result.Receipt.ReceiptDigestSHA256 != "" {
		t.Fatal("a refused resume produced a run receipt")
	}
	for name, provider := range counted {
		if provider.calls != 0 {
			t.Fatalf("a refused resume called the %s provider %d times", name, provider.calls)
		}
	}
}

// Restart is not a budget refill, and the step budget is not negotiable
// configuration: a resumed process that supplies a larger max_steps is
// refused rather than quietly granted more room.
func TestResumeCannotEnlargeTheStepBudget(t *testing.T) {
	state, config, store := checkpointHarness(t, 0, 0)

	config.CheckpointStore = &crashingStore{inner: store, crashAfterSave: 2}
	if _, err := Run(context.Background(), state, config); !errors.Is(err, errProcessDied) {
		t.Fatalf("expected the crash, got %v", err)
	}
	checkpoint, err := store.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	_, resumedConfig, _ := checkpointHarness(t, 0, 0)
	resumedConfig.CheckpointStore = store
	resumedConfig.CheckpointBinding = config.CheckpointBinding
	resumedConfig.WorkspaceIdentity = config.WorkspaceIdentity
	resumedConfig.RepositoryRoot = config.RepositoryRoot
	resumedConfig.MaxSteps = checkpoint.MaxSteps + 10

	_, assessment, err := Resume(context.Background(), checkpoint, bindingFor(checkpoint), resumedConfig)
	if err == nil {
		t.Fatal("a resumed run was allowed to enlarge max_steps")
	}
	if !strings.Contains(err.Error(), "immutable across restart") {
		t.Fatalf("unexpected error: %v", err)
	}
	// The assessment still stands as evidence that the boundary itself was
	// resumable; it was the caller's configuration that was refused.
	if !assessment.Allowed() {
		t.Fatalf("the boundary should have assessed as resumable: %v", assessment.Detail)
	}
}

// The same checkpoint resumed twice into isolated stores must produce the same
// typed decisions. Provider nondeterminism is not eliminated by this; what is
// proven is that O7's own orchestration and identity decisions are
// deterministic given deterministic owners.
func TestResumeIsDeterministicGivenDeterministicOwners(t *testing.T) {
	state, config, store := checkpointHarness(t, 0, 0)

	config.CheckpointStore = &crashingStore{inner: store, crashAfterSave: 2}
	if _, err := Run(context.Background(), state, config); !errors.Is(err, errProcessDied) {
		t.Fatalf("expected the crash, got %v", err)
	}
	checkpoint, err := store.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	replay := func(t *testing.T) ResumeAssessment {
		t.Helper()
		_, resumedConfig, isolated := checkpointHarness(t, 0, 0)
		resumedConfig.CheckpointStore = isolated
		resumedConfig.CheckpointBinding = config.CheckpointBinding
		resumedConfig.WorkspaceIdentity = config.WorkspaceIdentity
		resumedConfig.RepositoryRoot = config.RepositoryRoot

		_, assessment, err := Resume(context.Background(), checkpoint, bindingFor(checkpoint), resumedConfig)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		return assessment
	}

	first := replay(t)
	second := replay(t)
	if first.AssessmentDigestSHA256 != second.AssessmentDigestSHA256 {
		t.Fatal("the same checkpoint assessed differently on replay")
	}
	if !first.Allowed() || !second.Allowed() {
		t.Fatal("replay refused a boundary that should resume")
	}
}

// A store cannot be injected without the task identity a boundary must carry:
// otherwise the run would happily write checkpoints that cannot be resumed,
// and nobody would find out until the restart that needed them.
func TestCheckpointStoreRequiresItsTaskBinding(t *testing.T) {
	state, config, _ := checkpointHarness(t, 0, 0)
	config.CheckpointBinding = CheckpointBinding{}

	if _, err := Run(context.Background(), state, config); err == nil {
		t.Fatal("a checkpoint store with no task binding was accepted")
	}
}

// retryHarness drives the evaluator to fail the first attempt, so the run
// reaches the retry and replan boundaries a plain success never visits.
func retryHarness(t *testing.T, recommendation synthesis.Recommendation, retryBudget, replanBudget int) (synthesis.SessionState, Config, *MemoryCheckpointStore) {
	t.Helper()
	state, config := lifecycleHarness(
		t,
		retryBudget,
		replanBudget,
		string(evaluatorcomposition.FailureClassMechanicalCheckFailure),
		recommendation,
		func(input evaluatorcomposition.EvaluationInput) bool { return input.AttemptNumber == 1 },
	)
	store := NewMemoryCheckpointStore()
	config.CheckpointStore = store
	config.CheckpointBinding = CheckpointBinding{
		TaskID:                       "task.o7.durable",
		TaskControlStateDigestSHA256: strings.Repeat("4", 64),
		TaskControlGeneration:        2,
	}
	return state, config, store
}

// The remaining durable phases — attempting, retry, and replan — are only
// reached once O3 and O4 have really run, so they are proven on a session that
// actually consumes a retry or a replan.
//
// The budget assertion is the point: a resumed session continues with the
// counters the checkpoint recorded. If restart refilled them, the run would
// consume a second retry here and still succeed, which is exactly the silent
// failure this proves cannot happen.
func TestResumeFromAttemptRetryAndReplanBoundaries(t *testing.T) {
	cases := map[string]struct {
		recommendation synthesis.Recommendation
		retryBudget    int
		replanBudget   int
		crashAfterSave int
		wantPhase      synthesis.Phase
	}{
		"attempting": {synthesis.RecommendRetryGeneration, 1, 0, 4, synthesis.PhaseAttempting},
		"retry":      {synthesis.RecommendRetryGeneration, 1, 0, 5, synthesis.PhaseRetry},
		"replan":     {synthesis.RecommendReplan, 0, 1, 5, synthesis.PhaseReplan},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			state, config, store := retryHarness(t, testCase.recommendation, testCase.retryBudget, testCase.replanBudget)
			config.CheckpointStore = &crashingStore{inner: store, crashAfterSave: testCase.crashAfterSave}

			if _, err := Run(context.Background(), state, config); !errors.Is(err, errProcessDied) {
				t.Fatalf("expected the crash, got %v", err)
			}
			checkpoint, err := store.Latest(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := synthesis.Phase(checkpoint.SessionState.Phase); got != testCase.wantPhase {
				t.Fatalf("crashed at phase %q, wanted a %q boundary", got, testCase.wantPhase)
			}

			_, resumedConfig, _ := retryHarness(t, testCase.recommendation, testCase.retryBudget, testCase.replanBudget)
			resumedConfig.CheckpointStore = store
			resumedConfig.CheckpointBinding = config.CheckpointBinding
			resumedConfig.WorkspaceIdentity = config.WorkspaceIdentity
			resumedConfig.RepositoryRoot = config.RepositoryRoot
			resumedConfig.CandidateStore = config.CandidateStore
			resumedConfig.EvaluationEngine = config.EvaluationEngine

			result, assessment, err := Resume(context.Background(), checkpoint, bindingFor(checkpoint), resumedConfig)
			if err != nil {
				t.Fatalf("resume from %q: %v", testCase.wantPhase, err)
			}
			if !assessment.Allowed() {
				t.Fatalf("resume from %q refused: %v", testCase.wantPhase, assessment.Detail)
			}
			if result.Receipt.Disposition != DispositionCandidateReady {
				t.Fatalf("resumed run ended %q: %s", result.Receipt.Disposition, result.Receipt.Detail)
			}

			// Budgets survive the restart unchanged: the session still shows
			// exactly the one retry or replan it consumed before the crash.
			if result.SessionState.ConsumedRetryBudget() != testCase.retryBudget {
				t.Fatalf("consumed retry budget is %d, want %d — restart changed the budget",
					result.SessionState.ConsumedRetryBudget(), testCase.retryBudget)
			}
			if result.SessionState.ConsumedReplanBudget() != testCase.replanBudget {
				t.Fatalf("consumed replan budget is %d, want %d — restart changed the budget",
					result.SessionState.ConsumedReplanBudget(), testCase.replanBudget)
			}
			if result.SessionState.RemainingRetryBudget != 0 || result.SessionState.RemainingReplanBudget != 0 {
				t.Fatalf("resume left retry=%d replan=%d remaining; a restart refilled a budget",
					result.SessionState.RemainingRetryBudget, result.SessionState.RemainingReplanBudget)
			}
		})
	}
}
