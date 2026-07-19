// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeCodex(t *testing.T, answer string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n" +
		"out=\n" +
		"prev=\n" +
		"saw_exec=0\n" +
		"saw_readonly=0\n" +
		"saw_ephemeral=0\n" +
		"saw_ignore_user_config=0\n" +
		"saw_ignore_rules=0\n" +
		"saw_skip_git=0\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"exec\" ]; then saw_exec=1; fi\n" +
		"  if [ \"$prev\" = \"--sandbox\" ] && [ \"$arg\" = \"read-only\" ]; then saw_readonly=1; fi\n" +
		"  if [ \"$arg\" = \"--ephemeral\" ]; then saw_ephemeral=1; fi\n" +
		"  if [ \"$arg\" = \"--ignore-user-config\" ]; then saw_ignore_user_config=1; fi\n" +
		"  if [ \"$arg\" = \"--ignore-rules\" ]; then saw_ignore_rules=1; fi\n" +
		"  if [ \"$arg\" = \"--skip-git-repo-check\" ]; then saw_skip_git=1; fi\n" +
		"  if [ \"$prev\" = \"--output-last-message\" ]; then out=\"$arg\"; fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"cat >\"${out}.prompt\"\n" +
		"if [ \"$saw_exec$saw_readonly$saw_ephemeral$saw_ignore_user_config$saw_ignore_rules$saw_skip_git\" != 111111 ]; then\n" +
		"  echo BAD_ARGS >&2\n" +
		"  exit 17\n" +
		"fi\n" +
		"if [ -n \"$OPENAI_API_KEY\" ] || [ -n \"$OPENAI_BASE_URL\" ] || [ -n \"$OPENAI_PROJECT\" ]; then\n" +
		"  echo ENV_LEAK >&2\n" +
		"  exit 18\n" +
		"fi\n" +
		"printf '%s' '" + answer + "' >\"$out\"\n" +
		"printf '%s\n' progress-output\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}
	return path
}

func TestCodexCLIClient_ReturnsLastMessageAndStripsEnv(t *testing.T) {
	fake := writeFakeCodex(t, `{"candidate_class":"failure_mode"}`)
	t.Setenv("OPENAI_API_KEY", "sk-poison")
	t.Setenv("OPENAI_BASE_URL", "https://invalid.example")
	t.Setenv("OPENAI_PROJECT", "proj-poison")

	c := &CodexCLIClient{Binary: fake, Model: "gpt-test"}
	out, err := c.Complete(context.Background(), LLMRequest{System: "sys", User: "prompt"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != `{"candidate_class":"failure_mode"}` {
		t.Fatalf("result = %q, want last-message payload", out)
	}
}

func TestCodexPromptBoundsTheDrafter(t *testing.T) {
	prompt := codexPrompt(LLMRequest{System: "sys", User: "excerpt"})
	for _, want := range []string{"System instructions:", "User request:", "excerpt", "Do not inspect or edit files"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
