// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

// asMap unwraps a tool's Structured payload as a JSON-like object.
func asMap(t *testing.T, res *toolResult) map[string]interface{} {
	t.Helper()
	m, ok := res.Structured.(map[string]interface{})
	if !ok {
		t.Fatalf("structured payload is not an object: %T", res.Structured)
	}
	return m
}

// Pillar 3.1: impact returns REAL nodes in structuredContent, not just counts.
func TestImpact_StructuredNodes(t *testing.T) {
	b := testBridge(fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{
				DirectInvariants: []*awarenesspb.KnowledgeNode{
					{Id: "must_hold", Class: "invariant", Label: "Must hold", Severity: "critical"},
				},
				Symbols: []*awarenesspb.CodeSymbolNode{
					{Id: "x.go:F", Label: "F", File: "x.go", Language: "go", References: []string{"y.go:G"}},
				},
				Authority: testCurrentAuthority(""),
			}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_impact", map[string]interface{}{"file": "x.go"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	m := asMap(t, res)
	inv, ok := m["direct_invariants"].([]map[string]interface{})
	if !ok || len(inv) != 1 {
		t.Fatalf("direct_invariants not a node list: %#v", m["direct_invariants"])
	}
	if inv[0]["id"] != "invariant:must_hold" || inv[0]["label"] != "Must hold" || inv[0]["severity"] != "critical" {
		t.Errorf("node fields wrong: %#v", inv[0])
	}
	syms, ok := m["symbols"].([]map[string]interface{})
	if !ok || len(syms) != 1 || syms[0]["id"] != "x.go:F" {
		t.Fatalf("symbols not surfaced structurally: %#v", m["symbols"])
	}
	// And the authority rides as a structured object with the interpreted verdict.
	auth, ok := m["authority"].(map[string]interface{})
	if !ok || auth["verdict"] != "authoritative" || auth["state"] != "current" {
		t.Fatalf("structured authority wrong: %#v", m["authority"])
	}
	// The full provenance that left the text block is preserved in structured.
	if auth["certified_services_repo_commit"] != "svc789" {
		t.Errorf("provenance not preserved in structured authority: %#v", auth)
	}
}

// The text block carries a ONE-LINE authority (not the old ~19-line dump).
func TestAuthority_OneLineInTextFullInStructured(t *testing.T) {
	b := testBridge(fakeClient{
		impact: func(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority("")}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_impact", map[string]interface{}{"file": "x.go"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	authLines := 0
	for _, line := range strings.Split(res.Text, "\n") {
		if strings.HasPrefix(line, "authority") {
			authLines++
		}
	}
	if authLines != 1 {
		t.Fatalf("expected exactly one authority line in text, got %d:\n%s", authLines, res.Text)
	}
	if !strings.Contains(res.Text, "authority: authoritative (current)") {
		t.Errorf("one-line authority form wrong: %q", res.Text)
	}
}

// Pillar 3.1: the Propose write tool is exposed and structured.
func TestProposeTool_AcceptedStructured(t *testing.T) {
	var got *awarenesspb.ProposeRequest
	b := testBridge(fakeClient{
		propose: func(_ context.Context, in *awarenesspb.ProposeRequest) (*awarenesspb.ProposeResponse, error) {
			got = in
			return &awarenesspb.ProposeResponse{
				Status:        awarenesspb.ProposeStatus_PROPOSE_STATUS_ACCEPTED,
				CandidatePath: "docs/awareness/candidates/proposals/invariant.foo.yaml",
				NodeIds:       []string{"invariant.foo"},
			}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_propose", map[string]interface{}{
		"kind":             "invariant",
		"title":            "Foo must hold",
		"source_files":     []interface{}{"a.go"},
		"related_failures": []interface{}{"failure.x"},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got == nil || got.GetKind() != "invariant" || got.GetTitle() != "Foo must hold" ||
		len(got.GetSourceFiles()) != 1 || len(got.GetRelatedFailures()) != 1 {
		t.Fatalf("request not mapped from args: %#v", got)
	}
	m := asMap(t, res)
	if m["status"] != "ACCEPTED" || m["accepted"] != true {
		t.Errorf("structured status wrong: %#v", m)
	}
	if m["candidate_path"] != "docs/awareness/candidates/proposals/invariant.foo.yaml" {
		t.Errorf("candidate_path missing: %#v", m)
	}
}

func TestProposeTool_RequiresKind(t *testing.T) {
	b := testBridge(fakeClient{})
	_, err := b.callTool(context.Background(), "awareness_propose", map[string]interface{}{"title": "no kind"})
	if err == nil || !strings.Contains(err.Error(), "kind is required") {
		t.Fatalf("err=%v", err)
	}
}

// A propose against a server without propose enabled surfaces honestly, not as
// a silent success.
func TestProposeTool_UnavailableSurfaces(t *testing.T) {
	b := testBridge(fakeClient{}) // no propose stub → Unavailable
	_, err := b.callTool(context.Background(), "awareness_propose", map[string]interface{}{"kind": "invariant"})
	if err == nil || !strings.Contains(err.Error(), "propose") {
		t.Fatalf("expected a surfaced propose error, got %v", err)
	}
}

func TestToolsList_IncludesPropose(t *testing.T) {
	b := testBridge(fakeClient{})
	found := false
	for _, tl := range b.tools() {
		if tl.Name == "awareness_propose" {
			found = true
			if _, ok := tl.InputSchema["properties"]; !ok {
				t.Error("propose tool missing input schema properties")
			}
		}
	}
	if !found {
		t.Fatal("awareness_propose not advertised in tools/list")
	}
}

// The change-risk verdict must reach a structured consumer as fields.
//
// globulario/sensei#171 published blast radius and approval gate on the
// Preflight RPC so consumers would stop parsing the prose line, but the MCP
// bridge kept projecting only the string lists. Every MCP consumer — which is
// how agents actually reach this server — was therefore still left with the
// sentence, and a downstream repository that deleted its parser on the strength
// of #171 would have found the fields simply absent.
func TestPreflight_StructuredChangeRisk(t *testing.T) {
	b := testBridge(fakeClient{
		preflight: func(_ context.Context, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
			return &awarenesspb.PreflightResponse{
				Status:          awarenesspb.PreflightStatus_PREFLIGHT_STATUS_OK,
				RequiredActions: []string{"Change risk: blast=cluster, approval=human_approval_required"},
				ChangeRisk: &awarenesspb.ChangeRisk{
					BlastRadius:  awarenesspb.BlastRadius_BLAST_RADIUS_CLUSTER,
					ApprovalGate: awarenesspb.ApprovalGate_APPROVAL_GATE_HUMAN_APPROVAL_REQUIRED,
					Reasons:      []string{"touches an authority boundary"},
				},
				Authority: testCurrentAuthority(""),
			}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_preflight", map[string]interface{}{
		"task":  "widen an authority boundary",
		"files": []interface{}{"golang/server/preflight.go"},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	risk, ok := asMap(t, res)["change_risk"].(map[string]interface{})
	if !ok {
		t.Fatal("change_risk is absent from the structured payload; MCP consumers are still parsing prose")
	}
	if risk["blast_radius"] != "BLAST_RADIUS_CLUSTER" {
		t.Errorf("blast_radius=%v", risk["blast_radius"])
	}
	if risk["approval_gate"] != "APPROVAL_GATE_HUMAN_APPROVAL_REQUIRED" {
		t.Errorf("approval_gate=%v", risk["approval_gate"])
	}
	reasons, _ := risk["reasons"].([]string)
	if len(reasons) != 1 || reasons[0] != "touches an authority boundary" {
		t.Errorf("reasons=%v", risk["reasons"])
	}

	// The prose line stays, unchanged, for consumers that already read it.
	actions, _ := asMap(t, res)["required_actions"].([]string)
	if len(actions) != 1 || !strings.HasPrefix(actions[0], "Change risk: ") {
		t.Errorf("the prose line was disturbed: %v", asMap(t, res)["required_actions"])
	}
}

// A response carrying no verdict must say so explicitly rather than by omission.
//
// Preflight leaves change_risk unset when the request named no files. If the
// key vanished in that case, "no verdict was reached" would look exactly like
// "this build does not publish verdicts", and a consumer cannot fail closed
// intelligently on a distinction it cannot observe.
func TestPreflight_UnclassifiedChangeRiskIsStatedNotOmitted(t *testing.T) {
	b := testBridge(fakeClient{
		preflight: func(_ context.Context, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
			return &awarenesspb.PreflightResponse{
				Status:    awarenesspb.PreflightStatus_PREFLIGHT_STATUS_EMPTY,
				Authority: testCurrentAuthority(""),
			}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_preflight", map[string]interface{}{"task": "no files named"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	risk, ok := asMap(t, res)["change_risk"].(map[string]interface{})
	if !ok {
		t.Fatal("change_risk was omitted, so an unclassified verdict is indistinguishable from an old server")
	}
	if risk["blast_radius"] != "BLAST_RADIUS_UNSPECIFIED" || risk["approval_gate"] != "APPROVAL_GATE_UNSPECIFIED" {
		t.Errorf("an unclassified verdict was not reported as unspecified: %v", risk)
	}
	if _, present := risk["reasons"]; present {
		t.Errorf("reasons were invented for an unclassified verdict: %v", risk["reasons"])
	}
}

// Coverage is the other half of the same claim. The preflight verdict above it
// means nothing if the server has already said its own answer is under-covered,
// and that disclaimer was reaching text consumers only.
func TestPreflight_StructuredCoverage(t *testing.T) {
	b := testBridge(fakeClient{
		preflight: func(_ context.Context, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
			return &awarenesspb.PreflightResponse{
				Status: awarenesspb.PreflightStatus_PREFLIGHT_STATUS_DEGRADED,
				Coverage: &awarenesspb.CoverageSummary{
					DirectAnchorCount: 0,
					FileCount:         1,
					IndexedFileCount:  0,
					Sufficient:        false,
					Note:              "domain scope could not be verified — response is not proof of safety",
				},
				Authority: testCurrentAuthority(""),
			}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_preflight", map[string]interface{}{
		"task":  "widen an authority boundary",
		"files": []interface{}{"golang/server/preflight.go"},
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	cov, ok := asMap(t, res)["coverage"].(map[string]interface{})
	if !ok {
		t.Fatal("coverage is absent from the structured payload; MCP consumers cannot see that the answer is under-covered")
	}
	if cov["sufficient"] != false {
		t.Errorf("sufficient=%v, want the server's own false", cov["sufficient"])
	}
	if cov["note"] == nil || !strings.Contains(cov["note"].(string), "not proof of safety") {
		t.Errorf("note=%v", cov["note"])
	}
	if cov["indexed_file_count"] != int32(0) || cov["file_count"] != int32(1) {
		t.Errorf("counts did not survive: %v", cov)
	}
}

// A response with no coverage summary must say "no summary", not "insufficient".
//
// change_risk can be zero-valued because its enums declare UNSPECIFIED to be a
// real answer. `sufficient` is a plain bool, so a zero-valued object would
// assert a verdict the server never reached.
func TestPreflight_AbsentCoverageIsNullNotFabricated(t *testing.T) {
	b := testBridge(fakeClient{
		preflight: func(_ context.Context, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
			return &awarenesspb.PreflightResponse{
				Status:    awarenesspb.PreflightStatus_PREFLIGHT_STATUS_EMPTY,
				Authority: testCurrentAuthority(""),
			}, nil
		},
	})
	res, err := b.callTool(context.Background(), "awareness_preflight", map[string]interface{}{"task": "no files named"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	value, present := asMap(t, res)["coverage"]
	if !present {
		t.Fatal("the coverage key vanished, so 'no summary' is indistinguishable from an old server")
	}
	if value != nil {
		t.Fatalf("a coverage summary was invented where the server produced none: %v", value)
	}
}
