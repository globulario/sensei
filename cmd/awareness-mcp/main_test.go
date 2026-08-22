// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeClient struct {
	briefing  func(context.Context, *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error)
	impact    func(context.Context, *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error)
	resolve   func(context.Context, *awarenesspb.ResolveRequest) (*awarenesspb.ResolveResponse, error)
	query     func(context.Context, *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error)
	metadata  func(context.Context, *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error)
	preflight func(context.Context, *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error)
	editCheck func(context.Context, *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error)
	propose   func(context.Context, *awarenesspb.ProposeRequest) (*awarenesspb.ProposeResponse, error)
}

func (f fakeClient) Briefing(ctx context.Context, in *awarenesspb.BriefingRequest, _ ...grpc.CallOption) (*awarenesspb.BriefingResponse, error) {
	if f.briefing == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.briefing(ctx, in)
}
func (f fakeClient) Impact(ctx context.Context, in *awarenesspb.ImpactRequest, _ ...grpc.CallOption) (*awarenesspb.ImpactResponse, error) {
	if f.impact == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.impact(ctx, in)
}
func (f fakeClient) Resolve(ctx context.Context, in *awarenesspb.ResolveRequest, _ ...grpc.CallOption) (*awarenesspb.ResolveResponse, error) {
	if f.resolve == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.resolve(ctx, in)
}
func (f fakeClient) Query(ctx context.Context, in *awarenesspb.QueryRequest, _ ...grpc.CallOption) (*awarenesspb.QueryResponse, error) {
	if f.query == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.query(ctx, in)
}
func (f fakeClient) Metadata(ctx context.Context, in *awarenesspb.MetadataRequest, _ ...grpc.CallOption) (*awarenesspb.MetadataResponse, error) {
	if f.metadata == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.metadata(ctx, in)
}
func (f fakeClient) Preflight(ctx context.Context, in *awarenesspb.PreflightRequest, _ ...grpc.CallOption) (*awarenesspb.PreflightResponse, error) {
	if f.preflight == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.preflight(ctx, in)
}
func (f fakeClient) EditCheck(ctx context.Context, in *awarenesspb.EditCheckRequest, _ ...grpc.CallOption) (*awarenesspb.EditCheckResponse, error) {
	if f.editCheck == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.editCheck(ctx, in)
}
func (f fakeClient) Propose(ctx context.Context, in *awarenesspb.ProposeRequest, _ ...grpc.CallOption) (*awarenesspb.ProposeResponse, error) {
	if f.propose == nil {
		return nil, status.Error(codes.Unavailable, "no stub")
	}
	return f.propose(ctx, in)
}

func testBridge(c awarenessClient) *bridge {
	return newBridge(c, 5*time.Second, 2*time.Minute)
}

func TestTaskControlToolsAreExposedWithTypedContracts(t *testing.T) {
	tools := testBridge(fakeClient{}).tools()
	found := map[string]bool{}
	for _, tool := range tools {
		found[tool.Name] = true
	}
	for _, name := range []string{"task_status", "advance_task", "task_briefing"} {
		if !found[name] {
			t.Fatalf("missing MCP tool %s", name)
		}
	}
}

