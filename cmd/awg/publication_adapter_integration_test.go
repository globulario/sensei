//go:build integration

package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

// The lossless adapter, proven against REAL Oxigraph with SEEDED state.
//
// Every other falsifier runs against in-process fakes, which structurally
// cannot catch representation loss at the adapter boundary -- and that boundary
// is exactly where the term-kind defect lived. An earlier version of this test
// read whatever the developer's live store happened to contain and SKIPPED when
// there was none, so it was not CI evidence at all: it could pass by finding
// nothing. This seeds its own positive and malformed specimens and refuses to
// skip once Oxigraph is available.
func TestIntegration_PublicationAdapterPreservesTerms_RealOxigraph(t *testing.T) {
	oxi, err := findOxigraphBinary()
	if err != nil {
		t.Skipf("Oxigraph binary unavailable: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, oxi, "serve", "--location", t.TempDir(), "--bind", addr)
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Oxigraph: %v", err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()

	queryURL := "http://" + addr + "/query"
	if !waitForSPARQLHealthy(queryURL, 10*time.Second) {
		t.Fatalf("Oxigraph did not become healthy at %s:\n%s", queryURL, logs.String())
	}
	storeURL := "http://" + addr + "/store?default"

	good := publication.Receipt{
		Version:      publication.ReceiptV2,
		Domain:       "github.com/test/adapter",
		Revision:     strings.Repeat("a", 40),
		Tree:         strings.Repeat("b", 40),
		State:        publication.CleanExact,
		SourcePath:   "docs/awareness",
		SourceDigest: strings.Repeat("c", 64),
	}
	// A malformed sibling: same shape, but its revision is stored as an IRI
	// term and one field carries a language tag. Both are invisible through the
	// simplified transport.
	malformed := "<https://globular.io/awareness/publication/receipt/sha256-" + strings.Repeat("d", 64) + "> " +
		"<https://globular.io/awareness#publicationSourceRevision> <https://example.test/not-a-literal> .\n" +
		"<https://globular.io/awareness/publication/receipt/sha256-" + strings.Repeat("d", 64) + "> " +
		"<https://globular.io/awareness#publicationSourcePath> \"docs/awareness\"@en .\n"

	nt := append(good.Triples(), []byte(malformed)...)
	if err := uploadNTriples(http.DefaultClient, storeURL, nt); err != nil {
		t.Fatalf("seed store: %v", err)
	}

	c, err := oxigraph.New(queryURL)
	if err != nil {
		t.Fatalf("oxigraph client: %v", err)
	}
	defer c.Close()

	// 1. The pointer round-trips with its term kinds intact.
	ptr, err := c.DescribeTerms(ctx, publication.PointerIRI(good.Domain))
	if err != nil {
		t.Fatalf("describe pointer: %v", err)
	}
	target, outcome, perr := publication.DecodePointer(good.Domain, asTerms(ptr))
	if outcome != publication.PointerOK {
		t.Fatalf("a seeded pointer did not decode: outcome=%v err=%v", outcome, perr)
	}
	if target != good.IRI() {
		t.Fatalf("pointer target = %s, want %s", target, good.IRI())
	}

	// 2. The good receipt decodes through the real adapter.
	body, err := c.DescribeTerms(ctx, target)
	if err != nil {
		t.Fatalf("describe receipt: %v", err)
	}
	got, err := publication.DecodeStoredReceipt(target, asTerms(body))
	if err != nil {
		t.Fatalf("a correctly published receipt was refused after a real round trip: %v", err)
	}
	if got != good {
		t.Fatalf("the round trip changed the receipt:\n got %+v\nwant %+v", got, good)
	}

	// 3. THE POINT OF THE TEST: the malformed sibling's term kinds survive, so
	// the schema can refuse it. Through the simplified transport the IRI would
	// have arrived as a literal and the language tag would have vanished.
	badIRI := "https://globular.io/awareness/publication/receipt/sha256-" + strings.Repeat("d", 64)
	badBody, err := c.DescribeTerms(ctx, badIRI)
	if err != nil {
		t.Fatalf("describe malformed receipt: %v", err)
	}
	var sawIRITerm, sawLanguage bool
	for _, st := range badBody {
		if st.Object.Kind == store.TermIRI {
			sawIRITerm = true
		}
		if st.Object.Language != "" {
			sawLanguage = true
		}
	}
	if !sawIRITerm {
		t.Fatal("an IRI-valued field came back without its term kind: the adapter is still lossy")
	}
	if !sawLanguage {
		t.Fatal("a language tag was lost crossing the adapter")
	}
	if _, err := publication.DecodeStoredReceipt(badIRI, asTerms(badBody)); err == nil {
		t.Fatal("a malformed receipt decoded after a real round trip")
	}
}

func asTerms(in []store.Statement) []publication.RDFStatement {
	out := make([]publication.RDFStatement, 0, len(in))
	for _, st := range in {
		out = append(out, publication.RDFStatement{
			Predicate: st.Predicate,
			Object: publication.Term{
				Kind:     publication.TermKind(st.Object.Kind),
				Value:    st.Object.Value,
				Datatype: st.Object.Datatype,
				Language: st.Object.Language,
			},
		})
	}
	return out
}

// F3: one query evaluation, one world.
//
// Reading the marker, pointer and receipt separately and comparing digests
// accepts an A -> B -> A transition. This proves the single-evaluation read
// returns all three together, so they cannot come from different worlds.
func TestIntegration_AuthoritySnapshotReadsOneWorld_RealOxigraph(t *testing.T) {
	oxi, err := findOxigraphBinary()
	if err != nil {
		t.Skipf("Oxigraph binary unavailable: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, oxi, "serve", "--location", t.TempDir(), "--bind", addr)
	var logs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &logs, &logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start Oxigraph: %v", err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	queryURL := "http://" + addr + "/query"
	if !waitForSPARQLHealthy(queryURL, 10*time.Second) {
		t.Fatalf("Oxigraph did not become healthy:\n%s", logs.String())
	}

	good := publication.Receipt{
		Version: publication.ReceiptV2, Domain: "github.com/test/snapshot",
		Revision: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40),
		State: publication.CleanExact, SourcePath: "docs/awareness",
		SourceDigest: strings.Repeat("c", 64),
	}
	nt, marker := seedmeta.AppendMarker(good.Triples())
	if err := uploadNTriples(http.DefaultClient, "http://"+addr+"/store?default", nt); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c, err := oxigraph.New(queryURL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer c.Close()

	snap, err := c.DescribeAuthoritySnapshot(ctx, publication.PointerIRI(good.Domain))
	if err != nil {
		t.Fatalf("authority snapshot: %v", err)
	}
	if len(snap.Pointer) == 0 || len(snap.Receipt) == 0 || len(snap.Marker) == 0 {
		t.Fatalf("one evaluation did not return all three reads: pointer=%d receipt=%d marker=%d",
			len(snap.Pointer), len(snap.Receipt), len(snap.Marker))
	}
	// The marker in the snapshot is the one the seeded generation produced.
	var sawDigest bool
	for _, st := range snap.Marker {
		if strings.HasSuffix(st.Predicate, "seedDigestSha256") && st.Object.Value == marker.Digest {
			sawDigest = true
		}
	}
	if !sawDigest {
		t.Fatal("the snapshot's marker is not the generation that was published")
	}
	// The receipt arrived with its terms intact and decodes.
	if _, err := publication.DecodeStoredReceipt(good.IRI(), asTerms(snap.Receipt)); err != nil {
		t.Fatalf("the receipt read in the snapshot did not decode: %v", err)
	}
}
