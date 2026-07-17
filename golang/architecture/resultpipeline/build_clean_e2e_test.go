// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// e2eSeedClean seeds a genuinely clean, low-risk, committed-revision task whose
// real closure honestly reaches a non-uncertifiable verdict, so the whole
// pipeline (with the ValidateBuildResult gate) accepts it. It injects no
// synthetic closure report. It returns the repo, task dir, and committed result
// revision to drive ResultModeRevision.
func e2eSeedClean(t *testing.T) (repo, taskDir, resultRev string) {
	t.Helper()
	repo = t.TempDir()
	e2eGit(t, repo, "init", "-q")
	e2eWrite(t, repo, "docs/awareness/invariants.yaml", e2eInvariants)
	e2eWrite(t, repo, "src/model.go", "package src\n\nfunc Publish() {}\n")

	snapshot, supplementalBytes, err := graphbuild.SnapshotFromBuildInputs(
		GraphInputPolicyV1, repo, e2eDomain,
		[]graphbuild.SourceRoot{{FilesystemPath: filepath.Join(repo, "docs", "awareness"), SkipNestedGenerated: true}},
		nil,
	)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	baseInputs, err := resolveGraphInputs(repo, snapshot, supplementalBytes)
	if err != nil {
		t.Fatalf("resolve base inputs: %v", err)
	}
	baseCG, err := compileGovernedGraph(context.Background(), repo, baseInputs)
	if err != nil {
		t.Fatalf("compile base graph: %v", err)
	}
	baseGraphDigest := baseCG.artifact.GraphSemanticDigestSHA256

	profile, err := generatedartifact.ProfileForDomain(e2eDomain)
	if err != nil {
		t.Fatal(err)
	}
	srcManDigest, err := closureprotocol.SemanticDigest(baseCG.compilation.SourceManifest)
	if err != nil {
		t.Fatal(err)
	}
	gen, err := generatedartifact.Generate(context.Background(), generatedartifact.Context{
		RepositoryRoot: repo, RepositoryDomain: e2eDomain,
		GraphInputPolicyID: snapshot.PolicyID, GraphInputSnapshotDigestSHA256: snapshot.SnapshotDigestSHA256,
		SourceManifestDigestSHA256: srcManDigest, SupplementalGraphs: snapshot.SupplementalGraphs,
		GraphArtifact: baseCG.artifact,
	}, profile)
	if err != nil {
		t.Fatalf("generate artifacts: %v", err)
	}
	for _, o := range gen {
		e2eWrite(t, repo, o.Path, string(o.Bytes))
	}

	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "base")
	baseRev := e2eGit(t, repo, "rev-parse", "HEAD")

	baseTree, err := binding.ResolveTreeIdentity(context.Background(), repo, baseRev)
	if err != nil {
		t.Fatal(err)
	}

	// A trivial, low-risk, non-behavioral change, committed as the result revision.
	e2eWrite(t, repo, "src/model.go", "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "result")
	resultRev = e2eGit(t, repo, "rev-parse", "HEAD")

	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: e2eDomain, Revision: baseRev, RevisionStatus: "resolved", TreeDigestSHA256: baseTree.DigestSHA256},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: baseGraphDigest, DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.clean", SessionID: "session.clean"},
		Policies: closureprotocol.PolicyBinding{
			Admission: "admission.strict.v2", Certification: "certification.architectural_closure.v1",
			Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
			Ledger: "ledger.task.v1", Canonicalization: "canon.v1",
		},
	}

	taskDir = t.TempDir()
	validator := func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(validator))
	head := func() string {
		h, err := admission.TaskLedgerHead(taskDir)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	// The recorded closure request binds the RESULT revision (low-risk, read,
	// direction not-applicable) so the evidence dimension can verify the revision.
	resultBinding := binding.ToClaimDocumentBinding(base)
	resultBinding.Revision = resultRev
	resultBinding.RevisionStatus = "resolved"
	closureReq := closure.Request{
		SchemaVersion: "1", TaskID: base.Task.ID,
		Binding: resultBinding,
		Scope: closure.Scope{
			Domain: e2eDomain, TaskClass: "repository_repair", RiskClass: closure.RiskLowRisk,
			AccessMode: closure.AccessRead, DirectionRequirement: closure.DirectionNotApplicable, Files: []string{"src/model.go"},
		},
	}
	closureBytes, err := closure.MarshalCanonicalRequestYAML(closureReq)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreArtifactBytes(closureBytes, "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	snapshotBytes, err := yaml.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef, err := store.StoreArtifactBytes(snapshotBytes, "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string]closureprotocol.LedgerPayloadRef{"closure_request": ref, "graph_input_snapshot": snapshotRef}
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: base.Task.ID, SessionID: base.Task.SessionID, ExpectedHeadDigestSHA256: "",
		EventType: closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{
			SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventTaskPrepared,
			TaskID: base.Task.ID, SessionID: base.Task.SessionID, BaseBinding: &base,
			Artifacts: artifacts,
		},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: e0,
	}); err != nil {
		t.Fatalf("task_prepared: %v", err)
	}

	actor := e2eActor()
	actorDigest := closureprotocol.MustSemanticDigest(actor)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		t.Fatal(err)
	}
	resolution := e2eResolution(actorDigest, baseDigest)
	authorityDigest := resolution.AuthorityResolutionDigestSHA256
	if _, err := admission.RecordAuthorityResolved(store, head(), base.Task, resolution, actor, closureprotocol.ChangePlan{PlanID: "plan.clean"}, base, nil, e0); err != nil {
		t.Fatalf("authority: %v", err)
	}

	decision := closureprotocol.AdmissionDecision{
		DecisionID: "decision.clean", RequestDigestSHA256: "req0", PolicyID: "admission.strict.v2",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{{OperationID: "op.1", Verdict: "admitted"}},
		CapabilityID:      "cap.clean", CompletionPolicyID: "completion.architectural_closure.v1",
	}
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	if _, err := admission.RecordAdmissionDecided(store, head(), decision, base.Task, e0); err != nil {
		t.Fatalf("decision: %v", err)
	}

	consumption := closureprotocol.CapabilityConsumption{
		CapabilityID: "cap.clean", Task: base.Task, ConsumerActor: actor,
		ConsumedOperationIDs: []string{"op.1"}, ConsumedAt: "2026-07-16T12:00:00Z",
		DecisionDigestSHA256: decisionDigest, OneUseStatus: closureprotocol.ReceiptValid,
	}
	if _, err := admission.RecordAdmissionConsumed(store, head(), consumption, e0); err != nil {
		t.Fatalf("consumption: %v", err)
	}

	// Observe the committed result revision (not the worktree).
	observed, err := resulttransition.ObserveChange(repo, baseRev, resultRev, actorDigest, authorityDigest)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if _, err := admission.RecordChangeObserved(store, head(), base.Task, observed, e0); err != nil {
		t.Fatalf("observed: %v", err)
	}
	observedDigest, err := admission.ObservedChangeSetDigest(observed)
	if err != nil {
		t.Fatal(err)
	}

	scope := admission.ScopeVerification{
		CapabilityID: "cap.clean", DecisionDigestSHA256: decisionDigest,
		ActorBindingDigestSHA256: actorDigest, AuthorityResolutionDigestSHA256: authorityDigest,
		BaseTreeDigestSHA256: observed.BaseTreeDigestSHA256, ResultTreeDigestSHA256: observed.ResultTreeDigestSHA256,
		ObservedChangeSetDigestSHA256: observedDigest, VerifiedOperationIDs: []string{"op.1"},
		Status: closureprotocol.ReceiptValid, VerifiedAt: "2026-07-16T12:00:00Z",
	}
	sd, err := admission.ScopeVerificationDigest(scope)
	if err != nil {
		t.Fatal(err)
	}
	scope.ScopeVerificationDigestSHA256 = sd
	if _, err := admission.RecordScopeVerified(store, head(), base.Task, scope, e0); err != nil {
		t.Fatalf("scope: %v", err)
	}
	return repo, taskDir, resultRev
}

