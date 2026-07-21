// SPDX-License-Identifier: AGPL-3.0-only

package coldsource

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/globulario/sensei/golang/extractor"
)

// fakeLLM is a deterministic LLMClient stand-in for golden tests — no network.
type fakeLLM struct {
	reply string
	err   error
}

func (f fakeLLM) Complete(_ context.Context, _ LLMRequest) (string, error) {
	return f.reply, f.err
}

func llmBundle(theme string) Bundle {
	return Bundle{
		ThemeKey:    theme,
		SourceTypes: []SourceType{SourcePRReview, SourceRevertCommit},
		Signals: []ColdSignal{
			{SourceType: SourcePRReview, FilePath: "a.go", Line: 7, PRID: "41", CommentID: "99", MatchedText: "must not absorb 404 into unavailable"},
			{SourceType: SourceRevertCommit, CommitSHA: "abc123", MatchedText: "Revert the wrong 404 classification change"},
		},
	}
}

// 1. valid cited candidate accepted
func TestLLMDrafter_ValidCitedCandidateAccepted(t *testing.T) {
	b := llmBundle("golang.repository.upstream")
	reply := fmt.Sprintf(`{"candidate_class":"ForbiddenFixCandidate","theme":%q,
		"reason":"A 404 from the upstream provider is a permanent absence; classifying it as transient retries forever.",
		"confidence":"medium","activation_trigger":"edit under golang/repository/upstream",
		"required_tests":[],"source_paths":["file:a.go:7","commit:abc123"]}`, b.ThemeKey)

	p, err := LLMDrafter{Client: fakeLLM{reply: reply}}.Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if v := ValidateDraft(p, b); len(v) != 0 {
		t.Fatalf("valid candidate failed contract: %v", v)
	}
	if p.Status != "candidate" {
		t.Errorf("status must be candidate, got %q", p.Status)
	}
	if p.CandidateClass != extractor.CandidateForbiddenFix {
		t.Errorf("class = %q, want ForbiddenFixCandidate", p.CandidateClass)
	}
}

// 2. fabricated citation rejected (by the contract check, not the drafter)
func TestLLMDrafter_FabricatedCitationRejected(t *testing.T) {
	b := llmBundle("repo.x")
	reply := fmt.Sprintf(`{"candidate_class":"InvariantCandidate","theme":%q,"reason":"x",
		"confidence":"medium","activation_trigger":"t","required_tests":[],
		"source_paths":["file:not-in-bundle.go:1"]}`, b.ThemeKey)

	p, err := LLMDrafter{Client: fakeLLM{reply: reply}}.Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft (parse) should succeed: %v", err)
	}
	if v := ValidateDraft(p, b); len(v) == 0 {
		t.Fatalf("fabricated citation must be rejected by ValidateDraft")
	}
}

// 3. malformed JSON rejected
func TestLLMDrafter_MalformedRejected(t *testing.T) {
	_, err := LLMDrafter{Client: fakeLLM{reply: "here is the candidate: {oops not json"}}.
		Draft(context.Background(), llmBundle("repo.x"))
	var de DraftError
	if !errors.As(err, &de) || de.Kind != "malformed" {
		t.Fatalf("expected malformed DraftError, got %v", err)
	}
}

// 4. unknown class rejected
func TestLLMDrafter_UnknownClassRejected(t *testing.T) {
	b := llmBundle("repo.x")
	reply := fmt.Sprintf(`{"candidate_class":"WhateverCandidate","theme":%q,"reason":"x",
		"confidence":"medium","activation_trigger":"t","required_tests":[],
		"source_paths":["commit:abc123"]}`, b.ThemeKey)
	_, err := LLMDrafter{Client: fakeLLM{reply: reply}}.Draft(context.Background(), b)
	var de DraftError
	if !errors.As(err, &de) || de.Kind != "bad_class" {
		t.Fatalf("expected bad_class DraftError, got %v", err)
	}
}

// 5. status is forced to candidate (the schema can't set it; this is the guarantee)
func TestLLMDrafter_ForcesCandidateStatus(t *testing.T) {
	b := llmBundle("repo.x")
	// Even if the model tried to smuggle a status field, it's ignored.
	reply := fmt.Sprintf(`{"candidate_class":"InvariantCandidate","status":"active","theme":%q,
		"reason":"long enough to be meaningful","confidence":"high","activation_trigger":"t",
		"required_tests":[],"source_paths":["commit:abc123"]}`, b.ThemeKey)
	p, err := LLMDrafter{Client: fakeLLM{reply: reply}}.Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if p.Status != "candidate" {
		t.Fatalf("status must be forced to candidate, got %q", p.Status)
	}
}

// 6. shallow candidate rejected (by IsShallow on a denylisted theme)
func TestLLMDrafter_ShallowRejected(t *testing.T) {
	b := llmBundle("vendor.foo")
	reply := fmt.Sprintf(`{"candidate_class":"InvariantCandidate","theme":%q,"reason":"x",
		"confidence":"low","activation_trigger":"t","required_tests":[],"source_paths":["commit:abc123"]}`, b.ThemeKey)
	p, err := LLMDrafter{Client: fakeLLM{reply: reply}}.Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft: %v", err)
	}
	if shallow, _ := IsShallow(p, b); !shallow {
		t.Fatalf("vendored theme must be flagged shallow")
	}
}

// 7. missing required citation rejected (empty source_paths)
func TestLLMDrafter_MissingCitationRejected(t *testing.T) {
	b := llmBundle("repo.x")
	reply := fmt.Sprintf(`{"candidate_class":"InvariantCandidate","theme":%q,"reason":"x",
		"confidence":"medium","activation_trigger":"t","required_tests":[],"source_paths":[]}`, b.ThemeKey)
	p, err := LLMDrafter{Client: fakeLLM{reply: reply}}.Draft(context.Background(), b)
	if err != nil {
		t.Fatalf("draft (parse) should succeed: %v", err)
	}
	if v := ValidateDraft(p, b); len(v) == 0 {
		t.Fatalf("empty source_paths must be rejected by ValidateDraft")
	}
}

// 8. LLM client error surfaces as an llm_error DraftError (not a crash)
func TestLLMDrafter_ClientErrorSurfaces(t *testing.T) {
	_, err := LLMDrafter{Client: fakeLLM{err: errors.New("network down")}}.
		Draft(context.Background(), llmBundle("repo.x"))
	var de DraftError
	if !errors.As(err, &de) || de.Kind != "llm_error" {
		t.Fatalf("expected llm_error DraftError, got %v", err)
	}
}

// 9. LLM unavailable (no API key) produces a clear, typed error — no silent fallback
func TestNewAnthropicClientFromEnv_NoKeyClearError(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := NewAnthropicClientFromEnv("")
	if !errors.Is(err, ErrNoLLMConfig) {
		t.Fatalf("expected ErrNoLLMConfig, got %v", err)
	}
}
