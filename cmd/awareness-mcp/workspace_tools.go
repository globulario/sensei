// SPDX-License-Identifier: AGPL-3.0-only

// workspace_tools.go implements the three new canonical workspace MCP tools
// (docs/design/workspace-identity-admission-contracts.md):
// sensei_workspace_status, sensei_workspace_admit_change, and
// sensei_workspace_verify_admission. Each composes/projects existing typed
// Sensei owners into the two closed external contracts under
// docs/schemas/workspace/v1/ — it never reimplements domain resolution,
// revision/tree digesting, metadata interpretation, task resolution, or
// admission evaluation/verification.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/repodomain"
	"github.com/globulario/sensei/golang/architecture/tasksession"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
	awarenessclient "github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// workspaceSchemaDir locates the vendored, canonical workspace schemas
// relative to this binary's source tree. Mirrors the pattern other embedded-
// resource lookups in this repository use: resolved once from the module
// root, not guessed from the working directory.
const workspaceSchemaDir = "docs/schemas/workspace/v1"

// marshalForSchema serializes a composed record for schema validation using
// plain encoding/json — the same wire form the MCP structuredContent result
// carries.
func marshalForSchema(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// requireKnownProperties rejects any args key not in allowed, at runtime —
// a schema declaration alone is advisory, and the workspace contract
// requires runtime enforcement even when an MCP client ignores the
// advertised JSON Schema (mirrors the existing complete_task case's
// pattern).
func requireKnownProperties(args map[string]interface{}, allowed ...string) error {
	set := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		set[a] = true
	}
	for k := range args {
		if !set[k] {
			return fmt.Errorf("unknown property %q", k)
		}
	}
	return nil
}

// callWorkspaceStatus implements sensei_workspace_status: repo is required;
// task is optional and, when present at all (including ""), is resolved
// through the existing task-session owner exactly like task_status/
// task_briefing already do — an ABSENT task key means "not requested",
// never "use the active task by default," so this narrow evidence surface
// never silently guesses which task the caller meant.
func (b *bridge) callWorkspaceStatus(ctx context.Context, args map[string]interface{}) (*toolResult, error) {
	if err := requireKnownProperties(args, "repo", "task"); err != nil {
		return nil, err
	}
	repo, err := stringArg(args, "repo")
	if err != nil {
		return nil, err
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}

	// task must be validated as a TYPE before any presence/value logic runs:
	// a bare `task, _ := args["task"].(string)` type assertion would silently
	// turn a malformed JSON null/number/array/object into "" — which the
	// resolver below treats as "the active task." Absent key, present string
	// (including ""), and present-non-string are three distinct cases; only
	// the type check belongs here, so it can never be bypassed by whichever
	// function ends up doing the presence/value branching.
	var task string
	taskProvided := false
	if raw, present := args["task"]; present {
		s, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("property \"task\" must be a string")
		}
		task = s
		taskProvided = true
	}

	id, err := composeWorkspaceIdentity(ctx, b.client, absRepo, taskProvided, task)
	if err != nil {
		return nil, err
	}
	if errs := workspacecontract.ValidateIdentity(id); len(errs) != 0 {
		return nil, fmt.Errorf("internal: composed identity failed producer validation: %v", errs)
	}
	raw, err := marshalForSchema(id)
	if err != nil {
		return nil, err
	}
	if err := workspacecontract.ValidateIdentitySchema(raw); err != nil {
		return nil, fmt.Errorf("internal: composed identity failed schema validation: %w", err)
	}

	text := fmt.Sprintf("composition_state: %s\nrepository_domain_source: %s\nrepository_domain: %s\ntask_identity: %s",
		id.CompositionState, id.RepositoryDomainSource, id.Binding.RepositoryDomain, id.TaskIdentity.State)
	return &toolResult{Text: text, Structured: structFrom(id)}, nil
}

