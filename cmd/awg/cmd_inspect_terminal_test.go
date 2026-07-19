// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/completion"
)

func TestRunInspectTerminal_RejectsPositionalAndBadFormat(t *testing.T) {
	if code := runInspectTerminal([]string{"--repo", t.TempDir(), "committed"}); code != 2 {
		t.Fatalf("positional argument must exit 2, got %d", code)
	}
	if code := runInspectTerminal([]string{"--repo", t.TempDir(), "--format", "xml"}); code != 2 {
		t.Fatalf("bad --format must exit 2, got %d", code)
	}
}

// The read surface reports EVERY honest terminal state verbatim and exits 0 — it never
// re-maps a broken/contradictory reconstruction into a pass/fail verdict. Injected
// delegate so all nine states are proven without seeding a durable world.
func TestRunInspectTerminal_RendersEveryStateAndExitsZero(t *testing.T) {
	orig := inspectTerminalDelegate
	defer func() { inspectTerminalDelegate = orig }()

	states := []completion.TerminalState{
		completion.TerminalNotCompleted, completion.TerminalCommitted, completion.TerminalReceiptWithoutEvent,
		completion.TerminalEventWithoutValidReceipt, completion.TerminalContradictoryHistory, completion.TerminalWrongBinding,
		completion.TerminalIntegrityFailure, completion.TerminalProjectionStaleOrMissing, completion.TerminalUnsupported,
	}
	if len(states) != 9 {
		t.Fatalf("expected the full closed terminal-state set of 9, listed %d", len(states))
	}
	for _, st := range states {
		st := st
		inspectTerminalDelegate = func(ctx context.Context, req completion.Request) (completion.TerminalStateAssessment, error) {
			return completion.TerminalStateAssessment{State: st, DigestSHA256: "d"}, nil
		}
		var code int
		out := captureStdout(t, func() {
			code = runInspectTerminal([]string{"--repo", t.TempDir(), "--task-dir", t.TempDir()})
		})
		if code != 0 {
			t.Fatalf("state %s must exit 0 (read-only), got %d", st, code)
		}
		if !strings.Contains(out, string(st)) {
			t.Fatalf("state %s must be rendered verbatim: %q", st, out)
		}
	}
}
