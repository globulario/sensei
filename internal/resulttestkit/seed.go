// SPDX-License-Identifier: Apache-2.0

// Package resulttestkit seeds a real repository-level Phase-7 task — a temporary
// Git repository with committed base/result revisions, governed sources, generated
// artifacts, and a complete admitted+consumed+observed+scope-verified ledger — so
// tests across packages can exercise the result boundary end to end. It is imported
// only by test code, so it never enters a production binary.
package resulttestkit

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

// Domain is the repository domain used by the seed.
const Domain = "github.com/globulario/sensei"

var epoch = time.Unix(0, 0).UTC()

const invariants = `invariants:
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

// Options parameterizes the seed.
type Options struct {
	// ResultFiles overwrites these repo-relative files in the result tree before it
	// is committed (default: rewrites src/model.go to a no-op comment).
	ResultFiles map[string]string
	// Regenerate regenerates every governed artifact into the result tree (used for
	// governed-source changes) before committing the result.
	Regenerate bool
	// ScopeFiles is the closure scope (default ["src/model.go"]).
	ScopeFiles []string
	// Direction is the closure direction requirement (default "not_applicable"; use
	// "evolve" for a complete-but-blocked result).
	Direction string
	// AuthorizedSources are the source paths predeclared in the change plan
	// (default ["src/model.go"]).
	AuthorizedSources []string
	// AuthorizedGenerated are the required generated-artifact paths.
	AuthorizedGenerated []string
}

// Result is the seeded task.
type Result struct {
	Repo      string
	TaskDir   string
	ResultRev string
	TaskID    string
	SessionID string
}

// Seed builds the repository and ledger under baseDir (two temp subdirectories)
// and returns the task identity. baseDir must exist.
func Seed(baseDir string, opts Options) (Result, error) {
	if opts.Direction == "" {
		opts.Direction = closure.DirectionNotApplicable
	}
	if opts.ScopeFiles == nil {
		opts.ScopeFiles = []string{"src/model.go"}
	}
	if opts.AuthorizedSources == nil {
		opts.AuthorizedSources = []string{"src/model.go"}
	}
	if opts.ResultFiles == nil {
		opts.ResultFiles = map[string]string{"src/model.go": "package src\n\n// Publish is a no-op.\nfunc Publish() {}\n"}
	}

	repo, err := os.MkdirTemp(baseDir, "repo-")
	if err != nil {
		return Result{}, err
	}
	if _, err := git(repo, "init", "-q"); err != nil {
		return Result{}, err
	}
	if err := write(repo, "docs/awareness/invariants.yaml", invariants); err != nil {
		return Result{}, err
	}
	if err := write(repo, "src/model.go", "package src\n\nfunc Publish() {}\n"); err != nil {
		return Result{}, err
	}

	snapshot, err := snapshotOf(repo)
	if err != nil {
		return Result{}, err
	}
	baseGraph, _, err := compile(repo, snapshot)
	if err != nil {
		return Result{}, err
	}
	if err := generate(repo, snapshot); err != nil {
		return Result{}, err
	}
	if _, err := git(repo, "add", "-A"); err != nil {
		return Result{}, err
	}
	if _, err := git(repo, "commit", "-q", "-m", "base"); err != nil {
		return Result{}, err
	}
	baseRev, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	baseTree, err := binding.ResolveTreeIdentity(context.Background(), repo, baseRev)
	if err != nil {
		return Result{}, err
	}

	for rel, content := range opts.ResultFiles {
		if err := write(repo, rel, content); err != nil {
			return Result{}, err
		}
	}
	if opts.Regenerate {
		snap2, err := snapshotOf(repo)
		if err != nil {
			return Result{}, err
		}
		if err := generate(repo, snap2); err != nil {
			return Result{}, err
		}
	}
	if _, err := git(repo, "add", "-A"); err != nil {
		return Result{}, err
	}
	if _, err := git(repo, "commit", "-q", "-m", "result"); err != nil {
		return Result{}, err
	}
	resultRev, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}

	base := closureprotocol.BaseBinding{
		Repository: closureprotocol.RepositorySnapshot{Domain: Domain, Revision: baseRev, RevisionStatus: "resolved", TreeDigestSHA256: baseTree.DigestSHA256},
		Graph:      closureprotocol.GraphSnapshot{SchemaVersion: "awareness-ontology/0.2", DigestSHA256: baseGraph.GraphSemanticDigestSHA256, DigestStatus: "resolved"},
		Task:       closureprotocol.TaskBinding{ID: "task.e2e", SessionID: "session.e2e"},
		Policies: closureprotocol.PolicyBinding{
			Admission: "admission.strict.v2", Certification: "certification.architectural_closure.v1",
			Completion: "completion.architectural_closure.v1", Revocation: "revocation.architectural_closure.v1",
			Ledger: "ledger.task.v1", Canonicalization: "canon.v1",
		},
	}

	taskDir, err := os.MkdirTemp(baseDir, "task-")
	if err != nil {
		return Result{}, err
	}
	store := ledger.NewStore(taskDir, ledger.WithPayloadValidator(func(et closureprotocol.LedgerEventType, mt string, data []byte) error {
		return ledger.ValidateTaskEventPayload(et, data)
	}))
	head := func() (string, error) { return admission.TaskLedgerHead(taskDir) }

	resultBinding := binding.ToClaimDocumentBinding(base)
	resultBinding.Revision = resultRev
	resultBinding.RevisionStatus = "resolved"
	closureReq := closure.Request{
		SchemaVersion: "1", TaskID: base.Task.ID, Binding: resultBinding,
		Scope: closure.Scope{Domain: Domain, TaskClass: "repository_repair", RiskClass: closure.RiskLowRisk,
			AccessMode: closure.AccessRead, DirectionRequirement: opts.Direction, Files: opts.ScopeFiles},
	}
	closureBytes, err := closure.MarshalCanonicalRequestYAML(closureReq)
	if err != nil {
		return Result{}, err
	}
	ref, err := store.StoreArtifactBytes(closureBytes, "application/yaml")
	if err != nil {
		return Result{}, err
	}
	snapBytes, _ := yaml.Marshal(snapshot)
	snapRef, err := store.StoreArtifactBytes(snapBytes, "application/yaml")
	if err != nil {
		return Result{}, err
	}
	h, err := head()
	if err != nil {
		return Result{}, err
	}
	if _, err := store.Append(context.Background(), ledger.AppendRequest{
		TaskID: base.Task.ID, SessionID: base.Task.SessionID, EventType: closureprotocol.LedgerEventTaskPrepared,
		Payload: ledger.TaskEventPayload{SchemaVersion: ledger.EventPayloadSchemaVersion, EventType: closureprotocol.LedgerEventTaskPrepared,
			TaskID: base.Task.ID, SessionID: base.Task.SessionID, BaseBinding: &base,
			Artifacts: map[string]closureprotocol.LedgerPayloadRef{"closure_request": ref, "graph_input_snapshot": snapRef}},
		PayloadMediaType: "application/yaml", ProducerID: "test", ProducedAt: epoch,
	}); err != nil {
		return Result{}, fmt.Errorf("task_prepared: %w", err)
	}

	actor := closureprotocol.ActorBinding{PrincipalID: "actor.test", ActorKind: closureprotocol.ActorAgent, Roles: []string{"role.repository_repair_agent"}, Issuer: "sensei.local"}
	actorDigest := closureprotocol.MustSemanticDigest(actor)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		return Result{}, err
	}
	ops, verdicts, consumedOps := opsFor(opts.AuthorizedSources)
	resolution, err := resolutionOf(actorDigest, baseDigest, ops)
	if err != nil {
		return Result{}, err
	}
	authorityDigest := resolution.AuthorityResolutionDigestSHA256
	if h, err = head(); err != nil {
		return Result{}, err
	}
	if _, err := admission.RecordAuthorityResolved(store, h, base.Task, resolution, actor, closureprotocol.ChangePlan{PlanID: "plan.e2e", Operations: ops}, base, nil, epoch); err != nil {
		return Result{}, fmt.Errorf("authority: %w", err)
	}
	decision := closureprotocol.AdmissionDecision{DecisionID: "decision.e2e", RequestDigestSHA256: "req0", PolicyID: "admission.strict.v2",
		OperationVerdicts: verdicts, CapabilityID: "cap.e2e", CompletionPolicyID: "completion.architectural_closure.v1"}
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	if h, err = head(); err != nil {
		return Result{}, err
	}
	if _, err := admission.RecordAdmissionDecided(store, h, decision, base.Task, epoch); err != nil {
		return Result{}, fmt.Errorf("decision: %w", err)
	}
	consumption := closureprotocol.CapabilityConsumption{CapabilityID: "cap.e2e", Task: base.Task, ConsumerActor: actor,
		ConsumedOperationIDs: consumedOps, ConsumedAt: "2026-07-16T12:00:00Z", DecisionDigestSHA256: decisionDigest, OneUseStatus: closureprotocol.ReceiptValid}
	if h, err = head(); err != nil {
		return Result{}, err
	}
	if _, err := admission.RecordAdmissionConsumed(store, h, consumption, epoch); err != nil {
		return Result{}, fmt.Errorf("consume: %w", err)
	}
	observed, err := resulttransition.ObserveChange(repo, baseRev, resultRev, actorDigest, authorityDigest)
	if err != nil {
		return Result{}, fmt.Errorf("observe: %w", err)
	}
	if h, err = head(); err != nil {
		return Result{}, err
	}
	if _, err := admission.RecordChangeObserved(store, h, base.Task, observed, epoch); err != nil {
		return Result{}, fmt.Errorf("observed: %w", err)
	}
	scope, err := admission.VerifyScope(admission.ScopeExpectation{
		Decision: decision, Operations: ops, Consumption: consumption,
		ActorBindingDigestSHA256: actorDigest, AuthorityResolutionDigestSHA256: authorityDigest,
		BaseTreeDigestSHA256: observed.BaseTreeDigestSHA256, RequiredGeneratedArtifacts: append([]string(nil), opts.AuthorizedGenerated...),
	}, observed, "2026-07-16T12:00:00Z")
	if err != nil {
		return Result{}, fmt.Errorf("verify scope: %w", err)
	}
	if scope.Status != closureprotocol.ReceiptValid {
		return Result{}, fmt.Errorf("observed result does not match pre-authorized scope: %+v", scope.Violations)
	}
	if h, err = head(); err != nil {
		return Result{}, err
	}
	if _, err := admission.RecordScopeVerified(store, h, base.Task, scope, epoch); err != nil {
		return Result{}, fmt.Errorf("scope: %w", err)
	}
	return Result{Repo: repo, TaskDir: taskDir, ResultRev: resultRev, TaskID: base.Task.ID, SessionID: base.Task.SessionID}, nil
}

func snapshotOf(repo string) (graphbuild.GraphInputSnapshot, error) {
	s, _, err := graphbuild.SnapshotFromBuildInputs(
		resultpipeline.GraphInputPolicyV1, repo, Domain,
		[]graphbuild.SourceRoot{{FilesystemPath: filepath.Join(repo, "docs", "awareness"), SkipNestedGenerated: true}}, nil)
	return s, err
}

func compile(repo string, snapshot graphbuild.GraphInputSnapshot) (graphbuild.Artifact, string, error) {
	root, err := filepath.Abs(repo)
	if err != nil {
		return graphbuild.Artifact{}, "", err
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
		return graphbuild.Artifact{}, "", err
	}
	art, err := graphbuild.Finalize(context.Background(), graphbuild.FinalizeRequest{Compilation: comp})
	if err != nil {
		return graphbuild.Artifact{}, "", err
	}
	srcMan, err := closureprotocol.SemanticDigest(comp.SourceManifest)
	return art, srcMan, err
}

func generate(repo string, snapshot graphbuild.GraphInputSnapshot) error {
	profile, err := generatedartifact.ProfileForDomain(Domain)
	if err != nil {
		return err
	}
	graph, srcMan, err := compile(repo, snapshot)
	if err != nil {
		return err
	}
	gen, err := generatedartifact.Generate(context.Background(), generatedartifact.Context{
		RepositoryRoot: repo, RepositoryDomain: Domain, GraphInputPolicyID: snapshot.PolicyID,
		GraphInputSnapshotDigestSHA256: snapshot.SnapshotDigestSHA256, SourceManifestDigestSHA256: srcMan,
		SupplementalGraphs: snapshot.SupplementalGraphs, GraphArtifact: graph,
	}, profile)
	if err != nil {
		return err
	}
	for _, o := range gen {
		if err := write(repo, o.Path, string(o.Bytes)); err != nil {
			return err
		}
	}
	return nil
}

func opsFor(sourcePaths []string) (ops []closureprotocol.ChangeOperation, verdicts []closureprotocol.OperationAdmissionVerdict, consumedOps []string) {
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

func resolutionOf(actorDigest, baseDigest string, ops []closureprotocol.ChangeOperation) (closureprotocol.AuthorityResolution, error) {
	var opResults []closureprotocol.AuthorityResolutionOperation
	for _, op := range ops {
		opResults = append(opResults, closureprotocol.AuthorityResolutionOperation{OperationID: op.OperationID, Status: closureprotocol.ReceiptValid, SelectedMechanism: closureprotocol.MechanismRepositoryEdit})
	}
	r := closureprotocol.AuthorityResolution{
		ActorBindingDigestSHA256: actorDigest, BaseBindingDigestSHA256: baseDigest,
		ClosureAssessmentDigestSHA256: "closure0", OperationSetDigestSHA256: "ops0", AuthorityPolicyGraphDigestSHA256: "policygraph0",
		PolicyID: "admission.strict.v2", EvaluatedAt: "2026-07-16T12:00:00Z", Status: closureprotocol.ReceiptValid, OperationResults: opResults,
	}
	d, err := closureprotocol.AuthorityResolutionDigest(r)
	if err != nil {
		return closureprotocol.AuthorityResolution{}, err
	}
	r.AuthorityResolutionDigestSHA256 = d
	return r, nil
}

func git(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func write(repo, rel, content string) error {
	p := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(content), 0o644)
}
