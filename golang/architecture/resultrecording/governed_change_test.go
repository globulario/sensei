// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"context"
	"testing"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/closure"
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
	// The change is authorized BEFORE it is observed: the plan declares the source
	// mutation and the governed invariant mutation, and the three generated
	// artifacts (the generated-artifact contract) as required generated artifacts.
	repo, taskDir, resultRev := seedTask(t, func(tt *testing.T, r string) {
		rwrite(tt, r, "src/model.go", "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
		rwrite(tt, r, "docs/awareness/invariants.yaml", rInvariantsChanged)
		regenerateArtifacts(tt, r) // the governed change alters the graph → regenerate derived artifacts
	}, []string{"src/model.go"}, closure.DirectionNotApplicable,
		[]string{"src/model.go", "docs/awareness/invariants.yaml"},
		// An invariant change alters the graph-derived generated artifacts (the
		// embedded graph and its result manifest); proof_obligations is
		// authority-derived and unchanged, so it is not a required rebuild here.
		[]string{"golang/server/embeddata/awareness.nt", "golang/server/embeddata/awareness.result-manifest.tsv"})

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

	const wantInvID = "https://globular.io/awareness#invariant/test.publish_mutates_state"
	// The candidate's real impact report shows exactly the one invariant changed.
	assertOnlyInvariantChanged(t, c.BuildResult.GovernedKnowledgeImpactReport, wantInvID)

	res, err := RecordTransition(context.Background(), RecordRequest{TaskDirectory: taskDir, Candidate: c})
	if err != nil {
		t.Fatalf("record governed change: %v", err)
	}
	if res.Disposition != DispositionRecorded {
		t.Fatalf("disposition = %s", res.Disposition)
	}

	// Reload entirely from the ledger and prove the exact change survived storage.
	rt, err := LoadRecordedTransition(taskDir, c.Receipt.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("recorded transition invalid: %v", err)
	}
	assertOnlyInvariantChanged(t, rt.ImpactReport, wantInvID)

	// The receipt's ten impacts equal the stored full report (set-wise).
	if closureprotocol.MustSemanticDigest(sortedImpacts(rt.Receipt.GovernedKnowledgeImpacts)) != closureprotocol.MustSemanticDigest(sortedImpacts(rt.ImpactReport.Impacts)) {
		t.Fatal("receipt impacts differ from stored report")
	}

	// All ten stage artifacts survived byte-identically.
	want := map[closureprotocol.ResultPipelineStage][]byte{}
	for _, a := range c.BuildResult.StageArtifacts {
		want[a.Stage] = a.Bytes
	}
	if len(rt.Stages) != 10 {
		t.Fatalf("reloaded %d stages", len(rt.Stages))
	}
	for _, s := range rt.Stages {
		if string(s.Artifact.Bytes) != string(want[s.Stage]) {
			t.Fatalf("stage %s bytes did not survive byte-identically", s.Stage)
		}
	}
}

// assertOnlyInvariantChanged requires the invariants category to change with
// exactly wantID and every other category to be unchanged.
func assertOnlyInvariantChanged(t *testing.T, rep governedimpact.Report, wantID string) {
	t.Helper()
	for _, im := range rep.Impacts {
		changed := closureprotocol.GovernedKnowledgeImpactChanged(im)
		if im.Category == "invariants" {
			if !changed || len(im.ChangedRecordIDs) != 1 || im.ChangedRecordIDs[0] != wantID {
				t.Fatalf("invariants changed-set = %v, want exactly [%s]", im.ChangedRecordIDs, wantID)
			}
			continue
		}
		if changed {
			t.Fatalf("unrelated category %q changed: %v", im.Category, im.ChangedRecordIDs)
		}
	}
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
