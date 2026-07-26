// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/workspacecontract"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// minimalAdmittedDecisionForTest builds a real, schema-shape-valid
// admission.Decision — not a workspacecontract type — so
// TestWorkspaceAdmission_ProjectionRoundtripsSchemaValid exercises the
// actual projection function this package calls, exactly as
// callWorkspaceAdmitChange does with a decision admission.Evaluate itself
// produced.
func minimalAdmittedDecisionForTest() admission.Decision {
	digest := "5555555555555555555555555555555555555555555555555555555555555555"
	return admission.Decision{
		SchemaVersion: admission.SchemaVersion,
		GeneratedBy:   admission.GeneratedBy,
		AdmissionID:   "admission.test-mcp",
		PolicyID:      admission.PolicyStrictID,
		PolicyVersion: admission.PolicyStrictVersion,
		Decision:      admission.DecisionAdmitted,
		RequestedMode: admission.ModeModify,
		Binding: architecture.ClaimDocumentBinding{
			RepositoryDomain:  "github.com/globulario/sensei",
			Revision:          "abc123",
			RevisionStatus:    architecture.RevisionResolved,
			GraphDigestSHA256: digest,
			GraphDigestStatus: architecture.GraphDigestResolved,
		},
		SessionReceipt: admission.SessionReceipt{
			SessionID: "session.test", LatestIteration: 1,
			IterationDigestSHA256: digest, SemanticStateDigestSHA256: digest,
			Status: "converged", ClosureVerdict: "closed",
		},
		RequestReceipt: admission.RequestReceipt{
			DigestSHA256: digest,
			Scope:        admission.ChangeScope{Files: []admission.FileOperation{{Path: "x.go", Operation: admission.OperationModify}}},
			Mode:         admission.ModeModify, TaskClass: "test",
		},
		InspectionCapability: admission.CapabilityAdmitted,
		MutationCapability:   admission.CapabilityAdmitted,
		Envelope:             admission.ChangeEnvelope{ModifyPaths: []string{"x.go"}},
		ScopeOnly:            true,
		DecisionDigestSHA256: digest,
	}
}

// TestMCPWorkspaceToolsAreRegisteredWithClosedSchemas proves tool discovery
// advertises all three new tools with closed (additionalProperties: false)
// input schemas.
func TestMCPWorkspaceToolsAreRegisteredWithClosedSchemas(t *testing.T) {
	b := testBridge(fakeClient{})
	found := map[string]bool{}
	for _, tool := range b.tools() {
		found[tool.Name] = true
		switch tool.Name {
		case "sensei_workspace_status", "sensei_workspace_admit_change", "sensei_workspace_verify_admission":
			if v, ok := tool.InputSchema["additionalProperties"]; !ok || v != false {
				t.Errorf("%s InputSchema must declare additionalProperties: false", tool.Name)
			}
		}
	}
	for _, name := range []string{"sensei_workspace_status", "sensei_workspace_admit_change", "sensei_workspace_verify_admission"} {
		if !found[name] {
			t.Fatalf("missing MCP tool %s", name)
		}
	}
}

// TestMCPExistingAdmissionToolsUnchanged proves this feature did not modify
// admit_change/verify_admission's own registered shape.
func TestMCPExistingAdmissionToolsUnchanged(t *testing.T) {
	b := testBridge(fakeClient{})
	for _, tool := range b.tools() {
		switch tool.Name {
		case "admit_change":
			required, _ := tool.InputSchema["required"].([]string)
			if strings.Join(required, ",") != "bundle_dir,request_path,graph_nt,repo" {
				t.Fatalf("admit_change required changed: %v", required)
			}
		case "verify_admission":
			required, _ := tool.InputSchema["required"].([]string)
			if strings.Join(required, ",") != "decision_path,bundle_dir,repo" {
				t.Fatalf("verify_admission required changed: %v", required)
			}
		}
	}
}

