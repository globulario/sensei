// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// checkpointFixture builds a valid first checkpoint for a resumable phase. It
// reuses the same real session constructor the e2e test uses, so the embedded
// session digest is the digest O1 itself recomputes rather than a stand-in.
func checkpointFixture(t *testing.T, phase synthesis.Phase) Checkpoint {
	t.Helper()
	session := createO7Session(t, "github.com/globulario/sensei", "deadbeef", testZeroDigest, 2, 1)

	state := CheckpointSessionState{
		Session:               session,
		Phase:                 string(phase),
		RemainingRetryBudget:  session.RetryBudget,
		RemainingReplanBudget: session.ReplanBudget,
	}

	checkpoint := Checkpoint{
		SchemaVersion: CheckpointSchemaVersion,
		CheckpointID:  "o7.checkpoint.test.1",
		GeneratedBy:   CheckpointGeneratedBy,
		Sequence:      1,
		SessionState:  state,
		Trace: CheckpointTrace{
			O2ReceiptDigestsSHA256: []string{testZeroDigest},
		},
		StepsConsumed:                 1,
		MaxSteps:                      8,
		RepositoryDomain:              session.RepositoryDomain,
		BaseRevision:                  session.BaseRevision,
		WorkspaceIdentityDigestSHA256: session.WorkspaceIdentityDigestSHA256,
		GraphAuthorityDigestSHA256:    session.GraphAuthorityDigestSHA256,
		TaskID:                        "task.o7.test",
		TaskSessionDigestSHA256:       session.TaskSessionDigestSHA256,
		TaskControlStateDigestSHA256:  testZeroDigest,
		TaskControlGeneration:         3,
		ClosureReportDigestSHA256:     session.ClosureDigestSHA256,
		RunStartedAt:                  "2026-08-02T00:00:00Z",
		ObservedAt:                    "2026-08-02T00:00:05Z",
	}

	// Every phase past created must carry the interpretation O1 accepted, and
	// the closure receipt that promoted it — crossing created -> planning is
	// exactly the transition that requires a governing receipt, so a
	// checkpoint past that boundary without one is not a faithful fixture.
	if phase != synthesis.PhaseCreated {
		interpretation := interpretationFixture(t, session.SessionDigestSHA256)
		checkpoint.Interpretation = &interpretation
		checkpoint.SessionState.InterpretationDigestSHA256 = interpretation.InterpretationDigestSHA256
		checkpoint.Trace.InterpretationClosureReceiptDigestsSHA256 = []string{strings.Repeat("1", 64)}
	}
	if phase == synthesis.PhaseAttempting {
		plan := planFixture(t, checkpoint.Interpretation.InterpretationDigestSHA256)
		checkpoint.Plan = &plan
		checkpoint.SessionState.LatestPlanDigestSHA256 = plan.PlanDigestSHA256
		checkpoint.SessionState.PlanGeneration = plan.PlanGeneration
	}

	finalized, err := FinalizeCheckpoint(checkpoint)
	if err != nil {
		t.Fatalf("fixture checkpoint must finalize: %v", err)
	}
	return finalized
}

func interpretationFixture(t *testing.T, sessionDigest string) synthesis.Interpretation {
	t.Helper()
	interpretation := synthesis.Interpretation{
		SchemaVersion:            synthesis.InterpretationSchemaVersion,
		InterpretationID:         "interpretation.o7.test",
		SessionDigestSHA256:      sessionDigest,
		GeneratedBy:              synthesis.GeneratedBy,
		Objective:                "prove durable resume",
		ApplicableIntent:         []string{},
		BindingInvariants:        []string{},
		RelevantContracts:        []string{},
		AuthorityBoundaries:      []string{},
		KnownFailureModes:        []string{},
		ForbiddenFixes:           []string{},
		RequiredProofObligations: []string{},
		Assumptions:              []string{},
		UnresolvedQuestions:      []string{},
		SourceReferences:         []synthesis.SourceReference{},
		Limitations:              []synthesis.Limitation{},
	}
	digest, err := synthesis.InterpretationDigest(interpretation)
	if err != nil {
		t.Fatal(err)
	}
	interpretation.InterpretationDigestSHA256 = digest
	return interpretation
}

