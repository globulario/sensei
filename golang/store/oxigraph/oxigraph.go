// SPDX-License-Identifier: Apache-2.0

// Package oxigraph is the Store implementation backed by Oxigraph's
// SPARQL Protocol HTTP endpoint.
//
// Endpoint shape:
//
//	The configured queryURL is the SPARQL query endpoint, typically
//	http://host:7878/query for an Oxigraph server started via the
//	bundled scripts/bootstrap_oxigraph.sh helper. Future steps that need
//	the update endpoint (SPARQL Update) or the store endpoint (bulk
//	triple load) will derive them from queryURL or add separate flags;
//	the abstraction kept here stays minimal until a real caller forces
//	the choice.
//
// What this implementation does NOT do (deliberately):
//
//   - Connection pooling / keep-alive tuning. Go's http.Transport
//     defaults are appropriate for a low-RPC-rate server.
//
//   - SPARQL parsing or result decoding. Health only inspects the HTTP
//     response status; downstream callers (Resolve etc.) will decode
//     application/sparql-results+json themselves when they land.
//
//   - Retry logic. Failure is surfaced once with context so the caller
//     can decide what to do (e.g. honor -require-store, fall back, etc.).
package oxigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/rdf"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

// DefaultHTTPTimeout caps an individual SPARQL request at 10 s. This is
// a per-request ceiling; the caller's Context can shorten it but cannot
// extend it (Go's http.Client uses min(client.Timeout, ctx deadline)).
const DefaultHTTPTimeout = 10 * time.Second

// Client implements store.Store against Oxigraph's HTTP SPARQL endpoint.
//
// Zero value is NOT usable — go through New so URL validation runs at
// construction time, not at the first request.
type Client struct {
	queryURL string
	http     *http.Client
}

