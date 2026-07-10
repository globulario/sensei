// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBuildContractBootstrapDerivesFilesFromTestsAndIssue(t *testing.T) {
	root := t.TempDir()
	mustWriteBootstrapFile(t, root, "pkg/cmd/demo/list.go", `package demo

func listRun() string {
	return "viewer.gists"
}
`)
	mustWriteBootstrapFile(t, root, "pkg/cmd/demo/list_test.go", `package demo

import "testing"

func Test_listRun(t *testing.T) {}
`)
	mustWriteBootstrapFile(t, root, "pkg/cmd/demo/other.go", `package demo

func other() {}
`)

	res, err := buildContractBootstrap(root, "", "", bootstrapTask{
		Issue:    "The command must use viewer.gists and preserve listRun visibility behavior.",
		F2PTests: []string{"Test_listRun"},
	}, "test")
	if err != nil {
		t.Fatalf("buildContractBootstrap: %v", err)
	}

	if res.AWGStatus != "AWG-unavailable" {
		t.Fatalf("AWGStatus = %q, want AWG-unavailable", res.AWGStatus)
	}
	if len(res.LikelyImplementationFiles) == 0 || res.LikelyImplementationFiles[0] != "pkg/cmd/demo/list.go" {
		t.Fatalf("LikelyImplementationFiles = %v, want pkg/cmd/demo/list.go first", res.LikelyImplementationFiles)
	}
	if len(res.LikelyProvingTests) != 1 || res.LikelyProvingTests[0] != "pkg/cmd/demo/list_test.go" {
		t.Fatalf("LikelyProvingTests = %v, want pkg/cmd/demo/list_test.go", res.LikelyProvingTests)
	}
	if res.ContractStatus != "proposed" {
		t.Fatalf("ContractStatus = %q, want proposed", res.ContractStatus)
	}
	if res.ProofStatus != "proposed" {
		t.Fatalf("ProofStatus = %q, want proposed", res.ProofStatus)
	}
	if !res.ProofRequiredProposed {
		t.Fatalf("ProofRequiredProposed = false, want true")
	}
	if len(res.RequiredTestPathsProposed) != 1 || res.RequiredTestPathsProposed[0] != "pkg/cmd/demo/list_test.go" {
		t.Fatalf("RequiredTestPathsProposed = %v, want pkg/cmd/demo/list_test.go", res.RequiredTestPathsProposed)
	}
	if len(res.RequiredTestSymbolsProposed) != 1 || res.RequiredTestSymbolsProposed[0] != "Test_listRun" {
		t.Fatalf("RequiredTestSymbolsProposed = %v, want Test_listRun", res.RequiredTestSymbolsProposed)
	}
	if !res.PromotionRequired {
		t.Fatalf("PromotionRequired = false, want true")
	}
	if !containsString(res.CandidateFiles, "pkg/cmd/demo/list.go") {
		t.Fatalf("CandidateFiles = %v, want pkg/cmd/demo/list.go", res.CandidateFiles)
	}
	if len(res.MechanicalEvidence) != 1 {
		t.Fatalf("MechanicalEvidence = %v, want 1 entry", res.MechanicalEvidence)
	}
	if got := res.MechanicalEvidence[0]; got.File != "pkg/cmd/demo/list_test.go" || got.Line == 0 {
		t.Fatalf("MechanicalEvidence[0] = %+v, want test file with line", got)
	}
	if len(res.ProofProvenance) != 2 {
		t.Fatalf("ProofProvenance = %v, want 2 entries", res.ProofProvenance)
	}
	if got := res.ProofProvenance[0]; got.Source != "fail_to_pass_tests" || got.Confidence != "high" {
		t.Fatalf("ProofProvenance[0] = %+v, want fail_to_pass_tests/high", got)
	}
	if got := res.ProofProvenance[1]; got.Source != "test_file_matching" || got.Confidence != "medium" {
		t.Fatalf("ProofProvenance[1] = %+v, want test_file_matching/medium", got)
	}
	if res.ContractScaffold.ContractSetVersion != 1 {
		t.Fatalf("ContractScaffold.ContractSetVersion = %d, want 1", res.ContractScaffold.ContractSetVersion)
	}
	if len(res.ContractScaffold.Contracts) != 1 {
		t.Fatalf("ContractScaffold.Contracts = %v, want 1 contract", res.ContractScaffold.Contracts)
	}
	if got := res.ContractScaffold.Contracts[0].RequiredScope.Files; len(got) != 1 || got[0] != "pkg/cmd/demo/list.go" {
		t.Fatalf("ContractScaffold required_scope.files = %v, want pkg/cmd/demo/list.go", got)
	}
	if got := res.ContractScaffold.Contracts[0].RequiredTestPaths; len(got) != 1 || got[0] != "pkg/cmd/demo/list_test.go" {
		t.Fatalf("ContractScaffold required_test_paths = %v, want pkg/cmd/demo/list_test.go", got)
	}
}

