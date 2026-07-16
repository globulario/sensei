// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/prereview"
)

// SARIF must contain only genuinely file-anchored findings: a reviewer-attention
// item with no related files produces no SARIF result (SARIF results without a
// physical location are misleading in code-review tools).
func TestPreReviewSARIFOnlyFileAnchoredFindings(t *testing.T) {
	r := prereview.PreReviewReport{
		ReviewerAttention: []prereview.ReviewerAttentionItem{
			{ID: "anchored.finding", Category: prereview.AttentionArchitectQuestion, Question: "q1", RelatedFiles: []string{"gin.go"}},
			{ID: "floating.finding", Category: prereview.AttentionArchitectQuestion, Question: "q2"},
		},
	}
	out, err := preReviewSARIF(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "anchored.finding") {
		t.Fatalf("file-anchored finding missing from SARIF: %s", s)
	}
	if strings.Contains(s, "floating.finding") {
		t.Fatalf("non-file-anchored finding leaked into SARIF: %s", s)
	}
	if !strings.Contains(s, "artifactLocation") {
		t.Fatalf("SARIF result carries no physical location: %s", s)
	}
}

// The local graph approximation must not impersonate server-only surfaces: the
// gRPC-only briefing and impact-graph traversal are reported unavailable, never
// fabricated from local YAML.
func TestLocalGraphSourceReportsServerSurfacesUnavailable(t *testing.T) {
	dir, _ := mkPreReviewRepo(t)
	gc, err := (localGraphSource{repoRoot: dir}).CollectArchitecturalContext(context.Background(), prereview.GraphRequest{ChangedPaths: []string{"gin.go"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"impact_graph_traversal", "briefing"} {
		found := false
		for _, u := range gc.Unavailable {
			if u == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("gRPC-only surface %q not reported unavailable: %v", want, gc.Unavailable)
		}
	}
}
