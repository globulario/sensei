// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net"
	"strings"
	"testing"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// fakeServer implements the minimum AwarenessGraphServer for testing the client
// wiring without an Oxigraph backend.
type fakeServer struct {
	awarenesspb.UnimplementedAwarenessGraphServer
	briefingCalled  bool
	impactCalled    bool
	resolveCalled   bool
	queryCalled     bool
	metadataCalled  bool
	preflightCalled bool
	editCheckCalled bool
	proposeCalled   bool
}

func (f *fakeServer) Briefing(_ context.Context, _ *awarenesspb.BriefingRequest) (*awarenesspb.BriefingResponse, error) {
	f.briefingCalled = true
	return &awarenesspb.BriefingResponse{Status: awarenesspb.BriefingStatus_BRIEFING_STATUS_OK, Prose: "test"}, nil
}
func (f *fakeServer) Impact(_ context.Context, _ *awarenesspb.ImpactRequest) (*awarenesspb.ImpactResponse, error) {
	f.impactCalled = true
	return &awarenesspb.ImpactResponse{}, nil
}
func (f *fakeServer) Resolve(_ context.Context, _ *awarenesspb.ResolveRequest) (*awarenesspb.ResolveResponse, error) {
	f.resolveCalled = true
	return &awarenesspb.ResolveResponse{}, nil
}
func (f *fakeServer) Query(_ context.Context, _ *awarenesspb.QueryRequest) (*awarenesspb.QueryResponse, error) {
	f.queryCalled = true
	return &awarenesspb.QueryResponse{}, nil
}
func (f *fakeServer) Metadata(_ context.Context, _ *awarenesspb.MetadataRequest) (*awarenesspb.MetadataResponse, error) {
	f.metadataCalled = true
	return &awarenesspb.MetadataResponse{}, nil
}
func (f *fakeServer) Preflight(_ context.Context, _ *awarenesspb.PreflightRequest) (*awarenesspb.PreflightResponse, error) {
	f.preflightCalled = true
	return &awarenesspb.PreflightResponse{}, nil
}
func (f *fakeServer) EditCheck(_ context.Context, _ *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
	f.editCheckCalled = true
	return &awarenesspb.EditCheckResponse{}, nil
}
func (f *fakeServer) Propose(_ context.Context, _ *awarenesspb.ProposeRequest) (*awarenesspb.ProposeResponse, error) {
	f.proposeCalled = true
	return &awarenesspb.ProposeResponse{Status: awarenesspb.ProposeStatus_PROPOSE_STATUS_ACCEPTED}, nil
}

func startFakeServer(t *testing.T) (addr string, fake *fakeServer) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fake = &fakeServer{}
	gs := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(gs, fake)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	return lis.Addr().String(), fake
}

func TestDial_Insecure(t *testing.T) {
	addr, _ := startFakeServer(t)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if c.Stub() == nil {
		t.Fatal("Stub() returned nil")
	}
}

func TestAllRPCs(t *testing.T) {
	addr, fake := startFakeServer(t)
	c, err := Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx := context.Background()

	resp, err := c.Briefing(ctx, "file.go", "", "standard", "")
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if resp.Prose != "test" {
		t.Fatalf("Briefing prose = %q, want %q", resp.Prose, "test")
	}
	if !fake.briefingCalled {
		t.Fatal("server Briefing not called")
	}

	if _, err := c.Impact(ctx, "file.go", ""); err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if !fake.impactCalled {
		t.Fatal("server Impact not called")
	}

	if _, err := c.Resolve(ctx, "invariant", "foo", ""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !fake.resolveCalled {
		t.Fatal("server Resolve not called")
	}

	if _, err := c.Query(ctx, &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_FILE,
		File: "file.go",
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !fake.queryCalled {
		t.Fatal("server Query not called")
	}

	if _, err := c.Metadata(ctx); err != nil {
		t.Fatalf("Metadata: %v", err)
	}
	if !fake.metadataCalled {
		t.Fatal("server Metadata not called")
	}

	if _, err := c.Preflight(ctx, &awarenesspb.PreflightRequest{
		Task:  "test task",
		Files: []string{"file.go"},
	}); err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if !fake.preflightCalled {
		t.Fatal("server Preflight not called")
	}

	if _, err := c.EditCheck(ctx, "file.go", "package main", ""); err != nil {
		t.Fatalf("EditCheck: %v", err)
	}
	if !fake.editCheckCalled {
		t.Fatal("server EditCheck not called")
	}

	if _, err := c.Propose(ctx, &awarenesspb.ProposeRequest{Kind: "failure_mode", Title: "x"}); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if !fake.proposeCalled {
		t.Fatal("server Propose not called")
	}
}

func TestWithDialOptions(t *testing.T) {
	addr, _ := startFakeServer(t)
	c, err := Dial(addr, WithDialOptions(
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.Briefing(context.Background(), "f.go", "", "", "")
	if err != nil {
		t.Fatalf("Briefing with custom dial opts: %v", err)
	}
}

func TestClose_Nil(t *testing.T) {
	c := &Client{}
	if err := c.Close(); err != nil {
		t.Fatalf("Close on nil cc: %v", err)
	}
}

func TestWithTLS_InvalidFilesReturnError(t *testing.T) {
	_, err := Dial("127.0.0.1:1", WithTLS("/definitely/missing/ca.pem", "", ""))
	if err == nil {
		t.Fatal("Dial should fail when TLS files are invalid")
	}
	if !strings.Contains(err.Error(), "read CA") {
		t.Fatalf("err = %v, want CA read failure", err)
	}
}