// TestBuildCleanClosureAccepted proves the whole pipeline, including the
// ValidateBuildResult gate, accepts a genuinely clean result: extraction complete,
// proving ready, and the gate returns nil — with the repository and ledger
// unchanged by the build.
func TestBuildCleanClosureAccepted(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	statusBefore := e2eGit(t, repo, "status", "--porcelain")
	headBefore := e2eGit(t, repo, "rev-parse", "HEAD")
	ledgerBefore, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Build(context.Background(), BuildRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir,
		ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain,
	})
	if err != nil {
		t.Fatalf("Build clean: %v", err)
	}
	if res.ClosureReport.Verdict == closure.VerdictUncertifiable {
		t.Fatalf("clean fixture is not clean: verdict %s", res.ClosureReport.Verdict)
	}
	if res.ProofRequirements.ExtractionCompleteness != ExtractionComplete {
		t.Fatalf("completeness = %s, want complete", res.ProofRequirements.ExtractionCompleteness)
	}
	if res.ProofRequirements.ProvingDisposition != ProvingReady {
		t.Fatalf("disposition = %s, want ready", res.ProofRequirements.ProvingDisposition)
	}
	// The gate accepts the returned result and is deterministic.
	if err := ValidateBuildResult(res); err != nil {
		t.Fatalf("gate rejected an accepted build: %v", err)
	}

	// No repository or ledger mutation from building.
	if got := e2eGit(t, repo, "status", "--porcelain"); got != statusBefore {
		t.Fatalf("worktree changed: %q -> %q", statusBefore, got)
	}
	if got := e2eGit(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("HEAD moved: %s -> %s", headBefore, got)
	}
	ledgerAfter, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	if ledgerAfter != ledgerBefore {
		t.Fatalf("ledger head moved: %s -> %s", ledgerBefore, ledgerAfter)
	}
}

// TestBuildRefusesUncertifiable proves the prior uncertifiable fixture now fails
// closed at the gate, with the stable uncertifiable code.
func TestBuildRefusesUncertifiable(t *testing.T) {
	repo, taskDir := e2eSeed(t)
	_, err := Build(context.Background(), BuildRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir,
		ResultMode: resulttransition.ResultModeWorktree, RepositoryDomain: e2eDomain,
	})
	if err == nil {
		t.Fatal("expected Build to refuse an uncertifiable result")
	}
	if !strings.Contains(err.Error(), CodeProofExtractionUncertifiable) {
		t.Fatalf("want %s, got %v", CodeProofExtractionUncertifiable, err)
	}
}
