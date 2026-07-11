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
