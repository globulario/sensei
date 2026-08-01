// SPDX-License-Identifier: AGPL-3.0-only

package evaluatorcomposition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// failureClassSenseiGateBlockingFinding is intentionally not a
// GovernedFailureClass. A blocking gate verdict may represent an invariant,
// contract, scope, or forbidden-fix finding; the adapter preserves that
// evidence without silently choosing abort.
const failureClassSenseiGateBlockingFinding = "sensei-gate-blocking-finding"

// SenseiGateConfig names the existing Sensei CLI owner and its explicit
// runtime inputs. PolicyPath is caller-owned and frozen at construction. The
// adapter never reads policy from the candidate surface or implements gate
// policy itself.
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
// the user's live checkout is never passed to gate. The caller-owned gate
// policy is copied and digested at construction, then materialized under the
// disposable .git directory for each invocation so the candidate cannot
// select or rewrite its own evaluator policy.
type SenseiGateEvaluator struct {
	descriptor    EvaluatorDescriptor
	config        SenseiGateConfig
	policyContent []byte
	policyDigest  string
	surface       EvaluatorSurface
	runner        CommandRunner
	sink          EvidenceSink
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
	if strings.TrimSpace(config.PolicyPath) == "" || !filepath.IsAbs(config.PolicyPath) {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: PolicyPath must be a non-empty absolute path outside the candidate surface")
	}

	surfaceRoot, err := surface.RootPath()
	if err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: surface root: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(surfaceRoot)
	if err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: resolve surface root: %w", err)
	}
	policyInfo, err := os.Lstat(config.PolicyPath)
	if err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: inspect policy: %w", err)
	}
	if policyInfo.Mode()&os.ModeSymlink != 0 || !policyInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: PolicyPath must name a real, non-symlink regular file")
	}
	resolvedPolicy, err := filepath.EvalSymlinks(config.PolicyPath)
	if err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: resolve policy: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPolicy)
	if err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: compare policy and surface paths: %w", err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: PolicyPath %q is inside the candidate surface; a candidate must never select or rewrite its own gate policy", config.PolicyPath)
	}
	policyContent, err := os.ReadFile(resolvedPolicy)
	if err != nil {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: read policy snapshot: %w", err)
	}
	if len(policyContent) == 0 {
		return nil, fmt.Errorf("NewSenseiGateEvaluator: policy snapshot must not be empty")
	}
	policySum := sha256.Sum256(policyContent)
	policyDigest := hex.EncodeToString(policySum[:])

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
		RequiredCapabilities: []string{"sensei-gate-cli", "sealed-candidate-git-diff-surface", "sensei-gate-policy-sha256:" + policyDigest},
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
	return &SenseiGateEvaluator{
		descriptor:    descriptor,
		config:        config,
		policyContent: append([]byte(nil), policyContent...),
		policyDigest:  policyDigest,
		surface:       surface,
		runner:        runner,
		sink:          sink,
	}, nil
}

func (e *SenseiGateEvaluator) Describe(context.Context) (EvaluatorDescriptor, error) {
	if e == nil {
		return EvaluatorDescriptor{}, fmt.Errorf("SenseiGateEvaluator.Describe: nil evaluator")
	}
	return e.descriptor, nil
}

func materializeSenseiGatePolicy(rootPath string, content []byte) (string, error) {
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", err
	}
	defer root.Close()
	name := filepath.Join(".git", "sensei-o4-policy.yaml")
	if err := root.WriteFile(name, content, 0o600); err != nil {
		return "", err
	}
	return filepath.Join(rootPath, name), nil
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
	PolicyDigest  string          `json:"policy_digest_sha256"`
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
	policyPath, err := materializeSenseiGatePolicy(root, e.policyContent)
	if err != nil {
		return EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: materialize frozen policy %s: %w", e.policyDigest, err)
	}

	args := []string{
		"gate",
		"--diff", "HEAD",
		"--domain", input.RepositoryDomain,
		"--repo-root", root,
		"--enforce",
		"--json",
		"--event-log", "",
		"--policy", policyPath,
		"--total-timeout", e.config.TotalTimeout.String(),
		"--rpc-timeout", e.config.RPCTimeout.String(),
	}
	if strings.TrimSpace(e.config.Address) != "" {
		args = append(args, "--addr", e.config.Address)
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
		Args:          append([]string(nil), args...),
		Environment:   append([]string(nil), e.config.Environment...),
		SurfaceRef:    input.EvaluatorSurfaceRef,
		PolicyDigest:  e.policyDigest,
		Command:       command,
		Parsed:        parsed,
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
	reference, err := e.sink.Put(runCtx, evidenceBytes)
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