// callText runs a tool and returns just the human text block — the shape most
// existing tests assert on. Structured-payload tests call callTool directly.
func (b *bridge) callText(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	res, err := b.callTool(ctx, name, args)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

func testCurrentAuthority(commit string) *awarenesspb.GraphAuthority {
	return &awarenesspb.GraphAuthority{
		Authoritative:                   true,
		GraphFreshnessState:             awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		BuildProvenanceState:            awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
		SeedState:                       awarenesspb.SeedState_SEED_STATE_CURRENT,
		SourceRepoCommit:                commit,
		EmbeddedSeedDigestSha256:        "seed123",
		LiveStoreGraphDigestSha256:      "live123",
		LiveStoreGraphTripleCount:       42,
		EmbeddedTransactionStampPresent: true,
		EmbeddedTransactionMatchesSeed:  true,
		CertifiedAwarenessGraphCommit:   "awg456",
		CertifiedServicesRepoCommit:     "svc789",
		EmbeddedTransactionDetail:       "embedded transaction certifies embedded seed",
	}
}

// testGitHEAD resolves the current HEAD commit SHA for tests that need expected_head.
func testGitHEAD(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skipf("cannot resolve git HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestBriefingTool_ValidatesMissingFile(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "awareness_briefing", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "file is required") {
		t.Fatalf("err=%v", err)
	}
}

func TestBriefingTool_MapsOKResponse(t *testing.T) {
	b := testBridge(fakeClient{
		briefing: func(_ context.Context, in *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
			return &awarenesspb.BriefingResponse{
				Status:        awarenesspb.BriefingStatus_BRIEFING_STATUS_OK,
				ReferencedIds: []string{"invariant:x"},
				Prose:         "Awareness briefing for a.go",
				GeneratedInMs: 12,
				Authority:     testCurrentAuthority(""),
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_briefing", map[string]interface{}{"file": "a.go"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out, "status: BRIEFING_STATUS_OK") || !strings.Contains(out, "invariant:x") {
		t.Fatalf("out=%q", out)
	}
	// Text carries the ONE-LINE authority (full provenance moved to structuredContent).
	if !strings.Contains(out, "authority: authoritative (current)") {
		t.Fatalf("one-line authority missing from output: %q", out)
	}
}

func TestBriefingTool_MapsEmptyClearly(t *testing.T) {
	b := testBridge(fakeClient{
		briefing: func(_ context.Context, _ *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
			return &awarenesspb.BriefingResponse{Status: awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_briefing", map[string]interface{}{"file": "a.go"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(strings.ToLower(out), "no direct awareness anchors found") {
		t.Fatalf("out=%q", out)
	}
}

func TestImpactTool_ValidatesMissingFile(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "awareness_impact", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "file is required") {
		t.Fatalf("err=%v", err)
	}
}

func TestImpactTool_FormatsAuthority(t *testing.T) {
	b := testBridge(fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{
				DirectInvariants: []*awarenesspb.KnowledgeNode{{Id: "x", Class: "invariant"}},
				Authority:        testCurrentAuthority(""),
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_impact", map[string]interface{}{"file": "x.go"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(out, "authority: authoritative (current)") {
		t.Fatalf("one-line authority missing from impact output: %q", out)
	}
	// Impact text now lists the REAL node (id + label), not just a count.
	if !strings.Contains(out, "invariant:x") {
		t.Fatalf("impact must list the node id, not only counts: %q", out)
	}
}

func TestResolveTool_ValidatesMissingClassID(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "awareness_resolve", map[string]interface{}{"class": "invariant"})
	if err == nil || !strings.Contains(err.Error(), "class and id are required") {
		t.Fatalf("err=%v", err)
	}
}

func TestToolCall_MapsGRPCErrorsExplicitly(t *testing.T) {
	b := testBridge(fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return nil, errors.New("backend unavailable")
		},
	})
	_, err := b.callText(context.Background(), "awareness_impact", map[string]interface{}{"file": "x.go"})
	if err == nil || !strings.Contains(err.Error(), "impact rpc") {
		t.Fatalf("err=%v", err)
	}
}

func TestToolCall_DistinguishesBackendUnreachableFromNoGuidance(t *testing.T) {
	b := testBridge(fakeClient{
		briefing: func(_ context.Context, _ *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
			return nil, status.Error(codes.Unavailable, "connection refused")
		},
	})
	_, err := b.callText(context.Background(), "awareness_briefing", map[string]interface{}{"file": "x.go"})
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"briefing unavailable",
		"awareness-graph backend is unreachable",
		"not an empty/no-guidance result",
		"connection refused",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err=%q missing %q", err.Error(), want)
		}
	}
}

// TestQueryTool_TypedModesOnly pins the no_arbitrary_sparql contract in its
// current form: awareness_query IS registered (QueryRequest reserved the
// sparql field at the proto layer, so the typed API is the only shape that
// exists), but the bridge rejects any mode outside the enum — free-form
// query text has no path to the store.
func TestQueryTool_TypedModesOnly(t *testing.T) {
	b := testBridge(fakeClient{})
	found := false
	for _, tdef := range b.tools() {
		if tdef.Name == "awareness_query" {
			found = true
		}
	}
	if !found {
		t.Fatalf("awareness_query (typed) should be registered")
	}
	for _, bad := range []string{"", "sparql", "SELECT ?s WHERE { ?s ?p ?o }"} {
		_, err := b.callText(context.Background(), "awareness_query", map[string]interface{}{"mode": bad})
		if err == nil || !strings.Contains(err.Error(), "mode must be one of") {
			t.Fatalf("mode=%q should be rejected, err=%v", bad, err)
		}
	}
}

func TestQueryTool_ValidatesModeArgs(t *testing.T) {
	b := testBridge(fakeClient{})
	cases := []struct {
		args map[string]interface{}
		want string
	}{
		{map[string]interface{}{"mode": "by_file"}, "file is required"},
		{map[string]interface{}{"mode": "by_id"}, "id is required"},
		{map[string]interface{}{"mode": "related"}, "id is required"},
		{map[string]interface{}{"mode": "by_class"}, "class is required"},
		{map[string]interface{}{"mode": "by_class", "class": "bogus"}, "class is required"},
	}
	for _, tc := range cases {
		_, err := b.callText(context.Background(), "awareness_query", tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("args=%v want %q, err=%v", tc.args, tc.want, err)
		}
	}
}

func TestQueryTool_MapsRequestAndFormatsRows(t *testing.T) {
	var got *awarenesspb.QueryRequest
	b := testBridge(fakeClient{
		query: func(_ context.Context, in *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error) {
			got = in
			return &awarenesspb.QueryResponse{
				Rows: []*awarenesspb.QueryRow{
					{Id: "invariant:x", Class: "invariant", Label: "X invariant", Severity: "critical"},
				},
				GeneratedInMs: 3,
				Authority:     testCurrentAuthority(""),
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_query", map[string]interface{}{
		"mode": "by_class", "class": "contract", "limit": float64(10), "domain": "github.com/globulario/sensei",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.GetMode() != awarenesspb.QueryMode_QUERY_MODE_BY_CLASS ||
		got.GetClass() != awarenesspb.QueryClass_QUERY_CLASS_CONTRACT ||
		got.GetLimit() != 10 ||
		got.GetDomain() != "github.com/globulario/sensei" {
		t.Fatalf("request=%v", got)
	}
	if !strings.Contains(out, "rows: 1") || !strings.Contains(out, "invariant:x") || !strings.Contains(out, "critical") {
		t.Fatalf("out=%q", out)
	}
	if !strings.Contains(out, "authority: authoritative (current)") {
		t.Fatalf("one-line authority missing from query output: %q", out)
	}
}

func TestQueryTool_ExposesAllTypedProtoClasses(t *testing.T) {
	for _, class := range []string{
		"invariant",
		"failure_mode",
		"incident_pattern",
		"intent",
		"symbol",
		"source_file",
		"code_symbol",
		"forbidden_fix",
		"test",
		"meta_principle",
		"component",
		"boundary",
		"contract",
		"decision",
		"evidence",
		"design_pattern",
		"implementation_pattern",
		"pattern_misuse",
		"architecture_claim",
		"open_question",
		"architect_answer",
		"evidence_probe",
	} {
		if _, err := queryClassFromString(class); err != nil {
			t.Fatalf("query class %s not accepted: %v", class, err)
		}
	}

	b := testBridge(fakeClient{})
	var enumValues []string
	for _, tdef := range b.tools() {
		if tdef.Name != "awareness_query" {
			continue
		}
		props := tdef.InputSchema["properties"].(map[string]interface{})
		classProp := props["class"].(map[string]interface{})
		enumValues = classProp["enum"].([]string)
		if _, ok := props["domain"]; !ok {
			t.Fatal("awareness_query schema missing domain")
		}
		break
	}
	if len(enumValues) == 0 {
		t.Fatal("awareness_query schema class enum missing")
	}
	if len(enumValues) != len(mcpQueryClasses) {
		t.Fatalf("schema enum length=%d, want %d", len(enumValues), len(mcpQueryClasses))
	}
}

func TestQueryClassFromStringArchitectureClaim(t *testing.T) {
	got, err := queryClassFromString("architecture_claim")
	if err != nil {
		t.Fatalf("queryClassFromString: %v", err)
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_ARCHITECTURE_CLAIM {
		t.Fatalf("class=%s, want ARCHITECTURE_CLAIM", got)
	}
}

func TestMCPQueryClassOpenQuestion(t *testing.T) {
	got, err := queryClassFromString("open_question")
	if err != nil {
		t.Fatalf("queryClassFromString: %v", err)
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_OPEN_QUESTION {
		t.Fatalf("class=%s, want OPEN_QUESTION", got)
	}
}

func TestMCPQueryClassArchitectAnswer(t *testing.T) {
	got, err := queryClassFromString("architect_answer")
	if err != nil {
		t.Fatalf("queryClassFromString: %v", err)
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_ARCHITECT_ANSWER {
		t.Fatalf("class=%s, want ARCHITECT_ANSWER", got)
	}
}

func TestMCPQueryClassEvidenceProbe(t *testing.T) {
	got, err := queryClassFromString("evidence_probe")
	if err != nil {
		t.Fatalf("queryClassFromString: %v", err)
	}
	if got != awarenesspb.QueryClass_QUERY_CLASS_EVIDENCE_PROBE {
		t.Fatalf("class=%s, want EVIDENCE_PROBE", got)
	}
}

func TestMetadataTool_FormatsCounts(t *testing.T) {
	var got *awarenesspb.MetadataRequest
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, in *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			got = in
			return &awarenesspb.MetadataResponse{
				ServerVersion:                       "1.2.3",
				TripleCount:                         12062,
				InvariantCount:                      40,
				ArchitectureClaimCount:              6,
				OpenQuestionCount:                   2,
				ArchitectAnswerCount:                3,
				EvidenceProbeCount:                  4,
				EmbeddedSeedDigestSha256:            "abc123",
				EmbeddedSeedMarkerIri:               "https://globular.io/awareness#seedBuild/sha256-abc123",
				LiveStoreContainsEmbeddedSeedMarker: true,
				BuildProvenanceState:                awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
				CoverageState:                       awarenesspb.CoverageState_COVERAGE_STATE_SUFFICIENT,
				SeedState:                           awarenesspb.SeedState_SEED_STATE_CURRENT,
				CandidateQueueState:                 awarenesspb.CandidateQueueState_CANDIDATE_QUEUE_STATE_PRESENT,
				LocalCandidateFileCount:             2,
				LocalCandidateEntryCount:            5,
				BenchmarkState:                      awarenesspb.BenchmarkState_BENCHMARK_STATE_PRESENT,
				BenchmarkContractCount:              8,
				BenchmarkLearningEventCount:         12,
				BenchmarkLatestLearningEventUnix:    1718790863,
				BenchmarkLatestTaskId:               "cli__cli-1388",
				BenchmarkLatestScore:                100,
				BenchmarkLatestCertificationStatus:  "certified_clean_repair",
				BriefingCallCount:                   7,
				BriefingAgentCompactCount:           5,
				ResolveCallCount:                    11,
				ResolveFoundCount:                   8,
				ResolveMissCount:                    3,
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_metadata", map[string]interface{}{"domain": "github.com/globulario/sensei"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.GetDomain() != "github.com/globulario/sensei" {
		t.Fatalf("metadata domain not mapped: %q", got.GetDomain())
	}
	if !strings.Contains(out, "server_version: 1.2.3") || !strings.Contains(out, "triple_count: 12062") || !strings.Contains(out, "invariant_count: 40") {
		t.Fatalf("out=%q", out)
	}
	for _, want := range []string{
		"graph_authority.verdict: authoritative",
		"graph_authority.state: current",
		"embedded_seed_digest_sha256: abc123",
		"embedded_seed_marker_iri: https://globular.io/awareness#seedBuild/sha256-abc123",
		"live_store_contains_embedded_seed_marker: true",
		"build_provenance_state: BUILD_PROVENANCE_STATE_STAMPED",
		"coverage_state: COVERAGE_STATE_SUFFICIENT",
		"seed_state: SEED_STATE_CURRENT",
		"graph_freshness_state: GRAPH_FRESHNESS_STATE_CURRENT",
		"architecture_claim_count: 6",
		"open_question_count: 2",
		"architect_answer_count: 3",
		"evidence_probe_count: 4",
		"candidate_queue_state: CANDIDATE_QUEUE_STATE_PRESENT",
		"local_candidate_file_count: 2",
		"local_candidate_entry_count: 5",
		"benchmark_state: BENCHMARK_STATE_PRESENT",
		"benchmark_contract_count: 8",
		"benchmark_learning_event_count: 12",
		"benchmark_latest_task_id: cli__cli-1388",
		"benchmark_latest_score: 100",
		"benchmark_latest_certification_status: certified_clean_repair",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out=%q missing %q", out, want)
		}
	}
	for _, want := range []string{
		"briefing_call_count: 7",
		"briefing_agent_compact_count: 5",
		"resolve_call_count: 11",
		"resolve_found_count: 8",
		"resolve_miss_count: 3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out=%q missing %q", out, want)
		}
	}
}

func TestMetadataTool_MarksStaleGraphNonAuthoritative(t *testing.T) {
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return &awarenesspb.MetadataResponse{
				TripleCount:                         12062,
				EmbeddedSeedDigestSha256:            "abc123",
				BuildProvenanceState:                awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
				SeedState:                           awarenesspb.SeedState_SEED_STATE_STALE,
				GraphFreshnessState:                 awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_STALE,
				GraphFreshnessDetail:                "live store digest diverges from expected artifact",
				LiveStoreContainsEmbeddedSeedMarker: false,
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_metadata", map[string]interface{}{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	for _, want := range []string{
		"graph_authority.verdict: non_authoritative",
		"graph_authority.state: stale",
		"graph_authority.warning: live store digest diverges from expected artifact",
		"graph_freshness_state: GRAPH_FRESHNESS_STATE_STALE",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out=%q missing %q", out, want)
		}
	}
}

func TestMetadataTool_InfersCurrentAuthorityFromStampedFields(t *testing.T) {
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return &awarenesspb.MetadataResponse{
				TripleCount:                         42,
				EmbeddedSeedDigestSha256:            "abc123",
				BuildProvenanceState:                awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
				SeedState:                           awarenesspb.SeedState_SEED_STATE_CURRENT,
				LiveStoreContainsEmbeddedSeedMarker: true,
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_metadata", map[string]interface{}{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	for _, want := range []string{
		"graph_authority.verdict: authoritative",
		"graph_authority.state: current",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out=%q missing %q", out, want)
		}
	}
}

func TestFormatGraphAuthority_MarksUnavailableAsUnknown(t *testing.T) {
	out := formatGraphAuthority(nil)
	for _, want := range []string{
		"authority: non_authoritative (unknown)",
		"graph authority metadata unavailable",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out=%q missing %q", out, want)
		}
	}
}

func TestPreflightTool_ValidatesMissingTask(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callText(context.Background(), "awareness_preflight", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "task is required") {
		t.Fatalf("err=%v", err)
	}
}

func TestPreflightTool_MapsRequestAndFormatsVerdict(t *testing.T) {
	var got *awarenesspb.PreflightRequest
	b := testBridge(fakeClient{
		preflight: func(_ context.Context, in *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
			got = in
			return &awarenesspb.PreflightResponse{
				Status:          awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
				RiskClass:       awarenesspb.RiskClass_CONVERGENCE_RISK,
				Confidence:      awarenesspb.Confidence_CONFIDENCE_MEDIUM,
				RequiredActions: []string{"read heartbeat.go first"},
				BlindSpots:      []string{"none"},
				Authority:       testCurrentAuthority(""),
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_preflight", map[string]interface{}{
		"task":  "change install convergence",
		"files": []interface{}{"golang/node_agent/heartbeat.go"},
		"mode":  "standard",
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.GetTask() != "change install convergence" || len(got.GetFiles()) != 1 ||
		got.GetMode() != awarenesspb.PreflightMode_PREFLIGHT_STANDARD {
		t.Fatalf("request=%v", got)
	}
	if !strings.Contains(out, "risk_class: CONVERGENCE_RISK") || !strings.Contains(out, "read heartbeat.go first") {
		t.Fatalf("out=%q", out)
	}
	if !strings.Contains(out, "authority: authoritative (current)") {
		t.Fatalf("one-line authority missing from preflight output: %q", out)
	}
}

func TestServeStdio_AllowsLargeEditCheckPayloads(t *testing.T) {
	large := strings.Repeat("x", 128*1024)
	var gotContent string
	br := testBridge(fakeClient{
		editCheck: func(_ context.Context, in *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			gotContent = in.GetProposedContent()
			return &awarenesspb.EditCheckResponse{}, nil
		},
	})
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "awareness_edit_check",
			"arguments": map[string]interface{}{
				"file":             "a.go",
				"proposed_content": large,
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	var out bytes.Buffer
	if err := serveStdio(br, strings.NewReader(string(data)+"\n"), &out); err != nil {
		t.Fatalf("serveStdio: %v", err)
	}
	if gotContent != large {
		t.Fatalf("edit_check content length = %d, want %d", len(gotContent), len(large))
	}
	if !strings.Contains(out.String(), `"isError":false`) {
		t.Fatalf("response = %q", out.String())
	}
}

func TestServeStdio_SupportsContentLengthFraming(t *testing.T) {
	var gotContent string
	br := testBridge(fakeClient{
		editCheck: func(_ context.Context, in *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			gotContent = in.GetProposedContent()
			return &awarenesspb.EditCheckResponse{}, nil
		},
	})
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "awareness_edit_check",
			"arguments": map[string]interface{}{
				"file":             "a.go",
				"proposed_content": "framed payload",
			},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(data), data)

	var out bytes.Buffer
	if err := serveStdio(br, strings.NewReader(input), &out); err != nil {
		t.Fatalf("serveStdio: %v", err)
	}
	if gotContent != "framed payload" {
		t.Fatalf("edit_check content = %q", gotContent)
	}
	resp := out.String()
	if !strings.HasPrefix(resp, "Content-Length: ") {
		t.Fatalf("expected framed response, got %q", resp)
	}
	parts := strings.SplitN(resp, "\r\n\r\n", 2)
	if len(parts) != 2 || !strings.Contains(parts[1], `"isError":false`) {
		t.Fatalf("response = %q", resp)
	}
}

func TestServeStdio_InitializeRespondsWithProtocolVersionFramed(t *testing.T) {
	br := testBridge(fakeClient{})
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(req), req)

	var out bytes.Buffer
	if err := serveStdio(br, strings.NewReader(input), &out); err != nil {
		t.Fatalf("serveStdio: %v", err)
	}
	resp := out.String()
	if !strings.HasPrefix(resp, "Content-Length: ") {
		t.Fatalf("expected framed response, got %q", resp)
	}
	parts := strings.SplitN(resp, "\r\n\r\n", 2)
	if len(parts) != 2 || !strings.Contains(parts[1], `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("response = %q", resp)
	}
	if !strings.Contains(parts[1], `"serverInfo":{"name":"sensei","version":"0.1.0"}`) {
		t.Fatalf("initialize serverInfo should identify Sensei, got %q", parts[1])
	}
}

func TestAwarenessAddrs_LocalhostAddsFallback(t *testing.T) {
	got := awarenessAddrs("localhost:10120")
	if len(got) != 2 || got[0] != "localhost:10120" || got[1] != "localhost:9090" {
		t.Fatalf("got=%v", got)
	}
}

func TestFailoverClient_RetriesTransportFailures(t *testing.T) {
	var secondCalled bool
	c := &failoverClient{
		entries: []clientEntry{
			{
				addr: "localhost:10120",
				client: fakeClient{
					metadata: func(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
						return nil, status.Error(codes.Unavailable, "transport closed")
					},
				},
			},
			{
				addr: "localhost:9090",
				client: fakeClient{
					metadata: func(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
						secondCalled = true
						return &awarenesspb.MetadataResponse{ServerVersion: "ok"}, nil
					},
				},
			},
		},
	}
	resp, err := c.Metadata(context.Background(), &awarenesspb.MetadataRequest{})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !secondCalled || resp.GetServerVersion() != "ok" {
		t.Fatalf("secondCalled=%v resp=%v", secondCalled, resp)
	}
}

func TestAwarenessAuditDiffTool_Registered(t *testing.T) {
	br := &bridge{}
	tools := br.tools()
	found := false
	for _, tool := range tools {
		if tool.Name == "awareness_audit_diff" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("awareness_audit_diff tool not registered in tools()")
	}
}

func TestAwarenessAuditDiffTool_EvaluatesDiff(t *testing.T) {
	head := testGitHEAD(t)
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{
				Authority: testCurrentAuthority(head),
			}, nil
		},
	}
	br := testBridge(fake)
	validDiff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          validDiff,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool awareness_audit_diff failed: %v", err)
	}
	if res == nil || res.Text == "" {
		t.Fatal("expected non-empty toolResult text")
	}
	if !strings.Contains(res.Text, "decision: pass") {
		t.Fatalf("expected decision: pass, got text: %s", res.Text)
	}
}

func TestAwarenessAuditDiffTool_FailsOnStaleOrNilAuthority(t *testing.T) {
	head := testGitHEAD(t)
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{
				Authority: nil,
			}, nil
		},
	}
	br := testBridge(fake)
	validDiff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          validDiff,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	if !strings.Contains(res.Text, "decision: cannot_verify") {
		t.Fatalf("expected cannot_verify on nil authority, got text: %s", res.Text)
	}
}

// expected_head is optional per the governing contract §3. Omitting it is
// accepted; an add-file diff needs no base content and passes.
func TestAwarenessAuditDiffTool_ExpectedHeadOptional(t *testing.T) {
	head := testGitHEAD(t)
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(head)}, nil
		},
	}
	br := testBridge(fake)
	validDiff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff": validDiff,
	})
	if err != nil {
		t.Fatalf("expected no error when expected_head is omitted, got: %v", err)
	}
	if !strings.Contains(res.Text, "decision: pass") {
		t.Fatalf("expected decision: pass for add-file diff without expected_head, got text: %s", res.Text)
	}
}

// Without expected_head the base of a modified file cannot be bound to a
// canonical snapshot, and the tool must not read ambient working-tree state
// (§2). The modify hunk therefore degrades to cannot_verify.
func TestAwarenessAuditDiffTool_ModifyWithoutExpectedHeadCannotVerify(t *testing.T) {
	head := testGitHEAD(t)
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(head)}, nil
		},
	}
	br := testBridge(fake)
	modifyDiff := `diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,1 +1,1 @@
-old
+new
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff": modifyDiff,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	if !strings.Contains(res.Text, "decision: cannot_verify") {
		t.Fatalf("expected cannot_verify for modify diff without expected_head, got text: %s", res.Text)
	}
}

// Even with NO expected_head, an authoritative graph that exposes no
// source/build commit cannot bind the verdict to a rule snapshot and must fail
// closed. This exercises the omitted-head route the graph-commit guard used to
// skip.
func TestAwarenessAuditDiffTool_NoExpectedHeadMissingGraphCommitFailsClosed(t *testing.T) {
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{
				Authority: &awarenesspb.GraphAuthority{
					Authoritative:       true,
					GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
					// No SourceRepoCommit or GraphBuildCommit.
				},
			}, nil
		},
	}
	br := testBridge(fake)
	validDiff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff": validDiff,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	if !strings.Contains(res.Text, "cannot_verify") {
		t.Fatalf("expected cannot_verify when graph exposes no commit identity and no expected_head, got text: %s", res.Text)
	}
}

// An authoritative graph that exposes no source/build commit identity cannot be
// bound to expected_head and must fail closed.
func TestAwarenessAuditDiffTool_GraphNoCommitIdentityFailsClosed(t *testing.T) {
	head := testGitHEAD(t)
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{
				Authority: &awarenesspb.GraphAuthority{
					Authoritative:       true,
					GraphFreshnessState: awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
					// No SourceRepoCommit or GraphBuildCommit.
				},
			}, nil
		},
	}
	br := testBridge(fake)
	validDiff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          validDiff,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	if !strings.Contains(res.Text, "cannot_verify") {
		t.Fatalf("expected cannot_verify when graph exposes no commit identity, got text: %s", res.Text)
	}
}

// A graph compiled from a commit other than expected_head is NOT a mismatch.
// The two name different things — expected_head is the audited repository's
// base, the graph commit is the rule snapshot that supplied the requirements —
// and in a multi-domain deployment they are commits in different repositories.
// Requiring equality made every modified-file audit impossible: omitting
// expected_head prevents base reconstruction, supplying it was rejected here.
//
// The independent fail-closed rules are covered separately and must keep
// holding: TestAwarenessAuditDiffTool_GraphNoCommitIdentityFailsClosed and
// TestAwarenessAuditDiffTool_ModifyWithoutExpectedHeadCannotVerify.
func TestAwarenessAuditDiffTool_IndependentGraphCommitIsNotAMismatch(t *testing.T) {
	head := testGitHEAD(t)
	fake := fakeClient{
		editCheck: func(_ context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, req *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			// Graph was compiled from a different commit
			return &awarenesspb.ImpactResponse{
				Authority: testCurrentAuthority("aaaa" + head[4:]),
			}, nil
		},
	}
	br := testBridge(fake)
	validDiff := `diff --git a/main.go b/main.go
new file mode 100644
--- /dev/null
+++ b/main.go
@@ -0,0 +1,2 @@
+package main
+func main() {}
`
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          validDiff,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	// An independent graph snapshot identity must not, by itself, refuse the
	// audit. This diff adds a file, so no base reconstruction is required and
	// nothing else here is unverifiable.
	if strings.Contains(res.Text, "cannot_verify") {
		t.Fatalf("a graph snapshot commit independent of expected_head must not refuse the audit, got text: %s", res.Text)
	}
}

// The case that actually closed the loop: a MODIFIED file. Added files always
// escaped, because their new content reconstructs from the hunks alone, so an
// add-only regression would not prove the defect fixed. A modification needs
// base bytes, which need a pinned expected_head — the very input that used to
// be rejected as a graph-commit mismatch.
//
// The hunk is built from the real base bytes at HEAD so the test cannot rot as
// the file's content changes.
func TestAwarenessAuditDiffTool_ModifiedFileVerifiesWithIndependentGraphCommit(t *testing.T) {
	head := testGitHEAD(t)
	graphCommit := strings.Repeat("a", len(head))
	if graphCommit == strings.ToLower(head) {
		graphCommit = strings.Repeat("b", len(head))
	}

	toplevel, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSpace(string(toplevel))
	const target = "go.mod"
	baseBytes, err := gitShowAt(root, head, target)
	if err != nil {
		t.Fatalf("read base bytes for %s: %v", target, err)
	}
	firstLine := strings.SplitN(baseBytes, "\n", 2)[0]

	fake := fakeClient{
		editCheck: func(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			return &awarenesspb.EditCheckResponse{}, nil
		},
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(graphCommit)}, nil
		},
	}
	modifyDiff := "diff --git a/" + target + " b/" + target + "\n" +
		"--- a/" + target + "\n" +
		"+++ b/" + target + "\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-" + firstLine + "\n" +
		"+" + firstLine + " // audited\n"

	res, err := testBridge(fake).callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          modifyDiff,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	if strings.Contains(res.Text, "cannot_verify") {
		t.Fatalf("a modified file with a pinned candidate base must verify even when the rule-snapshot commit differs; got: %s", res.Text)
	}
}

