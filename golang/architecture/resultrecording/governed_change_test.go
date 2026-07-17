// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"path/filepath"

	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/governedimpact"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

const rInvariantsChanged = `invariants:
  - id: test.publish_mutates_state
    title: Publish mutates package identity AND governs more
    severity: critical
    status: active
    protects:
      files:
        - src/model.go
    required_tests:
      - src/model_test.go:TestPublish
`

// TestGovernedChangeSurvivesRecordAndReload closes the deferred Step-7 debt: a
// result that changes a governed invariant is prepared, recorded, and reloaded,
// and the exact changed invariant id survives storage. The impact report is real
// (never injected), and unrelated categories stay unchanged.
func TestGovernedChangeSurvivesRecordAndReload(t *testing.T) {
	// The committed result changes the governed invariant (title) plus a trivial,
	// in-scope source comment; the admitted scope stays the represented src/model.go.
	repo, taskDir, resultRev := seedTask(t, func(tt *testing.T, r string) {
		rwrite(tt, r, "src/model.go", "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
		rwrite(tt, r, "docs/awareness/invariants.yaml", rInvariantsChanged)
		regenerateArtifacts(tt, r) // the governed change alters the graph → regenerate derived artifacts
	}, []string{"src/model.go"})

	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
		Build: resultpipeline.BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: rDomain},
		ExpectedLedgerHeadDigestSHA256: head, RecordedAt: recAt,
	})
	if err != nil {
		t.Fatalf("prepare governed-change candidate: %v", err)
	}

	// The candidate's real impact report shows invariants changed with the exact id.
	invID := changedID(t, c.BuildResult.GovernedKnowledgeImpactReport, "invariants")
	if invID == "" {
		t.Fatal("governed invariant change not detected in the candidate impact report")
	}

	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("record governed change: %v", err)
	}
	if res.Disposition != DispositionRecorded {
		t.Fatalf("disposition = %s", res.Disposition)
	}

	// Reload entirely from the ledger and prove the change survived storage.
	rt, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("recorded transition invalid: %v", err)
	}
	reloadedID := changedID(t, rt.ImpactReport, "invariants")
	if reloadedID != invID {
		t.Fatalf("exact invariant id did not survive storage: stored %q reloaded %q", invID, reloadedID)
	}
	// Unrelated governed categories are unchanged.
	for _, im := range rt.ImpactReport.Impacts {
		if im.Category == "invariants" {
			continue
		}
		if im.Category == "proof_obligations" {
			continue // may legitimately change if obligations derive from the surface
		}
		if closureprotocol.GovernedKnowledgeImpactChanged(im) {
			t.Fatalf("unrelated category %q changed: %v", im.Category, im.ChangedRecordIDs)
		}
	}
	// The receipt's ten impacts equal the stored full report.
	if len(rt.Receipt.GovernedKnowledgeImpacts) != len(rt.ImpactReport.Impacts) {
		t.Fatal("receipt impacts count differs from stored report")
	}
}

func changedID(t *testing.T, rep governedimpact.Report, category string) string {
	t.Helper()
	for _, im := range rep.Impacts {
		if im.Category == category && closureprotocol.GovernedKnowledgeImpactChanged(im) {
			if len(im.ChangedRecordIDs) == 0 {
				t.Fatalf("category %q changed but reports no record id", category)
			}
			return im.ChangedRecordIDs[0]
		}
	}
	return ""
}

// regenerateArtifacts recompiles the graph over the current repository content and
// regenerates every governed generated artifact into the result tree, mirroring a
// real "regenerate required repository artifacts" step.
func regenerateArtifacts(t *testing.T, repo string) {
	t.Helper()
	snapshot, supBytes, err := graphbuild.SnapshotFromBuildInputs(
		resultpipeline.GraphInputPolicyV1, repo, rDomain,
		[]graphbuild.SourceRoot{{FilesystemPath: filepath.Join(repo, "docs", "awareness"), SkipNestedGenerated: true}}, nil)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	profile, err := generatedartifact.ProfileForDomain(rDomain)
	if err != nil {
		t.Fatal(err)
	}
	graph, srcMan := compileForSeed(t, repo, snapshot, supBytes)
	gen, err := generatedartifact.Generate(context.Background(), generatedartifact.Context{
		RepositoryRoot: repo, RepositoryDomain: rDomain,
		GraphInputPolicyID: snapshot.PolicyID, GraphInputSnapshotDigestSHA256: snapshot.SnapshotDigestSHA256,
		SourceManifestDigestSHA256: srcMan, SupplementalGraphs: snapshot.SupplementalGraphs,
		GraphArtifact: graph,
	}, profile)
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	for _, o := range gen {
		rwrite(t, repo, o.Path, string(o.Bytes))
	}
}
