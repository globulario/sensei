// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderMarkdownIsDeterministic(t *testing.T) {
	r := validReport()
	first := RenderMarkdown(r)
	second := RenderMarkdown(r)
	if !bytes.Equal(first, second) {
		t.Fatalf("RenderMarkdown is not deterministic:\n%s\n---\n%s", first, second)
	}
}

func TestRenderMarkdownHasRequiredSections(t *testing.T) {
	out := string(RenderMarkdown(validReport()))
	for _, want := range []string{
		"# Sensei Report",
		"## Summary",
		"## Current Work",
		"## Important Findings",
		"## Verification",
		"## Behavioral Memory",
		"## Reproduce",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected section %q in output:\n%s", want, out)
		}
	}
}

func TestRenderMarkdownRendersNoActiveTaskLiterally(t *testing.T) {
	out := string(RenderMarkdown(validReport()))
	if !strings.Contains(out, "no active task") {
		t.Fatalf("expected literal \"no active task\" in output:\n%s", out)
	}
}

func TestRenderMarkdownRendersReproductionCommands(t *testing.T) {
	out := string(RenderMarkdown(validReport()))
	if !strings.Contains(out, "sensei report\n") || !strings.Contains(out, "sensei report --check\n") {
		t.Fatalf("expected exact reproduction commands in output:\n%s", out)
	}
	if strings.Contains(out, "sensei verify") {
		t.Fatalf("must never reference the nonexistent sensei verify command:\n%s", out)
	}
}
