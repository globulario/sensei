// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// failureClassSenseiGateBlockingFinding is intentionally not a
// GovernedFailureClass. A blocking gate verdict may represent an
// invariant, contract, scope, or forbidden-fix finding; the adapter
// must preserve that evidence without silently choosing abort.
const failureClassSenseiGateBlockingFinding = "sensei-gate-blocking-finding"

// SenseiGateConfig names the existing Sensei CLI owner and its explicit
// runtime inputs. The adapter never implements gate policy itself.
type SenseiGateConfig struct {
	EvaluatorID      string
	EvaluatorVersion string
	SenseiExecutable string
	Address          string
	PolicyPath       string
	Environment      []string
	TotalTimeout     time.Duration
	RPCTimeout       time.Duration
}

// SenseiGateEvaluator invokes the existing `sensei gate --enforce --json`
// owner against a disposable git-diff surface. Its repository is private and
// reconstructed from the exact verified base commit plus sealed candidate;
// the user's live checkout is never passed to gate.
type SenseiGateEvaluator struct {
	descriptor EvaluatorDescriptor
	config     SenseiGateConfig
	surface    EvaluatorSurface
	runner     CommandRunner
	sink       EvidenceSink
}

func NewSenseiGateEvaluator(config SenseiGateConfig, surface EvaluatorSurface, runner CommandRunner, sink EvidenceSink) (*SenseiGateEvaluator, error) {
	config.EvaluatorID = strings.TrimSpace(config.EvaluatorID)
	config.EvaluatorVersion = strings.TrimSpace(config.EvaluatorVersion)
	if config.EvaluatorID == "" || config.EvaluatorVersion == "" {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: evaluator ID and version are required")
	}
	if !filepath.IsAbs(config.SenseiExecutable) {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: SenseiExecutable %q must be absolute", config.SenseiExecutable)
	}
	if surface == nil || surface.Mode() != SurfaceModeGitDiff {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: a git-diff evaluator surface is required")
	}
	if runner == nil || sink == nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: runner and sink are required")
	}
	config.Environment = append([]string(nil), config.Environment...)
	if config.TotalTimeout <= 0 {
		config.TotalTimeout = 5 * time.Minute
	}
	if config.RPCTimeout <= 0 {
		config.RPCTimeout = 10 * time.Second
	}

	descriptor := EvaluatorDescriptor{
		SchemaVersion:        EvaluatorDescriptorSchemaVersion,
		EvaluatorID:          config.EvaluatorID,
		EvaluatorKind:        "sensei-edit-diff-gate",
		EvaluatorVersion:     config.EvaluatorVersion,
		SupportedCheckIDs:    []string{"sensei-gate"},
		Deterministic:        false,
		RequiredCapabilities: []string{"sensei-gate-cli", "sealed-candidate-git-diff-surface"},
		Limitations: []synthesis.Limitation{
			{Source: "sensei gate", Scope: config.EvaluatorID, Reason: "the existing gate depends on the configured Sensei graph/server or frozen-contract owner; unavailability is recorded as unavailable, never passing", Blocking: false},
		},
	}
	descriptor = NormalizeEvaluatorDescriptor(descriptor)
	digest, err := EvaluatorDescriptorDigest(descriptor)
	if err != nil {
		return nil, err
	}
	descriptor.DescriptorDigestSHA256 = digest
	if err := ValidateEvaluatorDescriptor(descriptor); err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: descriptor: %w", err)
	}
	return &SenseiGateEvaluator{descriptor: descriptor, config: config, surface: surface, runner: runner, sink: sink}, nil
}

func (e *SenseiGateEvaluator) Describe(context.Context) (EvaluatorDescriptor, error) {
	if e == nil {
		return EvaluatorDescriptor{}, fmt.Errorf("SenseiGateEvaluator.Describe: nil evaluator")
	}
	return e.descriptor, nil
}

type senseiGateJSON struct {
	Diff        string          `json:"diff"`
	Domain      string          `json:"domain"`
	Enforced    bool            `json:"enforced"`
	Blocked     bool            `json:"blocked"`
	WouldBlock  int             `json:"would_block"`
	Warn        int             `json:"warn"`
	ScopeErrors int             `json:"scope_errors"`
	Verdict     string          `json:"verdict"`
	Files       json.RawMessage `json:"files"`
}

type senseiGateEvidence struct {
	SchemaVersion string          `json:"schema_version"`
	Executable    string          `json:"executable"`
	Args          []string        `json:"args"`
	Environment   []string        `json:"environment"`
	SurfaceRef    string          `json:"surface_ref"`
	Command       CommandResult   `json:"command"`
	Parsed        *senseiGateJSON `json:"parsed,omitempty"`
}

