// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	"github.com/globulario/sensei/golang/architecture/resultrecording"
	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// This file builds a repository-level Phase-7 E2E from a real temporary Git
// repository, committed base/result revisions, governed sources, generated
// artifacts, and a complete admitted+consumed+observed+scope-verified ledger, then
// drives it through the single orchestration owner tasksession.AdvanceResultTransition.
// The seed mirrors resultrecording's internal harness (which is package-private and
// belongs to accepted PR #57); it is not shared code, so it is reproduced here.

var e2eEpoch = time.Unix(0, 0).UTC()

const e2eDomain = "github.com/globulario/sensei"

const e2eInvariants = `invariants:
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

func e2egit(t *testing.T, repo string, args ...string) string {
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

func e2ewrite(t *testing.T, repo, rel, content string) {
	t.Helper()
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func e2eActor() closureprotocol.ActorBinding {
	return closureprotocol.ActorBinding{PrincipalID: "actor.test", ActorKind: closureprotocol.ActorAgent, Roles: []string{"role.repository_repair_agent"}, Issuer: "sensei.local"}
}

func e2eResolution(t *testing.T, actorDigest, baseDigest string, ops []closureprotocol.ChangeOperation) closureprotocol.AuthorityResolution {
	t.Helper()
	var opResults []closureprotocol.AuthorityResolutionOperation
	for _, op := range ops {
		opResults = append(opResults, closureprotocol.AuthorityResolutionOperation{OperationID: op.OperationID, Status: closureprotocol.ReceiptValid, SelectedMechanism: closureprotocol.MechanismRepositoryEdit})
	}
	r := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256:         actorDigest,
		BaseBindingDigestSHA256:          baseDigest,
		ClosureAssessmentDigestSHA256:    "closure0",
		OperationSetDigestSHA256:         "ops0",
		AuthorityPolicyGraphDigestSHA256: "policygraph0",
		PolicyID:                         "admission.strict.v2",
		EvaluatedAt:                      "2026-07-16T12:00:00Z",
		Status:                           closureprotocol.ReceiptValid,
		OperationResults:                 opResults,
	}
	d, err := closureprotocol.AuthorityResolutionDigest(r)
	if err != nil {
		t.Fatal(err)
	}
	r.AuthorityResolutionDigestSHA256 = d
	return r
}

func e2eOpsFor(sourcePaths []string) (ops []closureprotocol.ChangeOperation, verdicts []closureprotocol.OperationAdmissionVerdict, consumedOps []string) {
	paths := append([]string(nil), sourcePaths...)
	sort.Strings(paths)
	for i, p := range paths {
		opID := fmt.Sprintf("op.%d", i+1)
		ops = append(ops, closureprotocol.ChangeOperation{OperationID: opID, Kind: closureprotocol.OperationModify, TargetKind: "source_file", Target: p, SelectedMechanism: closureprotocol.MechanismRepositoryEdit})
		verdicts = append(verdicts, closureprotocol.OperationAdmissionVerdict{OperationID: opID, Verdict: "admitted"})
		consumedOps = append(consumedOps, opID)
	}
	return ops, verdicts, consumedOps
}

// e2eCompileForSeed mirrors resultpipeline.compileGovernedGraph (ClosureStrict +
// Finalize) so the base graph digest matches what the pipeline recomputes.
func e2eCompileForSeed(t *testing.T, repo string, snapshot graphbuild.GraphInputSnapshot) (graphbuild.Artifact, string) {
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
			FilesystemPath: filepath.Join(root, filepath.FromSlash(r.LogicalPath)), IdentityRoot: root,
			StripPathPrefixes: []string{root}, RepositoryDomain: snapshot.RepositoryDomain, DefaultDomain: dd,
			DefaultSourceSet: r.DefaultSourceSet, SkipNestedGenerated: r.SkipNestedGenerated,
		})
	}
	comp, err := graphbuild.Compile(context.Background(), graphbuild.CompileRequest{RepositoryRoot: repo, Sources: sources, Policy: graphbuild.ClosureStrictPolicy()})
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

// e2eGenerate regenerates the governed artifacts over the repo's current content.
func e2eGenerate(t *testing.T, repo string, snapshot graphbuild.GraphInputSnapshot) {
	t.Helper()
	profile, err := generatedartifact.ProfileForDomain(e2eDomain)
	if err != nil {
		t.Fatal(err)
	}
	graph, srcMan := e2eCompileForSeed(t, repo, snapshot)
	gen, err := generatedartifact.Generate(context.Background(), generatedartifact.Context{
		RepositoryRoot: repo, RepositoryDomain: e2eDomain, GraphInputPolicyID: snapshot.PolicyID,
		GraphInputSnapshotDigestSHA256: snapshot.SnapshotDigestSHA256, SourceManifestDigestSHA256: srcMan,
		SupplementalGraphs: snapshot.SupplementalGraphs, GraphArtifact: graph,
	}, profile)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, o := range gen {
		e2ewrite(t, repo, o.Path, string(o.Bytes))
	}
}

// e2eSeed builds a real admitted+scope-verified committed task and returns the
// repo, task dir, and committed result revision. resultMutate is applied to the
// result tree before it is committed.
func e2eSeed(t *testing.T, resultMutate func(*testing.T, string), scopeFiles []string, direction string, authorizedSources, authorizedGenerated []string) (repo, taskDir, resultRev string) {
	t.Helper()
	repo = t.TempDir()
	e2egit(t, repo, "init", "-q")
	e2ewrite(t, repo, "docs/awareness/invariants.yaml", e2eInvariants)
	e2ewrite(t, repo, "src/model.go", "package src\n\nfunc Publish() {}\n")

	snapshot, _, err := graphbuild.SnapshotFromBuildInputs(
		resultpipeline.GraphInputPolicyV1, repo, e2eDomain,
		[]graphbuild.SourceRoot{{FilesystemPath: filepath.Join(repo, "docs", "awareness"), SkipNestedGenerated: true}}, nil)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	baseGraph, _ := e2eCompileForSeed(t, repo, snapshot)
	e2eGenerate(t, repo, snapshot)
	e2egit(t, repo, "add", "-A")
	e2egit(t, repo, "commit", "-q", "-m", "base")
	baseRev := e2egit(t, repo, "rev-parse", "HEAD")
	baseTree, err := binding.ResolveTreeIdentity(context.Background(), repo, baseRev)
	if err != nil {
		t.Fatal(err)
	}

	resultMutate(t, repo)
	e2egit(t, repo, "add", "-A")
	e2egit(t, repo, "commit", "-q", "-m", "result")
	resultRev = e2egit(t, repo, "rev-parse", "HEAD")

	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: e2eDomain, Revision: baseRev, RevisionStatus: "resolved", TreeDigestSHA256: baseTree.DigestSHA256},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: baseGraph.GraphSemanticDigestSHA256, DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.e2e", SessionID: "session.e2e"},
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
		Scope: closure.Scope{Domain: e2eDomain, TaskClass: "repository_repair", RiskClass: closure.RiskLowRisk,
			AccessMode: closure.AccessRead, DirectionRequirement: direction, Files: scopeFiles},
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
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: e2eEpoch,
	}); err != nil {
		t.Fatalf("task_prepared: %v", err)
	}

	actor := e2eActor()
	actorDigest := closureprotocol.MustSemanticDigest(actor)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		t.Fatal(err)
	}
	ops, verdicts, consumedOps := e2eOpsFor(authorizedSources)
	changePlan := closureprotocol.ChangePlan{PlanID: "plan.e2e", Operations: ops}
	resolution := e2eResolution(t, actorDigest, baseDigest, ops)
	authorityDigest := resolution.AuthorityResolutionDigestSHA256
	if _, err := admission.RecordAuthorityResolved(store, head(), base.Task, resolution, actor, changePlan, base, nil, e2eEpoch); err != nil {
		t.Fatalf("authority: %v", err)
	}
	decision := closureprotocol.AdmissionDecision{DecisionID: "decision.e2e", RequestDigestSHA256: "req0", PolicyID: "admission.strict.v2",
		OperationVerdicts: verdicts, CapabilityID: "cap.e2e", CompletionPolicyID: "completion.architectural_closure.v1"}
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	if _, err := admission.RecordAdmissionDecided(store, head(), decision, base.Task, e2eEpoch); err != nil {
		t.Fatalf("decision: %v", err)
	}
	consumption := closureprotocol.CapabilityConsumption{CapabilityID: "cap.e2e", Task: base.Task, ConsumerActor: actor,
		ConsumedOperationIDs: consumedOps, ConsumedAt: "2026-07-16T12:00:00Z", DecisionDigestSHA256: decisionDigest, OneUseStatus: closureprotocol.ReceiptValid}
	if _, err := admission.RecordAdmissionConsumed(store, head(), consumption, e2eEpoch); err != nil {
		t.Fatalf("consume: %v", err)
	}
	observed, err := resulttransition.ObserveChange(repo, baseRev, resultRev, actorDigest, authorityDigest)
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if _, err := admission.RecordChangeObserved(store, head(), base.Task, observed, e2eEpoch); err != nil {
		t.Fatalf("observed: %v", err)
	}
	scope, err := admission.VerifyScope(admission.ScopeExpectation{
		Decision: decision, Operations: ops, Consumption: consumption,
		ActorBindingDigestSHA256: actorDigest, AuthorityResolutionDigestSHA256: authorityDigest,
		BaseTreeDigestSHA256: observed.BaseTreeDigestSHA256, RequiredGeneratedArtifacts: append([]string(nil), authorizedGenerated...),
	}, observed, "2026-07-16T12:00:00Z")
	if err != nil {
		t.Fatalf("verify scope: %v", err)
	}
	if scope.Status != closureprotocol.ReceiptValid {
		t.Fatalf("observed result does not match pre-authorized scope: %+v", scope.Violations)
	}
	if _, err := admission.RecordScopeVerified(store, head(), base.Task, scope, e2eEpoch); err != nil {
		t.Fatalf("scope: %v", err)
	}
	return repo, taskDir, resultRev
}

const e2eInvariantsChanged = `invariants:
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

