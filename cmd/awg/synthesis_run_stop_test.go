// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allStopReasons is the vocabulary under test. Listed explicitly rather than
// ranged from stopExitCodes, so a reason added to the map without a test — or
// dropped from the map — is visible here instead of silently self-satisfying.
var allStopReasons = []resolutionStopReason{
	stopUnclassified,
	stopNoTaskCheckpoint,
	stopTaskAwaitingAnswer,
	stopGraphIdentityUnusable,
	stopClosureUnavailable,
	stopInterpretationUnavailable,
	stopCognitiveProviderUnavailable,
	stopGenerationProviderUnavailable,
}

// THE requirement issue #149 states: the degraded worlds may not be collapsed.
// If two of them share an exit code, a caller scripting this command cannot
// tell them apart without parsing prose, which is exactly the state this
// vocabulary replaced.
func TestEveryStopReasonHasItsOwnExitCode(t *testing.T) {
	seen := map[int]resolutionStopReason{}
	for _, r := range allStopReasons {
		code, ok := stopExitCodes[r]
		if !ok {
			t.Errorf("%s has no exit code; it would fall through to the internal-defect code", r)
			continue
		}
		if prior, dup := seen[code]; dup {
			t.Errorf("%s and %s both exit %d; the two worlds are indistinguishable to a caller", prior, r, code)
		}
		seen[code] = r
		if strings.TrimSpace(stopMeanings[r]) == "" {
			t.Errorf("%s has no meaning; the exit code says which world, the meaning says why", r)
		}
	}
}

// The pre-run codes must not collide with the driver's disposition codes, or a
// caller would read "the generation provider is missing" as "the governed run
// hit its step limit" — a completely different situation with a different fix.
func TestStopCodesDoNotCollideWithDispositionCodes(t *testing.T) {
	dispositionCodes := map[int]string{
		exitCandidateReady:       "candidate-ready",
		exitInvalidInvocation:    "invalid-invocation",
		exitGovernedTerminalStop: "terminal-stop",
		exitGovernedProviderStop: "provider-stop",
		exitGovernedRunnerStop:   "runner-stop",
		exitStepLimitReached:     "step-limit",
		exitInternalDefect:       "internal-defect",
	}
	for _, r := range allStopReasons {
		code := stopExitCodes[r]
		if r == stopUnclassified {
			continue // deliberately retains exitResolutionFailure
		}
		if name, clash := dispositionCodes[code]; clash {
			t.Errorf("stop %s exits %d, which already means %q", r, code, name)
		}
	}
}

// A reason with no code is a defect in this vocabulary, and must fail loudly
// rather than borrow a typed world's code and misreport which state occurred.
func TestUnknownStopReasonIsAnInternalDefect(t *testing.T) {
	if got := stopExitCode("not-a-reason"); got != exitInternalDefect {
		t.Fatalf("unknown reason exited %d, want %d (internal defect)", got, exitInternalDefect)
	}
}

// The JSON form must be a STOP, not a report. Emitting a report-shaped object
// with empty receipt/disposition/candidate fields would let a caller read "no
// candidate" as "the run completed and produced nothing", which is the
// collapse this issue forbids.
func TestJSONStopIsTypedAndDoesNotImpersonateACompletedRun(t *testing.T) {
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := resolutionStop("json", stopNoTaskCheckpoint, "no active task pointer", "run 'sensei prepare-change'")
	w.Close()
	os.Stdout = stdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	var stop synthesisRunStop
	if err := json.Unmarshal([]byte(buf.String()), &stop); err != nil {
		t.Fatalf("stop output is not valid JSON: %v\n%s", err, buf.String())
	}
	if stop.Reason != string(stopNoTaskCheckpoint) {
		t.Errorf("reason = %q, want %q", stop.Reason, stopNoTaskCheckpoint)
	}
	if stop.ExitCode != code || code != stopExitCodes[stopNoTaskCheckpoint] {
		t.Errorf("exit code disagreement: returned %d, record says %d", code, stop.ExitCode)
	}
	if stop.RanDriver {
		t.Error("a pre-run stop claims it ran the governed driver")
	}
	if strings.TrimSpace(stop.Meaning) == "" {
		t.Error("the stop carries no meaning")
	}
	// A completed run reports a disposition; a stop must not.
	if strings.Contains(buf.String(), "\"disposition\"") || strings.Contains(buf.String(), "\"receipt\"") {
		t.Errorf("the stop record carries report fields, so it can be mistaken for a completed run:\n%s", buf.String())
	}
}

// End to end through the real command: a repository with no task checkpoint
// must exit with the no-task-checkpoint code specifically, not the generic
// resolution code it used to share with every other pre-run failure.
func TestSynthesisRun_NoTaskCheckpointExitsItsOwnCode(t *testing.T) {
	repo := t.TempDir()
	agent := filepath.Join(t.TempDir(), "fake-agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	interp := filepath.Join(t.TempDir(), "interpretation.json")
	if err := os.WriteFile(interp, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := runSynthesisRun([]string{
		"--repo", repo,
		"--interpretation", interp,
		"--agent", "codex",
		"--agent-command", agent,
	})
	want := stopExitCodes[stopNoTaskCheckpoint]
	if got != want {
		t.Fatalf("exit %d, want %d (no-task-checkpoint); %d is the generic resolution code this issue forbids collapsing into",
			got, want, exitResolutionFailure)
	}
}
