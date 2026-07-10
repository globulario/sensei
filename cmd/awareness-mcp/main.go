// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=mcp.bridge
// @awareness file_role=mcp_stdio_bridge
// @awareness implements=globular.awareness_graph:intent.awareness.mcp_bridge_exposes_safe_tools_only
// @awareness relates_to=globular.awareness_graph:intent.awareness.mcp_tools_use_gateway_client_pool
// @awareness risk=high

// Command awareness-mcp is a minimal MCP bridge for awareness-graph.
//
// Transport: JSON-RPC 2.0 messages over stdio. Strict MCP clients use
// Content-Length framing; a legacy one-JSON-object-per-line mode remains for
// older local scripts.
// Scope: exposes only safe tools (briefing/impact/resolve/query/metadata/
// preflight/edit_check/propose). Query is the constrained typed API —
// mode/id/class enums only; the proto reserves the old sparql field, so raw
// SPARQL cannot transit this bridge. propose is the sole write, and a SAFE one:
// it only queues a validated review-queue candidate (never mutates the live
// graph), so it does not breach the safe-tools-only contract.
//
// Responses carry BOTH a compact human `text` block (one-line authority) and a
// machine-parseable `structuredContent` object, so any agent parses JSON
// instead of regexing prose.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenessclient "github.com/globulario/sensei/golang/client"
	awarenesspb "github.com/globulario/sensei/golang/pb"
)

type awarenessClient interface {
	Briefing(ctx context.Context, in *awarenesspb.BriefingRequest, opts ...grpc.CallOption) (*awarenesspb.BriefingResponse, error)
	Impact(ctx context.Context, in *awarenesspb.ImpactRequest, opts ...grpc.CallOption) (*awarenesspb.ImpactResponse, error)
	Resolve(ctx context.Context, in *awarenesspb.ResolveRequest, opts ...grpc.CallOption) (*awarenesspb.ResolveResponse, error)
	Query(ctx context.Context, in *awarenesspb.QueryRequest, opts ...grpc.CallOption) (*awarenesspb.QueryResponse, error)
	Metadata(ctx context.Context, in *awarenesspb.MetadataRequest, opts ...grpc.CallOption) (*awarenesspb.MetadataResponse, error)
	Preflight(ctx context.Context, in *awarenesspb.PreflightRequest, opts ...grpc.CallOption) (*awarenesspb.PreflightResponse, error)
	EditCheck(ctx context.Context, in *awarenesspb.EditCheckRequest, opts ...grpc.CallOption) (*awarenesspb.EditCheckResponse, error)
	Propose(ctx context.Context, in *awarenesspb.ProposeRequest, opts ...grpc.CallOption) (*awarenesspb.ProposeResponse, error)
}

type bridge struct {
	client  awarenessClient
	timeout time.Duration
}

type clientEntry struct {
	addr   string
	client awarenessClient
	close  func() error
}

type failoverClient struct {
	entries []clientEntry
}

type tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (b *bridge) tools() []tool {
	return []tool{
		{
			Name:        "awareness_briefing",
			Description: "Get deterministic awareness briefing for a file before edits",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file":   map[string]interface{}{"type": "string"},
					"task":   map[string]interface{}{"type": "string"},
					"depth":  map[string]interface{}{"type": "string", "enum": []string{"agent_compact", "compact", "standard", "deep"}},
					"domain": map[string]interface{}{"type": "string", "description": "repo/domain scope, e.g. github.com/caddyserver/caddy; required when the graph hosts >1 domain"},
				},
				"required": []string{"file"},
			},
		},
		{
			Name:        "awareness_impact",
			Description: "Get direct awareness impact for a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file":   map[string]interface{}{"type": "string"},
					"domain": map[string]interface{}{"type": "string", "description": "repo/domain scope; required when the graph hosts >1 domain"},
				},
				"required": []string{"file"},
			},
		},
		{
			Name:        "awareness_resolve",
			Description: "Resolve one awareness node by class and id",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"class":  map[string]interface{}{"type": "string"},
					"id":     map[string]interface{}{"type": "string"},
					"domain": map[string]interface{}{"type": "string", "description": "optional repo/domain scope; a node outside this scope resolves to not-found"},
				},
				"required": []string{"class", "id"},
			},
		},
		{
			Name:        "awareness_query",
			Description: "Typed graph query: by_file, by_id, by_class, or related (no free-form queries)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mode": map[string]interface{}{
						"type": "string",
						"enum": []string{"by_file", "by_id", "by_class", "related"},
					},
					"file": map[string]interface{}{"type": "string", "description": "required for mode=by_file"},
					"id":   map[string]interface{}{"type": "string", "description": "class-qualified id; required for mode=by_id and mode=related"},
					"class": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"invariant", "failure_mode", "incident_pattern", "intent", "symbol", "source_file", "code_symbol"},
						"description": "required for mode=by_class",
					},
					"limit": map[string]interface{}{"type": "integer"},
				},
				"required": []string{"mode"},
			},
		},
		{
			Name:        "awareness_metadata",
			Description: "Graph coverage and freshness: build provenance, triple/node counts, staleness signal",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "awareness_preflight",
			Description: "Pre-edit decision support: risk class, required actions, forbidden fixes, tests to run for a task",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task":   map[string]interface{}{"type": "string"},
					"files":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"domain": map[string]interface{}{"type": "string", "description": "repo/domain scope passed through to per-file impact queries"},
					"mode": map[string]interface{}{
						"type": "string",
						"enum": []string{"compact", "standard"},
					},
				},
				"required": []string{"task"},
			},
		},
		{
			Name:        "awareness_edit_check",
			Description: "Warning-only: evaluate a proposed edit's content against active repo-scoped advisory rules for the file. Never blocks, never edits code.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file":             map[string]interface{}{"type": "string"},
					"proposed_content": map[string]interface{}{"type": "string", "description": "the proposed new content (full file or edited region); patterns are matched against this"},
					"domain":           map[string]interface{}{"type": "string", "description": "repo/domain scope; required when the graph hosts >1 domain"},
				},
				"required": []string{"file", "proposed_content"},
			},
		},
		{
			Name:        "awareness_propose",
			Description: "Propose a typed awareness entry (failure_mode | invariant | required_test | forbidden_fix | contract_unknown) learned while working. SAFE write: validated with the same contract-first rules as `awg propose` and written to the review queue (candidates/), NOT the live graph — a human/CI step promotes it. Requires the server to be started with propose enabled; otherwise returns unavailable.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"kind":               map[string]interface{}{"type": "string", "enum": []string{"failure_mode", "invariant", "required_test", "forbidden_fix", "contract_unknown"}},
					"title":              map[string]interface{}{"type": "string"},
					"id":                 map[string]interface{}{"type": "string", "description": "optional explicit id; required for required_test as path/to/file_test.go:TestName"},
					"description":        map[string]interface{}{"type": "string"},
					"severity":           map[string]interface{}{"type": "string", "enum": []string{"critical", "high", "warning"}},
					"source_files":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"related_invariants": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"related_failures":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"required_tests":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"forbidden_fixes":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"evidence":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"contract":           map[string]interface{}{"type": "string"},
					"proposed_contract":  map[string]interface{}{"type": "string"},
					"revision_request":   map[string]interface{}{"type": "string"},
					"repo":               map[string]interface{}{"type": "string"},
					"domain":             map[string]interface{}{"type": "string"},
				},
				"required": []string{"kind"},
			},
		},
	}
}

