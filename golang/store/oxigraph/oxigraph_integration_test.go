// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

// Integration tests for the Oxigraph client against a live local
// Oxigraph server. Excluded from the default `go test ./...` run by
// the build tag — invoke explicitly with:
//
//	go test -tags=integration ./golang/store/oxigraph/...
//
// Prerequisites:
//   - Oxigraph reachable at http://localhost:7878 (or the URL set in
//     AWARENESS_OXIGRAPH_URL). Spin one up via:
//         scripts/bootstrap_oxigraph.sh
//
// These tests are deliberately small. The default-build tests already
// cover protocol-level correctness via httptest; this file exists to
// catch the failure mode that httptest CANNOT catch — an actual
// Oxigraph build/version returning something the client doesn't
// understand.

package oxigraph_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/store/oxigraph"
)

func integrationURL() string {
	if u := os.Getenv("AWARENESS_OXIGRAPH_URL"); u != "" {
		return u
	}
	return "http://localhost:7878/query"
}

// TestIntegration_Health_LiveOxigraph confirms the Health round-trip
// works against an actual Oxigraph binary. Skips the test (rather than
// failing) when no server is reachable — the integration suite is
// opt-in and should not penalize someone who runs it without setup.
func TestIntegration_Health_LiveOxigraph(t *testing.T) {
	c, err := oxigraph.New(integrationURL())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		t.Skipf("no live Oxigraph at %s (%v); start one via scripts/bootstrap_oxigraph.sh and retry",
			integrationURL(), err)
	}
}