func gitShowAt(root, commit, path string) (string, error) {
	cmd := exec.Command("git", "show", commit+":"+path)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// TestMultiFileAuditIsNotStarvedByThePerRequestBudget reproduces issue #260's
// second finding.
//
// --timeout is documented as a PER-REQUEST gRPC budget but was applied as the
// deadline for the whole tools/call. awareness_audit_diff issues several gRPC
// calls per file, so a one-file diff fitted inside one request's budget and a
// two-file diff did not — deterministically, returning after exactly the
// timeout, with each file passing when audited alone.
//
// The symptom was evaluator_unavailable with no cause, which from the outside
// is indistinguishable from the change being bad.
func TestMultiFileAuditIsNotStarvedByThePerRequestBudget(t *testing.T) {
	head := testGitHEAD(t)
	// Each RPC costs a little. Well inside a per-request budget; fatal if the
	// whole call has to fit in that same budget.
	// Sized so ONE file's RPCs fit comfortably inside the per-request budget
	// and TWO files decisively do not. Loose margins here would let the test
	// pass with the budgets still shared, which is the defect itself.
	const perCall = 100 * time.Millisecond
	fake := fakeClient{
		editCheck: func(ctx context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
			select {
			case <-time.After(perCall):
				return &awarenesspb.EditCheckResponse{}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		impact: func(ctx context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			select {
			case <-time.After(perCall):
				return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority(head)}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	twoFiles := `diff --git a/one.go b/one.go
new file mode 100644
--- /dev/null
+++ b/one.go
@@ -0,0 +1,2 @@
+package one
+func One() {}
diff --git a/two.go b/two.go
new file mode 100644
--- /dev/null
+++ b/two.go
@@ -0,0 +1,2 @@
+package two
+func Two() {}
`
	// A single file costs at most two RPCs here, so 250ms is ample for one and
	// impossible for two.
	br := newBridge(fake, 250*time.Millisecond, 10*time.Second)
	res, err := br.callTool(context.Background(), "awareness_audit_diff", map[string]interface{}{
		"diff":          twoFiles,
		"expected_head": head,
	})
	if err != nil {
		t.Fatalf("callTool failed: %v", err)
	}
	if strings.Contains(res.Text, "evaluator_unavailable") {
		t.Errorf("a two-file diff was refused as evaluator_unavailable while each file fits the per-request budget:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "cannot_verify") {
		t.Errorf("a two-file diff could not be verified purely because of file count:\n%s", res.Text)
	}
}

// Separating the two budgets must not mean an unbounded request. The
// per-request budget still applies to each individual RPC, derived from the
// call's own context so cancelling the call still cancels the request.
func TestPerRequestBudgetStillBoundsOneRpc(t *testing.T) {
	p := &perRequestClient{timeout: 40 * time.Millisecond}

	ctx, cancel := p.rpcContext(context.Background())
	defer cancel()
	start := time.Now()
	<-ctx.Done()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the per-request context ran for %s; --timeout no longer bounds one request", elapsed)
	}
	if got := ctx.Err(); got != context.DeadlineExceeded {
		t.Errorf("per-request context ended with %v, want %v", got, context.DeadlineExceeded)
	}

	// Cancelling the whole call must still cancel an in-flight request.
	parent, cancelParent := context.WithCancel(context.Background())
	child, cancelChild := p.rpcContext(parent)
	defer cancelChild()
	cancelParent()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Error("cancelling the call did not cancel its in-flight request")
	}

	// A bridge with no explicit ceiling gets a generous one, never zero.
	if got := (&bridge{timeout: time.Second}).callCeiling(); got <= time.Second {
		t.Errorf("derived call ceiling = %s, want more than one request budget", got)
	}
	if got := (&bridge{}).callCeiling(); got <= 0 {
		t.Errorf("a bridge with no budgets got a %s ceiling; that would refuse every call", got)
	}
}

// TestEveryRpcCarriesThePerRequestBudget guards the regression the first #260
// fix introduced: moving the budget to one call site left the other eight RPCs
// bounded only by the 2m whole-call ceiling, so --timeout silently stopped
// meaning what it documents for awareness_briefing, awareness_impact and the
// rest.
//
// It walks the awarenessClient interface by reflection rather than listing the
// methods, so a method added to that surface later is covered without anyone
// remembering to add it here — the same reason the budget itself lives at the
// client seam.
func TestEveryRpcCarriesThePerRequestBudget(t *testing.T) {
	const budget = 60 * time.Millisecond

	// stalls forever unless its context is cancelled, so a bounded call returns
	// promptly with DeadlineExceeded and an unbounded one hangs past the check.
	inner := &stallingClient{}
	client := &perRequestClient{inner: inner, timeout: budget}

	iface := reflect.TypeOf((*awarenessClient)(nil)).Elem()
	v := reflect.ValueOf(client)
	if iface.NumMethod() == 0 {
		t.Fatal("awarenessClient exposes no methods; this guard would prove nothing")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		m := iface.Method(i)
		t.Run(m.Name, func(t *testing.T) {
			method := v.MethodByName(m.Name)
			if !method.IsValid() {
				t.Fatalf("perRequestClient does not implement %s", m.Name)
			}
			// (ctx, in) — the request type is the method's second parameter.
			args := []reflect.Value{
				reflect.ValueOf(context.Background()),
				reflect.New(m.Type.In(1).Elem()),
			}
			done := make(chan error, 1)
			go func() {
				out := method.Call(args)
				err, _ := out[len(out)-1].Interface().(error)
				done <- err
			}()
			select {
			case err := <-done:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("%s returned %v, want the per-request budget to expire it", m.Name, err)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("%s outlived the %s per-request budget; --timeout does not bound it", m.Name, budget)
			}
		})
	}
}

// TestSingleRequestToolIsBoundedByTheRequestBudget is the reproduction of the
// regression itself, from the caller's side.
//
// The reflection guard above proves perRequestClient bounds every method; it
// does NOT prove a tool handler reaches a backend through it. That distinction
// is exactly where the first #260 fix went wrong — the machinery existed and
// one call site used it, while awareness_briefing and its siblings went
// straight to the client and inherited only the 2m whole-call ceiling.
//
// So this drives a real handler against a backend that never answers, and
// requires it to give up on the per-request budget rather than the ceiling.
func TestSingleRequestToolIsBoundedByTheRequestBudget(t *testing.T) {
	const (
		budget  = 80 * time.Millisecond
		ceiling = 30 * time.Second
	)
	br := newBridge(stallingClient{}, budget, ceiling)

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		_, _ = br.callTool(context.Background(), "awareness_briefing", map[string]interface{}{
			"file": "cmd/awareness-mcp/main.go",
		})
		done <- time.Since(start)
	}()

	select {
	case elapsed := <-done:
		// Generous, but far below the ceiling: the point is which budget ended
		// the call, not how tight the timer is on a loaded machine.
		if elapsed > 5*time.Second {
			t.Errorf("awareness_briefing took %s against a stalled backend; --timeout (%s) did not bound its request, the whole-call ceiling (%s) did", elapsed, budget, ceiling)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("awareness_briefing never returned within 10s; the per-request budget of %s is not applied to it", budget)
	}
}

// stallingClient blocks every RPC until the caller's context ends, so the only
// thing that can return it is a deadline someone applied.
type stallingClient struct{}

func stall[T any](ctx context.Context) (*T, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (stallingClient) Briefing(ctx context.Context, _ *awarenesspb.BriefingRequest, _ ...grpc.CallOption) (*awarenesspb.BriefingResponse, error) {
	return stall[awarenesspb.BriefingResponse](ctx)
}
func (stallingClient) Impact(ctx context.Context, _ *awarenesspb.ImpactRequest, _ ...grpc.CallOption) (*awarenesspb.ImpactResponse, error) {
	return stall[awarenesspb.ImpactResponse](ctx)
}
func (stallingClient) Resolve(ctx context.Context, _ *awarenesspb.ResolveRequest, _ ...grpc.CallOption) (*awarenesspb.ResolveResponse, error) {
	return stall[awarenesspb.ResolveResponse](ctx)
}
func (stallingClient) Query(ctx context.Context, _ *awarenesspb.QueryRequest, _ ...grpc.CallOption) (*awarenesspb.QueryResponse, error) {
	return stall[awarenesspb.QueryResponse](ctx)
}
func (stallingClient) Metadata(ctx context.Context, _ *awarenesspb.MetadataRequest, _ ...grpc.CallOption) (*awarenesspb.MetadataResponse, error) {
	return stall[awarenesspb.MetadataResponse](ctx)
}
func (stallingClient) Preflight(ctx context.Context, _ *awarenesspb.PreflightRequest, _ ...grpc.CallOption) (*awarenesspb.PreflightResponse, error) {
	return stall[awarenesspb.PreflightResponse](ctx)
}
func (stallingClient) EditCheck(ctx context.Context, _ *awarenesspb.EditCheckRequest, _ ...grpc.CallOption) (*awarenesspb.EditCheckResponse, error) {
	return stall[awarenesspb.EditCheckResponse](ctx)
}
func (stallingClient) Propose(ctx context.Context, _ *awarenesspb.ProposeRequest, _ ...grpc.CallOption) (*awarenesspb.ProposeResponse, error) {
	return stall[awarenesspb.ProposeResponse](ctx)
}
