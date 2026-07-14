// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNoCodexCLI is returned when --drafter codex-cli is requested but the
// Codex CLI binary cannot be located.
var ErrNoCodexCLI = errors.New("codex CLI not found: install Codex (the `codex` binary) to use --drafter codex-cli")

// CodexCLIClient implements LLMClient by shelling out to `codex exec`. The CLI
// remains the authentication broker; Sensei never reads Codex credential files
// or session tokens directly.
type CodexCLIClient struct {
	Binary string
	Model  string
}

// NewCodexCLIClient locates the CLI and returns a client. If model is empty,
// the Codex CLI default is used.
func NewCodexCLIClient(model string) (*CodexCLIClient, error) {
	bin := findCodexCLI()
	if bin == "" {
		return nil, ErrNoCodexCLI
	}
	return &CodexCLIClient{Binary: bin, Model: strings.TrimSpace(model)}, nil
}

var findCodexCLI = findCodexCLIBinary

func findCodexCLIBinary() string {
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	for _, p := range []string{
		"/usr/local/bin/codex",
		"/usr/bin/codex",
		os.ExpandEnv("$HOME/.local/bin/codex"),
		os.ExpandEnv("$HOME/.codex/bin/codex"),
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

// Complete invokes Codex once in an empty read-only workspace. The prompt is
// supplied on stdin and the final answer is read from --output-last-message so
// progress output cannot pollute the candidate payload.
func (c *CodexCLIClient) Complete(ctx context.Context, req LLMRequest) (string, error) {
	workDir, err := os.MkdirTemp("", "sensei-codex-drafter-*")
	if err != nil {
		return "", fmt.Errorf("create codex drafter workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	outPath := filepath.Join(workDir, "last-message.txt")
	args := []string{
		"exec",
		"-c", `approval_policy="never"`,
		"--sandbox", "read-only",
		"--cd", workDir,
		"--skip-git-repo-check",
		"--ephemeral",
		"--ignore-user-config",
		"--ignore-rules",
		"--color", "never",
		"--output-last-message", outPath,
	}
	if c.Model != "" {
		args = append(args, "--model", c.Model)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, c.Binary, args...)
	cmd.Env = stripEnvKeys(os.Environ(),
		"OPENAI_API_KEY",
		"OPENAI_BASE_URL",
		"OPENAI_ORG_ID",
		"OPENAI_ORGANIZATION",
		"OPENAI_PROJECT",
		"AZURE_OPENAI_API_KEY",
	)
	cmd.Stdin = strings.NewReader(codexPrompt(req))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codex CLI failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		out := strings.TrimSpace(stdout.String())
		if out != "" {
			return out, nil
		}
		return "", fmt.Errorf("read codex CLI last message: %w", err)
	}
	out := strings.TrimSpace(string(data))
	if out == "" {
		return "", fmt.Errorf("codex CLI returned empty result")
	}
	return out, nil
}

func codexPrompt(req LLMRequest) string {
	var b strings.Builder
	if s := strings.TrimSpace(req.System); s != "" {
		b.WriteString("System instructions:\n")
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	b.WriteString("User request:\n")
	b.WriteString(req.User)
	b.WriteString("\n\nReturn only the requested candidate payload. Do not inspect or edit files; use only the bounded excerpts in this prompt.\n")
	return b.String()
}
