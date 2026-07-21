// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNoClaudeCLI is returned when --drafter claude-cli is requested but the
// Claude Code CLI binary cannot be located.
var ErrNoClaudeCLI = errors.New("claude CLI not found: install Claude Code (the `claude` binary) to use --drafter claude-cli")

// ClaudeCLIClient implements LLMClient by shelling out to the locally-installed
// Claude Code CLI in `--print --output-format json` mode. It uses the CLI's own
// configured authentication — e.g. a Claude subscription login in
// ~/.claude/.credentials.json — instead of a raw ANTHROPIC_API_KEY. This mirrors
// the strategy the Globular ai-executor uses (its claude.go): the authed CLI is
// the LLM backend, so no Console API key is required.
//
// A present ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN would OVERRIDE the CLI's
// subscription auth (and can be an invalid sandbox placeholder), so both are
// stripped from the child environment — the whole point of this drafter is to
// use the CLI's own login.
type ClaudeCLIClient struct {
	Binary string
	Model  string // CLI --model value (an alias like "opus"/"sonnet" or a full id)
}

// NewClaudeCLIClient locates the CLI and returns a client. model may be an awg
// model id (e.g. "claude-opus-4-8"); it is mapped to a CLI-friendly value.
func NewClaudeCLIClient(model string) (*ClaudeCLIClient, error) {
	bin := findClaudeCLI()
	if bin == "" {
		return nil, ErrNoClaudeCLI
	}
	return &ClaudeCLIClient{Binary: bin, Model: mapModelForCLI(model)}, nil
}

var findClaudeCLI = findClaudeCLIBinary

// findClaudeCLIBinary resolves the `claude` binary from PATH, then the same
// well-known install locations ai-executor checks.
func findClaudeCLIBinary() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	for _, p := range []string{
		"/usr/local/bin/claude",
		"/usr/bin/claude",
		os.ExpandEnv("$HOME/.local/bin/claude"),
		os.ExpandEnv("$HOME/.claude/bin/claude"),
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// mapModelForCLI turns an awg model id into a value the CLI --model accepts.
// A full id it does not recognize is passed through unchanged.
func mapModelForCLI(model string) string {
	m := strings.TrimSpace(model)
	switch {
	case m == "":
		return "sonnet"
	case strings.HasPrefix(m, "claude-opus"):
		return "opus"
	case strings.HasPrefix(m, "claude-sonnet"):
		return "sonnet"
	case strings.HasPrefix(m, "claude-haiku"):
		return "haiku"
	default:
		return m
	}
}

// claudeCLIResult is the envelope from `claude --print --output-format json`.
type claudeCLIResult struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
}

// Complete implements LLMClient by invoking the CLI once per request. The prompt
// System becomes --system-prompt and User is piped on stdin; the model's answer
// is the .result field of the JSON envelope.
func (c *ClaudeCLIClient) Complete(ctx context.Context, req LLMRequest) (string, error) {
	args := []string{
		"--print",
		"--output-format", "json",
		"--no-session-persistence",
		"--tools", "",
		"--model", c.Model,
	}
	if s := strings.TrimSpace(req.System); s != "" {
		args = append(args, "--system-prompt", s)
	}
	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Env = stripEnvKeys(os.Environ(), "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN")
	cmd.Stdin = strings.NewReader(req.User)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// The CLI can exit non-zero while still emitting a JSON envelope with
	// is_error=true (e.g. an auth failure), so inspect stdout before the error.
	runErr := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if runErr != nil && out == "" {
		return "", fmt.Errorf("claude CLI failed: %w (stderr: %s)", runErr, strings.TrimSpace(stderr.String()))
	}

	var res claudeCLIResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		// Not the wrapper envelope — if the CLI printed raw text, pass it through.
		if out != "" {
			return out, nil
		}
		return "", fmt.Errorf("parse claude CLI output: %w", err)
	}
	if res.IsError {
		return "", fmt.Errorf("claude CLI returned error: %s", strings.TrimSpace(res.Result))
	}
	if strings.TrimSpace(res.Result) == "" {
		return "", fmt.Errorf("claude CLI returned empty result")
	}
	return res.Result, nil
}

// stripEnvKeys returns env with any entry whose key is in keys removed.
func stripEnvKeys(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(e, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}
