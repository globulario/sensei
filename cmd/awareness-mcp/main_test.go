// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
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
	return &bridge{client: c, timeout: 5 * time.Second}
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

func testCurrentAuthority() *awarenesspb.GraphAuthority {
	return &awarenesspb.GraphAuthority{
		Authoritative:                   true,
		GraphFreshnessState:             awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_CURRENT,
		BuildProvenanceState:            awarenesspb.BuildProvenanceState_BUILD_PROVENANCE_STATE_STAMPED,
		SeedState:                       awarenesspb.SeedState_SEED_STATE_CURRENT,
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
				Authority:     testCurrentAuthority(),
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
				Authority:        testCurrentAuthority(),
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
				Authority:     testCurrentAuthority(),
			}, nil
		},
	})
	out, err := b.callText(context.Background(), "awareness_query", map[string]interface{}{
		"mode": "by_class", "class": "invariant", "limit": float64(10),
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got.GetMode() != awarenesspb.QueryMode_QUERY_MODE_BY_CLASS ||
		got.GetClass() != awarenesspb.QueryClass_QUERY_CLASS_INVARIANT ||
		got.GetLimit() != 10 {
		t.Fatalf("request=%v", got)
	}
	if !strings.Contains(out, "rows: 1") || !strings.Contains(out, "invariant:x") || !strings.Contains(out, "critical") {
		t.Fatalf("out=%q", out)
	}
	if !strings.Contains(out, "authority: authoritative (current)") {
		t.Fatalf("one-line authority missing from query output: %q", out)
	}
}

func TestMetadataTool_FormatsCounts(t *testing.T) {
	b := testBridge(fakeClient{
		metadata: func(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
			return &awarenesspb.MetadataResponse{
				ServerVersion:                       "1.2.3",
				TripleCount:                         12062,
				InvariantCount:                      40,
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
	out, err := b.callText(context.Background(), "awareness_metadata", map[string]interface{}{})
	if err != nil {
		t.Fatalf("err=%v", err)
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
				Authority:       testCurrentAuthority(),
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
