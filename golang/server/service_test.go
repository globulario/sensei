// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/globulario/awareness-graph/golang/store"
)

const registeredGRPCServiceName = "globular.awareness_graph.AwarenessGraph"

func TestSetupGrpcService_RegistersAwarenessService(t *testing.T) {
	gs := grpc.NewServer()
	setupGrpcService(gs, newServer(nil))
	info := gs.GetServiceInfo()
	if _, ok := info[registeredGRPCServiceName]; !ok {
		t.Fatalf("service %q not registered; got %v", registeredGRPCServiceName, info)
	}
}

func TestPrintDescribeJSON_ContainsSafeQueryFlag(t *testing.T) {
	cfg := defaultServiceConfig()
	var buf bytes.Buffer
	if err := printDescribeJSON(&buf, cfg); err != nil {
		t.Fatalf("printDescribeJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode describe json: %v", err)
	}
	if got["QueryExposedAsSPARQL"] != false {
		t.Fatalf("QueryExposedAsSPARQL=%v, want false", got["QueryExposedAsSPARQL"])
	}
	if got["Name"] != defaultServiceName {
		t.Fatalf("Name=%v, want %q", got["Name"], defaultServiceName)
	}
	if got["Protocol"] != "grpc" {
		t.Fatalf("Protocol=%v, want grpc", got["Protocol"])
	}
	if got["Port"] != float64(defaultServicePort) {
		t.Fatalf("Port=%v, want %d", got["Port"], defaultServicePort)
	}
	if got["OxigraphQueryURL"] != "http://localhost:7878/query" {
		t.Fatalf("OxigraphQueryURL=%v, want http://localhost:7878/query", got["OxigraphQueryURL"])
	}
}

func TestPrintHealthJSON_DegradedWhenURLInvalid(t *testing.T) {
	cfg := defaultServiceConfig()
	cfg.OxigraphQueryURL = "://bad-url"
	var buf bytes.Buffer
	exitCode := printHealthJSON(&buf, cfg)
	if exitCode != 0 {
		t.Fatalf("printHealthJSON exitCode=%d, want 0", exitCode)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode health json: %v", err)
	}
	if got["status"] != "degraded" {
		t.Fatalf("status=%v, want degraded", got["status"])
	}
	if got["query_exposed_as_sparql"] != false {
		t.Fatalf("query_exposed_as_sparql=%v, want false", got["query_exposed_as_sparql"])
	}
	if got["mcp_query_exposed_by_default"] != false {
		t.Fatalf("mcp_query_exposed_by_default=%v, want false", got["mcp_query_exposed_by_default"])
	}
}

type fakeHealthReporter struct {
	statuses []healthpb.HealthCheckResponse_ServingStatus
}

func (f *fakeHealthReporter) SetServingStatus(_ string, status healthpb.HealthCheckResponse_ServingStatus) {
	f.statuses = append(f.statuses, status)
}

type flapStore struct {
	errs []error
	idx  int
}

func (f *flapStore) Close() error { return nil }
func (f *flapStore) Health(_ context.Context) error {
	if f.idx >= len(f.errs) {
		return f.errs[len(f.errs)-1]
	}
	err := f.errs[f.idx]
	f.idx++
	return err
}
func (f *flapStore) Describe(_ context.Context, _ string) ([]store.Triple, error) { return nil, nil }
func (f *flapStore) DescribeInbound(_ context.Context, _ string) ([]store.InboundTriple, error) {
	return nil, nil
}
func (f *flapStore) ImpactForFile(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (f *flapStore) ClassFacts(_ context.Context, _ string, _ int) ([]store.ImpactFact, error) {
	return nil, nil
}
func (f *flapStore) CodeSymbolFacts(_ context.Context, _ string) ([]store.ImpactFact, error) {
	return nil, nil
}
func (f *flapStore) RenderingGroupsForFile(_ context.Context, _ string) ([]store.RenderingGroupInfo, error) {
	return nil, nil
}
func (f *flapStore) DetectFacts(_ context.Context) ([]store.ImpactFact, error) { return nil, nil }

func TestMonitorBackendHealth_TransitionsToNotServing(t *testing.T) {
	reporter := &fakeHealthReporter{}
	logger := log.New(io.Discard, "", 0)
	store := &flapStore{errs: []error{nil, errors.New("connection refused")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go monitorBackendHealth(ctx, reporter, store, "http://127.0.0.1:7878/query", logger, 10*time.Millisecond)

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(reporter.statuses) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(reporter.statuses) < 3 {
		t.Fatalf("recorded %d statuses, want at least 3", len(reporter.statuses))
	}
	if reporter.statuses[0] != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("initial status=%s, want SERVING", reporter.statuses[0])
	}
	if reporter.statuses[1] != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("healthy poll status=%s, want SERVING", reporter.statuses[1])
	}
	if reporter.statuses[2] != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("degraded status=%s, want NOT_SERVING", reporter.statuses[2])
	}
}
