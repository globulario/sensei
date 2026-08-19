// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/evaluatorcomposition"
	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// Section 11's drift-refusal law, proved where it actually has to hold.
//
// AssessResume already proves each drift class by name, but that is the
// ASSESSMENT layer: it decides, and deciding correctly is not the same
// property as the decision being obeyed. The stronger law is:
//
//	a refused resume must stop before any owner or provider call occurs.
//
// That boundary lives underneath the CLI, in the dispatcher. Proving it at the
// CLI instead would test presentation while leaving a path where a refused
// checkpoint still reaches execution -- and a refusal that is announced after
// a provider has already been handed the repository is not a refusal.

// countingOwners instruments EVERY owner the driver can reach, so "no owner
// call" is a claim about the whole capability set rather than about the two
// providers a test happened to wrap.
type countingOwners struct {
	interpretation *countingProvider
	planning       *countingProvider
	generation     *countingFactory
	evaluation     *countingEngine
	candidates     *countingCandidateStore
	checkpoints    *countingCheckpointStore
}

// total is every call that would constitute execution. Reported as one number
// because the assertion is "nothing ran", and a per-owner breakdown only
// matters once that fails.
func (o countingOwners) total() int {
	return o.interpretation.calls + o.planning.calls + o.generation.calls +
		o.evaluation.calls + o.candidates.puts
}

func (o countingOwners) breakdown() map[string]int {
	return map[string]int{
		"interpretation": o.interpretation.calls,
		"planning":       o.planning.calls,
		"generation":     o.generation.calls,
		"evaluation":     o.evaluation.calls,
		"candidate-seal": o.candidates.puts,
	}
}

type countingFactory struct {
	inner runnercomposition.GenerationProviderFactory
	calls int
}

func (f *countingFactory) NewProvider(workspace runnercomposition.CandidateWorkspace) (providerport.Provider, error) {
	f.calls++
	return f.inner.NewProvider(workspace)
}

type countingEngine struct {
	inner EvaluationEngine
	calls int
}

func (e *countingEngine) Evaluate(ctx context.Context, state synthesis.SessionState, handoff runnercomposition.VerifiedGenerationHandoff) (evaluatorcomposition.Result, error) {
	e.calls++
	return e.inner.Evaluate(ctx, state, handoff)
}

type countingCandidateStore struct {
	inner runnercomposition.CandidateArtifactStore
	puts  int
}

func (s *countingCandidateStore) Put(ctx context.Context, artifact runnercomposition.CandidateArtifact) error {
	s.puts++
	return s.inner.Put(ctx, artifact)
}

func (s *countingCandidateStore) Get(ctx context.Context, digestSHA256 string) (runnercomposition.CandidateArtifact, error) {
	return s.inner.Get(ctx, digestSHA256)
}

func (s *countingCandidateStore) PutAuxiliaryFile(ctx context.Context, name string, data []byte) error {
	return s.inner.PutAuxiliaryFile(ctx, name, data)
}

func (s *countingCandidateStore) VerifyRootIdentity(path string) error {
	return s.inner.VerifyRootIdentity(path)
}

// countingCheckpointStore proves the OTHER half of the law: a refused resume
// must not advance the durable history either. A refusal that appended a
// boundary would leave a session that had visibly moved without anything
// having happened.
type countingCheckpointStore struct {
	inner CheckpointStore
	saves int
}

func (s *countingCheckpointStore) Save(ctx context.Context, checkpoint Checkpoint) error {
	s.saves++
	return s.inner.Save(ctx, checkpoint)
}

func (s *countingCheckpointStore) Load(ctx context.Context, digest string) (Checkpoint, error) {
	return s.inner.Load(ctx, digest)
}

func (s *countingCheckpointStore) Latest(ctx context.Context) (Checkpoint, error) {
	return s.inner.Latest(ctx)
}

// interruptedSession produces a real checkpoint the way a crash would: the
// boundary is written, the owner call that followed it never became completed
// history. Every case below resumes from the same durable boundary so that the
// only variable is the drift.
func interruptedSession(t *testing.T) (Checkpoint, Config, *MemoryCheckpointStore) {
	t.Helper()
	state, config, store := checkpointHarness(t, 0, 0)
	config.CheckpointStore = &crashingStore{inner: store, crashAfterSave: 2}
	if _, err := Run(context.Background(), state, config); !errors.Is(err, errProcessDied) {
		t.Fatalf("expected the modelled crash, got %v", err)
	}
	checkpoint, err := store.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return checkpoint, config, store
}