func TestBuildContractBootstrap_MarksBackendDownDistinctly(t *testing.T) {
	root := t.TempDir()
	mustWriteBootstrapFile(t, root, "pkg/cmd/demo/list.go", "package demo\n\nfunc listRun() string {\n\treturn \"viewer.gists\"\n}\n")
	mustWriteBootstrapFile(t, root, "pkg/cmd/demo/list_test.go", "package demo\n\nimport \"testing\"\n\nfunc Test_listRun(t *testing.T) {}\n")

	prev := contractBootstrapConnectAWG
	contractBootstrapConnectAWG = func(string) (*client.Client, error) {
		return nil, fmt.Errorf("dial tcp 127.0.0.1:10120: connection refused")
	}
	defer func() { contractBootstrapConnectAWG = prev }()

	res, err := buildContractBootstrap(root, "127.0.0.1:10120", "", bootstrapTask{
		Issue:    "The command must use viewer.gists and preserve listRun visibility behavior.",
		F2PTests: []string{"Test_listRun"},
	}, "test")
	if err != nil {
		t.Fatalf("buildContractBootstrap: %v", err)
	}
	if res.AWGStatus != "AWG-down" {
		t.Fatalf("AWGStatus = %q, want AWG-down", res.AWGStatus)
	}
	if len(res.BlindSpots) == 0 || !strings.Contains(res.BlindSpots[0], "not a no-guidance result") {
		t.Fatalf("BlindSpots = %v, want explicit unreachable warning", res.BlindSpots)
	}
}

func TestRenderContractBootstrapPromptIncludesAWGCrossRefs(t *testing.T) {
	out := renderContractBootstrapPrompt(bootstrapResult{
		AWGStatus:                 "available",
		ContractStatus:            "proposed",
		ProofStatus:               "proposed",
		LikelyImplementationFiles: []string{"a.go"},
		LikelyProvingTests:        []string{"a_test.go"},
		CandidateFiles:            []string{"a.go", "helper.go"},
		MechanicalEvidence: []bootstrapEvidence{{
			File: "a_test.go",
			Line: 7,
			Text: "func TestThing(t *testing.T) {}",
		}},
		AWGAnchors: []string{"invariant:x", "failure_mode:y"},
		RequiredActions: []string{
			"Repair plan: globular.repair.example",
		},
		ForbiddenFixes:              []string{"Do not mask nil state as success"},
		TestsToRun:                  []string{"TestThing"},
		ProofRequiredProposed:       true,
		RequiredTestPathsProposed:   []string{"a_test.go"},
		RequiredTestSymbolsProposed: []string{"TestThing"},
		ProofProvenance: []bootstrapProofProvenance{{
			Source:     "fail_to_pass_tests",
			Confidence: "high",
			Evidence:   "task listed failing tests",
		}},
		PromotionRequired: true,
	})
	for _, want := range []string{
		"## Proposed repair-contract bootstrap (tool-level)",
		"AWG status: available",
		"Contract status: proposed",
		"Proof status: proposed",
		"AWG anchors consulted:",
		"invariant:x",
		"Repair plan: globular.repair.example",
		"Do not mask nil state as success",
		"TestThing",
		"Advisory proof obligations proposed by bootstrap:",
		"required_test_paths_proposed:",
		"required_test_symbols_proposed:",
		"promotion_required: true",
		"advisory only: proposed proof does not affect contract_clean until frozen into authoritative contract fields",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q\n%s", want, out)
		}
	}
}

