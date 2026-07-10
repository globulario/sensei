// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

func TestPropose_UnconfiguredReturnsUnavailable(t *testing.T) {
	s := newServer(nopStore{}) // awarenessDir empty → write path disabled
	_, err := s.Propose(context.Background(), &awarenesspb.ProposeRequest{
		Kind: "failure_mode", Title: "x", RelatedInvariants: []string{"i"}, Evidence: []string{"e"},
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("want Unavailable, got %v", err)
	}
}

func TestPropose_RejectsInvalidWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	s := newServer(nopStore{})
	s.awarenessDir = dir
	resp, err := s.Propose(context.Background(), &awarenesspb.ProposeRequest{
		Kind: "failure_mode", Title: "no contract link", // missing related/contract + evidence
	})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.GetStatus() != awarenesspb.ProposeStatus_PROPOSE_STATUS_REJECTED {
		t.Fatalf("status = %v, want REJECTED", resp.GetStatus())
	}
	if len(resp.GetValidationErrors()) == 0 {
		t.Error("expected validation errors")
	}
	// Nothing should have been written.
	if entries, _ := os.ReadDir(filepath.Join(dir, "candidates")); len(entries) != 0 {
		t.Error("rejected proposal must not write any candidate")
	}
}

func TestPropose_AcceptsAndWritesCandidate(t *testing.T) {
	dir := t.TempDir()
	s := newServer(nopStore{})
	s.awarenessDir = dir
	resp, err := s.Propose(context.Background(), &awarenesspb.ProposeRequest{
		Kind:              "failure_mode",
		Title:             "Stale seed served after reload",
		RelatedInvariants: []string{"awareness.x"},
		Evidence:          []string{"observed stale node after reload"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetStatus() != awarenesspb.ProposeStatus_PROPOSE_STATUS_ACCEPTED {
		t.Fatalf("status = %v, want ACCEPTED", resp.GetStatus())
	}
	if !strings.HasPrefix(resp.GetCandidatePath(), "candidates/proposals/failure_mode.") {
		t.Errorf("candidate_path = %q", resp.GetCandidatePath())
	}
	// The candidate file must exist under awarenessDir and be marked awaiting review.
	dest := filepath.Join(dir, filepath.FromSlash(resp.GetCandidatePath()))
	body, rerr := os.ReadFile(dest)
	if rerr != nil {
		t.Fatalf("candidate not written: %v", rerr)
	}
	if !strings.Contains(string(body), "status: awaiting_review") {
		t.Errorf("candidate missing awaiting_review marker:\n%s", body)
	}
}