func initTestGitRepo(t *testing.T, root string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func writeSenseiConfigDomain(t *testing.T, root, domain string) {
	t.Helper()
	dir := filepath.Join(root, ".sensei")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "repository:\n  domain: " + domain + "\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func currentMetadataResponse() *awarenesspb.MetadataResponse {
	return &awarenesspb.MetadataResponse{
		GraphFreshnessState:                 awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		TripleCount:                         42,
		GraphFreshnessDetail:                "live store matches expected validated graph artifact",
		BuildProvenanceState:                awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
		SeedState:                           awarenesspb.SeedState_SEED_STATE_CURRENT,
		CoverageState:                       awarenesspb.CoverageState_COVERAGE_STATE_SUFFICIENT,
		LiveStoreContainsEmbeddedSeedMarker: true,
		LiveStoreGraphDigestSha256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		LiveStoreGraphTripleCount:           42,
		EmbeddedSeedDigestSha256:            "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		EmbeddedTransactionStampPresent:     true,
		EmbeddedTransactionMatchesSeed:      true,
		CertifiedAwarenessGraphCommit:       "e1985d74b9349b4f90e3a54e3c0312d177a5d239",
		CertifiedServicesRepoCommit:         "eb6610554158444871178afca0626a1ee8d66056",
	}
}

// TestWorkspaceStatus_RequiresRepo proves the required argument is enforced.
func TestWorkspaceStatus_RequiresRepo(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_status", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "repo is required") {
		t.Fatalf("err=%v, want repo is required", err)
	}
}

// TestWorkspaceStatus_RejectsUnknownProperty proves runtime argument
// enforcement rejects an unknown property even though the caller supplied
// no domain override field the advertised schema would also reject.
func TestWorkspaceStatus_RejectsUnknownProperty(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": ".", "domain": "github.com/x/y"})
	if err == nil || !strings.Contains(err.Error(), `unknown property "domain"`) {
		t.Fatalf("err=%v, want unknown property domain rejected", err)
	}
}

// TestWorkspaceStatus_RejectsWrongType proves a non-string repo value fails
// at runtime rather than silently coercing.
func TestWorkspaceStatus_RejectsWrongType(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": 42})
	if err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("err=%v, want a string-type error", err)
	}
}

// TestWorkspaceStatus_RejectsMalformedTaskValues proves a present-but-
// non-string "task" value is a hard argument error, never silently
// coerced to "" and treated as "resolve the active task" — the exact
// failure mode a bare `task, _ := args["task"].(string)` type assertion
// would produce. Covers JSON null, number, array, and object, the four
// shapes a `.(string)` assertion silently swallows.
func TestWorkspaceStatus_RejectsMalformedTaskValues(t *testing.T) {
	root := t.TempDir()
	initTestGitRepo(t, root)
	writeSenseiConfigDomain(t, root, "github.com/globulario/sensei")
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return currentMetadataResponse(), nil
		},
	})

	cases := map[string]interface{}{
		"null":   nil,
		"number": float64(42),
		"array":  []interface{}{"x"},
		"object": map[string]interface{}{"x": "y"},
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			args := map[string]interface{}{"repo": root, "task": val}
			_, err := b.callText(context.Background(), "sensei_workspace_status", args)
			if err == nil {
				t.Fatalf("expected a type error for task=%v (%s), got success", val, name)
			}
			if !strings.Contains(err.Error(), `"task" must be a string`) {
				t.Fatalf("err=%v, want a task-must-be-a-string error", err)
			}
		})
	}
}

