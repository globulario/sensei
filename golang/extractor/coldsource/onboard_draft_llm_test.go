// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The wrapper {"candidates":[...]} the strict schema produces is unwrapped to
// the bare array the onboard command unmarshals into []propose.Request.
func TestDraftOnboardingCandidates_UnwrapsCandidatesObject(t *testing.T) {
	reply := `{"candidates":[{"kind":"invariant","title":"x","source_files":["a.go"]}]}`
	got, err := DraftOnboardingCandidates(context.Background(), fakeLLM{reply: reply}, "onboarding brief", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(got)), "[") {
		t.Fatalf("want a bare JSON array, got %s", got)
	}
	if !strings.Contains(string(got), `"kind":"invariant"`) {
		t.Errorf("array content lost: %s", got)
	}
}

// A backend that ignores output_config (e.g. claude-cli) may return a bare,
// possibly fenced, array; it must pass through unchanged.
func TestDraftOnboardingCandidates_AcceptsBareFencedArray(t *testing.T) {
	reply := "```json\n[{\"kind\":\"invariant\",\"title\":\"x\"}]\n```"
	got, err := DraftOnboardingCandidates(context.Background(), fakeLLM{reply: reply}, "onboarding brief", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "[") {
		t.Fatalf("fenced bare array should be returned as-is, got %s", got)
	}
}

func TestDraftOnboardingCandidates_EmptyCandidatesErrors(t *testing.T) {
	if _, err := DraftOnboardingCandidates(context.Background(), fakeLLM{reply: `{"candidates":[]}`}, "onboarding brief", 5); err == nil {
		t.Fatal("empty candidates should error, not return silently")
	}
}

func TestDraftOnboardingCandidates_ClientErrorPropagates(t *testing.T) {
	if _, err := DraftOnboardingCandidates(context.Background(), fakeLLM{err: errors.New("boom")}, "onboarding brief", 5); err == nil {
		t.Fatal("client error must propagate")
	}
}

func TestDraftOnboardingCandidates_NilClient(t *testing.T) {
	if _, err := DraftOnboardingCandidates(context.Background(), nil, "onboarding brief", 5); err == nil {
		t.Fatal("nil client must error")
	}
}
