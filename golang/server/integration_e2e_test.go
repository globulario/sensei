// SPDX-License-Identifier: Apache-2.0

//go:build integration

package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/globulario/sensei/golang/extractor"
	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

func integrationQueryURL() string {
	if u := os.Getenv("AWARENESS_OXIGRAPH_QUERY_URL"); u != "" {
		return u
	}
	return "http://localhost:7878/query"
}

func integrationUpdateURL() string {
	if u := os.Getenv("AWARENESS_OXIGRAPH_UPDATE_URL"); u != "" {
		return u
	}
	return "http://localhost:7878/store?default"
}

func uploadTriples(t *testing.T, updateURL string, nt []byte) {
	t.Helper()
	// Double-gate (ci.destructive_test_must_be_double_gated): the build tag is not
	// enough — this POSTs triples into a live Oxigraph and would pollute a real
	// store at the default localhost:7878. Require an explicit destructive opt-in.
	if os.Getenv("AWARENESS_OXIGRAPH_DESTRUCTIVE") != "1" {
		t.Skip("destructive: uploads triples to a live Oxigraph; set AWARENESS_OXIGRAPH_DESTRUCTIVE=1 and point at a THROWAWAY store (AWARENESS_OXIGRAPH_UPDATE_URL)")
	}
	req, err := http.NewRequest(http.MethodPost, updateURL, bytes.NewReader(nt))
	if err != nil {
		t.Fatalf("build load request: %v", err)
	}
	req.Header.Set("Content-Type", "application/n-triples")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("load request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("load status=%s body=%s", resp.Status, string(body))
	}
}

func TestIntegration_EndToEnd_LoadAndQuery(t *testing.T) {
	queryURL := integrationQueryURL()
	updateURL := integrationUpdateURL()

	storeClient, err := oxigraph.New(queryURL)
	if err != nil {
		t.Fatalf("oxigraph.New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := storeClient.Health(ctx); err != nil {
		t.Skipf("no live Oxigraph at %s (%v); run scripts/bootstrap_oxigraph.sh", queryURL, err)
	}

	var nt bytes.Buffer
	_, err = extractor.ImportAwarenessYAMLs("../extractor/testdata", &nt)
	if err != nil {
		t.Fatalf("ImportAwarenessYAMLs: %v", err)
	}
	if errs := extractor.ValidateNTriples(bytes.NewReader(nt.Bytes())); len(errs) > 0 {
		t.Fatalf("fixture contains invalid N-Triples: %v", errs[0])
	}
	uploadTriples(t, updateURL, nt.Bytes())

	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	awarenesspb.RegisterAwarenessGraphServer(s, newServer(storeClient))
	go func() { _ = s.Serve(lis) }()
	defer s.Stop()

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer conn.Close()
	client := awarenesspb.NewAwarenessGraphClient(conn)

	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	resolveResp, err := client.Resolve(rctx, &awarenesspb.ResolveRequest{
		Class: "invariant",
		Id:    "test.example.invariant",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !resolveResp.GetFound() {
		t.Fatal("Resolve found=false, want true")
	}

	impactResp, err := client.Impact(rctx, &awarenesspb.ImpactRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if len(impactResp.GetDirectInvariants()) == 0 {
		t.Fatal("Impact direct_invariants empty")
	}

	briefingResp, err := client.Briefing(rctx, &awarenesspb.BriefingRequest{File: "test/example.go"})
	if err != nil {
		t.Fatalf("Briefing: %v", err)
	}
	if briefingResp.GetStatus() != awarenesspb.BriefingStatus_BRIEFING_STATUS_OK {
		t.Fatalf("Briefing status=%v, want OK", briefingResp.GetStatus())
	}

	queryResp, err := client.Query(rctx, &awarenesspb.QueryRequest{
		Mode: awarenesspb.QueryMode_QUERY_MODE_BY_FILE,
		File: "test/example.go",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(queryResp.GetRows()) == 0 {
		t.Fatal("Query rows empty")
	}
}