// composeWorkspaceIdentity resolves every governed fact through its
// existing owner and hands them to workspacecontract.ComposeIdentity, which
// does only deterministic composition — never a live lookup itself.
func composeWorkspaceIdentity(ctx context.Context, client awarenessClient, absRepo string, taskProvided bool, task string) (workspacecontract.Identity, error) {
	var limitations []workspacecontract.Limitation

	domain, domainErr := repodomain.Configured(absRepo)
	if domainErr != nil {
		return workspacecontract.Identity{}, fmt.Errorf("repository domain configuration: %w", domainErr)
	}
	domainSource := workspacecontract.RepositoryDomainUnbound
	if domain != "" {
		domainSource = workspacecontract.RepositoryDomainConfigured
	} else {
		limitations = append(limitations, workspacecontract.Limitation{
			Source: "golang/architecture/repodomain", Scope: "repository_domain",
			Reason: "no repository.domain is configured in .sensei/config.yaml for this checkout", Blocking: true,
		})
	}

	revision, revisionStatus, revLimitations := architecture.ResolveRevision(absRepo, true)
	for _, l := range revLimitations {
		limitations = append(limitations, workspacecontract.Limitation{Source: l.Source, Scope: l.Scope, Reason: l.Reason, Blocking: l.Blocking})
	}

	var treeDigest string
	if revisionStatus == architecture.RevisionResolved {
		td, terr := binding.RepositoryTreeDigestSHA256(absRepo, revision)
		if terr != nil {
			limitations = append(limitations, workspacecontract.Limitation{
				Source: "golang/architecture/binding", Scope: "tree_digest_sha256", Reason: terr.Error(), Blocking: false,
			})
		} else {
			treeDigest = td
		}
	}

	metaReq := &awarenesspb.MetadataRequest{Domain: domain}
	metaResp, metaErr := client.Metadata(ctx, metaReq)
	var graphAuthority *workspacecontract.GraphAuthority
	coverageState := "COVERAGE_STATE_UNSPECIFIED"
	if metaErr != nil {
		limitations = append(limitations, workspacecontract.Limitation{
			Source: "golang/server Metadata RPC", Scope: "graph_authority", Reason: metaErr.Error(), Blocking: true,
		})
	} else {
		mv := awarenessclient.InterpretMetadataAuthority(metaResp)
		graphAuthority = &workspacecontract.GraphAuthority{
			Authoritative:                   mv.Authoritative,
			GraphFreshnessState:             awarenessclient.EffectiveMetadataFreshness(metaResp).String(),
			GraphFreshnessDetail:            metaResp.GetGraphFreshnessDetail(),
			SeedState:                       metaResp.GetSeedState().String(),
			BuildProvenanceState:            metaResp.GetBuildProvenanceState().String(),
			LiveStoreGraphDigestSHA256:      metaResp.GetLiveStoreGraphDigestSha256(),
			LiveStoreGraphTripleCount:       metaResp.GetLiveStoreGraphTripleCount(),
			EmbeddedSeedDigestSHA256:        metaResp.GetEmbeddedSeedDigestSha256(),
			EmbeddedTransactionStampPresent: metaResp.GetEmbeddedTransactionStampPresent(),
			EmbeddedTransactionMatchesSeed:  metaResp.GetEmbeddedTransactionMatchesSeed(),
			CertifiedAwarenessGraphCommit:   metaResp.GetCertifiedAwarenessGraphCommit(),
			CertifiedServicesRepoCommit:     metaResp.GetCertifiedServicesRepoCommit(),
		}
		if cs := metaResp.GetCoverageState().String(); cs != "" {
			coverageState = cs
		}
	}

	taskIdentity, taskLimitations := resolveWorkspaceTaskIdentity(absRepo, taskProvided, task)
	limitations = append(limitations, taskLimitations...)

	return workspacecontract.ComposeIdentity(workspacecontract.IdentityInputs{
		RepositoryDomainSource: domainSource,
		RepositoryDomain:       domain,
		Revision:               revision,
		RevisionStatus:         workspacecontract.RevisionStatus(revisionStatus),
		TreeDigestSHA256:       treeDigest,
		// GraphDigestSHA256/GraphDigestStatus deliberately stay unset
		// (not_requested/null): that pair carries ClaimDocumentBinding's
		// task/snapshot-scoped local graph.nt identity, a different
		// authority from GraphAuthority.LiveStoreGraphDigestSHA256 above.
		// sensei_workspace_status has no graph_nt parameter and resolves no
		// such snapshot, so honestly reporting "not requested" here is the
		// correct binding-vs-live-store distinction — never borrow the
		// live-store digest into this field, even though both describe "the
		// graph": they are not the same fact and must not share one value.
		GraphDigestSHA256: "",
		GraphDigestStatus: workspacecontract.GraphDigestNotRequested,
		GraphAuthority:    graphAuthority,
		CoverageState:     coverageState,
		TaskIdentity:      taskIdentity,
		Limitations:       limitations,
	}), nil
}

// resolveWorkspaceTaskIdentity distinguishes not_requested (taskProvided is
// false — the "task" key was absent from args entirely) from
// resolved/unavailable (taskProvided is true — the key was present, and ""
// per existing convention means "the active task"). The caller has already
// validated task's JSON type before this function ever sees it.
func resolveWorkspaceTaskIdentity(absRepo string, taskProvided bool, task string) (workspacecontract.TaskIdentity, []workspacecontract.Limitation) {
	if !taskProvided {
		return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityNotRequested}, nil
	}
	state, _, err := tasksession.ControlStatus(absRepo, strings.TrimSpace(task), strings.TrimSpace(task) == "")
	if err != nil {
		return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityUnavailable},
			[]workspacecontract.Limitation{{Source: "golang/architecture/tasksession", Scope: "task_identity", Reason: err.Error(), Blocking: false}}
	}
	if strings.TrimSpace(state.TaskID) == "" {
		return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityUnavailable},
			[]workspacecontract.Limitation{{Source: "golang/architecture/tasksession", Scope: "task_identity", Reason: "resolved task control state carries no task_id", Blocking: false}}
	}
	taskID := state.TaskID
	return workspacecontract.TaskIdentity{State: workspacecontract.TaskIdentityResolved, TaskID: &taskID}, nil
}