// callTool dispatches one MCP tool call. Only the typed tools are handled —
// briefing/impact/resolve/query/metadata/preflight. Query accepts enum modes
// and classes only; there is no path for free-form query text to reach the
// store (QueryRequest reserved the sparql field at the proto layer). Unknown
// names return an error. This is the enforcement point for the
// safe-tools-only contract.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=mcp.bridge
// @awareness implements=globular.awareness_graph:intent.awareness.mcp_bridge_exposes_safe_tools_only
// @awareness enforces=globular.awareness_graph:invariant.awareness.query.no_arbitrary_sparql
// @awareness protects=globular.awareness_graph:failure_mode.awareness.raw_sparql_exposed_to_agent
// @awareness risk=high
func (b *bridge) callTool(ctx context.Context, name string, args map[string]interface{}) (*toolResult, error) {
	switch name {
	case "awareness_briefing":
		file, _ := args["file"].(string)
		if strings.TrimSpace(file) == "" {
			return nil, fmt.Errorf("file is required")
		}
		task, _ := args["task"].(string)
		depth, _ := args["depth"].(string)
		domain, _ := args["domain"].(string)
		if strings.TrimSpace(depth) == "" {
			depth = "agent_compact"
		}
		req := &awarenesspb.BriefingRequest{File: strings.TrimSpace(file), Task: strings.TrimSpace(task), Depth: strings.TrimSpace(depth), Domain: strings.TrimSpace(domain)}
		resp, err := b.client.Briefing(ctx, req)
		if err != nil {
			return nil, toolRPCError("briefing", err)
		}
		var text string
		if resp.GetStatus() == awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
			text = fmt.Sprintf("%sstatus: EMPTY\nreferenced_ids: []\nno direct awareness anchors found for %s",
				formatGraphAuthority(resp.GetAuthority()), file)
		} else {
			text = fmt.Sprintf("%sstatus: %s\ngenerated_in_ms: %d\nreferenced_ids: %v\n\n%s",
				formatGraphAuthority(resp.GetAuthority()), resp.GetStatus().String(), resp.GetGeneratedInMs(), resp.GetReferencedIds(), resp.GetProse())
		}
		return &toolResult{Text: text, Structured: structBriefing(resp)}, nil

	case "awareness_impact":
		file, _ := args["file"].(string)
		if strings.TrimSpace(file) == "" {
			return nil, fmt.Errorf("file is required")
		}
		domain, _ := args["domain"].(string)
		resp, err := b.client.Impact(ctx, &awarenesspb.ImpactRequest{File: strings.TrimSpace(file), Domain: strings.TrimSpace(domain)})
		if err != nil {
			return nil, toolRPCError("impact", err)
		}
		return &toolResult{Text: formatImpact(resp), Structured: structImpact(resp)}, nil

	case "awareness_resolve":
		class, _ := args["class"].(string)
		id, _ := args["id"].(string)
		if strings.TrimSpace(class) == "" || strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("class and id are required")
		}
		domain, _ := args["domain"].(string)
		resp, err := b.client.Resolve(ctx, &awarenesspb.ResolveRequest{
			Class:  strings.TrimSpace(class),
			Id:     strings.TrimSpace(id),
			Domain: strings.TrimSpace(domain),
		})
		if err != nil {
			return nil, toolRPCError("resolve", err)
		}
		var text string
		if !resp.GetFound() {
			text = fmt.Sprintf("%snot found: %s:%s", formatGraphAuthority(resp.GetAuthority()), class, id)
		} else {
			n := resp.GetNode()
			text = fmt.Sprintf("%sfound: true\nid: %s:%s\nlabel: %s\nseverity: %s\nstatus: %s\nrelated_ids: %v",
				formatGraphAuthority(resp.GetAuthority()), n.GetClass(), n.GetId(), n.GetLabel(), n.GetSeverity(), n.GetStatus(), n.GetRelatedIds())
		}
		return &toolResult{Text: text, Structured: structResolve(resp)}, nil

	case "awareness_query":
		modeStr, _ := args["mode"].(string)
		mode, err := queryModeFromString(modeStr)
		if err != nil {
			return nil, err
		}
		req := &awarenesspb.QueryRequest{Mode: mode}
		if limit, ok := args["limit"].(float64); ok {
			req.Limit = int32(limit)
		}
		switch mode {
		case awarenesspb.QueryMode_QUERY_MODE_BY_FILE:
			file, _ := args["file"].(string)
			if strings.TrimSpace(file) == "" {
				return nil, fmt.Errorf("file is required for mode=by_file")
			}
			req.File = strings.TrimSpace(file)
		case awarenesspb.QueryMode_QUERY_MODE_BY_ID, awarenesspb.QueryMode_QUERY_MODE_RELATED:
			id, _ := args["id"].(string)
			if strings.TrimSpace(id) == "" {
				return nil, fmt.Errorf("id is required for mode=%s", modeStr)
			}
			req.Id = strings.TrimSpace(id)
		case awarenesspb.QueryMode_QUERY_MODE_BY_CLASS:
			classStr, _ := args["class"].(string)
			class, err := queryClassFromString(classStr)
			if err != nil {
				return nil, err
			}
			req.Class = class
		}
		resp, err := b.client.Query(ctx, req)
		if err != nil {
			return nil, toolRPCError("query", err)
		}
		return &toolResult{Text: formatQuery(resp), Structured: structQuery(resp)}, nil

	case "awareness_metadata":
		resp, err := b.client.Metadata(ctx, &awarenesspb.MetadataRequest{})
		if err != nil {
			return nil, toolRPCError("metadata", err)
		}
		return &toolResult{Text: formatMetadata(resp), Structured: structMetadata(resp)}, nil

	case "awareness_preflight":
		task, _ := args["task"].(string)
		if strings.TrimSpace(task) == "" {
			return nil, fmt.Errorf("task is required")
		}
		domain, _ := args["domain"].(string)
		req := &awarenesspb.PreflightRequest{Task: strings.TrimSpace(task), Domain: strings.TrimSpace(domain)}
		if raw, ok := args["files"].([]interface{}); ok {
			for _, f := range raw {
				if s, ok := f.(string); ok && strings.TrimSpace(s) != "" {
					req.Files = append(req.Files, strings.TrimSpace(s))
				}
			}
		}
		if modeStr, ok := args["mode"].(string); ok && modeStr != "" {
			mode, err := preflightModeFromString(modeStr)
			if err != nil {
				return nil, err
			}
			req.Mode = mode
		}
		resp, err := b.client.Preflight(ctx, req)
		if err != nil {
			return nil, toolRPCError("preflight", err)
		}
		return &toolResult{Text: formatPreflight(resp), Structured: structPreflight(resp)}, nil

	case "awareness_edit_check":
		file, _ := args["file"].(string)
		if strings.TrimSpace(file) == "" {
			return nil, fmt.Errorf("file is required")
		}
		content, _ := args["proposed_content"].(string)
		domain, _ := args["domain"].(string)
		resp, err := b.client.EditCheck(ctx, &awarenesspb.EditCheckRequest{
			File:            strings.TrimSpace(file),
			ProposedContent: content,
			Domain:          strings.TrimSpace(domain),
		})
		if err != nil {
			return nil, toolRPCError("edit_check", err)
		}
		return &toolResult{Text: formatEditCheck(resp), Structured: structEditCheck(resp)}, nil

	case "awareness_propose":
		return b.callPropose(ctx, args)

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func toolRPCError(surface string, err error) error {
	if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.DeadlineExceeded) {
		return fmt.Errorf("%s unavailable: awareness-graph backend is unreachable; this is not an empty/no-guidance result: %s", surface, st.Message())
	}
	return fmt.Errorf("%s rpc: %w", surface, err)
}