// New constructs a Client targeting the given Oxigraph SPARQL query
// endpoint. queryURL is typically http://localhost:7878/query.
//
// The URL is validated at construction time. An invalid URL is a
// configuration bug; surfacing it via a returned error here, before
// the server starts serving, means the failure mode is "fast exit on
// startup" instead of "every RPC errors at runtime."
func New(queryURL string) (*Client, error) {
	u, err := url.Parse(queryURL)
	if err != nil {
		return nil, fmt.Errorf("oxigraph: invalid URL %q: %w", queryURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("oxigraph: URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("oxigraph: URL missing host: %q", queryURL)
	}
	return &Client{
		queryURL: queryURL,
		http:     &http.Client{Timeout: DefaultHTTPTimeout},
	}, nil
}

// Close releases resources. The Go http.Client has no shutdown
// semantics to invoke (idle connections expire on their own); this
// method exists to satisfy the store.Store contract and to leave room
// for a future implementation that does hold real resources.
func (c *Client) Close() error { return nil }

// Health sends a trivial SPARQL ASK query to the endpoint and reports
// the result. The query asks nothing of the data ("ASK {}" matches the
// empty graph pattern, which is always true) — what we validate is
// that the SPARQL endpoint is wired, not just that some HTTP server is
// up at the configured URL.
//
// Why ASK and not GET /:
//   - A bare GET against an Oxigraph endpoint returns 200 even when the
//     SPARQL endpoint is misconfigured; only sending an actual SPARQL
//     request proves the backend is fully wired.
//   - ASK is the cheapest SPARQL form (no result rows to materialize).
//   - Standard SPARQL — works against any compliant store if we ever
//     swap backends.
//
// The function returns a wrapped error including the HTTP status and
// up to 1 KiB of response body when the endpoint responds with a non-2xx
// status, so the operator sees the backend's own message.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL,
		strings.NewReader("ASK {}"))
	if err != nil {
		return fmt.Errorf("oxigraph health: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("oxigraph health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("oxigraph health: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Describe returns direct outgoing triples for one subject IRI.
func (c *Client) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	q := fmt.Sprintf("SELECT ?p ?o WHERE { <%s> ?p ?o . }", iri)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph describe: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph describe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph describe: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out sparqlSelectResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph describe: decode sparql json: %w", err)
	}
	triples := make([]store.Triple, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.P.Type != "uri" || b.P.Value == "" || b.O.Value == "" {
			continue
		}
		triples = append(triples, store.Triple{
			Predicate:   b.P.Value,
			Object:      b.O.Value,
			ObjectIsIRI: b.O.Type == "uri",
		})
	}
	return triples, nil
}

// DescribeInbound returns the inverse of Describe: every (subject, predicate)
// whose object is the given IRI. Literal subjects cannot exist, but we filter
// to IRI subjects defensively so a malformed store cannot inject one.
func (c *Client) DescribeInbound(ctx context.Context, iri string) ([]store.InboundTriple, error) {
	q := fmt.Sprintf("SELECT ?s ?p WHERE { ?s ?p <%s> . FILTER(isIRI(?s)) }", iri)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph describe-inbound: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph describe-inbound: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph describe-inbound: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out sparqlInboundResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph describe-inbound: decode sparql json: %w", err)
	}
	triples := make([]store.InboundTriple, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.S.Type != "uri" || b.S.Value == "" || b.P.Type != "uri" || b.P.Value == "" {
			continue
		}
		triples = append(triples, store.InboundTriple{Subject: b.S.Value, Predicate: b.P.Value})
	}
	return triples, nil
}

// ImpactForFile returns direct node facts linked from one source-file IRI.
//
// Three paths are followed:
//  1. Direct: file → aw:implements → node  (invariants, intents, incident patterns
//     with explicit expressed_by / protects.files anchors).
//  2. Via invariant: file → aw:implements → invariant ← aw:affects ← failure_mode
//     This surfaces failure modes whose related_invariants list an invariant
//     anchored to the file, without requiring every failure mode to carry an
//     explicit protects.files entry.
//  3. File annotations: file → aw:enforces|aw:protects → invariant
//     This surfaces invariants linked via file_annotations YAML (the inverse
//     of the invariant-authored protects.files path).
func (c *Client) ImpactForFile(ctx context.Context, sourceFileIRI string) ([]store.ImpactFact, error) {
	q := fmt.Sprintf(
		`SELECT ?node ?type ?p ?o WHERE {
  {
    <%s> <https://globular.io/awareness#implements> ?node .
  } UNION {
    <%s> <https://globular.io/awareness#implements> ?inv .
    ?inv <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#Invariant> .
    ?node <https://globular.io/awareness#affects> ?inv .
    ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#FailureMode> .
  } UNION {
    <%s> <https://globular.io/awareness#enforces> ?node .
  } UNION {
    <%s> <https://globular.io/awareness#protects> ?node .
  }
  ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> ?type .
  OPTIONAL { ?node ?p ?o . }
}`,
		sourceFileIRI,
		sourceFileIRI,
		sourceFileIRI,
		sourceFileIRI,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph impact: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph impact: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph impact: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out sparqlImpactResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph impact: decode sparql json: %w", err)
	}
	facts := make([]store.ImpactFact, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.Node.Type != "uri" || b.Node.Value == "" || b.Type.Type != "uri" || b.Type.Value == "" {
			continue
		}
		facts = append(facts, store.ImpactFact{
			NodeIRI:     b.Node.Value,
			TypeIRI:     b.Type.Value,
			Predicate:   b.P.Value,
			Object:      b.O.Value,
			ObjectIsIRI: b.O.Type == "uri",
		})
	}
	return facts, nil
}

// ClassFacts returns direct facts for nodes of one awareness class.
func (c *Client) ClassFacts(ctx context.Context, classIRI string, limit int) ([]store.ImpactFact, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 300 {
		limit = 300
	}
	q := fmt.Sprintf(
		`SELECT ?node ?p ?o WHERE {
  {
    SELECT DISTINCT ?node WHERE {
      ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%s> .
    }
    ORDER BY ?node
    LIMIT %d
  }
  OPTIONAL { ?node ?p ?o . }
}`,
		classIRI, limit,
	)
	return c.classFactsFromQuery(ctx, q, classIRI)
}

// classFactsFromQuery runs a SELECT ?node ?p ?o query and decodes it into
// ImpactFacts (shared by ClassFacts and ClassFactsScoped).
func (c *Client) classFactsFromQuery(ctx context.Context, q, classIRI string) ([]store.ImpactFact, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph class facts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph class facts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph class facts: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out sparqlClassFactsResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph class facts: decode sparql json: %w", err)
	}
	facts := make([]store.ImpactFact, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.Node.Type != "uri" || b.Node.Value == "" {
			continue
		}
		facts = append(facts, store.ImpactFact{
			NodeIRI:     b.Node.Value,
			TypeIRI:     classIRI,
			Predicate:   b.P.Value,
			Object:      b.O.Value,
			ObjectIsIRI: b.O.Type == "uri",
		})
	}
	return facts, nil
}