// e2eRegenerate recomputes the graph-input snapshot over the repo's CURRENT
// content and regenerates every governed artifact into the tree — the "regenerate
// required repository artifacts" step a governed change triggers.
func e2eRegenerate(t *testing.T, repo string) {
	t.Helper()
	snapshot, _, err := graphbuild.SnapshotFromBuildInputs(
		resultpipeline.GraphInputPolicyV1, repo, e2eDomain,
		[]graphbuild.SourceRoot{{FilesystemPath: filepath.Join(repo, "docs", "awareness"), SkipNestedGenerated: true}}, nil)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	e2eGenerate(t, repo, snapshot)
}

// e2eSeedClean seeds a task whose committed result honestly closes (low-risk,
// read, direction-not-applicable, committed revision → verdict closed, extraction
// complete, proving ready).
func e2eSeedClean(t *testing.T) (repo, taskDir, resultRev string) {
	return e2eSeed(t, func(tt *testing.T, r string) {
		e2ewrite(tt, r, "src/model.go", "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
	},
		[]string{"src/model.go"}, closure.DirectionNotApplicable, []string{"src/model.go"}, nil)
}

func e2eAdvance(t *testing.T, repo, taskDir, resultRev string) AdvanceResult {
	t.Helper()
	res, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
		RepositoryRoot: repo, TaskDirectory: taskDir, RepositoryDomain: e2eDomain, ResultRevision: resultRev,
		Now: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("advance: %v", err)
	}
	return res
}

