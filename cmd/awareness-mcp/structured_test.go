// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
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
				Authority: testCurrentAuthority(),
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
			return &awarenesspb.ImpactResponse{Authority: testCurrentAuthority()}, nil
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
