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

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestPropose_UnconfiguredReturnsUnavailable(t *testing.T) {
	s := newTestServer(nopStore{}) // awarenessDir empty → write path disabled
	_, err := s.Propose(context.Background(), &awarenesspb.ProposeRequest{
		Kind: "failure_mode", Title: "x", RelatedInvariants: []string{"i"}, Evidence: []string{"e"},
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("want Unavailable, got %v", err)
	}
}

func TestPropose_RejectsInvalidWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(nopStore{})
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
	s := newTestServer(nopStore{})
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

// The applied_repair kind is only reachable over the wire if the transport
// carries survival_evidence. A required field that the RPC silently drops would
// make every remote applied_repair proposal fail validation for a reason the
// caller did supply — so prove both halves: the field survives the hop, and its
// absence is refused rather than quietly accepted.
func TestPropose_AppliedRepairCarriesSurvivalEvidenceOverTheWire(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(nopStore{})
	s.awarenessDir = dir

	req := &awarenesspb.ProposeRequest{
		Kind:             "applied_repair",
		Title:            "Verify the composed document parses before the rename",
		Description:      "Added a parse check before the atomic rename.",
		RelatedFailures:  []string{"failure.governed_append_corrupted_a_scaffolded_marker"},
		RequiredTests:    []string{"golang/architecture/governedmutation/apply_test.go:TestFirstAppendStaysValid"},
		SourceFiles:      []string{"golang/architecture/governedmutation/apply.go"},
		SurvivalEvidence: []string{"the guard caught a bug in its own change"},
	}
	resp, err := s.Propose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetStatus() != awarenesspb.ProposeStatus_PROPOSE_STATUS_ACCEPTED {
		t.Fatalf("status = %v, want ACCEPTED (errors: %v)", resp.GetStatus(), resp.GetValidationErrors())
	}
	body, rerr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(resp.GetCandidatePath())))
	if rerr != nil {
		t.Fatalf("candidate not written: %v", rerr)
	}
	for _, want := range []string{"survival_evidence", "the guard caught a bug in its own change"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("candidate is missing %q:\n%s", want, body)
		}
	}

	req.SurvivalEvidence = nil
	resp, err = s.Propose(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.GetStatus() != awarenesspb.ProposeStatus_PROPOSE_STATUS_REJECTED {
		t.Fatalf("an applied_repair with no survival evidence was accepted")
	}
}