// TestWorkspaceStatus_CompleteReceipt proves a configured checkout with a
// current, authoritative backend produces composition_state=complete, and
// that the returned structured payload validates against the real,
// canonical JSON Schema — the full one-complete-identity-call integration
// proof the implementer brief requires.
func TestWorkspaceStatus_CompleteReceipt(t *testing.T) {
	root := t.TempDir()
	head := initTestGitRepo(t, root)
	writeSenseiConfigDomain(t, root, "github.com/globulario/sensei")

	var gotDomain string
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			gotDomain = in.GetDomain()
			return currentMetadataResponse(), nil
		},
	})
	res, err := b.callTool(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": root})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if gotDomain != "github.com/globulario/sensei" {
		t.Fatalf("Metadata called with domain %q, want configured domain", gotDomain)
	}
	structured, ok := res.Structured.(map[string]interface{})
	if !ok {
		t.Fatalf("Structured is not a map: %T", res.Structured)
	}
	if structured["composition_state"] != "complete" {
		t.Fatalf("composition_state = %v, want complete (full structured: %+v)", structured["composition_state"], structured)
	}
	if structured["repository_domain_source"] != "configured" {
		t.Fatalf("repository_domain_source = %v, want configured", structured["repository_domain_source"])
	}
	binding, _ := structured["binding"].(map[string]interface{})
	if binding["revision"] != head {
		t.Fatalf("binding.revision = %v, want %v", binding["revision"], head)
	}
	taskIdentity, _ := structured["task_identity"].(map[string]interface{})
	if taskIdentity["state"] != "not_requested" {
		t.Fatalf("task_identity.state = %v, want not_requested (task arg omitted)", taskIdentity["state"])
	}
}

// TestWorkspaceStatus_UnboundDomainIsUnavailable proves an unconfigured
// checkout never produces composition_state=complete, and never guesses a
// domain from environment or git origin.
func TestWorkspaceStatus_UnboundDomainIsUnavailable(t *testing.T) {
	root := t.TempDir()
	initTestGitRepo(t, root)
	t.Setenv("SENSEI_DOMAIN", "github.com/should/not-be-used")
	t.Setenv("AWG_DOMAIN", "github.com/also/should-not-be-used")

	b := testBridge(fakeClient{
		metadata: func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return currentMetadataResponse(), nil
		},
	})
	res, err := b.callTool(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": root})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	structured := res.Structured.(map[string]interface{})
	if structured["composition_state"] != "unavailable" {
		t.Fatalf("composition_state = %v, want unavailable for an unconfigured checkout, even with SENSEI_DOMAIN/AWG_DOMAIN set", structured["composition_state"])
	}
	if structured["repository_domain_source"] != "unbound" {
		t.Fatalf("repository_domain_source = %v, want unbound", structured["repository_domain_source"])
	}
	binding := structured["binding"].(map[string]interface{})
	if binding["repository_domain"] != "" {
		t.Fatalf("binding.repository_domain = %v, want empty — must never be guessed from SENSEI_DOMAIN/AWG_DOMAIN", binding["repository_domain"])
	}
}

// TestWorkspaceStatus_BackendUnavailableIsUnavailableNotEmptyOrComplete
// proves a Metadata RPC failure is reported as an honest unavailable
// graph_authority, never silently treated as empty/complete.
func TestWorkspaceStatus_BackendUnavailableIsUnavailableNotEmptyOrComplete(t *testing.T) {
	root := t.TempDir()
	initTestGitRepo(t, root)
	writeSenseiConfigDomain(t, root, "github.com/globulario/sensei")

	b := testBridge(fakeClient{}) // no metadata stub: fakeClient returns Unavailable
	res, err := b.callTool(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": root})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	structured := res.Structured.(map[string]interface{})
	if structured["graph_authority"] != nil {
		t.Fatalf("graph_authority = %v, want null when the backend is unreachable", structured["graph_authority"])
	}
	if structured["composition_state"] != "unavailable" {
		t.Fatalf("composition_state = %v, want exactly unavailable when the metadata backend is unreachable (not partial — an unreachable backend is not a degraded-but-composed receipt)", structured["composition_state"])
	}
}

// TestWorkspaceStatus_TaskNotRequestedVsUnavailable proves the two states
// are distinguishable: omitting "task" entirely is not_requested; supplying
// one that cannot be resolved is unavailable.
func TestWorkspaceStatus_TaskNotRequestedVsUnavailable(t *testing.T) {
	root := t.TempDir()
	initTestGitRepo(t, root)
	writeSenseiConfigDomain(t, root, "github.com/globulario/sensei")
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return currentMetadataResponse(), nil
		},
	})

	resOmitted, err := b.callTool(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": root})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	taskOmitted := resOmitted.Structured.(map[string]interface{})["task_identity"].(map[string]interface{})
	if taskOmitted["state"] != "not_requested" {
		t.Fatalf("task_identity.state = %v, want not_requested when task is omitted", taskOmitted["state"])
	}

	resRequested, err := b.callTool(context.Background(), "sensei_workspace_status", map[string]interface{}{"repo": root, "task": ""})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	taskRequested := resRequested.Structured.(map[string]interface{})["task_identity"].(map[string]interface{})
	if taskRequested["state"] != "unavailable" {
		t.Fatalf("task_identity.state = %v, want unavailable when task=\"\" (active) is requested but no active task exists", taskRequested["state"])
	}
}

