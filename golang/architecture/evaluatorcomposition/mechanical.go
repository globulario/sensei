// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// MechanicalCommand is one exact shell-free check invocation. Env is the
// complete environment, not additions to ambient process state.
type MechanicalCommand struct {
	CheckID    string
	Executable string
	Args       []string
	Env        []string
}

// MechanicalEvaluator maps bounded process truth into existing O1
// CheckObservation values. It owns no recommendation or transition authority.
type MechanicalEvaluator struct {
	descriptor EvaluatorDescriptor
	surface    EvaluatorSurface
	runner     CommandRunner
	sink       EvidenceSink
	commands   []MechanicalCommand
}

func NewMechanicalEvaluator(evaluatorID, version string, deterministic bool, surface EvaluatorSurface, runner CommandRunner, sink EvidenceSink, commands []MechanicalCommand) (*MechanicalEvaluator, error) {
	evaluatorID = strings.TrimSpace(evaluatorID)
	version = strings.TrimSpace(version)
	if evaluatorID == "" || version == "" {
		return nil, fmt.Errorf("NewMechanicalEvaluator: evaluatorID and version must not be empty")
	}
	if surface == nil || runner == nil || sink == nil {
		return nil, fmt.Errorf("NewMechanicalEvaluator: surface, runner, and sink are required")
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("NewMechanicalEvaluator: at least one command is required")
	}
	seen := make(map[string]bool, len(commands))
	copied := make([]MechanicalCommand, len(commands))
	checkIDs := make([]string, len(commands))
	for i, command := range commands {
		command.CheckID = strings.TrimSpace(command.CheckID)
		if command.CheckID == "" {
			return nil, fmt.Errorf("NewMechanicalEvaluator: commands[%d].CheckID is empty", i)
		}
		if seen[command.CheckID] {
			return nil, fmt.Errorf("NewMechanicalEvaluator: duplicate check ID %q", command.CheckID)
		}
		seen[command.CheckID] = true
		if !filepath.IsAbs(command.Executable) {
			return nil, fmt.Errorf("NewMechanicalEvaluator: command %q executable %q must be absolute", command.CheckID, command.Executable)
		}
		command.Args = append([]string(nil), command.Args...)
		command.Env = append([]string(nil), command.Env...)
		copied[i] = command
		checkIDs[i] = command.CheckID
	}
	sort.Strings(checkIDs)
	descriptor := EvaluatorDescriptor{
		SchemaVersion:       EvaluatorDescriptorSchemaVersion,
		EvaluatorID:         evaluatorID,
		EvaluatorKind:       "mechanical-command",
		EvaluatorVersion:    version,
		SupportedCheckIDs:   checkIDs,
		Deterministic:       deterministic,
		RequiredCapabilities: []string{"bounded-process-execution", "sealed-candidate-surface"},
		Limitations: []synthesis.Limitation{
			{Source: "evaluatorcomposition.mechanical", Scope: evaluatorID, Reason: "process isolation is cooperative host-process execution, not a kernel sandbox", Blocking: false},
		},
	}
	descriptor = NormalizeEvaluatorDescriptor(descriptor)
	digest, err := EvaluatorDescriptorDigest(descriptor)
	if err != nil {
		return nil, err
	}
	descriptor.DescriptorDigestSHA256 = digest
	if err := ValidateEvaluatorDescriptor(descriptor); err != nil {
		return nil, fmt.Errorf("NewMechanicalEvaluator: descriptor: %w", err)
	}
	return &MechanicalEvaluator{descriptor: descriptor, surface: surface, runner: runner, sink: sink, commands: copied}, nil
}

func (e *MechanicalEvaluator) Describe(context.Context) (EvaluatorDescriptor, error) {
	if e == nil {
		return EvaluatorDescriptor{}, fmt.Errorf("MechanicalEvaluator.Describe: nil evaluator")
	}
	return e.descriptor, nil
}

type mechanicalCommandEvidence struct {
	CheckID           string         `json:"check_id"`
	Executable        string         `json:"executable"`
	Args              []string       `json:"args"`
	Env               []string       `json:"env"`
	WorkingSurfaceRef string         `json:"working_surface_ref"`
	Outcome           CommandOutcome `json:"outcome"`
	ExitCode          int            `json:"exit_code"`
	Stdout            []byte         `json:"stdout"`
	Stderr            []byte         `json:"stderr"`
	Truncated         bool           `json:"truncated"`
	Detail            string         `json:"detail"`
}

type mechanicalEvidenceBundle struct {
	SchemaVersion string                      `json:"schema_version"`
	Commands      []mechanicalCommandEvidence `json:"commands"`
}