// callPropose is the agent write path: submit a typed awareness entry to the
// review queue. It is a SAFE write — the server validates it (contract-first)
// and stores it under candidates/, never touching the live graph — so exposing
// it over MCP is consistent with the safe-tools-only bridge contract. A server
// without propose enabled returns Unavailable, surfaced honestly as a tool
// error rather than a silent success.
func (b *bridge) callPropose(ctx context.Context, args map[string]interface{}) (*toolResult, error) {
	kind := argString(args, "kind")
	if kind == "" {
		return nil, fmt.Errorf("kind is required (failure_mode | invariant | required_test | forbidden_fix | contract_unknown)")
	}
	req := &awarenesspb.ProposeRequest{
		Kind:              kind,
		Id:                argString(args, "id"),
		Title:             argString(args, "title"),
		Description:       argString(args, "description"),
		Severity:          argString(args, "severity"),
		SourceFiles:       argStrings(args, "source_files"),
		RelatedInvariants: argStrings(args, "related_invariants"),
		RelatedFailures:   argStrings(args, "related_failures"),
		RequiredTests:     argStrings(args, "required_tests"),
		ForbiddenFixes:    argStrings(args, "forbidden_fixes"),
		Evidence:          argStrings(args, "evidence"),
		Repo:              argString(args, "repo"),
		Domain:            argString(args, "domain"),
		Contract:          argString(args, "contract"),
		ProposedContract:  argString(args, "proposed_contract"),
		RevisionRequest:   argString(args, "revision_request"),
	}
	resp, err := b.client.Propose(ctx, req)
	if err != nil {
		return nil, toolRPCError("propose", err)
	}
	return &toolResult{Text: formatPropose(resp), Structured: structProposeResp(resp)}, nil
}

// formatPropose renders the compact human view of a propose result.
func formatPropose(resp *awarenesspb.ProposeResponse) string {
	status := strings.TrimPrefix(resp.GetStatus().String(), "PROPOSE_STATUS_")
	var b strings.Builder
	fmt.Fprintf(&b, "status: %s\n", status)
	if p := resp.GetCandidatePath(); p != "" {
		fmt.Fprintf(&b, "candidate_path: %s\n", p)
	}
	if ids := resp.GetNodeIds(); len(ids) > 0 {
		fmt.Fprintf(&b, "node_ids: %v\n", ids)
	}
	for _, e := range resp.GetValidationErrors() {
		fmt.Fprintf(&b, "validation_error: %s\n", e)
	}
	if n := resp.GetNote(); n != "" {
		fmt.Fprintf(&b, "note: %s\n", n)
	}
	return strings.TrimRight(b.String(), "\n")
}

// structProposeResp is the machine-parseable propose result.
func structProposeResp(resp *awarenesspb.ProposeResponse) map[string]interface{} {
	obj := map[string]interface{}{
		"status":          strings.TrimPrefix(resp.GetStatus().String(), "PROPOSE_STATUS_"),
		"accepted":        resp.GetStatus() == awarenesspb.ProposeStatus_PROPOSE_STATUS_ACCEPTED,
		"generated_in_ms": resp.GetGeneratedInMs(),
	}
	if p := resp.GetCandidatePath(); p != "" {
		obj["candidate_path"] = p
	}
	if ids := resp.GetNodeIds(); len(ids) > 0 {
		obj["node_ids"] = ids
	}
	if e := resp.GetValidationErrors(); len(e) > 0 {
		obj["validation_errors"] = e
	}
	if n := resp.GetNote(); n != "" {
		obj["note"] = n
	}
	return obj
}

// argString reads a trimmed string arg (empty when absent or not a string).
func argString(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

// argStrings reads a []string arg from a JSON array (skips empty/non-string
// entries); nil when absent.
func argStrings(args map[string]interface{}, key string) []string {
	raw, ok := args[key].([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func isTransportFailure(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Unauthenticated:
			return true
		}
	}
	return false
}

func callWithFailover[T any](entries []clientEntry, invoke func(awarenessClient) (T, error)) (T, error) {
	var zero T
	if len(entries) == 0 {
		return zero, status.Error(codes.Unavailable, "no awareness-graph backends configured")
	}
	transportFailures := make([]string, 0, len(entries))
	for _, entry := range entries {
		out, err := invoke(entry.client)
		if err == nil {
			return out, nil
		}
		if !isTransportFailure(err) {
			return zero, err
		}
		msg := err.Error()
		if st, ok := status.FromError(err); ok {
			msg = st.Message()
		}
		transportFailures = append(transportFailures, fmt.Sprintf("%s: %s", entry.addr, msg))
	}
	return zero, status.Error(codes.Unavailable, "awareness-graph transport failed on all configured addresses: "+strings.Join(transportFailures, "; "))
}

func (f *failoverClient) Briefing(ctx context.Context, in *awarenesspb.BriefingRequest, opts ...grpc.CallOption) (*awarenesspb.BriefingResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.BriefingResponse, error) {
		return c.Briefing(ctx, in, opts...)
	})
}

func (f *failoverClient) Impact(ctx context.Context, in *awarenesspb.ImpactRequest, opts ...grpc.CallOption) (*awarenesspb.ImpactResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.ImpactResponse, error) {
		return c.Impact(ctx, in, opts...)
	})
}

func (f *failoverClient) Resolve(ctx context.Context, in *awarenesspb.ResolveRequest, opts ...grpc.CallOption) (*awarenesspb.ResolveResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.ResolveResponse, error) {
		return c.Resolve(ctx, in, opts...)
	})
}

func (f *failoverClient) Query(ctx context.Context, in *awarenesspb.QueryRequest, opts ...grpc.CallOption) (*awarenesspb.QueryResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.QueryResponse, error) {
		return c.Query(ctx, in, opts...)
	})
}

