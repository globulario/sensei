// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeFakeClaude writes an executable stub that mimics `claude --print
// --output-format json`. If ANTHROPIC_API_KEY is set in its environment it
// signals a leak (so the test can prove the client strips it); otherwise it
// emits the given envelope JSON.
func writeFakeClaude(t *testing.T, envelope string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n" +
		"saw_tools=0\n" +
		"prev=\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--tools\" ] && [ -z \"$arg\" ]; then saw_tools=1; fi\n" +
		"  prev=\"$arg\"\n" +
		"done\n" +
		"if [ \"$saw_tools\" != 1 ]; then\n" +
		"  printf '%s' '{\"is_error\":true,\"result\":\"TOOLS_NOT_DISABLED\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"cat >/dev/null\n" + // drain stdin (the prompt)
		"if [ -n \"$ANTHROPIC_API_KEY\" ] || [ -n \"$ANTHROPIC_AUTH_TOKEN\" ]; then\n" +
		"  printf '%s' '{\"is_error\":true,\"result\":\"ENV_LEAK\"}'\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s' '" + envelope + "'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	return path
}

func TestClaudeCLIClient_UnwrapsResultAndStripsEnv(t *testing.T) {
	// The model's answer is a JSON candidate object carried in .result.
	fake := writeFakeClaude(t, `{"type":"result","is_error":false,"result":"{\"candidate_class\":\"failure_mode\"}"}`)

	// A bad key in the parent env must NOT reach the child (would yield ENV_LEAK).
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-bad")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "bad-token")

	c := &ClaudeCLIClient{Binary: fake, Model: "sonnet"}
	out, err := c.Complete(context.Background(), LLMRequest{System: "sys", User: "prompt"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != `{"candidate_class":"failure_mode"}` {
		t.Fatalf("result = %q, want the unwrapped .result payload", out)
	}
}

func TestClaudeCLIClient_PropagatesIsError(t *testing.T) {
	fake := writeFakeClaude(t, `{"type":"result","is_error":true,"result":"Invalid API key"}`)
	c := &ClaudeCLIClient{Binary: fake, Model: "sonnet"}
	if _, err := c.Complete(context.Background(), LLMRequest{User: "x"}); err == nil {
		t.Fatal("expected error when envelope is_error=true, got nil")
	}
}

func TestMapModelForCLI(t *testing.T) {
	cases := map[string]string{
		"":                  "sonnet",
		"claude-opus-4-8":   "opus",
		"claude-sonnet-4-6": "sonnet",
		"claude-haiku-4-5":  "haiku",
		"opus":              "opus",
		"some-future-id":    "some-future-id",
	}
	for in, want := range cases {
		if got := mapModelForCLI(in); got != want {
			t.Errorf("mapModelForCLI(%q) = %q, want %q", in, got, want)
		}
	}
}