func planFixture(t *testing.T, interpretationDigest string) synthesis.Plan {
	t.Helper()
	plan := synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan.o7.test",
		InterpretationDigestSHA256: interpretationDigest,
		PlanGeneration:             1,
		Steps:                      []synthesis.PlanStep{},
		Assumptions:                []string{},
		Risks:                      []string{},
		StopConditions:             []string{},
	}
	digest, err := synthesis.PlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanDigestSHA256 = digest
	return plan
}

// The checkpoint's identity must not move when only observation timestamps
// change, in the same spirit as RunReceiptDigest. Otherwise two checkpoints
// recording the same governed facts would be different documents.
func TestCheckpointDigestExcludesObservationTimestamps(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhaseCreated)

	moved := checkpoint
	moved.RunStartedAt = "2027-01-01T00:00:00Z"
	moved.ObservedAt = "2027-01-01T00:00:09Z"

	before, err := CheckpointDigest(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	after, err := CheckpointDigest(moved)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("observation timestamps changed checkpoint identity: %q -> %q", before, after)
	}
}

// Every field below is authority or evidence. Changing any one of them must
// invalidate the checkpoint, because a resumed driver reads all of them as
// already-accepted fact.
func TestCheckpointDigestBindsEveryAuthorityAndEvidenceField(t *testing.T) {
	base := checkpointFixture(t, synthesis.PhaseAttempting)
	other := strings.Repeat("c", 64)

	mutations := map[string]func(*Checkpoint){
		"o1 phase":                 func(c *Checkpoint) { c.SessionState.Phase = string(synthesis.PhasePlanned) },
		"o1 retry budget":          func(c *Checkpoint) { c.SessionState.RemainingRetryBudget = 0 },
		"o1 replan budget":         func(c *Checkpoint) { c.SessionState.RemainingReplanBudget = 0 },
		"o1 attempt number":        func(c *Checkpoint) { c.SessionState.AttemptNumber = 7 },
		"accepted interpretation":  func(c *Checkpoint) { c.Interpretation = nil },
		"accepted plan":            func(c *Checkpoint) { c.Plan = nil },
		"candidate digest":         func(c *Checkpoint) { c.CandidateArtifactDigestSHA256 = &other },
		"trace evidence":           func(c *Checkpoint) { c.Trace.RunnerReceiptDigestsSHA256 = []string{other} },
		"steps consumed":           func(c *Checkpoint) { c.StepsConsumed = 4 },
		"max steps":                func(c *Checkpoint) { c.MaxSteps = 99 },
		"previous checkpoint link": func(c *Checkpoint) { c.PreviousCheckpointDigestSHA256 = &other },
		"sequence":                 func(c *Checkpoint) { c.Sequence = 2 },
		"workspace identity":       func(c *Checkpoint) { c.WorkspaceIdentityDigestSHA256 = other },
		"graph authority":          func(c *Checkpoint) { c.GraphAuthorityDigestSHA256 = other },
		"task control state":       func(c *Checkpoint) { c.TaskControlStateDigestSHA256 = other },
		"task control generation":  func(c *Checkpoint) { c.TaskControlGeneration = 99 },
		"closure report":           func(c *Checkpoint) { c.ClosureReportDigestSHA256 = other },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			mutated := base
			mutate(&mutated)

			digest, err := CheckpointDigest(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if digest == base.CheckpointDigestSHA256 {
				t.Fatalf("changing %s left the checkpoint digest unchanged", name)
			}
			// The mutated document still declares the ORIGINAL digest, which
			// is exactly the tampering case validation has to refuse.
			if err := ValidateCheckpoint(mutated); err == nil {
				t.Fatalf("changing %s produced a checkpoint that still validates", name)
			}
		})
	}
}

// A checkpoint that arrives with a field this closed schema does not define
// must be refused while it is still bytes: decoding first would silently drop
// the unknown field and validate whatever remained.
func TestCheckpointDocumentRejectsUnknownFields(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
	data, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["resume_without_checking"] = true
	tampered, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCheckpointDocument(tampered); err == nil {
		t.Fatal("an unknown checkpoint field was accepted")
	}
	if err := ValidateCheckpointDocument(data); err != nil {
		t.Fatalf("the untampered document must validate: %v", err)
	}
}

func TestCheckpointRejectsWrongSchemaVersion(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
	checkpoint.SchemaVersion = "sensei.synthesisdriver.checkpoint.v2"
	if err := ValidateCheckpoint(checkpoint); err == nil {
		t.Fatal("a foreign schema_version was accepted")
	}
}

