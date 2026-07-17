// SPDX-License-Identifier: Apache-2.0

package resultrecording

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/generatedartifact"
	"github.com/globulario/sensei/golang/architecture/graphbuild"
	"github.com/globulario/sensei/golang/architecture/ledger"
	"github.com/globulario/sensei/golang/architecture/resultpipeline"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

var e0 = time.Unix(0, 0).UTC()

const rDomain = "github.com/globulario/sensei"

const rInvariants = `invariants:
  - id: test.publish_mutates_state
    title: Publish mutates package identity
    severity: critical
    status: active
    protects:
      files:
        - src/model.go
    required_tests:
      - src/model_test.go:TestPublish
`

func rgit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func rwrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ractor() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{PrincipalID: "actor.test", ActorKind: closureprotocol.ActorAgent, Roles: []string{"role.repository_repair_agent"}, Issuer: "sensei.local"}
}

func rresolution(t *testing.T, actorDigest, baseDigest string) closureprotocol.AuthorityResolution {
	t.Helper()
	r := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:         actorDigest,
		BaseBindingDigestSHA256:          baseDigest,
		ClosureAssessmentDigestSHA256:    "closure0",
		OperationSetDigestSHA256:         "ops0",
		AuthorityPolicyGraphDigestSHA256: "policygraph0",
		PolicyID:                         "admission.strict.v2",
		EvaluatedAt:                      "2026-07-16T12:00:00Z",
		Status:                           closureprotocol.ReceiptValid,
		OperationResults: []closureprotocol.AuthorityResolutionOperation{
			{OperationID: "op.1", Status: closureprotocol.ReceiptValid, SelectedMechanism: closureprotocol.MechanismRepositoryEdit},
		},
	}
	d, err := closureprotocol.AuthorityResolutionDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.AuthorityResolutionDigestSHA256 = d
	return r
}

// seedCleanTask seeds a real admitted, scope-verified, committed-revision task
// whose closure honestly closes, and returns the task dir + committed result
// revision. resultSrc is the committed result content of src/model.go.
func seedCleanTask(t *testing.T, resultSrc string) (repo, taskDir, resultRev string) {
	t.Helper()
	repo = t.TempDir()
	rgit(t, repo, "init", "-q")
	rwrite(t, repo, "docs/awareness/invariants.yaml", rInvariants)
	rwrite(t, repo, "src/model.go", "package src\n\nfunc Publish() {}\n")

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
	// The generated artifacts must exist in the committed base; compile the base
	// graph to feed the generator exactly as the pipeline expects.
	baseGraph, srcManDigest := compileForSeed(t, repo, snapshot, supBytes)
	gen, err := generatedartifact.Generate(context.Background(), generatedartifact.Context{
		RepositoryRoot: repo, RepositoryDomain: rDomain,
		GraphInputPolicyID: snapshot.PolicyID, GraphInputSnapshotDigestSHA256: snapshot.SnapshotDigestSHA256,
		SourceManifestDigestSHA256: srcManDigest, SupplementalGraphs: snapshot.SupplementalGraphs,
		GraphArtifact: baseGraph,
	}, profile)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, o := range gen {
		rwrite(t, repo, o.Path, string(o.Bytes))
	}
	rgit(t, repo, "add", "-A")
	rgit(t, repo, "commit", "-q", "-m", "base")
	baseRev := rgit(t, repo, "rev-parse", "HEAD")
	baseTree, err := binding.ResolveTreeIdentity(context.Background(), repo, baseRev)
	if err != nil {
		t.Fatal(err)
	}

	rwrite(t, repo, "src/model.go", resultSrc)
	rgit(t, repo, "add", "-A")
	rgit(t, repo, "commit", "-q", "-m", "result")
	resultRev = rgit(t, repo, "rev-parse", "HEAD")

	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: rDomain, Revision: baseRev, RevisionStatus: "resolved", TreeDigestSHA256: baseTree.DigestSHA256},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: baseGraph.GraphSemanticDigestSHA256, DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.rec", SessionID: "session.rec"},
		Policies: closureprotocol.PolicyBinding{
			Admission: "admission.strict.v2", Certification: "certification.architectural_closure.v1",
			Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
			Ledger: "ledger.task.v1", Canonicalization: "canon.v1",
		},
	}

	taskDir = t.TempDir()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	head := func() string {
		h, err := admission.TaskLedgerHead(taskDir)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	resultBinding := binding.ToClaimDocumentBinding(base)
	resultBinding.Revision = resultRev
	resultBinding.RevisionStatus = "resolved"
	closureReq := closure.Request{
		SchemaVersion: "1", TaskID: base.Task.ID, Binding: resultBinding,
		Scope: closure.Scope{Domain: rDomain, TaskClass: "repository_repair", RiskClass: closure.RiskLowRisk,
			AccessMode: closure.AccessRead, DirectionRequirement: closure.DirectionNotApplicable, Files: []string{"src/model.go"}},
	}
	closureBytes, err := closure.MarshalCanonicalRequestYAML(closureReq)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreArtifactBytes(closureBytes, "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	snapBytes, _ := yaml.Marshal(snapshot)
	snapRef, err := store.StoreArtifactBytes(snapBytes, "application/yaml")
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string]closureprotocol.LedgerPayloadRef{"closure_request": ref, "graph_input_snapshot": snapRef}
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: base.Task.ID, SessionID: base.Task.SessionID, EventType: closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventTaskPrepared,
			TaskID: base.Task.ID, SessionID: base.Task.SessionID, BaseBinding: &base, Artifacts: artifacts},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: e0,
	}); err != nil {
		t.Fatalf("task_prepared: %v", err)
	}

	actor := ractor()
	actorDigest := closureprotocol.MustSemanticDigest(actor)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		t.Fatal(err)
	}
	resolution := rresolution(t, actorDigest, baseDigest)
	authorityDigest := resolution.AuthorityResolutionDigestSHA256
	if _, err := admission.RecordAuthorityResolved(store, head(), base.Task, resolution, actor, closureprotocol.ChangePlan{PlanID: "plan.rec"}, base, nil, e0); err != nil {
		t.Fatalf("authority: %v", err)
	}
	decision := closureprotocol.AdmissionDecision{DecisionID: "decision.rec", RequestDigestSHA256: "req0", PolicyID: "admission.strict.v2",
		OperationVerdicts: []closureprotocol.OperationAdmissionVerdict{{OperationID: "op.1", Verdict: "admitted"}},
		CapabilityID:      "cap.rec", CompletionPolicyID: "completion.architectural_closure.v1"}
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	if _, err := admission.RecordAdmissionDecided(store, head(), decision, base.Task, e0); err != nil {
		t.Fatalf("decision: %v", err)
	}
	consumption := closureprotocol.CapabilityConsumption{CapabilityID: "cap.rec", Task: base.Task, ConsumerActor: actor,
		ConsumedOperationIDs: []string{"op.1"}, ConsumedAt: "2026-07-16T12:00:00Z", DecisionDigestSHA256: decisionDigest, OneUseStatus: closureprotocol.ReceiptValid}
	if _, err := admission.RecordAdmissionConsumed(store, head(), consumption, e0); err != nil {
		t.Fatalf("consume: %v", err)
	}
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
	scope := admission.ScopeVerification{CapabilityID: "cap.rec", DecisionDigestSHA256: decisionDigest,
		ActorBindingDigestSHA256: actorDigest, AuthorityResolutionDigestSHA256: authorityDigest,
		BaseTreeDigestSHA256: observed.BaseTreeDigestSHA256, ResultTreeDigestSHA256: observed.ResultTreeDigestSHA256,
		ObservedChangeSetDigestSHA256: observedDigest, VerifiedOperationIDs: []string{"op.1"},
		Status: closureprotocol.ReceiptValid, VerifiedAt: "2026-07-16T12:00:00Z"}
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

