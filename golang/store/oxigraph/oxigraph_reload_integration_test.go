// SPDX-License-Identifier: AGPL-3.0-only

//go:build integration

// Live-Oxigraph proof of reload replace-semantics (Weak Spot 2). The
// default-build httptest fixture (oxigraph_reload_test.go) proves the client
// issues PUT /store?default; this proves an actual Oxigraph build honours it as
// REPLACE — reload is idempotent and removed triples do not linger.
//
// DESTRUCTIVE: it REPLACES the default graph of the target store. Run only
// against a throwaway Oxigraph (scripts/bootstrap_oxigraph.sh), never a store
// you care about — so it is double-gated: the `integration` build tag AND an
// explicit opt-in env var AWARENESS_OXIGRAPH_DESTRUCTIVE=1. Without the env var
// it SKIPS even under the integration tag, so it cannot silently clobber a live
// dev store reachable at the default localhost:7878. Skips (does not fail) when
// no live Oxigraph is reachable.
//
//	AWARENESS_OXIGRAPH_DESTRUCTIVE=1 go test -tags=integration ./golang/store/oxigraph/...

package oxigraph_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/store/oxigraph"
)

func TestIntegration_Load_ReplaceSemantics_Idempotent(t *testing.T) {
	if os.Getenv("AWARENESS_OXIGRAPH_DESTRUCTIVE") != "1" {
		t.Skip("destructive reload test: set AWARENESS_OXIGRAPH_DESTRUCTIVE=1 and point at a THROWAWAY Oxigraph " +
			"(it replaces the default graph). Skipped to protect any live store at the default endpoint.")
	}
	c, err := oxigraph.New(integrationURL())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Health(ctx); err != nil {
		t.Skipf("no live Oxigraph at %s (%v); start one via scripts/bootstrap_oxigraph.sh", integrationURL(), err)
	}

	full := "<urn:reload:s> <urn:reload:p> <urn:reload:a> .\n" +
		"<urn:reload:s> <urn:reload:p> <urn:reload:b> .\n" +
		"<urn:reload:s> <urn:reload:p> <urn:reload:c> .\n"
	smaller := "<urn:reload:s> <urn:reload:p> <urn:reload:a> .\n" +
		"<urn:reload:s> <urn:reload:p> <urn:reload:b> .\n"

	if err := c.Load(ctx, strings.NewReader(full)); err != nil {
		t.Fatalf("Load #1: %v", err)
	}
	n1, err := c.CountTriples(ctx)
	if err != nil {
		t.Fatalf("CountTriples #1: %v", err)
	}
	// IDEMPOTENCE: reload identical content → count unchanged.
	if err := c.Load(ctx, strings.NewReader(full)); err != nil {
		t.Fatalf("Load #2: %v", err)
	}
	n2, err := c.CountTriples(ctx)
	if err != nil {
		t.Fatalf("CountTriples #2: %v", err)
	}
	if n1 != n2 {
		t.Fatalf("reload of identical content changed the triple count: %d -> %d (replace must be idempotent — POST/merge would accumulate)", n1, n2)
	}
	// REPLACE: reload with a triple removed → count drops (no lingering triple).
	if err := c.Load(ctx, strings.NewReader(smaller)); err != nil {
		t.Fatalf("Load #3: %v", err)
	}
	n3, err := c.CountTriples(ctx)
	if err != nil {
		t.Fatalf("CountTriples #3: %v", err)
	}
	if n3 >= n2 {
		t.Fatalf("reload with a triple removed did not lower the count: %d -> %d (a lingering triple means merge, not replace)", n2, n3)
	}
}