// e2eForbiddenEvents is the set of later-phase / out-of-slice ledger events that
// must NEVER appear: this slice records only a result transition.
var e2eAllowedEvents = map[closureprotocol.LedgerEventType]bool{
	closureprotocol.LedgerEventTaskPrepared:             true,
	closureprotocol.LedgerEventAuthorityResolved:        true,
	closureprotocol.LedgerEventAdmissionDecided:         true,
	closureprotocol.LedgerEventAdmissionConsumed:        true,
	closureprotocol.LedgerEventChangeObserved:           true,
	closureprotocol.LedgerEventScopeVerified:            true,
	closureprotocol.LedgerEventResultTransitionRecorded: true,
}

func e2eLedgerEvents(t *testing.T, taskDir string) []closureprotocol.LedgerEventType {
	t.Helper()
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	chain, err := store.VerifyChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	var out []closureprotocol.LedgerEventType
	for _, e := range chain.Entries {
		out = append(out, e.Entry.EventType)
	}
	return out
}

func e2eCountTransitions(events []closureprotocol.LedgerEventType) int {
	n := 0
	for _, e := range events {
		if e == closureprotocol.LedgerEventResultTransitionRecorded {
			n++
		}
	}
	return n
}

// Scenario 1 — ready path: scope_verified → advance → exactly one recorded
// transition → independent reload/validation → phase proving.
func TestE2EReadyPathRecordsAndAdvancesToProving(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	res := e2eAdvance(t, repo, taskDir, resultRev)

	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded (refusal=%s %s)", res.Outcome, res.RefusalCode, res.RefusalDetail)
	}
	if res.TaskPhase != closureprotocol.PhaseProving {
		t.Fatalf("phase = %s, want proving", res.TaskPhase)
	}
	if res.TransitionEntryDigestSHA256 == "" || res.CurrentLedgerHeadSHA256 == "" {
		t.Fatal("must report transition entry identity and current head")
	}
	if e2eCountTransitions(e2eLedgerEvents(t, taskDir)) != 1 {
		t.Fatal("exactly one transition event expected")
	}
	// Independent reload + validation, entirely from the ledger.
	rt, err := resultrecording.LoadRecordedTransition(taskDir, res.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := resultrecording.ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// Scenario 2 — complete-but-blocked: a result whose direction requirement raises
// architect questions records exactly one transition, stays scope_verified, and
// retains its waiting reasons; no proving/certification/completion is claimed.
func TestE2ECompleteButBlockedStaysScopeVerified(t *testing.T) {
	repo, taskDir, resultRev := e2eSeed(t,
		func(tt *testing.T, r string) {
			e2ewrite(tt, r, "src/model.go", "package src\n\n// evolve\nfunc Publish() {}\n")
		},
		[]string{"src/model.go"}, closure.DirectionEvolve, []string{"src/model.go"}, nil)
	res := e2eAdvance(t, repo, taskDir, resultRev)

	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded (refusal=%s %s)", res.Outcome, res.RefusalCode, res.RefusalDetail)
	}
	if res.TaskPhase != closureprotocol.PhaseScopeVerified {
		t.Fatalf("phase = %s, want scope_verified (blocked)", res.TaskPhase)
	}
	if len(res.WaitingReasons) == 0 {
		t.Fatal("a complete-but-blocked result must retain its waiting reasons")
	}
	if strings.Contains(res.OperationalStatus, "certified") || strings.Contains(res.OperationalStatus, "completed") {
		t.Fatalf("status %q must not claim certified/completed", res.OperationalStatus)
	}
	if e2eCountTransitions(e2eLedgerEvents(t, taskDir)) != 1 {
		t.Fatal("exactly one transition event expected")
	}
}