func (e *SenseiGateEvaluator) Evaluate(ctx context.Context, input EvaluationInput) (EvaluatorResult, error) {
	if e == nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: nil evaluator")
	}
	if err := ValidateEvaluationInput(input); err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: invalid input: %w", err)
	}
	if input.EvaluatorSurfaceRef != e.surface.Ref() {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: surface reference mismatch")
	}
	root, err := e.surface.RootPath()
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: surface: %w", err)
	}

	args := []string{
		"gate",
		"--diff", "HEAD",
		"--domain", input.RepositoryDomain,
		"--repo-root", root,
		"--enforce",
		"--json",
		"--event-log", "",
		"--total-timeout", e.config.TotalTimeout.String(),
		"--rpc-timeout", e.config.RPCTimeout.String(),
	}
	if strings.TrimSpace(e.config.Address) != "" {
		args = append(args, "--addr", e.config.Address)
	}
	if strings.TrimSpace(e.config.PolicyPath) != "" {
		args = append(args, "--policy", e.config.PolicyPath)
	}

	deadline, err := time.Parse(time.RFC3339, input.DeadlineAt)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: deadline_at: %w", err)
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	command, err := e.runner.Run(runCtx, CommandRequest{
		Executable: e.config.SenseiExecutable,
		Args:       args,
		Env:        append([]string(nil), e.config.Environment...),
		Dir:        root,
	}, input.MaxEvidenceBytes)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: command port: %w", err)
	}

	var parsed *senseiGateJSON
	if len(command.Stdout) > 0 {
		var value senseiGateJSON
		if json.Unmarshal(command.Stdout, &value) == nil {
			parsed = &value
		}
	}
	evidence := senseiGateEvidence{
		SchemaVersion: "sensei.evaluatorcomposition.sensei-gate-evidence.v1",
		Executable:    e.config.SenseiExecutable,
		Args:          append([]string(nil), args...), Environment: append([]string(nil), e.config.Environment...),
		SurfaceRef: input.EvaluatorSurfaceRef, Command: command, Parsed: parsed,
	}
	evidenceBytes, err := json.Marshal(evidence)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: marshal evidence: %w", err)
	}
	if input.MaxEvidenceCount < 1 {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: max_evidence_count cannot carry gate evidence")
	}
	if int64(len(evidenceBytes)) > input.MaxEvidenceBytes {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: evidence size %d exceeds max_evidence_bytes %d", len(evidenceBytes), input.MaxEvidenceBytes)
	}
	reference, err := e.sink.Put(ctx, evidenceBytes)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: persist evidence: %w", err)
	}

	observation := synthesis.CheckObservation{CheckID: "sensei-gate", EvidenceReferences: []string{reference.Reference}}
	terminalOutcome := EvaluatorOutcomeCompleted
	failureReasons := []string{}
	limitations := append([]synthesis.Limitation(nil), e.descriptor.Limitations...)
	if command.Truncated {
		limitations = append(limitations, synthesis.Limitation{Source: "sensei gate", Scope: e.descriptor.EvaluatorID, Reason: "gate output was truncated at the precommitted evidence byte limit", Blocking: true})
	}

	switch command.Outcome {
	case CommandOutcomeCompleted:
		if parsed == nil || !parsed.Enforced || parsed.Diff != "HEAD" || parsed.Domain != input.RepositoryDomain {
			observation.Status = synthesis.CheckUnavailable
			observation.Detail = "Sensei gate returned invalid or identity-mismatched JSON evidence"
			terminalOutcome = EvaluatorOutcomeUnavailable
		} else if parsed.Blocked || parsed.WouldBlock > 0 || parsed.ScopeErrors > 0 {
			// Exit 0 with blocked/scope-error JSON contradicts enforcing gate
			// semantics. Refuse the impossible world rather than treating it as
			// a pass.
			observation.Status = synthesis.CheckUnavailable
			observation.Detail = "Sensei gate exit code contradicted its enforcing JSON verdict"
			terminalOutcome = EvaluatorOutcomeUnavailable
		} else {
			observation.Status = synthesis.CheckPassed
			observation.Detail = parsed.Verdict
		}
	case CommandOutcomeExited:
		if command.ExitCode == 1 && parsed != nil && parsed.Enforced && parsed.Blocked && parsed.Domain == input.RepositoryDomain {
			observation.Status = synthesis.CheckFailed
			observation.Detail = parsed.Verdict
			failureReasons = append(failureReasons, failureClassSenseiGateBlockingFinding)
		} else if command.ExitCode == 2 {
			observation.Status = synthesis.CheckUnavailable
			if parsed != nil && parsed.Verdict != "" {
				observation.Detail = parsed.Verdict
			} else {
				observation.Detail = "Sensei gate could not verify the sealed diff"
			}
			terminalOutcome = EvaluatorOutcomeUnavailable
		} else {
			observation.Status = synthesis.CheckUnavailable
			observation.Detail = fmt.Sprintf("Sensei gate exited %d with invalid or contradictory evidence", command.ExitCode)
			terminalOutcome = EvaluatorOutcomeUnavailable
		}
	case CommandOutcomeTimedOut:
		observation.Status = synthesis.CheckUnavailable
		observation.Detail = "Sensei gate timed out: " + command.Detail
		terminalOutcome = EvaluatorOutcomeTimedOut
	case CommandOutcomeCancelled:
		observation.Status = synthesis.CheckUnavailable
		observation.Detail = "Sensei gate cancelled: " + command.Detail
		terminalOutcome = EvaluatorOutcomeCancelled
	default:
		observation.Status = synthesis.CheckUnavailable
		observation.Detail = "Sensei gate unavailable: " + command.Detail
		terminalOutcome = EvaluatorOutcomeUnavailable
	}

	result := EvaluatorResult{
		SchemaVersion:                   EvaluatorResultSchemaVersion,
		EvaluatorID:                     e.descriptor.EvaluatorID,
		EvaluatorDescriptorDigestSHA256: e.descriptor.DescriptorDigestSHA256,
		EvaluationInputDigestSHA256:     input.EvaluationInputDigestSHA256,
		TerminalOutcome:                 terminalOutcome,
		Checks:                          []synthesis.CheckObservation{observation},
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
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: constructed invalid result: %w", err)
	}
	return result, nil
}