// callWorkspaceAdmitChange implements sensei_workspace_admit_change: it
// mirrors admit_change's exact arguments and delegates to admission.Evaluate
// exactly once, then projects the result into
// sensei.workspace.admission.v1. It never mutates admit_change's own
// structured-output shape.
func (b *bridge) callWorkspaceAdmitChange(args map[string]interface{}) (*toolResult, error) {
	if err := requireKnownProperties(args, "bundle_dir", "request_path", "graph_nt", "repo", "policy"); err != nil {
		return nil, err
	}
	bundleDir, err := stringArg(args, "bundle_dir")
	if err != nil {
		return nil, err
	}
	requestPath, err := stringArg(args, "request_path")
	if err != nil {
		return nil, err
	}
	graphNT, err := stringArg(args, "graph_nt")
	if err != nil {
		return nil, err
	}
	repo, err := stringArg(args, "repo")
	if err != nil {
		return nil, err
	}
	policy, err := stringArg(args, "policy")
	if err != nil {
		return nil, err
	}
	bundleDir, requestPath, graphNT, repo = strings.TrimSpace(bundleDir), strings.TrimSpace(requestPath), strings.TrimSpace(graphNT), strings.TrimSpace(repo)
	if bundleDir == "" || requestPath == "" || graphNT == "" || repo == "" {
		return nil, fmt.Errorf("bundle_dir, request_path, graph_nt, and repo are required")
	}
	decision, err := admission.Evaluate(admission.EvaluateOptions{BundleDir: bundleDir, RequestPath: requestPath, GraphNT: graphNT, Repo: repo, PolicyID: strings.TrimSpace(policy)})
	if err != nil {
		return nil, err
	}
	rec := workspacecontract.ProjectDecision(decision)
	return workspaceAdmissionResult(rec)
}

// callWorkspaceVerifyAdmission implements sensei_workspace_verify_admission:
// it loads the exact referenced decision artifact (the same file
// verify_admission itself reads from decision_path), delegates verification
// to admission.Verify exactly once, and projects both together so the
// returned record retains the original decision's identity.
func (b *bridge) callWorkspaceVerifyAdmission(args map[string]interface{}) (*toolResult, error) {
	if err := requireKnownProperties(args, "decision_path", "bundle_dir", "repo"); err != nil {
		return nil, err
	}
	decisionPath, err := stringArg(args, "decision_path")
	if err != nil {
		return nil, err
	}
	bundleDir, err := stringArg(args, "bundle_dir")
	if err != nil {
		return nil, err
	}
	repo, err := stringArg(args, "repo")
	if err != nil {
		return nil, err
	}
	decisionPath, bundleDir, repo = strings.TrimSpace(decisionPath), strings.TrimSpace(bundleDir), strings.TrimSpace(repo)
	if decisionPath == "" || bundleDir == "" || repo == "" {
		return nil, fmt.Errorf("decision_path, bundle_dir, and repo are required")
	}
	decision, err := admission.LoadDecision(decisionPath)
	if err != nil {
		return nil, err
	}
	verification, err := admission.Verify(admission.VerifyOptions{DecisionPath: decisionPath, BundleDir: bundleDir, Repo: repo})
	if err != nil {
		return nil, err
	}
	rec, err := workspacecontract.ProjectVerification(decision, verification)
	if err != nil {
		return nil, err
	}
	return workspaceAdmissionResult(rec)
}

// workspaceAdmissionResult runs the shared producer + schema validation
// every returned admission record must pass before an MCP result is built.
func workspaceAdmissionResult(rec workspacecontract.Admission) (*toolResult, error) {
	if errs := workspacecontract.ValidateAdmission(rec); len(errs) != 0 {
		return nil, fmt.Errorf("internal: composed admission record failed producer validation: %v", errs)
	}
	raw, err := marshalForSchema(rec)
	if err != nil {
		return nil, err
	}
	if err := workspacecontract.ValidateAdmissionSchema(raw); err != nil {
		return nil, fmt.Errorf("internal: composed admission record failed schema validation: %w", err)
	}
	text := fmt.Sprintf("record_kind: %s\ndecision: %s\nadmission_id: %s", rec.RecordKind, rec.Decision, rec.AdmissionID)
	if rec.Verification != nil {
		text += fmt.Sprintf("\nverification_status: %s", rec.Verification.Status)
	}
	return &toolResult{Text: text, Structured: structFrom(rec)}, nil
}
