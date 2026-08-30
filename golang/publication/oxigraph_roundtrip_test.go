package publication_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

// REAL OXIGRAPH, not a fake.
//
// Every falsifier so far runs against in-process fakes, which cannot catch
// representation loss at the ADAPTER boundary -- and that boundary is precisely
// where the term-kind defect lived. This proves the terms a real store returns
// are the terms that were written.
func TestRealOxigraphPreservesTermsAcrossTheAdapter(t *testing.T) {
	endpoint := os.Getenv("SENSEI_TEST_OXIGRAPH_QUERY")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:7881/query"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(endpoint, "/query")+"/query?query=ASK%7B%7D", nil)
	if resp, err := http.DefaultClient.Do(req); err != nil {
		t.Skipf("no live oxigraph at %s: %v", endpoint, err)
	} else {
		resp.Body.Close()
	}

	c, err := oxigraph.New(endpoint)
	if err != nil {
		t.Skipf("oxigraph client: %v", err)
	}
	defer c.Close()

	// Read the live sensei-code publication through the lossless path.
	stmts, err := c.DescribeTerms(ctx, publication.PointerIRI("github.com/globulario/sensei-code"))
	if err != nil {
		t.Skipf("describe pointer: %v", err)
	}
	var target string
	for _, st := range stmts {
		if st.Predicate == publication.CurrentPublicationPredicate {
			if st.Object.Kind != "IRI" {
				t.Fatalf("the live pointer target came back as %s, not IRI: the adapter lost the term kind", st.Object.Kind)
			}
			target = st.Object.Value
		}
	}
	if target == "" {
		t.Skip("no current publication in the live store")
	}

	body, err := c.DescribeTerms(ctx, target)
	if err != nil {
		t.Fatalf("describe receipt: %v", err)
	}
	var terms []publication.RDFStatement
	for _, st := range body {
		terms = append(terms, publication.RDFStatement{
			Predicate: st.Predicate,
			Object: publication.Term{
				Kind:     publication.TermKind(st.Object.Kind),
				Value:    st.Object.Value,
				Datatype: st.Object.Datatype,
				Language: st.Object.Language,
			},
		})
	}
	r, err := publication.DecodeStoredReceipt(target, terms)
	if err != nil {
		t.Fatalf("the live receipt was refused by the schema: %v", err)
	}
	t.Logf("live receipt decoded: version=%s revision=%s path=%s state=%s",
		r.Version, r.Revision, r.SourcePath, r.State)

	// Every publication field must have come back as a LITERAL. If the adapter
	// simplified terms, this is where it shows.
	for _, st := range terms {
		if !strings.HasPrefix(st.Predicate, publication.PublicationFieldPrefix) {
			continue
		}
		if st.Object.Kind != publication.TermLiteral {
			t.Fatalf("%s came back as %s from a real store", st.Predicate, st.Object.Kind)
		}
	}
}