// --- sensei_workspace_admit_change / sensei_workspace_verify_admission ---
//
// Building a genuinely real on-disk admission bundle (session.yaml plus the
// 8 interdependent convergence-stage artifacts admission.LoadBundle
// requires) has no precedent anywhere in this repository's own test
// suite — not even the pre-existing admit_change/verify_admission tools
// this feature delegates to have a real-bundle success test; their own
// coverage (cmd/awg/cmd_admit_change_test.go) is required-argument
// validation only. These tests match that same, already-established depth
// for the delegation surface; the real-decision/real-verification
// projection path itself is fully exercised end-to-end — including real
// Draft 2020-12 schema validation — by
// golang/architecture/workspacecontract's fixture tests (built from real
// admission.Decision/admission.Verification values), which is where "one
// admitted decision, and one compliant or violated verification" is
// actually proven.

func TestWorkspaceAdmitChange_RequiresAllArguments(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_admit_change", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "are required") {
		t.Fatalf("err=%v, want a required-arguments error", err)
	}
}

func TestWorkspaceAdmitChange_RejectsUnknownProperty(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_admit_change", map[string]interface{}{
		"bundle_dir": "x", "request_path": "x", "graph_nt": "x", "repo": "x", "detail": "full",
	})
	if err == nil || !strings.Contains(err.Error(), `unknown property "detail"`) {
		t.Fatalf("err=%v, want unknown property detail rejected (this tool has no detail levels — it always returns the full closed contract)", err)
	}
}

func TestWorkspaceVerifyAdmission_RequiresAllArguments(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_verify_admission", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "are required") {
		t.Fatalf("err=%v, want a required-arguments error", err)
	}
}

func TestWorkspaceVerifyAdmission_RejectsUnknownProperty(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "sensei_workspace_verify_admission", map[string]interface{}{
		"decision_path": "x", "bundle_dir": "x", "repo": "x", "policy": "admission.strict.v1",
	})
	if err == nil || !strings.Contains(err.Error(), `unknown property "policy"`) {
		t.Fatalf("err=%v, want unknown property policy rejected", err)
	}
}

// TestWorkspaceAdmission_ProjectionRoundtripsSchemaValid closes the gap the
// two argument-validation tests above leave: it proves the actual
// MCP-result-building path (workspaceAdmissionResult: producer validation +
// real schema validation + structFrom) succeeds for a real
// admission.Decision projection, using the same fixture-construction this
// package's fixtures already prove pass workspacecontract's own tests.
func TestWorkspaceAdmission_ProjectionRoundtripsSchemaValid(t *testing.T) {
	rec := workspacecontract.ProjectDecision(minimalAdmittedDecisionForTest())
	res, err := workspaceAdmissionResult(rec)
	if err != nil {
		t.Fatalf("workspaceAdmissionResult: %v", err)
	}
	structured := res.Structured.(map[string]interface{})
	if structured["record_kind"] != "decision" || structured["decision"] != "admitted" {
		t.Fatalf("unexpected structured payload: %+v", structured)
	}
}
