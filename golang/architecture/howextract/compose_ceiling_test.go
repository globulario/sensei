// SPDX-License-Identifier: AGPL-3.0-only

package howextract

import (
	"context"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/extractbudget"
)

// Recording that a run stopped is not the same as stopping it. The between-stage
// checks mark the outcome and fall through, so composition used to capture
// evidence for every fact already gathered and walk the tree for a snapshot
// manifest — work that scales with the partial result and outlasts the ceiling
// that produced it.
func TestCompositionStopsCapturingAtTheCeiling(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	facts := []architecture.Fact{{
		ID: "fact.one", Kind: "state", Subject: "pkg.Thing", Predicate: "declares_state",
		Object: "x", Extractor: "go_semantic_extractor", Confidence: 0.5,
		Evidence: architecture.Evidence{SourceFile: "a.go", LineStart: 1, LineEnd: 1},
	}}

	doc, err := composeReceiptsAndCoverage(ctx, root, facts, "example.com/eval", Options{
		CapturedAt: "2026-08-20T00:00:00Z",
		Repository: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "example.com/eval",
			Revision:          strings.Repeat("a", 40),
			RevisionStatus:    "resolved",
			GraphDigestSHA256: strings.Repeat("b", 64),
			GraphDigestStatus: "resolved",
		},
	}, nil, nil, nil, runMetrics{consumption: extractbudget.Consumption{Observations: len(facts)}})
	if err != nil {
		t.Fatalf("a stopped composition must still produce a document: %v", err)
	}

	var captureStop, manifestStop bool
	for _, lim := range doc.Limitations {
		if strings.Contains(lim.Reason, "evidence capture stopped") && lim.Blocking {
			captureStop = true
		}
		if strings.Contains(lim.Reason, "source snapshot manifest was not built") && lim.Blocking {
			manifestStop = true
		}
	}
	if !captureStop {
		t.Fatalf("evidence capture did not report stopping: %+v", doc.Limitations)
	}
	if !manifestStop {
		t.Fatalf("the snapshot manifest did not report being skipped: %+v", doc.Limitations)
	}
}