// Evaluating is the one nonterminal phase that must never be serialized as a
// durable boundary: O4 resolves it inside a single owner call, so a
// checkpoint claiming it would be a half-consumed process-local handoff.
// Terminal phases are reloadable but start no new synthesis work, so they are
// not continuation checkpoints either.
func TestCheckpointRefusesNonResumablePhases(t *testing.T) {
	for _, phase := range []synthesis.Phase{
		synthesis.PhaseEvaluating,
		synthesis.PhaseSucceeded,
		synthesis.PhaseFailed,
	} {
		t.Run(string(phase), func(t *testing.T) {
			if ResumablePhase(phase) {
				t.Fatalf("phase %q must not be a durable boundary", phase)
			}
			checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
			checkpoint.SessionState.Phase = string(phase)
			if _, err := FinalizeCheckpoint(checkpoint); err == nil {
				t.Fatalf("a %q checkpoint was accepted as a durable boundary", phase)
			}
		})
	}
}

func TestResumablePhasesCoverEveryDurableBoundary(t *testing.T) {
	for _, phase := range ResumablePhases() {
		if !ResumablePhase(phase) {
			t.Fatalf("phase %q is listed as durable but rejected by ResumablePhase", phase)
		}
		checkpoint := checkpointFixture(t, phase)
		if err := ValidateCheckpoint(checkpoint); err != nil {
			t.Fatalf("phase %q must produce a valid checkpoint: %v", phase, err)
		}
	}
}

// The projection is only safe if it is lossless. This proves every
// SessionState field survives the round trip, and fails when a new field is
// added to O1 without being carried.
func TestCheckpointSessionStateProjectionIsLossless(t *testing.T) {
	session := createO7Session(t, "github.com/globulario/sensei", "deadbeef", testZeroDigest, 3, 2)
	state := synthesis.SessionState{
		Session:                      session,
		Phase:                        synthesis.PhaseRetry,
		InterpretationDigestSHA256:   strings.Repeat("a", 64),
		PlanGeneration:               2,
		AttemptNumber:                3,
		ExpectedPlanGeneration:       3,
		ExpectedAttemptNumber:        4,
		LatestPlanDigestSHA256:       strings.Repeat("b", 64),
		LatestAttemptDigestSHA256:    strings.Repeat("c", 64),
		LatestEvaluationDigestSHA256: strings.Repeat("d", 64),
		RemainingRetryBudget:         1,
		RemainingReplanBudget:        0,
	}

	restored := FromSessionState(state).ToSessionState()
	if !reflect.DeepEqual(state, restored) {
		t.Fatalf("projection lost O1 state:\n want %+v\n got  %+v", state, restored)
	}

	// Guard against a field being added to O1 and silently not carried: the
	// projection must expose exactly as many fields as SessionState has.
	stateFields := reflect.TypeOf(synthesis.SessionState{}).NumField()
	projectionFields := reflect.TypeOf(CheckpointSessionState{}).NumField()
	if stateFields != projectionFields {
		t.Fatalf("synthesis.SessionState has %d fields but the checkpoint projection carries %d; carry the new field or state why it is excluded", stateFields, projectionFields)
	}
}

