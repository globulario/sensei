// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/commandprovider"
	"github.com/globulario/sensei/golang/architecture/providerport"
)

// CommandProfile identifies how one vendor CLI is invoked and how its final
// textual answer is extracted. It does not select a provider dynamically.
type CommandProfile string

const (
	ProfileCodex  CommandProfile = "codex"
	ProfileClaude CommandProfile = "claude"
)

// StructuredAgent runs one bounded vendor command and returns only the final
// textual payload extracted from that vendor's noninteractive envelope. It
// assigns no O1/O2/O3 meaning to those bytes.
type StructuredAgent interface {
	Complete(ctx context.Context, prompt []byte, observer providerport.Observer) ([]byte, error)
}

// CommandAgentConfig is the complete process capability granted to one vendor
// command. WorkDir must be an empty, dedicated, real directory. No repository
// or candidate-buffer path is added by this package.
type CommandAgentConfig struct {
	Profile CommandProfile
	Command string
	Args    []string
	WorkDir string

	EnvironmentAllowlist []string
	MaxStdoutBytes       int64
	MaxStderrBytes       int64

	// MaxStructuredPayloadBytes bounds the vendor's extracted final textual
	// answer. MaxMutationPlanBytes is the generation-specific compatibility
	// name retained for O6C. When MaxStructuredPayloadBytes is zero, the
	// mutation-plan limit is used as the structured payload limit.
	MaxStructuredPayloadBytes int
	MaxMutationPlanBytes      int
}

type commandAgent struct {
	config CommandAgentConfig
}

// NewCodexAgent returns the O6C generation-facing Agent profile.
func NewCodexAgent(config CommandAgentConfig) (Agent, error) {
	if config.MaxMutationPlanBytes <= 0 {
		return nil, fmt.Errorf("agentcommand: mutation-plan limit must be positive")
	}
	return newCommandAgent(codexConfig(config))
}

// NewClaudeAgent returns the O6C generation-facing Agent profile.
func NewClaudeAgent(config CommandAgentConfig) (Agent, error) {
	if config.MaxMutationPlanBytes <= 0 {
		return nil, fmt.Errorf("agentcommand: mutation-plan limit must be positive")
	}
	return newCommandAgent(claudeConfig(config))
}

// NewCodexStructuredAgent returns the same confined Codex process boundary
// without assigning generation semantics to the returned bytes.
func NewCodexStructuredAgent(config CommandAgentConfig) (StructuredAgent, error) {
	return newCommandAgent(codexConfig(config))
}

// NewClaudeStructuredAgent returns the same confined Claude process boundary
// without assigning generation semantics to the returned bytes.
func NewClaudeStructuredAgent(config CommandAgentConfig) (StructuredAgent, error) {
	return newCommandAgent(claudeConfig(config))
}

func codexConfig(config CommandAgentConfig) CommandAgentConfig {
	config.Profile = ProfileCodex
	base := []string{"exec", "--sandbox", "read-only", "--ask-for-approval", "never", "--skip-git-repo-check"}
	base = append(base, config.Args...)
	config.Args = append(base, "-")
	return config
}

func claudeConfig(config CommandAgentConfig) CommandAgentConfig {
	config.Profile = ProfileClaude
	base := []string{
		"-p",
		"--output-format", "json",
		"--max-turns", "1",
		"--permission-mode", "plan",
		"--disallowedTools", "Bash,Edit,Write,Read,Glob,Grep,WebFetch,WebSearch",
	}
	config.Args = append(base, config.Args...)
	return config
}

func newCommandAgent(config CommandAgentConfig) (*commandAgent, error) {
	if config.Profile != ProfileCodex && config.Profile != ProfileClaude {
		return nil, fmt.Errorf("agentcommand: unsupported command profile %q", config.Profile)
	}
	if config.MaxStructuredPayloadBytes <= 0 {
		config.MaxStructuredPayloadBytes = config.MaxMutationPlanBytes
	}
	if config.MaxStdoutBytes <= 0 || config.MaxStderrBytes <= 0 || config.MaxStructuredPayloadBytes <= 0 {
		return nil, fmt.Errorf("agentcommand: command and structured-payload limits must be positive")
	}
	if err := validateAgentCommandPath(config.Command); err != nil {
		return nil, err
	}
	if err := validateEmptyAgentWorkDir(config.WorkDir); err != nil {
		return nil, err
	}
	config.Args = append([]string{}, config.Args...)
	config.EnvironmentAllowlist = append([]string{}, config.EnvironmentAllowlist...)
	return &commandAgent{config: config}, nil
}

