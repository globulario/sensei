// SPDX-License-Identifier: AGPL-3.0-only

package agentcommand

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// CommandAgentConfig is the complete process capability granted to one vendor
// command. WorkDir should be an empty, dedicated directory. No repository or
// candidate-buffer path is added by this package.
type CommandAgentConfig struct {
	Profile CommandProfile
	Command string
	Args    []string
	WorkDir string

	EnvironmentAllowlist []string
	MaxStdoutBytes        int64
	MaxStderrBytes        int64
	MaxMutationPlanBytes  int
}

type commandAgent struct {
	config CommandAgentConfig
}

// NewCodexAgent returns a direct-argv Codex Exec profile. The default argv
// uses a read-only sandbox, refuses approvals, skips ambient repository
// discovery, and reads the prompt from stdin. ExtraArgs are inserted before
// the final stdin marker so callers may select a model or profile explicitly.
func NewCodexAgent(config CommandAgentConfig) (Agent, error) {
	config.Profile = ProfileCodex
	base := []string{"exec", "--sandbox", "read-only", "--ask-for-approval", "never", "--skip-git-repo-check"}
	base = append(base, config.Args...)
	config.Args = append(base, "-")
	return newCommandAgent(config)
}

// NewClaudeAgent returns a noninteractive, single-turn Claude Code profile.
// Built-in filesystem, shell, Git-like discovery, and web tools are denied;
// the model receives only the prompt bytes Sensei writes to stdin.
func NewClaudeAgent(config CommandAgentConfig) (Agent, error) {
	config.Profile = ProfileClaude
	base := []string{
		"-p",
		"--output-format", "json",
		"--max-turns", "1",
		"--permission-mode", "plan",
		"--disallowedTools", "Bash,Edit,Write,Read,Glob,Grep,WebFetch,WebSearch",
	}
	config.Args = append(base, config.Args...)
	return newCommandAgent(config)
}

func newCommandAgent(config CommandAgentConfig) (Agent, error) {
	if config.Profile != ProfileCodex && config.Profile != ProfileClaude {
		return nil, fmt.Errorf("agentcommand: unsupported command profile %q", config.Profile)
	}
	if config.MaxStdoutBytes <= 0 || config.MaxStderrBytes <= 0 || config.MaxMutationPlanBytes <= 0 {
		return nil, fmt.Errorf("agentcommand: command and mutation-plan limits must be positive")
	}
	config.Args = append([]string{}, config.Args...)
	config.EnvironmentAllowlist = append([]string{}, config.EnvironmentAllowlist...)
	return &commandAgent{config: config}, nil
}

func (a *commandAgent) Generate(ctx context.Context, prompt GenerationPrompt, observer providerport.Observer) (MutationPlan, error) {
	promptBytes, err := encodeAgentPrompt(prompt)
	if err != nil {
		return MutationPlan{}, err
	}
	result, err := commandprovider.RunRawCommand(ctx, commandprovider.RawCommand{
		Command:              a.config.Command,
		Args:                 a.config.Args,
		WorkDir:              a.config.WorkDir,
		EnvironmentAllowlist: a.config.EnvironmentAllowlist,
		MaxStdoutBytes:       a.config.MaxStdoutBytes,
		MaxStderrBytes:       a.config.MaxStderrBytes,
	}, promptBytes)
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
		return MutationPlan{}, err
	}

	payload, err := a.extractFinalPayload(result.Stdout)
	if err != nil {
		return MutationPlan{}, err
	}
	if len(payload) > a.config.MaxMutationPlanBytes {
		return MutationPlan{}, invalidOutput("mutation plan exceeded %d bytes", a.config.MaxMutationPlanBytes)
	}
	return decodeMutationPlanProposal(payload)
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