// resumeConfig rebuilds the capability set a restarted process would compose,
// with every owner counted.
func resumeConfig(t *testing.T, original Config, store CheckpointStore) (Config, countingOwners) {
	t.Helper()
	_, config, _ := checkpointHarness(t, 0, 0)
	config.CheckpointBinding = original.CheckpointBinding
	config.WorkspaceIdentity = original.WorkspaceIdentity
	config.RepositoryRoot = original.RepositoryRoot

	owners := countingOwners{
		interpretation: &countingProvider{inner: config.InterpretationProvider},
		planning:       &countingProvider{inner: config.PlanningProvider},
		generation:     &countingFactory{inner: config.GenerationFactory},
		evaluation:     &countingEngine{inner: config.EvaluationEngine},
		candidates:     &countingCandidateStore{inner: config.CandidateStore},
		checkpoints:    &countingCheckpointStore{inner: store},
	}
	config.InterpretationProvider = owners.interpretation
	config.PlanningProvider = owners.planning
	config.GenerationFactory = owners.generation
	config.EvaluationEngine = owners.evaluation
	config.CandidateStore = owners.candidates
	config.CheckpointStore = owners.checkpoints
	return config, owners
}

// TestEveryDriftClassRefusesBeforeAnyOwnerCall is exhaustive BY CONSTRUCTION
// over ResumeIdentitySet: every field of the observed boundary gets its own
// case, mapped to the refusal reason it must produce. A field added to that
// struct without a drift check therefore fails here rather than silently
// becoming a boundary nobody compares -- which is the failure this whole
// section exists to prevent, since an unchecked field is indistinguishable
// from a checked one right up to the moment it matters.
func TestEveryDriftClassRefusesBeforeAnyOwnerCall(t *testing.T) {
	const otherDigest = "1111111111111111111111111111111111111111111111111111111111111111"

	cases := []struct {
		field string
		drift func(*ResumeIdentitySet)
		want  ResumeRefusalReason
	}{
		{"RepositoryDomain", func(s *ResumeIdentitySet) { s.RepositoryDomain = "github.com/example/other" }, RefusalRepositoryDomainDrift},
		{"BaseRevision", func(s *ResumeIdentitySet) { s.BaseRevision = strings.Repeat("b", 40) }, RefusalBaseRevisionDrift},
		{"WorkspaceIdentityDigestSHA256", func(s *ResumeIdentitySet) { s.WorkspaceIdentityDigestSHA256 = otherDigest }, RefusalWorkspaceIdentityDrift},
		{"GraphAuthorityDigestSHA256", func(s *ResumeIdentitySet) { s.GraphAuthorityDigestSHA256 = otherDigest }, RefusalGraphAuthorityDrift},
		{"TaskID", func(s *ResumeIdentitySet) { s.TaskID = "task.o7.somethingelse" }, RefusalTaskIdentityDrift},
		{"TaskSessionDigestSHA256", func(s *ResumeIdentitySet) { s.TaskSessionDigestSHA256 = otherDigest }, RefusalTaskIdentityDrift},
		{"TaskControlStateDigestSHA256", func(s *ResumeIdentitySet) { s.TaskControlStateDigestSHA256 = otherDigest }, RefusalTaskControlDrift},
		{"TaskControlGeneration", func(s *ResumeIdentitySet) { s.TaskControlGeneration++ }, RefusalTaskControlDrift},
		{"ClosureReportDigestSHA256", func(s *ResumeIdentitySet) { s.ClosureReportDigestSHA256 = otherDigest }, RefusalClosureDrift},
	}

	// Every field of the observed identity set must appear above. Counted
	// rather than eyeballed, so the table cannot fall behind the struct.
	if fields := resumeIdentityFieldCount(); len(cases) != fields {
		t.Fatalf("ResumeIdentitySet has %d fields but the drift matrix covers %d; a boundary field with no drift case is a boundary nobody compares", fields, len(cases))
	}

	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			checkpoint, original, store := interruptedSession(t)
			config, owners := resumeConfig(t, original, store)

			binding := bindingFor(checkpoint)
			tc.drift(&binding.Current)

			savesBefore := owners.checkpoints.saves
			result, assessment, err := Resume(context.Background(), checkpoint, binding, config)

			// A refusal is a governed answer, not a failure to answer.
			if err != nil {
				t.Fatalf("a refused resume returned an error: %v", err)
			}
			if assessment.Allowed() {
				t.Fatalf("%s drift was allowed to resume", tc.field)
			}
			if assessment.RefusalReason == nil {
				t.Fatal("a refusal carried no reason")
			}
			// The typed refusal must survive the dispatcher unchanged: the
			// driver reports the assessment's verdict, it does not re-derive
			// or re-word one of its own.
			if *assessment.RefusalReason != tc.want {
				t.Fatalf("%s drift refused as %q, want %q", tc.field, *assessment.RefusalReason, tc.want)
			}

			// THE LAW: nothing ran.
			if owners.total() != 0 {
				t.Fatalf("a refused resume called owners: %v", owners.breakdown())
			}
			// No checkpoint advancement.
			if owners.checkpoints.saves != savesBefore {
				t.Fatalf("a refused resume appended %d checkpoint(s)", owners.checkpoints.saves-savesBefore)
			}
			// No terminal evidence: a refused resume produces an assessment,
			// never a run receipt, because no run occurred to receipt.
			if result.Receipt.ReceiptDigestSHA256 != "" {
				t.Fatal("a refused resume produced a run receipt")
			}
			if result.Candidate != nil {
				t.Fatal("a refused resume produced a candidate artifact")
			}
			if result.Interpretation != nil || result.Plan != nil {
				t.Fatal("a refused resume produced accepted synthesis artifacts")
			}
		})
	}
}

