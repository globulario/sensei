// SPDX-License-Identifier: AGPL-3.0-only

package oxigraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/globulario/sensei/golang/store"
)

// DescribeTerms is Describe WITHOUT the lossy simplification.
//
// Describe returns store.Triple, which remembers only ObjectIsIRI. That
// collapses plain literals, typed literals, language-tagged literals and blank
// nodes into "not an IRI", and it DROPS any binding whose object value is
// empty. A verifier reading through it cannot prove the term it hashes is the
// term the store holds -- which is exactly the class of defect the publication
// receipt exists to rule out.
//
// This is deliberately a separate method rather than a change to Describe:
// seven call sites depend on the simplified shape, and widening all of them to
// serve one authority-bearing path would trade a narrow guarantee for a broad
// migration. Callers that must verify use this; callers that browse use the
// other.
func (c *Client) DescribeTerms(ctx context.Context, iri string) ([]store.Statement, error) {
	if err := store.ValidateQueryIRI(iri); err != nil {
		return nil, fmt.Errorf("oxigraph describe-terms: %w", err)
	}
	q := fmt.Sprintf("SELECT ?p ?o WHERE { <%s> ?p ?o . }", iri)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return nil, fmt.Errorf("oxigraph describe-terms: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oxigraph describe-terms: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("oxigraph describe-terms: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var out struct {
		Results struct {
			Bindings []struct {
				P struct {
					Type  string `json:"type"`
					Value string `json:"value"`
				} `json:"p"`
				O struct {
					Type     string `json:"type"`
					Value    string `json:"value"`
					Datatype string `json:"datatype"`
					Lang     string `json:"xml:lang"`
				} `json:"o"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("oxigraph describe-terms: decode sparql json: %w", err)
	}

	stmts := make([]store.Statement, 0, len(out.Results.Bindings))
	for _, b := range out.Results.Bindings {
		if b.P.Type != "uri" || b.P.Value == "" {
			continue
		}
		// An EMPTY lexical value is kept. Describe drops it, which makes an
		// empty required field indistinguishable from an absent one -- two
		// different worlds, and only one of them is legal.
		stmts = append(stmts, store.Statement{
			Predicate: b.P.Value,
			Object: store.Term{
				Kind:     termKind(b.O.Type),
				Value:    b.O.Value,
				Datatype: b.O.Datatype,
				Language: b.O.Lang,
			},
		})
	}
	return stmts, nil
}

// termKind maps SPARQL JSON term types. An unrecognised type is UNKNOWN rather
// than assumed to be a literal: a reader that cannot name the term it received
// must not vouch for it.
func termKind(t string) store.TermKind {
	switch t {
	case "uri":
		return store.TermIRI
	case "literal", "typed-literal":
		return store.TermLiteral
	case "bnode":
		return store.TermBlank
	}
	return store.TermUnknown
}