func TestBuildBootstrapContractScaffoldSplitsAWGAnchors(t *testing.T) {
	res := bootstrapResult{
		Domain:                      "github.com/cli/cli",
		LikelyImplementationFiles:   []string{"command/repo.go"},
		LikelyProvingTests:          []string{"command/repo_test.go"},
		AWGAnchors:                  []string{"component:component.command.repo", "invariant:repo.non_tty_scriptability", "meta_principle:meta.signal_over_noise", "failure_mode:repo.output_noise", "intent:repo.cli_tty_behavior", "test:repo:TestRepoForkNonTTY"},
		ProofRequiredProposed:       true,
		RequiredTestPathsProposed:   []string{"command/repo_test.go"},
		RequiredTestSymbolsProposed: []string{"TestRepoForkNonTTY"},
		ProofProvenance:             []bootstrapProofProvenance{{Source: "fail_to_pass_tests", Confidence: "high", Evidence: "task listed failing tests"}},
		PromotionRequired:           true,
	}

	got := buildBootstrapContractScaffold(bootstrapTask{
		InstanceID: "cli__cli-1388",
		Domain:     "github.com/cli/cli",
	}, res)

	if got.TaskID != "cli__cli-1388" {
		t.Fatalf("TaskID = %q, want cli__cli-1388", got.TaskID)
	}
	if got.Repo != "github.com/cli/cli" {
		t.Fatalf("Repo = %q, want github.com/cli/cli", got.Repo)
	}
	if len(got.Contracts) != 1 {
		t.Fatalf("Contracts = %v, want 1 contract", got.Contracts)
	}
	c := got.Contracts[0]
	if c.ID != "contract.bootstrap.cli.cli.1388" {
		t.Fatalf("ID = %q, want contract.bootstrap.cli.cli.1388", c.ID)
	}
	if !containsString(c.Invariants, "repo.non_tty_scriptability") || !containsString(c.Invariants, "meta.signal_over_noise") {
		t.Fatalf("Invariants = %v, want repo/non_tty + meta.signal_over_noise", c.Invariants)
	}
	if !containsString(c.FailureModes, "repo.output_noise") {
		t.Fatalf("FailureModes = %v, want repo.output_noise", c.FailureModes)
	}
	if !containsString(c.Intents, "repo.cli_tty_behavior") {
		t.Fatalf("Intents = %v, want repo.cli_tty_behavior", c.Intents)
	}
	if !containsString(c.RequiredTests, "repo:TestRepoForkNonTTY") {
		t.Fatalf("RequiredTests = %v, want repo:TestRepoForkNonTTY", c.RequiredTests)
	}
	if !containsString(c.Components, "component.command.repo") {
		t.Fatalf("Components = %v, want component.command.repo", c.Components)
	}
	if !containsString(c.AWGAnchors, "invariant:repo.non_tty_scriptability") {
		t.Fatalf("AWGAnchors = %v, want invariant:repo.non_tty_scriptability", c.AWGAnchors)
	}
	if !c.ProofRequired {
		t.Fatalf("ProofRequired = false, want true")
	}
}

func TestEnrichBootstrapWithAWG_RequiresAuthoritativePreflight(t *testing.T) {
	res := bootstrapResult{}
	client := fakeBootstrapAWGClient{
		preflightResp: &awarenesspb.PreflightResponse{
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       false,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE,
			},
			RequiredActions: []string{"should not survive"},
		},
	}

	err := enrichBootstrapWithAWG(context.Background(), client, bootstrapTask{Issue: "repair task"}, "", []string{"a.go"}, []string{"a.go"}, &res)
	if err == nil {
		t.Fatalf("enrichBootstrapWithAWG error = nil, want authority failure")
	}
	if res.AWGStatus != "AWG-non-authoritative" {
		t.Fatalf("AWGStatus = %q, want AWG-non-authoritative", res.AWGStatus)
	}
	if len(res.RequiredActions) != 0 {
		t.Fatalf("RequiredActions = %v, want empty on non-authoritative preflight", res.RequiredActions)
	}
}

func TestEnrichBootstrapWithAWG_DistinguishesBackendDownFromNoGuidance(t *testing.T) {
	res := bootstrapResult{}
	client := fakeBootstrapAWGClient{
		preflightErr: status.Error(codes.Unavailable, "connection refused"),
	}

	err := enrichBootstrapWithAWG(context.Background(), client, bootstrapTask{Issue: "repair task"}, "", []string{"a.go"}, []string{"a.go"}, &res)
	if err == nil {
		t.Fatalf("enrichBootstrapWithAWG error = nil, want backend-down failure")
	}
	if res.AWGStatus != "AWG-down" {
		t.Fatalf("AWGStatus = %q, want AWG-down", res.AWGStatus)
	}
	for _, want := range []string{
		"backend unreachable",
		"not a no-guidance result",
		"connection refused",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err=%q missing %q", err.Error(), want)
		}
	}
}

func TestEnrichBootstrapWithAWG_DropsImpactEvidenceOnNonAuthoritativeImpact(t *testing.T) {
	res := bootstrapResult{}
	client := fakeBootstrapAWGClient{
		preflightResp: &awarenesspb.PreflightResponse{
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       true,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			},
			RequiredActions: []string{"read a.go"},
			FilesToRead:     []string{"a.go"},
		},
		impactByFile: map[string]*awarenesspb.ImpactResponse{
			"a.go": {
				Authority: &awarenesspb.GraphAuthority{
					Authoritative:       false,
					GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNKNOWN,
				},
				DirectInvariants: []*awarenesspb.KnowledgeNode{{Id: "should.not.leak"}},
			},
		},
	}

	err := enrichBootstrapWithAWG(context.Background(), client, bootstrapTask{Issue: "repair task"}, "", []string{"a.go"}, []string{"a.go"}, &res)
	if err == nil {
		t.Fatalf("enrichBootstrapWithAWG error = nil, want authority failure")
	}
	if res.AWGStatus != "AWG-non-authoritative" {
		t.Fatalf("AWGStatus = %q, want AWG-non-authoritative", res.AWGStatus)
	}
	if len(res.AWGFiles) != 0 {
		t.Fatalf("AWGFiles = %v, want empty on non-authoritative impact", res.AWGFiles)
	}
	if len(res.AWGAnchors) != 0 {
		t.Fatalf("AWGAnchors = %v, want empty on non-authoritative impact", res.AWGAnchors)
	}
	if len(res.RequiredActions) != 1 || res.RequiredActions[0] != "read a.go" {
		t.Fatalf("RequiredActions = %v, want authoritative preflight guidance preserved", res.RequiredActions)
	}
}

