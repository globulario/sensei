// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
)

// TestBriefing_ImplementationPattern_SmokeExampleOutput prints the
// prose body for a representative gRPC-client task so a reviewer can see
// exactly what an agent receives. Not a correctness check — run with
//
//	go test -run TestBriefing_ImplementationPattern_SmokeExampleOutput -v
func TestBriefing_ImplementationPattern_SmokeExampleOutput(t *testing.T) {
	s := newPatternTestServer(t)
	resp, err := s.Briefing(context.Background(), &awarenesspb.BriefingRequest{
		File: "golang/awareness_graph/awareness_graph_client/awareness_graph_client.go",
		Task: "create a new Go client for a Globular gRPC service",
	})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	t.Logf("STATUS: %s", resp.GetStatus())
	t.Logf("PROSE:\n%s", resp.GetProse())
	t.Logf("STRUCTURED implementation_patterns: %+v", resp.GetImplementationPatterns())
}
