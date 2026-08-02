// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

type fixedAgent struct {
	plan   MutationPlan
	err    error
	prompt GenerationPrompt
}

func (a *fixedAgent) Generate(_ context.Context, prompt GenerationPrompt, _ providerport.Observer) (MutationPlan, error) {
	a.prompt = prompt
	return a.plan, a.err
}

type recordingWorkspace struct {
	snapshot map[string][]byte
	calls    []string
	closed   bool
	evidence runnercomposition.CandidateEvidence
}

func (w *recordingWorkspace) ReadSnapshot(path string) ([]byte, error) {
	if w.closed {
		return nil, runnercomposition.ErrWorkspaceClosed
	}
	content, ok := w.snapshot[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte{}, content...), nil
}
func (w *recordingWorkspace) WriteCandidate(path string, content []byte) error {
	w.calls = append(w.calls, "write:"+path+":"+string(content))
	return nil
}
func (w *recordingWorkspace) Delete(path string) error {
	w.calls = append(w.calls, "delete:"+path)
	return nil
}
func (w *recordingWorkspace) Rename(oldPath, newPath string) error {
	w.calls = append(w.calls, "rename:"+oldPath+":"+newPath)
	return nil
}
func (w *recordingWorkspace) SetMode(path string, mode runnercomposition.CandidateFileMode) error {
	w.calls = append(w.calls, "set-mode:"+path+":"+string(mode))
	return nil
}
func (w *recordingWorkspace) Symlink(path, target string) error {
	w.calls = append(w.calls, "symlink:"+path+":"+target)
	return nil
}
func (w *recordingWorkspace) Close() error {
	w.closed = true
	return nil
}
func (w *recordingWorkspace) PreviewCandidateEvidence(context.Context) (runnercomposition.CandidateEvidence, error) {
	if w.closed {
		return runnercomposition.CandidateEvidence{}, runnercomposition.ErrWorkspaceClosed
	}
	return w.evidence, nil
}

func finalizedPlan(t *testing.T, operations []MutationOperation) MutationPlan {
	t.Helper()
	plan := NormalizeMutationPlan(MutationPlan{
		SchemaVersion: MutationPlanSchemaVersion,
		Summary:       "bounded test mutation",
		Operations:    operations,
	})
	digest, err := MutationPlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.MutationPlanDigestSHA256 = digest
	return plan
}

func requestFixture(t *testing.T) providerport.Request {
	t.Helper()
	plan := synthesis.NormalizePlan(synthesis.Plan{
		SchemaVersion:              synthesis.PlanSchemaVersion,
		PlanID:                     "plan.agentcommand.test",
		InterpretationDigestSHA256: strings.Repeat("a", 64),
		PlanGeneration:             1,
		Steps: []synthesis.PlanStep{{
			StepID:          "step-1",
			Description:     "modify the accepted files",
			IntendedFiles:   []string{"a.txt", "missing.txt"},
			IntendedSymbols: []string{},
			ExpectedEvidence: []string{},
		}},
		Assumptions:    []string{},
		Risks:          []string{},
		StopConditions: []string{},
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:   "planner.test",
			ProviderKind: "fixture",
			ObservedAt:   "2026-08-02T00:00:00Z",
		},
	})
	planDigest, err := synthesis.PlanDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.PlanDigestSHA256 = planDigest
	generation := 1
	attempt := 1
	return providerport.NormalizeRequest(providerport.Request{
		SchemaVersion:              providerport.RequestSchemaVersion,
		RequestID:                  "request.agentcommand.test",
		Operation:                  providerport.OperationGeneration,
		SessionDigestSHA256:        strings.Repeat("b", 64),
		RepositoryDomain:           "github.com/globulario/sensei",
		BaseRevision:               strings.Repeat("c", 40),
		ParentArtifactDigestSHA256: planDigest,
		ExpectedPlanGeneration:     &generation,
		ExpectedAttemptNumber:      &attempt,
		DeadlineAt:                 "2026-08-02T00:10:00Z",
		MaxObservationCount:        8,
		MaxObservationBytes:        4096,
		GenerationPayload:          &plan,
		RequestDigestSHA256:        strings.Repeat("d", 64),
	})
}