// The positive control. Without it the matrix above is satisfied by a Resume
// that refuses everything, or calls nothing ever -- both of which would pass
// nine subtests while proving the opposite of what is claimed.
func TestAnUndriftedResumeDoesCallTheSelectedOwner(t *testing.T) {
	checkpoint, original, store := interruptedSession(t)
	config, owners := resumeConfig(t, original, store)

	savesBefore := owners.checkpoints.saves
	result, assessment, err := Resume(context.Background(), checkpoint, bindingFor(checkpoint), config)
	if err != nil {
		t.Fatalf("an undrifted resume failed: %v", err)
	}
	if !assessment.Allowed() {
		t.Fatalf("an unchanged boundary was refused as %v", assessment.RefusalReason)
	}
	if owners.total() == 0 {
		t.Fatal("an allowed resume called no owner at all, so the refusal matrix proves nothing")
	}
	// The phase captured in the checkpoint decides which owner runs next, and
	// completed history is not replayed: interpretation was already accepted
	// before the crash, so resuming continues from planning rather than
	// re-asking for an interpretation.
	if owners.interpretation.calls != 0 {
		t.Fatalf("resume replayed interpretation %d time(s); completed history was reinterpreted", owners.interpretation.calls)
	}
	if owners.planning.calls == 0 {
		t.Fatal("resume did not call the planning provider the captured phase selected")
	}
	if owners.checkpoints.saves <= savesBefore {
		t.Fatal("an allowed resume recorded no new durable boundary")
	}
	if result.Receipt.ReceiptDigestSHA256 == "" {
		t.Fatal("an allowed resume produced no run receipt")
	}
}

// Admission and application are absent from Config by design, so "a refused
// resume performs no admission or application" holds structurally rather than
// by counting: the driver has no capability to admit or apply, and those are
// separate commands (sensei admit-change / sensei synthesis-apply) operating
// on a persisted bundle. This test pins that structure, so wiring either owner
// into the driver later cannot happen silently without this law being
// revisited.
func TestTheDriverHoldsNoAdmissionOrApplicationCapability(t *testing.T) {
	_, config, _ := checkpointHarness(t, 0, 0)
	for name, present := range driverCapabilityNames(config) {
		lowered := strings.ToLower(name)
		if strings.Contains(lowered, "admission") || strings.Contains(lowered, "admit") || strings.Contains(lowered, "apply") {
			t.Fatalf("Config exposes %q (set=%v): the driver must not be able to admit or apply", name, present)
		}
	}
}

// resumeIdentityFieldCount counts the observed-boundary fields by reflection
// rather than by a hand-maintained number, so the matrix above is checked
// against the struct itself.
func resumeIdentityFieldCount() int {
	return reflect.TypeOf(ResumeIdentitySet{}).NumField()
}

// driverCapabilityNames reports every capability field on Config and whether
// it is populated, for the structural assertion that none of them is an
// admission or application owner.
func driverCapabilityNames(config Config) map[string]bool {
	value := reflect.ValueOf(config)
	typ := value.Type()
	out := make(map[string]bool, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := value.Field(i)
		out[typ.Field(i).Name] = field.IsValid() && !field.IsZero()
	}
	return out
}