func (e *MechanicalEvaluator) Evaluate(ctx context.Context, input EvaluationInput) (EvaluatorResult, error) {
	if e == nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: nil evaluator")
	}
	if err := ValidateEvaluationInput(input); err != nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: invalid input: %w", err)
	}
	if input.EvaluatorSurfaceRef != e.surface.Ref() {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: input surface ref does not match constructed surface")
	}
	root, err := e.surface.RootPath()
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: surface: %w", err)
	}
	deadline, err := time.Parse(time.RFC3339, input.DeadlineAt)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: deadline_at: %w", err)
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	checks := make([]synthesis.CheckObservation, 0, len(e.commands))
	commandEvidence := make([]mechanicalCommandEvidence, 0, len(e.commands))
	failureReasons := make([]string, 0, 1)
	limitations := append([]synthesis.Limitation(nil), e.descriptor.Limitations...)
	remainingBytes := input.MaxEvidenceBytes
	terminalOutcome := EvaluatorOutcomeCompleted
	stopped := false

	for _, command := range e.commands {
		if stopped {
			checks = append(checks, synthesis.CheckObservation{CheckID: command.CheckID, Status: synthesis.CheckSkipped, Detail: "not run after evaluator terminal outcome", EvidenceReferences: []string{}})
			continue
		}
		result, runErr := e.runner.Run(runCtx, CommandRequest{
			Executable: command.Executable,
			Args:       append([]string(nil), command.Args...),
			Env:        append([]string(nil), command.Env...),
			Dir:        root,
		}, remainingBytes)
		if runErr != nil {
			return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: check %q command port: %w", command.CheckID, runErr)
		}
		used := int64(len(result.Stdout) + len(result.Stderr))
		if used > remainingBytes {
			return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: command runner exceeded evidence budget")
		}
		remainingBytes -= used
		commandEvidence = append(commandEvidence, mechanicalCommandEvidence{
			CheckID: command.CheckID, Executable: command.Executable,
			Args: append([]string(nil), command.Args...), Env: append([]string(nil), command.Env...),
			WorkingSurfaceRef: input.EvaluatorSurfaceRef, Outcome: result.Outcome,
			ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr,
			Truncated: result.Truncated, Detail: result.Detail,
		})
		if result.Truncated {
			limitations = append(limitations, synthesis.Limitation{Source: "evaluatorcomposition.mechanical", Scope: command.CheckID, Reason: "captured process output was truncated at the precommitted evidence byte limit", Blocking: false})
		}

		observation := synthesis.CheckObservation{CheckID: command.CheckID, EvidenceReferences: []string{}}
		switch result.Outcome {
		case CommandOutcomeCompleted:
			observation.Status = synthesis.CheckPassed
			observation.Detail = "command exited 0"
		case CommandOutcomeExited:
			observation.Status = synthesis.CheckFailed
			observation.Detail = fmt.Sprintf("command exited %d", result.ExitCode)
			if len(failureReasons) == 0 {
				failureReasons = append(failureReasons, string(FailureClassMechanicalCheckFailure))
			}
		case CommandOutcomeTimedOut:
			observation.Status = synthesis.CheckUnavailable
			observation.Detail = "command timed out: " + result.Detail
			terminalOutcome = EvaluatorOutcomeTimedOut
			stopped = true
		case CommandOutcomeCancelled:
			observation.Status = synthesis.CheckUnavailable
			observation.Detail = "command cancelled: " + result.Detail
			terminalOutcome = EvaluatorOutcomeCancelled
			stopped = true
		default:
			observation.Status = synthesis.CheckUnavailable
			observation.Detail = "command unavailable: " + result.Detail
			terminalOutcome = EvaluatorOutcomeUnavailable
			stopped = true
		}
		checks = append(checks, observation)
	}

	if input.MaxEvidenceCount < 1 {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: max_evidence_count %d cannot carry the required command evidence bundle", input.MaxEvidenceCount)
	}
	bundleBytes, err := json.Marshal(mechanicalEvidenceBundle{SchemaVersion: "sensei.evaluatorcomposition.mechanical-evidence.v1", Commands: commandEvidence})
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: marshal evidence: %w", err)
	}
	if int64(len(bundleBytes)) > input.MaxEvidenceBytes {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: evidence bundle size %d exceeds max_evidence_bytes %d", len(bundleBytes), input.MaxEvidenceBytes)
	}
	reference, err := e.sink.Put(ctx, bundleBytes)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: persist evidence: %w", err)
	}
	for i := range checks {
		if checks[i].Status != synthesis.CheckSkipped {
			checks[i].EvidenceReferences = []string{reference.Reference}
		}
	}

	result := EvaluatorResult{
		SchemaVersion:                   EvaluatorResultSchemaVersion,
		EvaluatorID:                     e.descriptor.EvaluatorID,
		EvaluatorDescriptorDigestSHA256: e.descriptor.DescriptorDigestSHA256,
		EvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
		TerminalOutcome:                 terminalOutcome,
		Checks:                          checks,
		EvidenceReferences:              []EvidenceReference{reference},
		ClassifiedFailureReasons:        failureReasons,
		Limitations:                     limitations,
		CleanupSucceeded:                nil,
	}
	result = NormalizeEvaluatorResult(result)
	digest, err := EvaluatorResultDigest(result)
	if err != nil {
		return EvaluatorResult{}, err
	}
	result.ResultDigestSHA256 = digest
	if err := ValidateEvaluatorResult(result); err != nil {
		return EvaluatorResult{}, fmt.Errorf("MechanicalEvaluator.Evaluate: constructed invalid result: %w", err)
	}
	return result, nil
}