// compileForSeed mirrors resultpipeline.compileGovernedGraph exactly (ClosureStrict
// policy + Finalize) so the base graph digest matches what the pipeline recomputes.
func compileForSeed(t *testing.T, repo string, snapshot graphbuild.GraphInputSnapshot, supBytes map[string][]byte) (graphbuild.Artifact, string) {
	t.Helper()
	root, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	var sources []graphbuild.SourceRoot
	for _, r := range snapshot.SourceRoots {
		dd := r.DefaultDomain
		if dd == "" {
			dd = snapshot.RepositoryDomain
		}
		sources = append(sources, graphbuild.SourceRoot{
			FilesystemPath:      filepath.Join(root, filepath.FromSlash(r.LogicalPath)),
			IdentityRoot:        root,
			StripPathPrefixes:   []string{root},
			RepositoryDomain:    snapshot.RepositoryDomain,
			DefaultDomain:       dd,
			DefaultSourceSet:    r.DefaultSourceSet,
			SkipNestedGenerated: r.SkipNestedGenerated,
		})
	}
	comp, err := graphbuild.Compile(context.Background(), graphbuild.CompileRequest{
		RepositoryRoot: repo, Sources: sources, Policy: graphbuild.ClosureStrictPolicy(),
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	art, err := graphbuild.Finalize(context.Background(), graphbuild.FinalizeRequest{Compilation: comp})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	srcMan, err := closureprotocol.SemanticDigest(comp.SourceManifest)
	if err != nil {
		t.Fatal(err)
	}
	return art, srcMan
}

// cleanCandidate seeds a clean task and prepares its transition candidate.
func cleanCandidate(t *testing.T, recordedAt string) (taskDir string, candidate resultpipeline.TransitionCandidate) {
	t.Helper()
	repo, taskDir, resultRev := seedCleanTask(t, "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
	head, err := admission.TaskLedgerHead(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := resultpipeline.PrepareTransition(context.Background(), resultpipeline.PrepareTransitionRequest{
		Build: resultpipeline.BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir,
			ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: rDomain},
		ExpectedLedgerHeadDigestSHA256: head, RecordedAt: recordedAt,
	})
	if err != nil {
		t.Fatalf("prepare candidate: %v", err)
	}
	return taskDir, c
}