func TestGenerationProviderAppliesOnlyTypedWorkspaceOperations(t *testing.T) {
	operations := []MutationOperation{
		{OperationID: "1", Kind: MutationWrite, Path: "a.txt", Content: []byte("after\n")},
		{OperationID: "2", Kind: MutationRename, Path: "a.txt", NewPath: "b.txt", Content: []byte{}},
		{OperationID: "3", Kind: MutationSetMode, Path: "b.txt", Mode: runnercomposition.ModeExecutable, Content: []byte{}},
		{OperationID: "4", Kind: MutationSymlink, Path: "link.txt", SymlinkTarget: "b.txt", Content: []byte{}},
		{OperationID: "5", Kind: MutationDelete, Path: "b.txt", Content: []byte{}},
	}
	agent := &fixedAgent{plan: finalizedPlan(t, operations)}
	workspace := &recordingWorkspace{
		snapshot: map[string][]byte{"a.txt": []byte("before\n")},
		evidence: runnercomposition.CandidateEvidence{
			InputCandidateDigestSHA256:        strings.Repeat("1", 64),
			ProposedChangeDigestSHA256:        strings.Repeat("2", 64),
			FinalCandidateContentDigestSHA256: strings.Repeat("3", 64),
		},
	}
	factory, err := NewFactory(Config{
		Agent:            agent,
		ProviderID:       "agent.test",
		ObservedAt:       "2026-08-02T00:00:00Z",
		ProducedAt:       "2026-08-02T00:01:00Z",
		MaxSnapshotBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := factory.NewProvider(workspace)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Execute(context.Background(), requestFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != providerport.OutcomeCompleted || result.GenerationPayload == nil {
		t.Fatalf("result = %#v", result)
	}
	wantCalls := []string{
		"write:a.txt:after\n",
		"rename:a.txt:b.txt",
		"set-mode:b.txt:executable",
		"symlink:link.txt:b.txt",
		"delete:b.txt",
	}
	if !reflect.DeepEqual(workspace.calls, wantCalls) {
		t.Fatalf("workspace calls = %#v, want %#v", workspace.calls, wantCalls)
	}
	if result.GenerationPayload.InputCandidateDigestSHA256 != workspace.evidence.InputCandidateDigestSHA256 ||
		result.GenerationPayload.ProposedChangeDigestSHA256 != workspace.evidence.ProposedChangeDigestSHA256 {
		t.Fatalf("attempt did not use O3 evidence: %#v", result.GenerationPayload)
	}
	if len(agent.prompt.SnapshotFiles) != 2 || agent.prompt.SnapshotFiles[0].Path != "a.txt" || agent.prompt.SnapshotFiles[1].Path != "missing.txt" || !agent.prompt.SnapshotFiles[1].Missing {
		t.Fatalf("snapshot disclosure = %#v", agent.prompt.SnapshotFiles)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := providerport.ValidateResultSchema(data); err != nil {
		t.Fatalf("result schema: %v", err)
	}
}

func TestGenerationProviderMapsAgentInvalidOutputToTypedResult(t *testing.T) {
	agent := &fixedAgent{err: &InvalidOutputError{Detail: "unknown field"}}
	workspace := &recordingWorkspace{snapshot: map[string][]byte{"a.txt": []byte("before")}}
	factory, err := NewFactory(Config{
		Agent:            agent,
		ProviderID:       "agent.test",
		ObservedAt:       "2026-08-02T00:00:00Z",
		ProducedAt:       "2026-08-02T00:01:00Z",
		MaxSnapshotBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := factory.NewProvider(workspace)
	if err != nil {
		t.Fatal(err)
	}
	result, err := provider.Execute(context.Background(), requestFixture(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalOutcome != providerport.OutcomeInvalidOutput || !strings.Contains(result.Detail, "unknown field") {
		t.Fatalf("result = %#v", result)
	}
}

func TestMutationPlanRejectsTraversalDuplicateAndUnknownKind(t *testing.T) {
	cases := []MutationPlan{
		finalizedPlan(t, []MutationOperation{{OperationID: "1", Kind: MutationWrite, Path: "../escape", Content: []byte("x")}}),
		finalizedPlan(t, []MutationOperation{
			{OperationID: "same", Kind: MutationDelete, Path: "a.txt", Content: []byte{}},
			{OperationID: "same", Kind: MutationDelete, Path: "b.txt", Content: []byte{}},
		}),
		finalizedPlan(t, []MutationOperation{{OperationID: "1", Kind: MutationKind("execute-shell"), Path: "a.txt", Content: []byte{}}}),
	}
	for _, plan := range cases {
		if err := ValidateMutationPlan(plan); err == nil {
			t.Fatalf("invalid plan accepted: %#v", plan)
		}
	}
}

func TestFactoryRequiresO3EvidenceCapability(t *testing.T) {
	factory, err := NewFactory(Config{
		Agent:            &fixedAgent{},
		ProviderID:       "agent.test",
		ObservedAt:       "2026-08-02T00:00:00Z",
		ProducedAt:       "2026-08-02T00:01:00Z",
		MaxSnapshotBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.NewProvider(workspaceWithoutPreview{})
	if err == nil || !strings.Contains(err.Error(), "evidence preview") {
		t.Fatalf("error = %v", err)
	}
}

type workspaceWithoutPreview struct{}

func (workspaceWithoutPreview) ReadSnapshot(string) ([]byte, error) { return nil, errors.New("unused") }
func (workspaceWithoutPreview) WriteCandidate(string, []byte) error { return nil }
func (workspaceWithoutPreview) Delete(string) error { return nil }
func (workspaceWithoutPreview) Rename(string, string) error { return nil }
func (workspaceWithoutPreview) SetMode(string, runnercomposition.CandidateFileMode) error { return nil }
func (workspaceWithoutPreview) Symlink(string, string) error { return nil }
func (workspaceWithoutPreview) Close() error { return nil }