// Scenario 4 — exact replay: a second advance appends no second event and reports
// the current ledger/projection state.
func TestE2EReplayIsIdempotent(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	first := e2eAdvance(t, repo, taskDir, resultRev)
	second := e2eAdvance(t, repo, taskDir, resultRev)

	if second.Outcome != OutcomeRecorded {
		t.Fatalf("replay outcome = %s", second.Outcome)
	}
	if second.TransitionDisposition != resultrecording.DispositionReplayed && second.TransitionDisposition != resultrecording.DispositionReconciled {
		t.Fatalf("replay disposition = %s, want replayed/reconciled", second.TransitionDisposition)
	}
	if second.TransitionEntryDigestSHA256 != first.TransitionEntryDigestSHA256 {
		t.Fatal("replay reported a different transition entry")
	}
	if e2eCountTransitions(e2eLedgerEvents(t, taskDir)) != 1 {
		t.Fatal("replay must not append a second transition event")
	}
}

// Scenario 5 — concurrency / stale head: several advances race over one
// scope-verified task. Exactly one may record; the others reconcile (replay) or
// fail closed (refused/stale because the ledger moved during their preparation).
// No advance is a false success, no two disagree on the transition entry, and no
// second transition event is ever appended. (A mid-record stale expected head maps
// to OutcomeStale via resultrecording.RecordTransition, proven in Step 8; here we
// prove the orchestrator preserves single-recording under real concurrency.)
func TestE2EConcurrentAdvancesRecordExactlyOnce(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	const n = 4
	type outcome struct {
		res AdvanceResult
		err error
	}
	results := make(chan outcome, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := AdvanceResultTransition(context.Background(), AdvanceResultRequest{
				RepositoryRoot: repo, TaskDirectory: taskDir, RepositoryDomain: e2eDomain, ResultRevision: resultRev,
				Now: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
			})
			results <- outcome{res, err}
		}()
	}
	wg.Wait()
	close(results)

	recorded := 0
	var entry string
	for o := range results {
		if o.err != nil {
			t.Fatalf("concurrent advance returned a hard error: %v", o.err)
		}
		switch o.res.Outcome {
		case OutcomeRecorded:
			// Replay/reconcile counts as recorded-outcome but not a fresh record.
			if o.res.TransitionDisposition == resultrecording.DispositionRecorded {
				recorded++
			}
			if entry == "" {
				entry = o.res.TransitionEntryDigestSHA256
			} else if o.res.TransitionEntryDigestSHA256 != entry {
				t.Fatal("concurrent advances disagree on the transition entry identity")
			}
		case OutcomeRefused, OutcomeStale:
			// A loser whose ledger moved during preparation fails closed — never a
			// false success and never a recorded transition.
			if o.res.TransitionRecorded {
				t.Fatal("a refused/stale advance must record nothing")
			}
		default:
			t.Fatalf("unexpected concurrent outcome %s", o.res.Outcome)
		}
	}
	if recorded != 1 {
		t.Fatalf("exactly one advance may perform a fresh record; got %d", recorded)
	}
	if e2eCountTransitions(e2eLedgerEvents(t, taskDir)) != 1 {
		t.Fatal("no second transition event may be appended under concurrency")
	}
}

