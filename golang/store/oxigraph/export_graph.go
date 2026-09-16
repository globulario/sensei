// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/repodomain"
)

// ExportDomainGraph reads back ONE publication domain's named graph as
// N-Triples.
//
// It is the exact inverse of LoadGraph and addresses the same URL, which is the
// point: a read that reconstructed the graph from a query pattern
// (CONSTRUCT { ?s ?p ?o }) would read the store's UNION of every domain and
// call it one repository's graph. The graph slot is the provenance slot, so the
// read is scoped by it rather than by filtering afterwards.
//
// The domain is validated with the same canonical contract LoadGraph applies.
// A non-canonical spelling addresses a DIFFERENT graph URL, so a caller passing
// "GitHub.com/org/repo" would get an empty export of a graph nobody publishes
// to and no error to tell it apart from a domain that exists and is empty.
//
// Read-only: a GET against the Graph Store Protocol changes no triple, no
// marker and no generation.
func (c *Client) ExportDomainGraph(ctx context.Context, domain string) ([]byte, error) {
	if strings.TrimSpace(domain) == "" {
		return nil, fmt.Errorf("oxigraph export: refusing to export an unnamed domain; the default graph is not one repository's graph")
	}
	if err := repodomain.Validate(domain); err != nil {
		return nil, fmt.Errorf("oxigraph export: %q is not a canonical publication domain: %w", domain, err)
	}
	exportClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.graphURL(domain), nil)
	if err != nil {
		return nil, fmt.Errorf("oxigraph export: build request: %w", err)
	}
	req.Header.Set("Accept", "application/n-triples")

	resp, err := exportClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph export: %w", err)
	}
	defer resp.Body.Close()
	// A domain with no named graph is 404 here. That is NOT an empty graph: it
	// is "this store publishes nothing for that domain", and returning empty
	// bytes would let a caller snapshot nothing and bind a task to it.
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("oxigraph export: this store serves no graph for domain %s", domain)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph export: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("oxigraph export: read body: %w", err)
	}
	return out, nil
}
