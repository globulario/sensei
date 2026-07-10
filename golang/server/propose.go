// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/propose"
)

// Propose is the agent write path. It validates a typed feedback entry with the
// SAME contract-first rules as `awg propose` (via the shared propose package),
// then writes it as a candidate under <awarenessDir>/candidates/proposals/. It
// NEVER mutates the live corpus and NEVER rebuilds — the proposal is not a graph
// node until a human/CI step promotes it, so the served graph never silently
// mutates durable truth.
//
// Requires the server to be started with -awareness-dir; otherwise Unavailable,
// so a read-only served graph does not gain a write surface by accident.
func (s *server) Propose(_ context.Context, req *awarenesspb.ProposeRequest) (*awarenesspb.ProposeResponse, error) {
	start := time.Now()
	root := strings.TrimSpace(s.awarenessDir)
	if root == "" {
		return nil, status.Error(codes.Unavailable,
			"propose write path is not configured; start the server with -awareness-dir <docs/awareness>")
	}

	pr := propose.Request{
		Kind:              req.GetKind(),
		ID:                req.GetId(),
		Title:             req.GetTitle(),
		Description:       req.GetDescription(),
		Severity:          req.GetSeverity(),
		SourceFiles:       req.GetSourceFiles(),
		RelatedInvariants: req.GetRelatedInvariants(),
		RelatedFailures:   req.GetRelatedFailures(),
		RequiredTests:     req.GetRequiredTests(),
		ForbiddenFixes:    req.GetForbiddenFixes(),
		Evidence:          req.GetEvidence(),
		Repo:              req.GetRepo(),
		Domain:            req.GetDomain(),
		Contract:          req.GetContract(),
		ProposedContract:  req.GetProposedContract(),
		RevisionRequest:   req.GetRevisionRequest(),
	}
	propose.Normalize(&pr)
	if errs := propose.Validate(pr); len(errs) > 0 {
		return &awarenesspb.ProposeResponse{
			Status:           awarenesspb.ProposeStatus_PROPOSE_STATUS_REJECTED,
			ValidationErrors: errs,
			Note:             "contract-first validation failed; nothing written",
			GeneratedInMs:    time.Since(start).Milliseconds(),
		}, nil
	}

	cand, err := propose.RenderCandidate(pr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "render candidate: %v", err)
	}

	// The RelPath is package-produced (candidates/proposals/…), but verify the
	// resolved destination stays under awarenessDir before writing.
	cleanRoot := filepath.Clean(root)
	dest := filepath.Join(cleanRoot, filepath.FromSlash(cand.RelPath))
	if dest != cleanRoot && !strings.HasPrefix(dest, cleanRoot+string(os.PathSeparator)) {
		return nil, status.Error(codes.Internal, "candidate path escaped the awareness directory")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, status.Errorf(codes.Internal, "create candidates dir: %v", err)
	}
	if err := os.WriteFile(dest, cand.Content, 0o644); err != nil {
		return nil, status.Errorf(codes.Internal, "write candidate: %v", err)
	}

	return &awarenesspb.ProposeResponse{
		Status:        awarenesspb.ProposeStatus_PROPOSE_STATUS_ACCEPTED,
		CandidatePath: cand.RelPath,
		NodeIds:       cand.NodeIDs,
		Note:          "queued for human review; not a live graph node until promoted into the corpus",
		GeneratedInMs: time.Since(start).Milliseconds(),
	}, nil
}