// Scenario 3 — governed change: a committed result that changes a governed
// invariant (plus its regenerated artifacts) survives the complete orchestration
// with the EXACT governed record identity preserved through storage and reload.
func TestE2EGovernedChangeSurvivesOrchestration(t *testing.T) {
	const wantInvID = "https://globular.io/awareness#invariant/test.publish_mutates_state"
	repo, taskDir, resultRev := e2eSeed(t,
		func(tt *testing.T, r string) {
			e2ewrite(tt, r, "src/model.go", "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n")
			e2ewrite(tt, r, "docs/awareness/invariants.yaml", e2eInvariantsChanged)
			e2eRegenerate(tt, r)
		},
		[]string{"src/model.go"}, closure.DirectionNotApplicable,
		[]string{"src/model.go", "docs/awareness/invariants.yaml"},
		[]string{"golang/server/embeddata/awareness.nt", "golang/server/embeddata/awareness.result-manifest.tsv"})

	res := e2eAdvance(t, repo, taskDir, resultRev)
	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s (refusal=%s %s)", res.Outcome, res.RefusalCode, res.RefusalDetail)
	}

	// Reload entirely from the ledger and prove the exact changed invariant survived.
	rt, err := resultrecording.LoadRecordedTransition(taskDir, res.TransitionID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := resultrecording.ValidateRecordedTransition(rt); err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, im := range rt.ImpactReport.Impacts {
		changed := closureprotocol.GovernedKnowledgeImpactChanged(im)
		if im.Category == "invariants" {
			if !changed || len(im.ChangedRecordIDs) != 1 || im.ChangedRecordIDs[0] != wantInvID {
				t.Fatalf("invariants changed-set = %v, want exactly [%s]", im.ChangedRecordIDs, wantInvID)
			}
			continue
		}
		if changed {
			t.Fatalf("unrelated category %q changed: %v", im.Category, im.ChangedRecordIDs)
		}
	}
}

// Scenario 9 — boundary proof: after a recorded transition, the ledger contains
// only phase-≤7 events (no evidence, proof, certification, completion, revocation,
// migration, or Phase-8 event), and the projected status never claims certified or
// completed. CorrectnessCertified stays false.
func TestE2EBoundaryProofNoLaterPhaseEvent(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	res := e2eAdvance(t, repo, taskDir, resultRev)
	if res.Outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	for _, e := range e2eLedgerEvents(t, taskDir) {
		if !e2eAllowedEvents[e] {
			t.Fatalf("out-of-slice ledger event escaped: %s", e)
		}
	}
	if strings.Contains(res.OperationalStatus, "certified") || strings.Contains(res.OperationalStatus, "completed") {
		t.Fatalf("status %q claims certified/completed", res.OperationalStatus)
	}
	if res.TaskPhase == closureprotocol.PhaseCertified || res.TaskPhase == closureprotocol.PhaseCompleted {
		t.Fatalf("phase %s is a later-phase claim", res.TaskPhase)
	}
}