// Restart is not a budget refill. A checkpoint may never claim more remaining
// budget than the session was granted, or more consumed steps than its own
// immutable max.
func TestCheckpointRefusesImpossibleBudgets(t *testing.T) {
	cases := map[string]func(*Checkpoint){
		"retry budget above the session grant":  func(c *Checkpoint) { c.SessionState.RemainingRetryBudget = c.SessionState.Session.RetryBudget + 1 },
		"replan budget above the session grant": func(c *Checkpoint) { c.SessionState.RemainingReplanBudget = c.SessionState.Session.ReplanBudget + 1 },
		"negative remaining retry budget":       func(c *Checkpoint) { c.SessionState.RemainingRetryBudget = -1 },
		"steps consumed above max steps":        func(c *Checkpoint) { c.StepsConsumed = c.MaxSteps + 1 },
		"non-positive max steps":                func(c *Checkpoint) { c.MaxSteps = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
			mutate(&checkpoint)
			if _, err := FinalizeCheckpoint(checkpoint); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// The boundary identities are the facts resume compares current observations
// against. If they were allowed to differ from the session O1 already bound,
// resume would be checking continuity against something the session never had.
func TestCheckpointBoundaryMustMatchTheSessionBinding(t *testing.T) {
	other := strings.Repeat("e", 64)
	cases := map[string]func(*Checkpoint){
		"repository domain":  func(c *Checkpoint) { c.RepositoryDomain = "github.com/other/repo" },
		"base revision":      func(c *Checkpoint) { c.BaseRevision = "cafebabe" },
		"workspace identity": func(c *Checkpoint) { c.WorkspaceIdentityDigestSHA256 = other },
		"graph authority":    func(c *Checkpoint) { c.GraphAuthorityDigestSHA256 = other },
		"task session":       func(c *Checkpoint) { c.TaskSessionDigestSHA256 = other },
		"closure report":     func(c *Checkpoint) { c.ClosureReportDigestSHA256 = other },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
			mutate(&checkpoint)
			if _, err := FinalizeCheckpoint(checkpoint); err == nil {
				t.Fatalf("checkpoint %s was allowed to diverge from the session binding", name)
			}
		})
	}
}

// A tampered interpretation or plan must not re-enter a resumed session under
// an identity O1 accepted for different content.
func TestCheckpointRefusesTamperedAcceptedArtifacts(t *testing.T) {
	t.Run("interpretation content", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
		checkpoint.Interpretation.Objective = "something the accepted digest never covered"
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("a tampered interpretation was accepted")
		}
	})

	t.Run("interpretation not the one o1 accepted", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
		checkpoint.SessionState.InterpretationDigestSHA256 = strings.Repeat("f", 64)
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("an interpretation unrelated to the accepted digest was accepted")
		}
	})

	t.Run("plan content", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhaseAttempting)
		checkpoint.Plan.PlanID = "plan.substituted"
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("a tampered plan was accepted")
		}
	})

	t.Run("embedded session digest", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		checkpoint.SessionState.Session.Objective = "a different objective"
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("a tampered session was accepted")
		}
	})
}

// A checkpoint past created cannot continue without the premise O1 accepted:
// the next owner call would otherwise have to invent it.
func TestCheckpointRequiresAcceptedArtifactsForItsPhase(t *testing.T) {
	t.Run("planning without interpretation", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhasePlanning)
		checkpoint.Interpretation = nil
		checkpoint.SessionState.InterpretationDigestSHA256 = ""
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("a planning checkpoint with no accepted interpretation was accepted")
		}
	})

	t.Run("attempting without plan", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhaseAttempting)
		checkpoint.Plan = nil
		checkpoint.SessionState.LatestPlanDigestSHA256 = ""
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("an attempting checkpoint with no accepted plan was accepted")
		}
	})
}

// The chain has to be structural: a first checkpoint has no predecessor, and
// every later one names exactly which checkpoint it continues.
func TestCheckpointChainPositionIsStructural(t *testing.T) {
	previous := strings.Repeat("a", 64)

	t.Run("first checkpoint cannot claim a predecessor", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		checkpoint.Sequence = 1
		checkpoint.PreviousCheckpointDigestSHA256 = &previous
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("a first checkpoint with a predecessor was accepted")
		}
	})

	t.Run("later checkpoint must name its predecessor", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		checkpoint.Sequence = 2
		checkpoint.PreviousCheckpointDigestSHA256 = nil
		if _, err := FinalizeCheckpoint(checkpoint); err == nil {
			t.Fatal("a later checkpoint with no predecessor was accepted")
		}
	})

	t.Run("linked checkpoint validates", func(t *testing.T) {
		checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
		checkpoint.Sequence = 2
		checkpoint.PreviousCheckpointDigestSHA256 = &previous
		if _, err := FinalizeCheckpoint(checkpoint); err != nil {
			t.Fatalf("a correctly linked checkpoint was refused: %v", err)
		}
	})
}

// A nonterminal checkpoint carrying an O1 terminal receipt is a lineage
// contradiction: the receipt says the session ended, the phase says it can
// continue.
func TestCheckpointRefusesTerminalReceiptAtNonterminalPhase(t *testing.T) {
	checkpoint := checkpointFixture(t, synthesis.PhaseCreated)
	checkpoint.SessionState.Receipt = &synthesis.Receipt{
		SchemaVersion:       synthesis.ReceiptSchemaVersion,
		ReceiptID:           "receipt.o7.test",
		SessionDigestSHA256: checkpoint.SessionState.Session.SessionDigestSHA256,
	}
	if _, err := FinalizeCheckpoint(checkpoint); err == nil {
		t.Fatal("a nonterminal checkpoint carrying a terminal receipt was accepted")
	}
}