func TestEnrichBootstrapWithAWG_PopulatesAuthoritativeEvidence(t *testing.T) {
	res := bootstrapResult{}
	client := fakeBootstrapAWGClient{
		preflightResp: &awarenesspb.PreflightResponse{
			Authority: &awarenesspb.GraphAuthority{
				Authoritative:       true,
				GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
			},
			RequiredActions: []string{"read a.go"},
			ForbiddenFixes:  []string{"do not fake success"},
			TestsToRun:      []string{"TestThing"},
			FilesToRead:     []string{"a.go"},
			BlindSpots:      []string{"none"},
		},
		impactByFile: map[string]*awarenesspb.ImpactResponse{
			"a.go": {
				Authority: &awarenesspb.GraphAuthority{
					Authoritative:       true,
					GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
				},
				DirectArchitecture: []*awarenesspb.KnowledgeNode{{Id: "component.demo"}},
				DirectInvariants:   []*awarenesspb.KnowledgeNode{{Id: "demo.invariant"}},
				RequiredTests:      []*awarenesspb.KnowledgeNode{{Id: "demo:TestThing"}},
			},
		},
	}

	if err := enrichBootstrapWithAWG(context.Background(), client, bootstrapTask{Issue: "repair task"}, "", []string{"a.go"}, []string{"a.go"}, &res); err != nil {
		t.Fatalf("enrichBootstrapWithAWG: %v", err)
	}
	if res.AWGStatus != "AWG-authoritative" {
		t.Fatalf("AWGStatus = %q, want AWG-authoritative", res.AWGStatus)
	}
	if len(res.RequiredActions) != 1 || res.RequiredActions[0] != "read a.go" {
		t.Fatalf("RequiredActions = %v, want authoritative preflight guidance", res.RequiredActions)
	}
	if len(res.AWGFiles) != 1 || res.AWGFiles[0].File != "a.go" {
		t.Fatalf("AWGFiles = %v, want a.go entry", res.AWGFiles)
	}
	if !containsString(res.AWGAnchors, "component:component.demo") || !containsString(res.AWGAnchors, "invariant:demo.invariant") || !containsString(res.AWGAnchors, "test:demo:TestThing") {
		t.Fatalf("AWGAnchors = %v, want component/invariant/test anchors", res.AWGAnchors)
	}
}

func TestRenderContractBootstrapPrompt_StatesBackendDownExplicitly(t *testing.T) {
	out := renderContractBootstrapPrompt(bootstrapResult{
		AWGStatus:      "AWG-down",
		ContractStatus: "proposed",
		ProofStatus:    "proposed",
		BlindSpots: []string{
			"awareness-graph backend unreachable; AWG cross-reference was not obtained and this is not a no-guidance result: connection refused",
		},
	})
	for _, want := range []string{
		"AWG status: AWG-down",
		"The awareness-graph backend was unreachable.",
		"AWG blind spots:",
		"not a no-guidance result",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q\n%s", want, out)
		}
	}
}

type fakeBootstrapAWGClient struct {
	preflightResp   *awarenesspb.PreflightResponse
	preflightErr    error
	impactByFile    map[string]*awarenesspb.ImpactResponse
	impactErrByFile map[string]error
}

func (f fakeBootstrapAWGClient) Preflight(context.Context, *awarenesspb.PreflightRequest, ...grpc.CallOption) (*awarenesspb.PreflightResponse, error) {
	return f.preflightResp, f.preflightErr
}

func (f fakeBootstrapAWGClient) Impact(_ context.Context, req *awarenesspb.ImpactRequest, _ ...grpc.CallOption) (*awarenesspb.ImpactResponse, error) {
	if err := f.impactErrByFile[req.GetFile()]; err != nil {
		return nil, err
	}
	if resp, ok := f.impactByFile[req.GetFile()]; ok {
		return resp, nil
	}
	return &awarenesspb.ImpactResponse{
		Authority: &awarenesspb.GraphAuthority{
			Authoritative:       true,
			GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		},
	}, nil
}

func mustWriteBootstrapFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", rel, err)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
