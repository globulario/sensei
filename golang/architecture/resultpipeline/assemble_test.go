// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/maintenance"
	"github.com/globulario/sensei/golang/architecture/plane"
)

func hx(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

// syntheticStages assembles the ten stage artifacts from minimal synthetic stage
// outputs, returning them with the result binding they are bound to.
func syntheticStages(t *testing.T) (closureprotocol.ResultBinding, string, []PipelineArtifact) {
	t.Helper()
	graphDigest := hx("graph")
	rb := closureprotocol.ResultBinding{
		BaseRevision:           "0123456789abcdef",
		PatchDigestSHA256:      hx("patch"),
		ResultTreeDigestSHA256: hx("tree"),
		GraphDigestSHA256:      graphDigest,
	}
	if err := closureprotocol.ValidateResultBinding(rb); err != nil {
		t.Fatalf("synthetic result binding invalid: %v", err)
	}
	rbDigest, err := closureprotocol.ResultBindingDigest(rb)
	if err != nil {
		t.Fatal(err)
	}
	cg := compiledGraph{
		compilation: graphbuild.Compilation{SourceManifest: graphbuild.SourceManifest{SchemaVersion: "graphbuild.source-manifest/v1"}},
		artifact:    graphbuild.Artifact{GraphSemanticDigestSHA256: graphDigest, NTriples: []byte("<s> <p> <o> .\n")},
	}
	stages, err := assembleStages(rbDigest, cg,
		InferredClaimsBundle{},
		maintenance.Result{},
		plane.Report{},
		closure.Report{},
		ArchitectQuestionsBundle{AllBlockersAccountedFor: true},
		ProofRequirementDocument{SchemaVersion: "1"},
		generatedartifact.VerificationManifest{SchemaVersion: "1"},
	)
	if err != nil {
		t.Fatalf("assembleStages: %v", err)
	}
	manifest, err := artifactManifest(rbDigest, stages)
	if err != nil {
		t.Fatalf("artifactManifest: %v", err)
	}
	return rb, rbDigest, append(stages, manifest)
}

func syntheticReceipt(rb closureprotocol.ResultBinding, rbDigest string, stages []PipelineArtifact) closureprotocol.ResultTransitionReceipt {
	receipts, derivations := []closureprotocol.ArtifactReceipt{}, []closureprotocol.ArtifactDerivation{}
	for _, a := range stages {
		receipts = append(receipts, a.Receipt)
		derivations = append(derivations, a.Derivation)
	}
	return closureprotocol.ResultTransitionReceipt{
		Task:                              closureprotocol.TaskBinding{ID: "task.1", SessionID: "session.1"},
		BaseBindingDigestSHA256:           hx("base"),
		ActorBindingDigestSHA256:          hx("actor"),
		AuthorityResolutionDigestSHA256:   hx("authority"),
		AdmissionDecisionDigestSHA256:     hx("decision"),
		CapabilityConsumptionDigestSHA256: hx("consumption"),
		ObservedChangeSetDigestSHA256:     hx("observed"),
		ScopeVerificationDigestSHA256:     hx("scope"),
		ResultBinding:                     rb,
		ResultBindingDigestSHA256:         rbDigest,
		OperationalArtifactReceipts:       receipts,
		Derivations:                       derivations,
		PipelinePolicyID:                  DefaultPipelinePolicyID,
		RecordedAt:                        "2026-07-16T00:00:00Z",
		Status:                            "valid",
	}
}

// Tests 1-4, 30, 31: the ten assembled stages form a contract-valid transition —
// every mandatory stage present exactly once, no self-reference, relative paths,
// and the frozen validator accepts them.
func TestAssembledStagesAreContractValid(t *testing.T) {
	rb, rbDigest, stages := syntheticStages(t)
	if len(stages) != len(closureprotocol.ResultPipelineStages) {
		t.Fatalf("got %d stages, want %d", len(stages), len(closureprotocol.ResultPipelineStages))
	}
	seen := map[closureprotocol.ResultPipelineStage]int{}
	for _, a := range stages {
		seen[a.Stage]++
		if a.Receipt.ResultBindingDigestSHA256 != rbDigest {
			t.Fatalf("stage %s not bound to the current result", a.Stage)
		}
		if strings.HasPrefix(a.LogicalPath, "/") || strings.Contains(a.LogicalPath, "..") {
			t.Fatalf("stage %s logical path is not relative: %s", a.Stage, a.LogicalPath)
		}
	}
	for _, st := range closureprotocol.ResultPipelineStages {
		if seen[st] != 1 {
			t.Fatalf("stage %s appears %d times, want exactly 1", st, seen[st])
		}
	}
	if err := closureprotocol.ValidateResultTransitionReceipt(syntheticReceipt(rb, rbDigest, stages)); err != nil {
		t.Fatalf("assembled transition is not contract-valid: %v", err)
	}
}

// Test 4 / omission law: removing any mandatory stage makes the transition
// invalid — a missing stage is never silently tolerated.
func TestRemovingAnyStageFailsValidation(t *testing.T) {
	rb, rbDigest, stages := syntheticStages(t)
	for i, dropped := range stages {
		subset := make([]PipelineArtifact, 0, len(stages)-1)
		subset = append(subset, stages[:i]...)
		subset = append(subset, stages[i+1:]...)
		if err := closureprotocol.ValidateResultTransitionReceipt(syntheticReceipt(rb, rbDigest, subset)); err == nil {
			t.Fatalf("dropping stage %s still validated", dropped.Stage)
		}
	}
}

// Test 30: the artifact manifest lists the first nine stages but never references
// itself (its own receipt digest is not among the manifest's stage entries).
func TestArtifactManifestHasNoSelfReference(t *testing.T) {
	_, _, stages := syntheticStages(t)
	manifest := stages[len(stages)-1]
	if manifest.Stage != closureprotocol.StageArtifactManifest {
		t.Fatalf("last stage is %s, want artifact_manifest", manifest.Stage)
	}
	for _, a := range stages[:len(stages)-1] {
		if a.Stage == closureprotocol.StageArtifactManifest {
			t.Fatal("artifact_manifest appears among the first nine stages")
		}
	}
	// The manifest's own receipt digest must not appear in its derivation inputs.
	for _, in := range manifest.Derivation.InputArtifactReceiptDigestsSHA256 {
		if in == manifest.Receipt.ReceiptDigestSHA256 {
			t.Fatal("artifact_manifest derivation references its own receipt")
		}
	}
}

// Every stage binds the same result graph and tree (Tests 15/16 at the assembly
// layer): all receipts carry the one result-binding digest.
func TestAllStagesBindSameResult(t *testing.T) {
	_, rbDigest, stages := syntheticStages(t)
	for _, a := range stages {
		for _, b := range a.Derivation.InputBindingDigestsSHA256 {
			if b != rbDigest {
				t.Fatalf("stage %s input binding %s is not the current result", a.Stage, b)
			}
		}
	}
}