func (a *commandAgent) Complete(ctx context.Context, prompt []byte, observer providerport.Observer) ([]byte, error) {
	// Revalidate immediately before execution. The directory was empty at
	// construction, but callers retain the path and could populate it later.
	// Refusing here prevents cwd from becoming an ambient repository channel.
	if err := validateEmptyAgentWorkDir(a.config.WorkDir); err != nil {
		return nil, err
	}
	result, err := commandprovider.RunRawCommand(ctx, commandprovider.RawCommand{
		Command:              a.config.Command,
		Args:                 a.config.Args,
		WorkDir:              a.config.WorkDir,
		EnvironmentAllowlist: a.config.EnvironmentAllowlist,
		MaxStdoutBytes:       a.config.MaxStdoutBytes,
		MaxStderrBytes:       a.config.MaxStderrBytes,
	}, append([]byte{}, prompt...))
	if len(result.Stderr) != 0 && observer != nil {
		for _, line := range strings.Split(strings.TrimSpace(string(result.Stderr)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				if observeErr := observer.Observe(line); observeErr != nil {
					break
				}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	payload, err := a.extractFinalPayload(result.Stdout)
	if err != nil {
		return nil, err
	}
	if len(payload) > a.config.MaxStructuredPayloadBytes {
		return nil, invalidOutput("structured payload exceeded %d bytes", a.config.MaxStructuredPayloadBytes)
	}
	return append([]byte{}, payload...), nil
}

func (a *commandAgent) Generate(ctx context.Context, prompt GenerationPrompt, observer providerport.Observer) (MutationPlan, error) {
	promptBytes, err := encodeAgentPrompt(prompt)
	if err != nil {
		return MutationPlan{}, err
	}
	payload, err := a.Complete(ctx, promptBytes, observer)
	if err != nil {
		return MutationPlan{}, err
	}
	if a.config.MaxMutationPlanBytes > 0 && len(payload) > a.config.MaxMutationPlanBytes {
		return MutationPlan{}, invalidOutput("mutation plan exceeded %d bytes", a.config.MaxMutationPlanBytes)
	}
	return decodeMutationPlanProposal(payload)
}

func validateAgentCommandPath(command string) error {
	command = strings.TrimSpace(command)
	if !filepath.IsAbs(command) {
		return fmt.Errorf("agentcommand: command must be an absolute path: %q", command)
	}
	info, err := os.Stat(command)
	if err != nil {
		return fmt.Errorf("agentcommand: inspect command: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("agentcommand: command is a directory: %q", command)
	}
	return nil
}

func validateEmptyAgentWorkDir(workDir string) error {
	workDir = strings.TrimSpace(workDir)
	if !filepath.IsAbs(workDir) {
		return fmt.Errorf("agentcommand: work directory must be an absolute path: %q", workDir)
	}
	info, err := os.Lstat(workDir)
	if err != nil {
		return fmt.Errorf("agentcommand: inspect work directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("agentcommand: work directory must be a real, non-symlink directory: %q", workDir)
	}
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return fmt.Errorf("agentcommand: read work directory: %w", err)
	}
	if len(entries) != 0 {
		return fmt.Errorf("agentcommand: work directory must be empty, found %d entries", len(entries))
	}
	return nil
}

func encodeAgentPrompt(prompt GenerationPrompt) ([]byte, error) {
	data, err := json.Marshal(prompt)
	if err != nil {
		return nil, fmt.Errorf("agentcommand: encode generation prompt: %w", err)
	}
	var out bytes.Buffer
	out.WriteString("You are a bounded software mutation planner. You have no repository tools and no authority to run commands, edit files, use Git, access the network, admit a change, or declare completion.\n")
	out.WriteString("Return exactly one JSON object and nothing else. The object must contain schema_version=\"sensei.agentcommand.mutationplan.v1\", summary, and operations. Each operation must contain all fields: operation_id, kind, path, new_path, content, mode, symlink_target. content is base64 because it is a JSON byte string. Unused fields must be empty. Kinds: write, delete, rename, set-mode, symlink. Modes: regular or executable.\n")
	out.WriteString("Use only files disclosed in snapshot_files and the accepted plan. Do not invent additional repository context.\n\nGENERATION_PROMPT_JSON\n")
	out.Write(data)
	out.WriteByte('\n')
	return out.Bytes(), nil
}

func (a *commandAgent) extractFinalPayload(stdout []byte) ([]byte, error) {
	switch a.config.Profile {
	case ProfileCodex:
		return bytes.TrimSpace(stdout), nil
	case ProfileClaude:
		decoder := json.NewDecoder(bytes.NewReader(stdout))
		var envelope struct {
			Result string `json:"result"`
		}
		if err := decoder.Decode(&envelope); err != nil {
			return nil, invalidOutput("decode Claude JSON envelope: %v", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			if err == nil {
				return nil, invalidOutput("Claude output contained multiple JSON documents")
			}
			return nil, invalidOutput("Claude output contained trailing data: %v", err)
		}
		if strings.TrimSpace(envelope.Result) == "" {
			return nil, invalidOutput("Claude JSON envelope has no result text")
		}
		return []byte(strings.TrimSpace(envelope.Result)), nil
	default:
		return nil, invalidOutput("unsupported command profile %q", a.config.Profile)
	}
}

type mutationPlanProposal struct {
	SchemaVersion string              `json:"schema_version"`
	Summary       string              `json:"summary"`
	Operations    []MutationOperation `json:"operations"`
}

func decodeMutationPlanProposal(data []byte) (MutationPlan, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var proposal mutationPlanProposal
	if err := decoder.Decode(&proposal); err != nil {
		return MutationPlan{}, invalidOutput("decode mutation plan: %v", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return MutationPlan{}, invalidOutput("mutation plan contained multiple JSON documents")
		}
		return MutationPlan{}, invalidOutput("mutation plan contained trailing data: %v", err)
	}
	plan := NormalizeMutationPlan(MutationPlan{
		SchemaVersion: proposal.SchemaVersion,
		Summary:       proposal.Summary,
		Operations:    proposal.Operations,
	})
	digest, err := MutationPlanDigest(plan)
	if err != nil {
		return MutationPlan{}, invalidOutput("compute mutation plan digest: %v", err)
	}
	plan.MutationPlanDigestSHA256 = digest
	if err := ValidateMutationPlan(plan); err != nil {
		return MutationPlan{}, err
	}
	return plan, nil
}