func (f *failoverClient) Metadata(ctx context.Context, in *awarenesspb.MetadataRequest, opts ...grpc.CallOption) (*awarenesspb.MetadataResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.MetadataResponse, error) {
		return c.Metadata(ctx, in, opts...)
	})
}

func (f *failoverClient) Preflight(ctx context.Context, in *awarenesspb.PreflightRequest, opts ...grpc.CallOption) (*awarenesspb.PreflightResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.PreflightResponse, error) {
		return c.Preflight(ctx, in, opts...)
	})
}

func (f *failoverClient) EditCheck(ctx context.Context, in *awarenesspb.EditCheckRequest, opts ...grpc.CallOption) (*awarenesspb.EditCheckResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.EditCheckResponse, error) {
		return c.EditCheck(ctx, in, opts...)
	})
}

func (f *failoverClient) Propose(ctx context.Context, in *awarenesspb.ProposeRequest, opts ...grpc.CallOption) (*awarenesspb.ProposeResponse, error) {
	return callWithFailover(f.entries, func(c awarenessClient) (*awarenesspb.ProposeResponse, error) {
		return c.Propose(ctx, in, opts...)
	})
}

func awarenessAddrs(raw string) []string {
	seen := make(map[string]struct{})
	addrs := make([]string, 0, 3)
	appendAddr := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		addrs = append(addrs, addr)
	}
	for _, addr := range strings.Split(raw, ",") {
		appendAddr(addr)
	}
	if len(addrs) == 0 {
		appendAddr("localhost:10120")
	}
	if len(addrs) == 1 {
		host := addrs[0]
		if parsedHost, _, err := net.SplitHostPort(addrs[0]); err == nil {
			host = parsedHost
		}
		switch strings.Trim(host, "[]") {
		case "localhost", "127.0.0.1", "::1":
			appendAddr("localhost:10120")
			appendAddr("localhost:9090")
		}
	}
	return addrs
}

// queryModeFromString maps the MCP-facing mode strings onto the proto enum.
// Anything outside the enum is rejected here — this mapping is the reason
// free-form query text can never reach the store through this bridge.
func queryModeFromString(s string) (awarenesspb.QueryMode, error) {
	switch strings.TrimSpace(s) {
	case "by_file":
		return awarenesspb.QueryMode_QUERY_MODE_BY_FILE, nil
	case "by_id":
		return awarenesspb.QueryMode_QUERY_MODE_BY_ID, nil
	case "by_class":
		return awarenesspb.QueryMode_QUERY_MODE_BY_CLASS, nil
	case "related":
		return awarenesspb.QueryMode_QUERY_MODE_RELATED, nil
	default:
		return awarenesspb.QueryMode_QUERY_MODE_UNSPECIFIED, fmt.Errorf("mode must be one of by_file|by_id|by_class|related, got %q", s)
	}
}

func queryClassFromString(s string) (awarenesspb.QueryClass, error) {
	switch strings.TrimSpace(s) {
	case "invariant":
		return awarenesspb.QueryClass_QUERY_CLASS_INVARIANT, nil
	case "failure_mode":
		return awarenesspb.QueryClass_QUERY_CLASS_FAILURE_MODE, nil
	case "incident_pattern":
		return awarenesspb.QueryClass_QUERY_CLASS_INCIDENT_PATTERN, nil
	case "intent":
		return awarenesspb.QueryClass_QUERY_CLASS_INTENT, nil
	case "symbol":
		return awarenesspb.QueryClass_QUERY_CLASS_SYMBOL, nil
	case "source_file":
		return awarenesspb.QueryClass_QUERY_CLASS_SOURCE_FILE, nil
	case "code_symbol":
		return awarenesspb.QueryClass_QUERY_CLASS_CODE_SYMBOL, nil
	default:
		return awarenesspb.QueryClass_QUERY_CLASS_UNSPECIFIED, fmt.Errorf("class is required for mode=by_class: one of invariant|failure_mode|incident_pattern|intent|symbol|source_file|code_symbol, got %q", s)
	}
}

func preflightModeFromString(s string) (awarenesspb.PreflightMode, error) {
	switch strings.TrimSpace(s) {
	case "compact":
		return awarenesspb.PreflightMode_PREFLIGHT_COMPACT, nil
	case "standard":
		return awarenesspb.PreflightMode_PREFLIGHT_STANDARD, nil
	default:
		return awarenesspb.PreflightMode_PREFLIGHT_MODE_UNSPECIFIED, fmt.Errorf("mode must be compact or standard, got %q", s)
	}
}

