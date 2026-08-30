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

// DescribeAuthoritySnapshot reads the graph marker, the domain pointer and the
// receipt it names in ONE query evaluation.
//
// WHY NOT ANOTHER DOUBLE READ. The previous approach read the generation, then
// the publication, then the generation again, and required the two digests to
// match. A digest can RETURN to an earlier value: a store moving A -> B -> A
// while the publication is read yields matching endpoints and a receipt from a
// world the freshness evidence never describes. Endpoint equality is not
// continuity, and no number of additional reads fixes that -- each pair has the
// same hole.
//
// One SPARQL evaluation is answered from a single consistent view of the store,
// so the marker and the receipt cannot come from different worlds. That is a
// snapshot rather than a comparison.
func (c *Client) DescribeAuthoritySnapshot(ctx context.Context, pointerIRI string) (store.AuthoritySnapshot, error) {
	if err := store.ValidateQueryIRI(pointerIRI); err != nil {
		return store.AuthoritySnapshot{}, fmt.Errorf("oxigraph authority snapshot: %w", err)
	}
	// ?src distinguishes which subject each row describes, so one result set
	// carries three different reads without ambiguity.
	q := fmt.Sprintf(`SELECT ?src ?p ?o WHERE {
  { <%s> ?p ?o . BIND("pointer" AS ?src) }
  UNION
  { <%s> <%scurrentPublication> ?r . ?r ?p ?o . BIND("receipt" AS ?src) }
  UNION
  { ?m <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%sSeedBuild> . ?m ?p ?o . BIND("marker" AS ?src) }
}`, pointerIRI, pointerIRI, seedNamespace, seedNamespace)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.queryURL, strings.NewReader(q))
	if err != nil {
		return store.AuthoritySnapshot{}, fmt.Errorf("oxigraph authority snapshot: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return store.AuthoritySnapshot{}, fmt.Errorf("oxigraph authority snapshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return store.AuthoritySnapshot{}, fmt.Errorf("oxigraph authority snapshot: %s: %s",
			resp.Status, strings.TrimSpace(string(body)))
	}

	var out struct {
		Results struct {
			Bindings []struct {
				Src struct{ Value string } `json:"src"`
				P   struct {
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
		return store.AuthoritySnapshot{}, fmt.Errorf("oxigraph authority snapshot: decode: %w", err)
	}

	var snap store.AuthoritySnapshot
	for _, b := range out.Results.Bindings {
		if b.P.Type != "uri" || b.P.Value == "" {
			continue
		}
		st := store.Statement{
			Predicate: b.P.Value,
			Object: store.Term{
				Kind:     termKind(b.O.Type),
				Value:    b.O.Value,
				Datatype: b.O.Datatype,
				Language: b.O.Lang,
			},
		}
		switch b.Src.Value {
		case "pointer":
			snap.Pointer = append(snap.Pointer, st)
		case "receipt":
			snap.Receipt = append(snap.Receipt, st)
		case "marker":
			snap.Marker = append(snap.Marker, st)
		}
	}
	return snap, nil
}

const seedNamespace = "https://globular.io/awareness#"
