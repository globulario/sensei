// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/providerport"
	"github.com/globulario/sensei/golang/architecture/runnercomposition"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

var _ runnercomposition.GenerationProviderFactory = (*Factory)(nil)
var _ providerport.Provider = (*generationProvider)(nil)

// Config freezes one agent bridge's capability and observation identity.
type Config struct {
	Agent           Agent
	ProviderID      string
	ProviderKind    string
	ModelIdentifier string
	ObservedAt      string
	ProducedAt      string
	MaxSnapshotBytes int
}

// Factory creates one fresh workspace-bound O2 provider per O3 attempt.
type Factory struct {
	config       Config
	capabilities providerport.Capabilities
}

func NewFactory(config Config) (*Factory, error) {
	config.ProviderID = strings.TrimSpace(config.ProviderID)
	config.ProviderKind = strings.TrimSpace(config.ProviderKind)
	config.ModelIdentifier = strings.TrimSpace(config.ModelIdentifier)
	config.ObservedAt = strings.TrimSpace(config.ObservedAt)
	config.ProducedAt = strings.TrimSpace(config.ProducedAt)
	if config.Agent == nil {
		return nil, errors.New("agentcommand: agent is required")
	}
	if config.ProviderID == "" {
		return nil, errors.New("agentcommand: provider id is required")
	}
	if config.ProviderKind == "" {
		config.ProviderKind = "agent-command"
	}
	if _, err := time.Parse(time.RFC3339, config.ObservedAt); err != nil {
		return nil, fmt.Errorf("agentcommand: observed_at must be RFC3339: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, config.ProducedAt); err != nil {
		return nil, fmt.Errorf("agentcommand: produced_at must be RFC3339: %w", err)
	}
	if config.MaxSnapshotBytes <= 0 {
		return nil, errors.New("agentcommand: max snapshot bytes must be positive")
	}

	capabilities := providerport.Capabilities{
		SchemaVersion: providerport.CapabilitiesSchemaVersion,
		ProviderObservation: synthesis.ProviderObservation{
			ProviderID:      config.ProviderID,
			ProviderKind:    config.ProviderKind,
			ModelIdentifier: config.ModelIdentifier,
			ObservedAt:      config.ObservedAt,
		},
		SupportedOperations: []providerport.Operation{providerport.OperationGeneration},
	}
	digest, err := providerport.CapabilitiesDigest(capabilities)
	if err != nil {
		return nil, fmt.Errorf("agentcommand: compute capabilities digest: %w", err)
	}
	capabilities.CapabilitiesDigestSHA256 = digest
	return &Factory{config: config, capabilities: capabilities}, nil
}

func (f *Factory) NewProvider(workspace runnercomposition.CandidateWorkspace) (providerport.Provider, error) {
	previewer, ok := workspace.(runnercomposition.CandidateEvidencePreviewer)
	if !ok {
		return nil, errors.New("agentcommand: O3 workspace does not expose candidate evidence preview")
	}
	return &generationProvider{
		config:       f.config,
		capabilities: f.capabilities,
		workspace:    workspace,
		previewer:    previewer,
	}, nil
}

type generationProvider struct {
	config       Config
	capabilities providerport.Capabilities
	workspace    runnercomposition.CandidateWorkspace
	previewer    runnercomposition.CandidateEvidencePreviewer
}

func (p *generationProvider) Describe(ctx context.Context) (providerport.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return providerport.Capabilities{}, err
	}
	out := p.capabilities
	out.SupportedOperations = append([]providerport.Operation{}, out.SupportedOperations...)
	return out, nil
}