// CodeSymbolFacts returns all triples for CodeSymbol nodes that are
// anchored to the given source-file IRI via aw:definedInFile.
// NodeIRI in each fact is the symbol IRI; TypeIRI is the CodeSymbol class IRI.
func (c *Client) CodeSymbolFacts(ctx context.Context, sourceFileIRI string) ([]store.ImpactFact, error) {
	q := fmt.Sprintf(
		`SELECT ?node ?p ?o WHERE {
  ?node <https://globular.io/awareness#definedInFile> <%s> .
  ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <https://globular.io/awareness#CodeSymbol> .
  ?node ?p ?o .
}
ORDER BY ?node ?p ?o`,
		sourceFileIRI,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph code symbols: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph code symbols: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph code symbols: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out sparqlClassFactsResult // ?sym ?p ?o — same column shape as ClassFacts
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph code symbols: decode sparql json: %w", err)
	}
	const codeSymbolTypeIRI = "https://globular.io/awareness#CodeSymbol"
	facts := make([]store.ImpactFact, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.Node.Type != "uri" || b.Node.Value == "" {
			continue
		}
		facts = append(facts, store.ImpactFact{
			NodeIRI:     b.Node.Value,
			TypeIRI:     codeSymbolTypeIRI,
			Predicate:   b.P.Value,
			Object:      b.O.Value,
			ObjectIsIRI: b.O.Type == "uri",
		})
	}
	return facts, nil
}

