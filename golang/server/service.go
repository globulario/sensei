// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

const backendHealthPollInterval = 2 * time.Second

type grpcHealthReporter interface {
	SetServingStatus(service string, servingStatus healthpb.HealthCheckResponse_ServingStatus)
}

type backendHealthMonitor struct {
	service string
	logger  loggerLike
	status  atomic.Int32
}

type loggerLike interface {
	Printf(format string, v ...any)
}

func setupGrpcService(gs *grpc.Server, srv *server) *health.Server {
	awarenesspb.RegisterAwarenessGraphServer(gs, srv)

	// Standard gRPC health protocol — required for Envoy active health checks.
	healthSrv := health.NewServer()
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gs, healthSrv)

	reflection.Register(gs)
	return healthSrv
}

func newBackendHealthMonitor(service string, reporter grpcHealthReporter, logger loggerLike) *backendHealthMonitor {
	m := &backendHealthMonitor{
		service: service,
		logger:  logger,
	}
	m.status.Store(int32(healthpb.HealthCheckResponse_SERVICE_UNKNOWN))
	m.setStatus(reporter, healthpb.HealthCheckResponse_SERVING, "backend healthy")
	return m
}

func (m *backendHealthMonitor) setStatus(reporter grpcHealthReporter, next healthpb.HealthCheckResponse_ServingStatus, reason string) {
	if reporter == nil {
		return
	}
	prev := healthpb.HealthCheckResponse_ServingStatus(m.status.Swap(int32(next)))
	reporter.SetServingStatus(m.service, next)
	if m.logger != nil && prev != next {
		m.logger.Printf("awareness-graph: gRPC health %s -> %s (%s)", prev.String(), next.String(), reason)
	}
}

func monitorBackendHealth(ctx context.Context, reporter grpcHealthReporter, st store.Store, endpoint string, logger loggerLike, interval time.Duration) {
	if reporter == nil || st == nil {
		return
	}
	monitor := newBackendHealthMonitor("", reporter, logger)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		checkCtx, cancel := context.WithTimeout(ctx, startupHealthTimeout)
		err := st.Health(checkCtx)
		cancel()
		if err != nil {
			monitor.setStatus(reporter, healthpb.HealthCheckResponse_NOT_SERVING, fmt.Sprintf("backend unhealthy at %s: %v", endpoint, err))
		} else {
			monitor.setStatus(reporter, healthpb.HealthCheckResponse_SERVING, fmt.Sprintf("backend healthy at %s", endpoint))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type serviceDescribe struct {
	ID                   string   `json:"Id"`
	Name                 string   `json:"Name"`
	Description          string   `json:"Description"`
	Protocol             string   `json:"Protocol"`
	Port                 int      `json:"Port"`
	Proxy                int      `json:"Proxy"`
	PublisherID          string   `json:"PublisherId"`
	Keywords             []string `json:"Keywords"`
	KeepAlive            bool     `json:"KeepAlive"`
	KeepUpToDate         bool     `json:"KeepUpToDate"`
	Dependencies         []string `json:"Dependencies"`
	Permissions          []string `json:"Permissions"`
	OxigraphQueryURL     string   `json:"OxigraphQueryURL"`
	QueryExposedAsSPARQL bool     `json:"QueryExposedAsSPARQL"`
	Version              string   `json:"Version"`
}

type serviceHealth struct {
	ServiceName              string `json:"service_name"`
	Version                  string `json:"version"`
	Status                   string `json:"status"`
	OxigraphQueryURL         string `json:"oxigraph_query_url"`
	BackendHealthy           bool   `json:"backend_healthy"`
	BackendError             string `json:"backend_error,omitempty"`
	QueryExposedAsSPARQL     bool   `json:"query_exposed_as_sparql"`
	MCPQueryExposedByDefault bool   `json:"mcp_query_exposed_by_default"`
}

func printDescribeJSON(out io.Writer, cfg serviceConfig) error {
	payload := serviceDescribe{
		ID:                   cfg.ID,
		Name:                 cfg.Name,
		Description:          cfg.Description,
		Protocol:             cfg.Protocol,
		Port:                 cfg.Port,
		Proxy:                cfg.Proxy,
		PublisherID:          cfg.PublisherID,
		Keywords:             cfg.Keywords,
		KeepAlive:            cfg.KeepAlive,
		KeepUpToDate:         cfg.KeepUpToDate,
		Dependencies:         cfg.Dependencies,
		Permissions:          cfg.Permissions,
		OxigraphQueryURL:     cfg.OxigraphQueryURL,
		QueryExposedAsSPARQL: false,
		Version:              Version,
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func printHealthJSON(out io.Writer, cfg serviceConfig) int {
	h := serviceHealth{
		ServiceName:              cfg.Name,
		Version:                  Version,
		Status:                   "ok",
		OxigraphQueryURL:         cfg.OxigraphQueryURL,
		QueryExposedAsSPARQL:     false,
		MCPQueryExposedByDefault: false,
	}
	s, err := oxigraph.New(cfg.OxigraphQueryURL)
	if err != nil {
		h.Status = "degraded"
		h.BackendHealthy = false
		h.BackendError = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), startupHealthTimeout)
		err = s.Health(ctx)
		cancel()
		if err != nil {
			h.Status = "degraded"
			h.BackendHealthy = false
			h.BackendError = err.Error()
		} else {
			h.BackendHealthy = true
		}
		_ = s.Close()
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(h); err != nil {
		fmt.Fprintf(os.Stderr, "awareness-graph: --health encode: %v\n", err)
		return 1
	}
	return 0
}