func (p *generationProvider) Execute(ctx context.Context, request providerport.Request, observer providerport.Observer) (providerport.Result, error) {
	if request.Operation != providerport.OperationGeneration {
		return terminalResult(request, providerport.OutcomeUnsupportedCapability, "agent command supports generation only")
	}
	if request.GenerationPayload == nil || request.ExpectedPlanGeneration == nil || request.ExpectedAttemptNumber == nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "generation request is missing plan, generation, or attempt identity")
	}

	prompt, err := p.buildPrompt(*request.GenerationPayload, request)
	if err != nil {
		return providerport.Result{}, err
	}
	plan, err := p.config.Agent.Generate(ctx, prompt, observer)
	if err != nil {
		var invalid *InvalidOutputError
		if errors.As(err, &invalid) {
			return terminalResult(request, providerport.OutcomeInvalidOutput, invalid.Detail)
		}
		return providerport.Result{}, fmt.Errorf("agentcommand: execute agent: %w", err)
	}
	plan = NormalizeMutationPlan(plan)
	if err := ValidateMutationPlan(plan); err != nil {
		var invalid *InvalidOutputError
		if errors.As(err, &invalid) {
			return terminalResult(request, providerport.OutcomeInvalidOutput, invalid.Detail)
		}
		return providerport.Result{}, err
	}
	if err := applyMutationPlan(p.workspace, plan); err != nil {
		return terminalResult(request, providerport.OutcomeInvalidOutput, "apply mutation plan: "+err.Error())
	}

	evidence, err := p.previewer.PreviewCandidateEvidence(ctx)
	if err != nil {
		return providerport.Result{}, fmt.Errorf("agentcommand: preview candidate evidence: %w", err)
	}

	attempt := synthesis.NormalizeAttempt(synthesis.Attempt{
		SchemaVersion:                   synthesis.AttemptSchemaVersion,
		AttemptID:                      fmt.Sprintf("attempt.%s.%d.%d", request.GenerationPayload.PlanID, *request.ExpectedPlanGeneration, *request.ExpectedAttemptNumber),
		AttemptNumber:                  *request.ExpectedAttemptNumber,
		PlanGeneration:                 *request.ExpectedPlanGeneration,
		PlanDigestSHA256:               request.GenerationPayload.PlanDigestSHA256,
		InputCandidateDigestSHA256:     evidence.InputCandidateDigestSHA256,
		ProviderObservation:            p.capabilities.ProviderObservation,
		ProposedChangeDigestSHA256:     evidence.ProposedChangeDigestSHA256,
		EvidenceReferences: []string{
			"mutation-plan:" + plan.MutationPlanDigestSHA256,
			"final-candidate:" + evidence.FinalCandidateContentDigestSHA256,
		},
		TerminalProviderStatus: synthesis.ProviderStatusCompleted,
		ProducedAt:              p.config.ProducedAt,
	})
	attemptDigest, err := synthesis.AttemptDigest(attempt)
	if err != nil {
		return providerport.Result{}, fmt.Errorf("agentcommand: compute attempt digest: %w", err)
	}
	attempt.AttemptDigestSHA256 = attemptDigest
	payloadDigest := attemptDigest

	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     providerport.OutcomeCompleted,
		PayloadDigestSHA256: &payloadDigest,
		GenerationPayload:   &attempt,
	})
	resultDigest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, fmt.Errorf("agentcommand: compute result digest: %w", err)
	}
	result.ResultDigestSHA256 = resultDigest
	return result, nil
}

func (p *generationProvider) buildPrompt(plan synthesis.Plan, request providerport.Request) (GenerationPrompt, error) {
	paths := make(map[string]struct{})
	for _, step := range plan.Steps {
		for _, path := range step.IntendedFiles {
			if err := runnercomposition.ValidateCandidatePath(path); err != nil {
				return GenerationPrompt{}, fmt.Errorf("agentcommand: invalid intended file %q: %w", path, err)
			}
			paths[path] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)

	files := make([]SnapshotFile, 0, len(ordered))
	used := 0
	for _, path := range ordered {
		content, err := p.workspace.ReadSnapshot(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				files = append(files, SnapshotFile{Path: path, Missing: true, Content: []byte{}})
				continue
			}
			return GenerationPrompt{}, fmt.Errorf("agentcommand: read snapshot %q: %w", path, err)
		}
		used += len(content)
		if used > p.config.MaxSnapshotBytes {
			return GenerationPrompt{}, fmt.Errorf("agentcommand: accepted-plan snapshot disclosure exceeds %d bytes", p.config.MaxSnapshotBytes)
		}
		files = append(files, SnapshotFile{Path: path, Content: append([]byte{}, content...)})
	}
	return GenerationPrompt{
		SchemaVersion:       GenerationPromptSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		RepositoryDomain:   request.RepositoryDomain,
		BaseRevision:       request.BaseRevision,
		Plan:               synthesis.NormalizePlan(plan),
		SnapshotFiles:      files,
	}, nil
}

func applyMutationPlan(workspace runnercomposition.CandidateWorkspace, plan MutationPlan) error {
	for _, operation := range plan.Operations {
		var err error
		switch operation.Kind {
		case MutationWrite:
			err = workspace.WriteCandidate(operation.Path, operation.Content)
		case MutationDelete:
			err = workspace.Delete(operation.Path)
		case MutationRename:
			err = workspace.Rename(operation.Path, operation.NewPath)
		case MutationSetMode:
			err = workspace.SetMode(operation.Path, operation.Mode)
		case MutationSymlink:
			err = workspace.Symlink(operation.Path, operation.SymlinkTarget)
		default:
			err = fmt.Errorf("unknown mutation kind %q", operation.Kind)
		}
		if err != nil {
			return fmt.Errorf("operation %q (%s): %w", operation.OperationID, operation.Kind, err)
		}
	}
	return nil
}

func terminalResult(request providerport.Request, outcome providerport.TerminalOutcome, detail string) (providerport.Result, error) {
	result := providerport.NormalizeResult(providerport.Result{
		SchemaVersion:       providerport.ResultSchemaVersion,
		RequestDigestSHA256: request.RequestDigestSHA256,
		Operation:           request.Operation,
		TerminalOutcome:     outcome,
		Detail:              detail,
	})
	digest, err := providerport.ResultDigest(result)
	if err != nil {
		return providerport.Result{}, err
	}
	result.ResultDigestSHA256 = digest
	return result, nil
}