// DetectFacts returns all direct facts for every node carrying a detect block
// (aw:detectForbiddenPattern OR aw:detectRequiredPattern). Each row carries the
// node IRI, its rdf:type, and one predicate/object — the same flat shape as
// ImpactForFile — so the server can reify the detect patterns plus the node's
// domain/provenance facts in a single pass.
func (c *Client) DetectFacts(ctx context.Context) ([]store.ImpactFact, error) {
	// FILTER EXISTS (not UNION) selects the detect-carrying nodes: a UNION over
	// the two detect predicates would bind ?node twice for a node with both,
	// cross-producting every OPTIONAL ?p ?o row (and so duplicating list-valued
	// facts like citations). EXISTS keeps one binding per node.
	const q = `SELECT ?node ?type ?p ?o WHERE {
  ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> ?type .
  FILTER EXISTS {
    { ?node <https://globular.io/awareness#detectForbiddenPattern> ?dfp }
    UNION
    { ?node <https://globular.io/awareness#detectRequiredPattern> ?drp }
  }
  OPTIONAL { ?node ?p ?o }
}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph detect facts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph detect facts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph detect facts: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out sparqlImpactResult // ?node ?type ?p ?o — same shape as ImpactForFile
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph detect facts: decode sparql json: %w", err)
	}
	facts := make([]store.ImpactFact, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.Node.Type != "uri" || b.Node.Value == "" || b.Type.Type != "uri" || b.Type.Value == "" {
			continue
		}
		facts = append(facts, store.ImpactFact{
			NodeIRI:     b.Node.Value,
			TypeIRI:     b.Type.Value,
			Predicate:   b.P.Value,
			Object:      b.O.Value,
			ObjectIsIRI: b.O.Type == "uri",
		})
	}
	return facts, nil
}

// QueryURL returns the configured endpoint URL. Useful for logging and
// for diagnostics that want to surface the target an operator
// configured.
func (c *Client) QueryURL() string { return c.queryURL }

// storeURL derives the Graph Store Protocol endpoint from queryURL by
// replacing the trailing "/query" path segment with "/store?default".
// Oxigraph co-locates both endpoints on the same port.
func (c *Client) storeURL() string {
	base := strings.TrimSuffix(c.queryURL, "/query")
	return base + "/store?default"
}

// CountTriples returns the number of triples in the default graph.
// Uses a cheap SELECT COUNT(*) query; returns 0 on any error so callers
// can safely treat a query failure as "store appears empty" without hard-failing.
func (c *Client) CountTriples(ctx context.Context) (int64, error) {
	const q = `SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return 0, fmt.Errorf("oxigraph count: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("oxigraph count: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("oxigraph count: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Results struct {
			Bindings []struct {
				N struct {
					Value string `json:"value"`
				} `json:"n"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("oxigraph count: decode: %w", err)
	}
	if len(result.Results.Bindings) == 0 {
		return 0, nil
	}
	var n int64
	fmt.Sscanf(result.Results.Bindings[0].N.Value, "%d", &n)
	return n, nil
}

// CountByClass returns the number of distinct subjects with rdf:type = classIRI.
// Used by Metadata to report coverage stats per knowledge class.
// Returns 0 on any error so the metadata response degrades gracefully rather
// than failing hard when one class query has a transient issue.
func (c *Client) CountByClass(ctx context.Context, classIRI string) (int64, error) {
	q := fmt.Sprintf(
		`SELECT (COUNT(DISTINCT ?s) AS ?n) WHERE { ?s a <%s> }`,
		classIRI,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return 0, fmt.Errorf("oxigraph count-by-class: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("oxigraph count-by-class: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("oxigraph count-by-class: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Results struct {
			Bindings []struct {
				N struct {
					Value string `json:"value"`
				} `json:"n"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("oxigraph count-by-class: decode: %w", err)
	}
	if len(result.Results.Bindings) == 0 {
		return 0, nil
	}
	var n int64
	fmt.Sscanf(result.Results.Bindings[0].N.Value, "%d", &n)
	return n, nil
}

// SeedMarkers returns complete SeedBuild marker nodes currently present in the
// live graph. It is used by startup repair code to refresh a stale runtime
// marker file only when the store itself already carries one unambiguous marker.
func (c *Client) SeedMarkers(ctx context.Context) ([]seedmeta.Marker, error) {
	q := fmt.Sprintf(`SELECT ?m ?digest ?count WHERE {
  ?m <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%sSeedBuild> .
  ?m <%sseedDigestSha256> ?digest .
  ?m <%sseedTripleCount> ?count .
}`, seedmeta.NamespaceIRI, seedmeta.NamespaceIRI, seedmeta.NamespaceIRI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph seed markers: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph seed markers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("oxigraph seed markers: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Results struct {
			Bindings []struct {
				M struct {
					Value string `json:"value"`
				} `json:"m"`
				Digest struct {
					Value string `json:"value"`
				} `json:"digest"`
				Count struct {
					Value string `json:"value"`
				} `json:"count"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oxigraph seed markers: decode: %w", err)
	}
	out := make([]seedmeta.Marker, 0, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		if b.M.Value == "" || b.Digest.Value == "" || b.Count.Value == "" {
			continue
		}
		var n int64
		if _, err := fmt.Sscanf(b.Count.Value, "%d", &n); err != nil || n <= 0 {
			continue
		}
		out = append(out, seedmeta.Marker{
			IRI:         b.M.Value,
			Digest:      b.Digest.Value,
			TripleCount: n,
		})
	}
	return out, nil
}

// Domains returns the distinct aw:repo domain keys present in the graph — the
// selectable domains (e.g. "github.com/owner/repo"), excluding shared
// meta-principles (which carry aw:domain "shared", not aw:repo). Used by
// Metadata to offer a domain filter.
func (c *Client) Domains(ctx context.Context) ([]string, error) {
	q := `SELECT DISTINCT ?repo WHERE { ?s <https://globular.io/awareness#repo> ?repo }`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph domains: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph domains: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("oxigraph domains: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Results struct {
			Bindings []struct {
				Repo struct {
					Value string `json:"value"`
				} `json:"repo"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oxigraph domains: decode: %w", err)
	}
	out := make([]string, 0, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		if b.Repo.Value != "" {
			out = append(out, b.Repo.Value)
		}
	}
	return out, nil
}

// CountTriplesInDomain counts triples whose SUBJECT is visible to a domain
// scope — the per-repo analogue of CountTriples. In scope: aw:repo == domain,
// or a shared node, or (when domain is the home domain) any untagged subject.
func (c *Client) CountTriplesInDomain(ctx context.Context, domain, home string) (int64, error) {
	repo := rdf.Lit(domain)
	branches := []string{
		fmt.Sprintf(`{ ?s <https://globular.io/awareness#repo> %s }`, repo),
		`{ ?s <https://globular.io/awareness#domain> "shared" }`,
	}
	if domain == home {
		branches = append(branches, `{
  ?s ?scopeP ?scopeO .
  FILTER NOT EXISTS { ?s <https://globular.io/awareness#repo> ?r }
  FILTER NOT EXISTS { ?s <https://globular.io/awareness#domain> "shared" }
}`)
	}
	q := fmt.Sprintf(`SELECT (COUNT(*) AS ?n) WHERE {
  {
    SELECT DISTINCT ?s WHERE {
      %s
    }
  }
  ?s ?p ?o .
}`, strings.Join(branches, "\n      UNION\n      "))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return 0, fmt.Errorf("oxigraph count-triples-in-domain: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("oxigraph count-triples-in-domain: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("oxigraph count-triples-in-domain: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Results struct {
			Bindings []struct {
				N struct{ Value string } `json:"n"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("oxigraph count-triples-in-domain: decode: %w", err)
	}
	if len(result.Results.Bindings) == 0 {
		return 0, nil
	}
	var n int64
	fmt.Sscanf(result.Results.Bindings[0].N.Value, "%d", &n)
	return n, nil
}

// ClassNodeDomains returns every node of classIRI with its raw domain
// attribution, UNCAPPED — for accurate domain-scoped counting and filtering
// (unlike ClassFacts, which caps at 300). The value is the SET of a node's
// domains: each aw:repo literal, plus "shared" when aw:domain is shared. A node
// may carry more than one aw:repo (authored in multiple repos); an empty slice
// means untagged (the caller applies the home-domain default). Nodes with no
// repo/domain still appear (empty slice) via the OPTIONAL join.
func (c *Client) ClassNodeDomains(ctx context.Context, classIRI string) (map[string][]string, error) {
	q := fmt.Sprintf(`SELECT ?node ?repo ?dom WHERE {
    ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%s> .
    OPTIONAL { ?node <https://globular.io/awareness#repo> ?repo }
    OPTIONAL { ?node <https://globular.io/awareness#domain> ?dom }
  }`, classIRI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph class-node-domains: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph class-node-domains: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("oxigraph class-node-domains: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var result struct {
		Results struct {
			Bindings []struct {
				Node struct{ Value string } `json:"node"`
				Repo struct{ Value string } `json:"repo"`
				Dom  struct{ Value string } `json:"dom"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oxigraph class-node-domains: decode: %w", err)
	}
	out := make(map[string][]string, len(result.Results.Bindings))
	seen := map[string]map[string]bool{} // node → set of domains already added
	for _, b := range result.Results.Bindings {
		if b.Node.Value == "" {
			continue
		}
		if _, ok := out[b.Node.Value]; !ok {
			out[b.Node.Value] = nil // ensure untagged nodes appear (empty slice)
			seen[b.Node.Value] = map[string]bool{}
		}
		add := func(d string) {
			if d != "" && !seen[b.Node.Value][d] {
				seen[b.Node.Value][d] = true
				out[b.Node.Value] = append(out[b.Node.Value], d)
			}
		}
		add(b.Repo.Value) // each aw:repo the node carries
		if b.Dom.Value == "shared" {
			add("shared")
		}
	}
	return out, nil
}

// ClassFactsScoped is ClassFacts restricted to nodes visible to a domain scope:
// its selection subquery applies the domain filter (shared always; a repo node
// only when its aw:repo matches; an untagged node only when the scope is the
// home domain) so the LIMIT lands on in-scope nodes, not an arbitrary page.
// Mirrors the InScope core (golang/server/scope.go).
func (c *Client) ClassFactsScoped(ctx context.Context, classIRI, domain, home string, limit int) ([]store.ImpactFact, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 300 {
		limit = 300
	}
	q := fmt.Sprintf(
		`SELECT ?node ?p ?o WHERE {
  {
    SELECT DISTINCT ?node WHERE {
      ?node <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%s> .
      OPTIONAL { ?node <https://globular.io/awareness#repo> ?repo }
      OPTIONAL { ?node <https://globular.io/awareness#domain> ?dom }
      FILTER(
        (BOUND(?dom) && ?dom = "shared") ||
        (BOUND(?repo) && ?repo = "%s") ||
        (!BOUND(?repo) && (!BOUND(?dom) || ?dom != "shared") && "%s" = "%s")
      )
    }
    ORDER BY ?node
    LIMIT %d
  }
  OPTIONAL { ?node ?p ?o . }
}`,
		classIRI, domain, home, domain, limit,
	)
	return c.classFactsFromQuery(ctx, q, classIRI)
}

// Subjects returns every distinct subject IRI in the default graph. Blank-node
// and non-IRI subjects are skipped (they cannot be reconciled by IRI). Used by
// `awg reconcile` to diff the live store's node set against the committed seed
// and surface runtime-only orphans (e.g. an additive `awg promote`/`propose`
// load that never made it back into the authored corpus).
func (c *Client) Subjects(ctx context.Context) ([]string, error) {
	const q = `SELECT DISTINCT ?s WHERE { ?s ?p ?o }`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph subjects: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph subjects: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("oxigraph subjects: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Results struct {
			Bindings []struct {
				S struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"s"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oxigraph subjects: decode: %w", err)
	}
	out := make([]string, 0, len(result.Results.Bindings))
	for _, b := range result.Results.Bindings {
		if b.S.Type != "uri" || b.S.Value == "" {
			continue // skip blank nodes / non-IRI subjects
		}
		out = append(out, b.S.Value)
	}
	return out, nil
}

// Load replaces the default graph with the N-Triples content from r.
// Uses the Oxigraph Graph Store Protocol (PUT /store?default) so the
// operation is atomic from the store's perspective — readers see either
// the old graph or the new one, never a partial state.
//
// The upload has a 60 s deadline so a slow network does not hang startup
// indefinitely; the caller should also pass a context with a deadline.
func (c *Client) Load(ctx context.Context, r io.Reader) error {
	loadClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.storeURL(), r)
	if err != nil {
		return fmt.Errorf("oxigraph load: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/n-triples")

	resp, err := loadClient.Do(req)
	if err != nil {
		return fmt.Errorf("oxigraph load: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("oxigraph load: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// updateURL derives the SPARQL Update endpoint from queryURL by replacing the
// trailing "/query" path segment with "/update". Oxigraph co-locates query,
// update, and store endpoints on the same port.
func (c *Client) updateURL() string {
	base := strings.TrimSuffix(c.queryURL, "/query")
	return base + "/update"
}

// Update executes a SPARQL Update (INSERT/DELETE) against the store. Unlike Load
// (a whole-graph PUT replace), Update mutates in place — the primitive behind a
// domain-scoped, non-destructive rebuild: it lets `sensei build --repo` remove
// only one repo's slice (subjects tagged aw:repo == domain) without disturbing
// other domains, shared nodes, or the home slice.
func (c *Client) Update(ctx context.Context, update string) error {
	updateClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.updateURL(), strings.NewReader(update))
	if err != nil {
		return fmt.Errorf("oxigraph update: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-update")
	resp, err := updateClient.Do(req)
	if err != nil {
		return fmt.Errorf("oxigraph update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("oxigraph update: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Append merges N-Triples into the default graph via a Graph Store Protocol
// POST (additive), in contrast to Load's PUT (replace). Used by the scoped
// rebuild to add a freshly compiled domain slice — and its recomputed marker —
// on top of the domains already present. Identical triples are idempotent (the
// store deduplicates on merge).
func (c *Client) Append(ctx context.Context, r io.Reader) error {
	appendClient := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.storeURL(), r)
	if err != nil {
		return fmt.Errorf("oxigraph append: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/n-triples")
	resp, err := appendClient.Do(req)
	if err != nil {
		return fmt.Errorf("oxigraph append: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("oxigraph append: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// DumpNTriples returns the entire default graph serialized as N-Triples via a
// Graph Store Protocol GET. The scoped rebuild reads the whole graph back after
// its DELETE/append so it can recompute the single whole-graph marker (digest +
// total triple count) that the server verifies — the marker is global, so any
// scoped change must restamp it over the post-update contents.
func (c *Client) DumpNTriples(ctx context.Context) ([]byte, error) {
	dumpClient := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.storeURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("oxigraph dump: build request: %w", err)
	}
	req.Header.Set("Accept", "application/n-triples")
	resp, err := dumpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph dump: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph dump: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

type sparqlSelectResult struct {
	Results struct {
		Bindings []struct {
			P struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"p"`
			O struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"o"`
		} `json:"bindings"`
	} `json:"results"`
}

// sparqlInboundResult decodes the ?s ?p bindings of DescribeInbound.
type sparqlInboundResult struct {
	Results struct {
		Bindings []struct {
			S struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"s"`
			P struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"p"`
		} `json:"bindings"`
	} `json:"results"`
}

type sparqlImpactResult struct {
	Results struct {
		Bindings []struct {
			Node struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"node"`
			Type struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"type"`
			P struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"p"`
			O struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"o"`
		} `json:"bindings"`
	} `json:"results"`
}

type sparqlClassFactsResult struct {
	Results struct {
		Bindings []struct {
			Node struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"node"`
			P struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"p"`
			O struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"o"`
		} `json:"bindings"`
	} `json:"results"`
}

// RenderingGroupsForFile queries rendering groups the file belongs to.
func (c *Client) RenderingGroupsForFile(ctx context.Context, sourceFileIRI string) ([]store.RenderingGroupInfo, error) {
	q := fmt.Sprintf(
		`SELECT ?group ?label ?contract WHERE {
  <%s> <https://globular.io/awareness#memberOfGroup> ?group .
  OPTIONAL { ?group <http://www.w3.org/2000/01/rdf-schema#label> ?label . }
  OPTIONAL { ?group <http://www.w3.org/2000/01/rdf-schema#comment> ?contract . }
}`, sourceFileIRI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("oxigraph rendering groups: %s: %s", resp.Status, string(body))
	}
	var out struct {
		Results struct {
			Bindings []struct {
				Group struct {
					Value string `json:"value"`
				} `json:"group"`
				Label struct {
					Value string `json:"value"`
				} `json:"label"`
				Contract struct {
					Value string `json:"value"`
				} `json:"contract"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var groups []store.RenderingGroupInfo
	seen := map[string]bool{}
	for _, b := range out.Results.Bindings {
		if b.Group.Value == "" || seen[b.Group.Value] {
			continue
		}
		seen[b.Group.Value] = true
		id := b.Group.Value
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
		groups = append(groups, store.RenderingGroupInfo{
			IRI:      b.Group.Value,
			ID:       id,
			Label:    b.Label.Value,
			Contract: b.Contract.Value,
		})
	}
	return groups, nil
}

// Compile-time assertion that *Client satisfies store.Store. If the
// interface acquires a method *Client doesn't implement, the build
// fails here with a clear "missing method" error.
var _ store.Store = (*Client)(nil)