func formatQuery(resp *awarenesspb.QueryResponse) string {
	var b strings.Builder
	b.WriteString(formatGraphAuthority(resp.GetAuthority()))
	fmt.Fprintf(&b, "rows: %d\ngenerated_in_ms: %d\n", len(resp.GetRows()), resp.GetGeneratedInMs())
	for _, r := range resp.GetRows() {
		fmt.Fprintf(&b, "\n- id: %s\n  class: %s\n  label: %s", r.GetId(), r.GetClass(), r.GetLabel())
		if r.GetSeverity() != "" {
			fmt.Fprintf(&b, "\n  severity: %s", r.GetSeverity())
		}
		if r.GetStatus() != "" {
			fmt.Fprintf(&b, "\n  status: %s", r.GetStatus())
		}
		if r.GetRelation() != "" {
			fmt.Fprintf(&b, "\n  relation: %s", r.GetRelation())
		}
		if r.GetSourceFile() != "" {
			fmt.Fprintf(&b, "\n  source_file: %s", r.GetSourceFile())
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatMetadata(resp *awarenesspb.MetadataResponse) string {
	var b strings.Builder
	mv := awarenessclient.InterpretMetadataAuthority(resp)
	verdict, state, warning := mv.Verdict, mv.State, mv.Warning
	effectiveState := awarenessclient.EffectiveMetadataFreshness(resp)
	fmt.Fprintf(&b, "graph_authority.verdict: %s\n", verdict)
	fmt.Fprintf(&b, "graph_authority.state: %s\n", state)
	if warning != "" {
		fmt.Fprintf(&b, "graph_authority.warning: %s\n", warning)
	}
	fmt.Fprintf(&b, "graph_build_commit: %s\n", resp.GetGraphBuildCommit())
	fmt.Fprintf(&b, "graph_build_time_unix: %d\n", resp.GetGraphBuildTimeUnix())
	fmt.Fprintf(&b, "source_repo_commit: %s\n", resp.GetSourceRepoCommit())
	fmt.Fprintf(&b, "embedded_seed_digest_sha256: %s\n", resp.GetEmbeddedSeedDigestSha256())
	fmt.Fprintf(&b, "embedded_seed_marker_iri: %s\n", resp.GetEmbeddedSeedMarkerIri())
	fmt.Fprintf(&b, "live_store_contains_embedded_seed_marker: %t\n", resp.GetLiveStoreContainsEmbeddedSeedMarker())
	fmt.Fprintf(&b, "build_provenance_state: %s\n", resp.GetBuildProvenanceState().String())
	fmt.Fprintf(&b, "coverage_state: %s\n", resp.GetCoverageState().String())
	fmt.Fprintf(&b, "seed_state: %s\n", resp.GetSeedState().String())
	fmt.Fprintf(&b, "graph_freshness_state: %s\n", effectiveState.String())
	if raw := resp.GetGraphFreshnessState(); raw != awarenesspb.GraphFreshnessState_GRAPH_FRESHNESS_STATE_UNSPECIFIED && raw != effectiveState {
		fmt.Fprintf(&b, "graph_freshness_state_raw: %s\n", raw.String())
	}
	fmt.Fprintf(&b, "graph_freshness_detail: %s\n", resp.GetGraphFreshnessDetail())
	fmt.Fprintf(&b, "candidate_queue_state: %s\n", resp.GetCandidateQueueState().String())
	fmt.Fprintf(&b, "local_candidate_file_count: %d\n", resp.GetLocalCandidateFileCount())
	fmt.Fprintf(&b, "local_candidate_entry_count: %d\n", resp.GetLocalCandidateEntryCount())
	fmt.Fprintf(&b, "benchmark_state: %s\n", resp.GetBenchmarkState().String())
	fmt.Fprintf(&b, "benchmark_contract_count: %d\n", resp.GetBenchmarkContractCount())
	fmt.Fprintf(&b, "benchmark_learning_event_count: %d\n", resp.GetBenchmarkLearningEventCount())
	fmt.Fprintf(&b, "benchmark_latest_learning_event_unix: %d\n", resp.GetBenchmarkLatestLearningEventUnix())
	fmt.Fprintf(&b, "benchmark_latest_task_id: %s\n", resp.GetBenchmarkLatestTaskId())
	fmt.Fprintf(&b, "benchmark_latest_score: %d\n", resp.GetBenchmarkLatestScore())
	fmt.Fprintf(&b, "benchmark_latest_certification_status: %s\n", resp.GetBenchmarkLatestCertificationStatus())
	fmt.Fprintf(&b, "server_version: %s\n", resp.GetServerVersion())
	fmt.Fprintf(&b, "server_started_unix: %d\n", resp.GetServerStartedUnix())
	fmt.Fprintf(&b, "triple_count: %d\n", resp.GetTripleCount())
	fmt.Fprintf(&b, "invariant_count: %d\n", resp.GetInvariantCount())
	fmt.Fprintf(&b, "failure_mode_count: %d\n", resp.GetFailureModeCount())
	fmt.Fprintf(&b, "incident_pattern_count: %d\n", resp.GetIncidentPatternCount())
	fmt.Fprintf(&b, "intent_count: %d\n", resp.GetIntentCount())
	fmt.Fprintf(&b, "forbidden_fix_count: %d\n", resp.GetForbiddenFixCount())
	fmt.Fprintf(&b, "required_test_count: %d\n", resp.GetRequiredTestCount())
	fmt.Fprintf(&b, "source_file_count: %d\n", resp.GetSourceFileCount())
	fmt.Fprintf(&b, "code_symbol_count: %d\n", resp.GetCodeSymbolCount())
	fmt.Fprintf(&b, "briefing_call_count: %d\n", resp.GetBriefingCallCount())
	fmt.Fprintf(&b, "briefing_agent_compact_count: %d\n", resp.GetBriefingAgentCompactCount())
	fmt.Fprintf(&b, "resolve_call_count: %d\n", resp.GetResolveCallCount())
	fmt.Fprintf(&b, "resolve_found_count: %d\n", resp.GetResolveFoundCount())
	fmt.Fprintf(&b, "resolve_miss_count: %d\n", resp.GetResolveMissCount())
	fmt.Fprintf(&b, "generated_in_ms: %d", resp.GetGeneratedInMs())
	return b.String()
}

// formatGraphAuthority renders the authority stamp as ONE line for the text
// block (Pillar 3.1 — the ~19-line prose dump moved into structuredContent via
// authorityStruct). Trailing newline so callers can prepend it to a body.
func formatGraphAuthority(authority *awarenesspb.GraphAuthority) string {
	return authorityOneLine(authority) + "\n"
}

// Authority interpretation (verdict / freshness / warning) is centralized in
// package client — see golang/client/authority.go — so the honesty signal
// stays identical across the CLI, this bridge, and editor clients.

func formatEditCheck(resp *awarenesspb.EditCheckResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "rules_evaluated: %d\n", resp.GetRulesEvaluated())
	ws := resp.GetWarnings()
	fmt.Fprintf(&b, "warnings: %d\n", len(ws))
	if len(ws) == 0 {
		b.WriteString("\nno advisory rule tripped for this edit.\n")
		return b.String()
	}
	for _, w := range ws {
		fmt.Fprintf(&b, "\n[%s] %s (%s)\n  %s\n  %s\n", w.GetSeverity(), w.GetRuleId(), w.GetClass(), w.GetMessage(), w.GetDetail())
		if p := w.GetProvenance(); p != "" {
			fmt.Fprintf(&b, "  provenance: %s\n", p)
		}
	}
	return b.String()
}

func formatPreflight(resp *awarenesspb.PreflightResponse) string {
	var b strings.Builder
	b.WriteString(formatGraphAuthority(resp.GetAuthority()))
	fmt.Fprintf(&b, "status: %s\n", resp.GetStatus().String())
	fmt.Fprintf(&b, "risk_class: %s\n", resp.GetRiskClass().String())
	fmt.Fprintf(&b, "confidence: %s\n", resp.GetConfidence().String())
	writeNodes := func(title string, nodes []*awarenesspb.KnowledgeNode) {
		if len(nodes) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, n := range nodes {
			fmt.Fprintf(&b, "- [%s] %s — %s\n", n.GetSeverity(), n.GetId(), n.GetLabel())
		}
	}
	writeNodes("direct_invariants", resp.GetDirectInvariants())
	writeNodes("direct_failure_modes", resp.GetDirectFailureModes())
	writeNodes("direct_intents", resp.GetDirectIntents())
	writeNodes("direct_forbidden_fixes", resp.GetDirectForbiddenFixes())
	writeNodes("direct_required_tests", resp.GetDirectRequiredTests())
	if pats := resp.GetImplementationPatterns(); len(pats) > 0 {
		b.WriteString("\nimplementation_patterns:\n")
		for _, p := range pats {
			fmt.Fprintf(&b, "- [%s] %s — %s\n", p.GetMatchStrength(), p.GetId(), p.GetLabel())
			for _, m := range p.GetMustFollow() {
				fmt.Fprintf(&b, "  must_follow: %s\n", m)
			}
		}
	}
	writeList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, s := range items {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	writeList("required_actions", resp.GetRequiredActions())
	writeList("forbidden_fixes", resp.GetForbiddenFixes())
	writeList("tests_to_run", resp.GetTestsToRun())
	writeList("files_to_read", resp.GetFilesToRead())
	writeList("blind_spots", resp.GetBlindSpots())
	if cov := resp.GetCoverage(); cov != nil {
		fmt.Fprintf(&b, "\ncoverage: anchors=%d files=%d indexed=%d sufficient=%v",
			cov.GetDirectAnchorCount(), cov.GetFileCount(), cov.GetIndexedFileCount(), cov.GetSufficient())
		if cov.GetNote() != "" {
			fmt.Fprintf(&b, " — %s", cov.GetNote())
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\ngenerated_in_ms: %d", resp.GetGeneratedInMs())
	return b.String()
}

func formatImpact(resp *awarenesspb.ImpactResponse) string {
	var b strings.Builder
	b.WriteString(formatGraphAuthority(resp.GetAuthority()))
	// Text now lists the REAL nodes (id + label), not just counts — the full
	// structured payload rides in structuredContent (structImpact).
	writeNodeLines := func(title string, nodes []*awarenesspb.KnowledgeNode) {
		fmt.Fprintf(&b, "%s: %d\n", title, len(nodes))
		for _, n := range nodes {
			sev := n.GetSeverity()
			if sev == "" {
				sev = "-"
			}
			fmt.Fprintf(&b, "  - [%s] %s:%s — %s\n", sev, n.GetClass(), n.GetId(), n.GetLabel())
		}
	}
	writeNodeLines("direct_invariants", resp.GetDirectInvariants())
	writeNodeLines("direct_failure_modes", resp.GetDirectFailureModes())
	writeNodeLines("direct_incident_patterns", resp.GetDirectIncidentPatterns())
	writeNodeLines("direct_intents", resp.GetDirectIntents())
	writeNodeLines("required_tests", resp.GetRequiredTests())
	writeNodeLines("forbidden_fixes", resp.GetForbiddenFixes())
	if syms := resp.GetSymbols(); len(syms) > 0 {
		fmt.Fprintf(&b, "symbols: %d\n", len(syms))
	}
	return strings.TrimRight(b.String(), "\n")
}

// toolResult is one tool call's output: a compact human-facing Text block AND a
// machine-parseable Structured object. Text keeps a one-line authority + the
// essentials; Structured carries the full nodes/provenance so an agent parses
// JSON instead of regexing prose (Pillar 3.1). Structured is emitted as the MCP
// result's `structuredContent`; clients that don't read it still get Text.
type toolResult struct {
	Text       string
	Structured interface{}
}

// authorityOneLine collapses the graph-authority stamp to a single line, e.g.
// "authority: authoritative (current)" or
// "authority: non_authoritative (stale) — <warning>". The full provenance moves
// into the structured object (authorityStruct), so text stays terse.
func authorityOneLine(a *awarenesspb.GraphAuthority) string {
	av := awarenessclient.InterpretAuthority(a)
	line := fmt.Sprintf("authority: %s (%s)", av.Verdict, av.State)
	if av.Warning != "" {
		line += " — " + av.Warning
	}
	return line
}

// authorityStruct is the full, machine-parseable authority object: the
// interpreted verdict PLUS the raw provenance fields (build commits, digests,
// certification). Nothing the old ~19-line text block carried is lost — it just
// moves from prose into JSON.
func authorityStruct(a *awarenesspb.GraphAuthority) map[string]interface{} {
	av := awarenessclient.InterpretAuthority(a)
	obj := map[string]interface{}{
		"verdict":       av.Verdict,
		"state":         av.State,
		"authoritative": av.Authoritative,
	}
	if av.Warning != "" {
		obj["warning"] = av.Warning
	}
	if a == nil {
		return obj
	}
	obj["graph_freshness_state"] = a.GetGraphFreshnessState().String()
	obj["build_provenance_state"] = a.GetBuildProvenanceState().String()
	obj["seed_state"] = a.GetSeedState().String()
	obj["graph_build_commit"] = a.GetGraphBuildCommit()
	obj["graph_build_time_unix"] = a.GetGraphBuildTimeUnix()
	obj["source_repo_commit"] = a.GetSourceRepoCommit()
	obj["live_store_graph_triple_count"] = a.GetLiveStoreGraphTripleCount()
	obj["embedded_transaction_matches_seed"] = a.GetEmbeddedTransactionMatchesSeed()
	obj["certified_awareness_graph_commit"] = a.GetCertifiedAwarenessGraphCommit()
	obj["certified_services_repo_commit"] = a.GetCertifiedServicesRepoCommit()
	if d := strings.TrimSpace(a.GetGraphFreshnessDetail()); d != "" {
		obj["graph_freshness_detail"] = d
	}
	if d := strings.TrimSpace(a.GetEmbeddedTransactionDetail()); d != "" {
		obj["embedded_transaction_detail"] = d
	}
	return obj
}

// nodeObj serializes one KnowledgeNode to a compact JSON object. Empty optional
// fields are omitted so the structured payload stays lean.
func nodeObj(n *awarenesspb.KnowledgeNode) map[string]interface{} {
	obj := map[string]interface{}{
		"id":    n.GetClass() + ":" + n.GetId(),
		"class": n.GetClass(),
		"label": n.GetLabel(),
	}
	if s := n.GetSeverity(); s != "" {
		obj["severity"] = s
	}
	if s := n.GetStatus(); s != "" {
		obj["status"] = s
	}
	if r := n.GetRelatedIds(); len(r) > 0 {
		obj["related_ids"] = r
	}
	return obj
}

// nodeObjs serializes a slice of KnowledgeNodes; returns nil for an empty slice
// so the key can be omitted upstream.
func nodeObjs(nodes []*awarenesspb.KnowledgeNode) []map[string]interface{} {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeObj(n))
	}
	return out
}

// putNodes adds a node list to obj only when non-empty, keeping the structured
// impact payload free of empty arrays.
func putNodes(obj map[string]interface{}, key string, nodes []*awarenesspb.KnowledgeNode) {
	if s := nodeObjs(nodes); s != nil {
		obj[key] = s
	}
}

// structImpact is the machine-parseable Impact payload: the REAL nodes the file
// is anchored to (not just counts — that was the Pillar 3.1 blocker), split
// direct vs inferred, plus symbols and their references.
func structImpact(resp *awarenesspb.ImpactResponse) map[string]interface{} {
	obj := map[string]interface{}{"authority": authorityStruct(resp.GetAuthority())}
	putNodes(obj, "direct_invariants", resp.GetDirectInvariants())
	putNodes(obj, "direct_failure_modes", resp.GetDirectFailureModes())
	putNodes(obj, "direct_incident_patterns", resp.GetDirectIncidentPatterns())
	putNodes(obj, "direct_intents", resp.GetDirectIntents())
	putNodes(obj, "inferred_invariants", resp.GetInferredInvariants())
	putNodes(obj, "inferred_failure_modes", resp.GetInferredFailureModes())
	putNodes(obj, "inferred_incident_patterns", resp.GetInferredIncidentPatterns())
	putNodes(obj, "inferred_intents", resp.GetInferredIntents())
	putNodes(obj, "required_tests", resp.GetRequiredTests())
	putNodes(obj, "forbidden_fixes", resp.GetForbiddenFixes())
	putNodes(obj, "direct_architecture", resp.GetDirectArchitecture())
	if syms := resp.GetSymbols(); len(syms) > 0 {
		list := make([]map[string]interface{}, 0, len(syms))
		for _, s := range syms {
			so := map[string]interface{}{"id": s.GetId(), "label": s.GetLabel(), "file": s.GetFile()}
			if l := s.GetLanguage(); l != "" {
				so["language"] = l
			}
			if r := s.GetReferences(); len(r) > 0 {
				so["references"] = r
			}
			list = append(list, so)
		}
		obj["symbols"] = list
	}
	return obj
}

// structBriefing is the machine-parseable Briefing payload.
func structBriefing(resp *awarenesspb.BriefingResponse) map[string]interface{} {
	status := "EMPTY"
	if resp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_EMPTY {
		status = resp.GetStatus().String()
	}
	ids := resp.GetReferencedIds()
	if ids == nil {
		ids = []string{}
	}
	return map[string]interface{}{
		"authority":       authorityStruct(resp.GetAuthority()),
		"status":          status,
		"generated_in_ms": resp.GetGeneratedInMs(),
		"referenced_ids":  ids,
		"prose":           resp.GetProse(),
	}
}

// structResolve is the machine-parseable Resolve payload.
func structResolve(resp *awarenesspb.ResolveResponse) map[string]interface{} {
	obj := map[string]interface{}{
		"authority": authorityStruct(resp.GetAuthority()),
		"found":     resp.GetFound(),
	}
	if resp.GetFound() {
		obj["node"] = nodeObj(resp.GetNode())
	}
	return obj
}

// structQuery is the machine-parseable Query payload.
func structQuery(resp *awarenesspb.QueryResponse) map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(resp.GetRows()))
	for _, r := range resp.GetRows() {
		row := map[string]interface{}{"id": r.GetId(), "class": r.GetClass(), "label": r.GetLabel()}
		if v := r.GetSeverity(); v != "" {
			row["severity"] = v
		}
		if v := r.GetStatus(); v != "" {
			row["status"] = v
		}
		if v := r.GetRelation(); v != "" {
			row["relation"] = v
		}
		if v := r.GetSourceFile(); v != "" {
			row["source_file"] = v
		}
		rows = append(rows, row)
	}
	return map[string]interface{}{
		"authority":       authorityStruct(resp.GetAuthority()),
		"generated_in_ms": resp.GetGeneratedInMs(),
		"rows":            rows,
	}
}

// structMetadata is the machine-parseable Metadata payload: the counts and
// state signals the text block renders, as a JSON object.
func structMetadata(resp *awarenesspb.MetadataResponse) map[string]interface{} {
	mv := awarenessclient.InterpretMetadataAuthority(resp)
	auth := map[string]interface{}{"verdict": mv.Verdict, "state": mv.State, "authoritative": mv.Authoritative}
	if mv.Warning != "" {
		auth["warning"] = mv.Warning
	}
	return map[string]interface{}{
		"authority":              auth,
		"graph_freshness_state":  awarenessclient.EffectiveMetadataFreshness(resp).String(),
		"build_provenance_state": resp.GetBuildProvenanceState().String(),
		"seed_state":             resp.GetSeedState().String(),
		"coverage_state":         resp.GetCoverageState().String(),
		"graph_build_commit":     resp.GetGraphBuildCommit(),
		"source_repo_commit":     resp.GetSourceRepoCommit(),
		"triple_count":           resp.GetTripleCount(),
		"invariant_count":        resp.GetInvariantCount(),
		"failure_mode_count":     resp.GetFailureModeCount(),
		"incident_pattern_count": resp.GetIncidentPatternCount(),
		"intent_count":           resp.GetIntentCount(),
		"forbidden_fix_count":    resp.GetForbiddenFixCount(),
		"required_test_count":    resp.GetRequiredTestCount(),
		"source_file_count":      resp.GetSourceFileCount(),
		"code_symbol_count":      resp.GetCodeSymbolCount(),
		"server_version":         resp.GetServerVersion(),
		"generated_in_ms":        resp.GetGeneratedInMs(),
	}
}

// structPreflight is the machine-parseable Preflight payload.
func structPreflight(resp *awarenesspb.PreflightResponse) map[string]interface{} {
	obj := map[string]interface{}{
		"authority":  authorityStruct(resp.GetAuthority()),
		"status":     resp.GetStatus().String(),
		"risk_class": resp.GetRiskClass().String(),
		"confidence": resp.GetConfidence().String(),
	}
	putNodes(obj, "direct_invariants", resp.GetDirectInvariants())
	putNodes(obj, "direct_failure_modes", resp.GetDirectFailureModes())
	putNodes(obj, "direct_intents", resp.GetDirectIntents())
	putNodes(obj, "direct_forbidden_fixes", resp.GetDirectForbiddenFixes())
	putNodes(obj, "direct_required_tests", resp.GetDirectRequiredTests())
	putStrings(obj, "required_actions", resp.GetRequiredActions())
	putStrings(obj, "forbidden_fixes", resp.GetForbiddenFixes())
	putStrings(obj, "tests_to_run", resp.GetTestsToRun())
	putStrings(obj, "files_to_read", resp.GetFilesToRead())
	putStrings(obj, "blind_spots", resp.GetBlindSpots())
	return obj
}

// structEditCheck is the machine-parseable EditCheck payload — the warnings an
// agent self-governs on, each carrying rule id, message, enforcement, and
// provenance (this is the "gate result over MCP" an agent needs).
func structEditCheck(resp *awarenesspb.EditCheckResponse) map[string]interface{} {
	ws := resp.GetWarnings()
	list := make([]map[string]interface{}, 0, len(ws))
	for _, w := range ws {
		wo := map[string]interface{}{
			"rule_id":     w.GetRuleId(),
			"class":       w.GetClass(),
			"severity":    w.GetSeverity(),
			"message":     w.GetMessage(),
			"enforcement": w.GetEnforcement(),
		}
		if d := w.GetDetail(); d != "" {
			wo["detail"] = d
		}
		if p := w.GetProvenance(); p != "" {
			wo["provenance"] = p
		}
		list = append(list, wo)
	}
	return map[string]interface{}{
		"rules_evaluated": resp.GetRulesEvaluated(),
		"warnings":        list,
	}
}

// putStrings adds a string list to obj only when non-empty.
func putStrings(obj map[string]interface{}, key string, items []string) {
	if len(items) > 0 {
		obj[key] = items
	}
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcErr     `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func responsePayload(id interface{}, result interface{}, err error) ([]byte, error) {
	out := rpcResp{JSONRPC: "2.0", ID: id}
	if err != nil {
		code := -32000
		msg := err.Error()
		if st, ok := status.FromError(err); ok {
			msg = st.Code().String() + ": " + st.Message()
		}
		out.Error = &rpcErr{Code: code, Message: msg}
	} else {
		out.Result = result
	}
	return json.Marshal(out)
}

type stdioSession struct {
	r       *bufio.Reader
	w       *bufio.Writer
	modeSet bool
	framed  bool
}

func newStdioSession(r io.Reader, w io.Writer) *stdioSession {
	return &stdioSession{
		r: bufio.NewReader(r),
		w: bufio.NewWriter(w),
	}
}

func (s *stdioSession) readMessage() ([]byte, error) {
	for {
		if s.modeSet {
			if s.framed {
				return s.readFramedMessage("")
			}
			line, err := s.r.ReadString('\n')
			if err != nil {
				if err == io.EOF && strings.TrimSpace(line) == "" {
					return nil, io.EOF
				}
				if strings.TrimSpace(line) != "" {
					return []byte(strings.TrimSpace(line)), nil
				}
				return nil, err
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			return []byte(line), nil
		}

		line, err := s.r.ReadString('\n')
		if err != nil {
			if err == io.EOF && strings.TrimSpace(line) == "" {
				return nil, io.EOF
			}
			if strings.TrimSpace(line) != "" {
				s.modeSet = true
				return []byte(strings.TrimSpace(line)), nil
			}
			return nil, err
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
			s.modeSet = true
			s.framed = true
			return s.readFramedMessage(line)
		}
		s.modeSet = true
		return []byte(trimmed), nil
	}
}

func (s *stdioSession) readFramedMessage(firstLine string) ([]byte, error) {
	contentLength := -1
	parseHeader := func(line string) error {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header line %q", strings.TrimSpace(line))
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return fmt.Errorf("invalid Content-Length %q: %w", strings.TrimSpace(parts[1]), err)
			}
			contentLength = n
		}
		return nil
	}
	if strings.TrimSpace(firstLine) != "" {
		if err := parseHeader(firstLine); err != nil {
			return nil, err
		}
	}
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		if err := parseHeader(line); err != nil {
			return nil, err
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(s.r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (s *stdioSession) writeResponse(id interface{}, result interface{}, err error) error {
	payload, marshalErr := responsePayload(id, result, err)
	if marshalErr != nil {
		return marshalErr
	}
	if s.modeSet && s.framed {
		if _, err := fmt.Fprintf(s.w, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
			return err
		}
		if _, err := s.w.Write(payload); err != nil {
			return err
		}
	} else {
		if _, err := s.w.Write(append(payload, '\n')); err != nil {
			return err
		}
	}
	return s.w.Flush()
}

func serveStdio(br *bridge, r io.Reader, w io.Writer) error {
	session := newStdioSession(r, w)
	for {
		msg, err := session.readMessage()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		var req rpcReq
		if err := json.Unmarshal(msg, &req); err != nil {
			if writeErr := session.writeResponse(nil, nil, fmt.Errorf("invalid json: %w", err)); writeErr != nil {
				return writeErr
			}
			continue
		}
		var id interface{}
		if len(req.ID) > 0 {
			_ = json.Unmarshal(req.ID, &id)
		}
		// JSON-RPC 2.0: a request without an "id" is a notification. The
		// server MUST NOT send a response to a notification — not even an
		// error. Replying to e.g. notifications/initialized produces a stray
		// frame that strict MCP clients (Claude Code's loader, `claude mcp
		// list`) treat as a fatal handshake violation → "Failed to connect".
		if len(req.ID) == 0 {
			continue
		}
		switch req.Method {
		case "initialize":
			// The initialize result MUST carry protocolVersion. Echo the
			// client's requested version when present (falling back to a
			// supported default); omitting it makes strict MCP clients reject
			// the handshake → "Failed to connect".
			protocolVersion := "2024-11-05"
			if len(req.Params) > 0 {
				var p struct {
					ProtocolVersion string `json:"protocolVersion"`
				}
				if json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
					protocolVersion = p.ProtocolVersion
				}
			}
			if err := session.writeResponse(id, map[string]interface{}{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]string{"name": "awareness-mcp", "version": "0.1.0"},
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
			}, nil); err != nil {
				return err
			}
		case "tools/list":
			if err := session.writeResponse(id, map[string]interface{}{"tools": br.tools()}, nil); err != nil {
				return err
			}
		case "tools/call":
			var params struct {
				Name      string                 `json:"name"`
				Arguments map[string]interface{} `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				if writeErr := session.writeResponse(id, nil, fmt.Errorf("bad tools/call params: %w", err)); writeErr != nil {
					return writeErr
				}
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), br.timeout)
			res, err := br.callTool(ctx, params.Name, params.Arguments)
			cancel()
			if err != nil {
				if writeErr := session.writeResponse(id, nil, err); writeErr != nil {
					return writeErr
				}
				continue
			}
			result := map[string]interface{}{
				"content": []map[string]interface{}{{"type": "text", "text": res.Text}},
				"isError": false,
			}
			// structuredContent lets an agent parse the answer as JSON instead of
			// regexing the prose (Pillar 3.1). Older clients ignore the field and
			// still get the text block.
			if res.Structured != nil {
				result["structuredContent"] = res.Structured
			}
			if err := session.writeResponse(id, result, nil); err != nil {
				return err
			}
		case "ping":
			if err := session.writeResponse(id, map[string]string{"pong": "true"}, nil); err != nil {
				return err
			}
		default:
			if err := session.writeResponse(id, nil, fmt.Errorf("method not found: %s", req.Method)); err != nil {
				return err
			}
		}
	}
}

// main connects to awareness-graph over direct insecure gRPC.
// NOTE: this bridge uses custom service discovery (--awareness-addr flag) rather
// than the Globular gateway/client pool. Production traffic should go through
// the Globular MCP service which resolves endpoints from etcd and uses mTLS.
//
// @awareness namespace=globular.awareness_graph
// @awareness component=mcp.bridge
// @awareness relates_to=globular.awareness_graph:intent.awareness.mcp_tools_use_gateway_client_pool
func main() {
	awarenessAddr := flag.String("awareness-addr", "localhost:10120", "awareness-graph gRPC address (or comma-separated fallback list)")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request gRPC timeout")
	flag.Parse()

	addrs := awarenessAddrs(*awarenessAddr)
	entries := make([]clientEntry, 0, len(addrs))
	for _, addr := range addrs {
		conn, err := awarenessclient.DialConn(addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awareness-mcp: dial %s: %v\n", addr, err)
			continue
		}
		entries = append(entries, clientEntry{
			addr:   addr,
			client: awarenesspb.NewAwarenessGraphClient(conn),
			close:  conn.Close,
		})
	}
	if len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "awareness-mcp: no valid awareness-graph addresses from %q\n", *awarenessAddr)
		os.Exit(1)
	}
	defer func() {
		for _, entry := range entries {
			if entry.close != nil {
				_ = entry.close()
			}
		}
	}()

	br := &bridge{
		client:  &failoverClient{entries: entries},
		timeout: *timeout,
	}
	if err := serveStdio(br, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "awareness-mcp: serve: %v\n", err)
		os.Exit(1)
	}
}
