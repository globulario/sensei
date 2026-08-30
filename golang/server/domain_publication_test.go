// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"github.com/globulario/sensei/golang/publication"
	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store"
)

const pubTestDomain = "github.com/globulario/sensei-code"

func healthyReceipt() publication.Receipt {
	return publication.Receipt{
		Version:      publication.ReceiptV2,
		Domain:       pubTestDomain,
		Revision:     "f6b4755ff4d12591e9e802b2094b16a938260cc2",
		Tree:         "ad916f771bbc07523c92ff299c27af53c852aacd",
		State:        publication.CleanExact,
		SourcePath:   "docs/awareness",
		SourceDigest: "cff0d6113939b6f986b873dffad22847491669d903d1254386ef57c18cdf9c23",
	}
}

// receiptTriples renders a receipt's own subject triples as the store would
// return them from Describe.
func receiptTriples(r publication.Receipt) []store.Triple {
	var out []store.Triple
	for _, line := range strings.Split(string(r.Triples()), "\n") {
		if !strings.HasPrefix(line, "<"+r.IRI()+">") {
			continue
		}
		rest := strings.TrimPrefix(line, "<"+r.IRI()+"> <")
		i := strings.Index(rest, ">")
		if i < 0 {
			continue
		}
		pred := rest[:i]
		obj := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest[i+1:]), "."))
		if strings.HasPrefix(obj, `"`) {
			obj = strings.Trim(obj, `"`)
			out = append(out, store.Triple{Predicate: pred, Object: obj})
			continue
		}
		out = append(out, store.Triple{Predicate: pred, Object: strings.Trim(obj, "<>"), ObjectIsIRI: true})
	}
	return out
}

// publicationStore serves a pointer and a receipt body by bounded Describe.
//
// storedTarget is what the POINTER names; body is what actually lives there.
// Keeping them separable is the whole point: a tampered receipt is one whose
// body no longer hashes to the target the pointer preserved.
type publicationStore struct {
	fakeStore
	storedTarget string
	body         []store.Triple
	describes    *int64
	failDump     bool
}

func (p publicationStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	if p.describes != nil {
		atomic.AddInt64(p.describes, 1)
	}
	switch iri {
	case publication.PointerIRI(pubTestDomain):
		if p.storedTarget == "" {
			return nil, nil
		}
		return []store.Triple{{
			Predicate:   publication.CurrentPublicationPredicate,
			Object:      p.storedTarget,
			ObjectIsIRI: true,
		}}, nil
	case p.storedTarget:
		return p.body, nil
	}
	return p.fakeStore.Describe(ctx, iri)
}

// DumpNTriples exists ONLY to fail. Falsifier 4: a bounded resolver must never
// reach for the whole graph.
func (p publicationStore) DumpNTriples(context.Context) ([]byte, error) {
	if p.failDump {
		panic("DumpNTriples was called: the publication lookup is not bounded")
	}
	return nil, nil
}

func serverWith(st store.Store) *server {
	s := newServer(st)
	s.homeDomain = "github.com/globulario/sensei"
	return s
}

// FALSIFIER 1. The pointer keeps naming receipt A while A's identity-bearing
// fields are mutated. The recomputed identity must disagree with the stored
// target, and the answer must be UNREADABLE rather than a confident VERIFIED.
func TestATamperedReceiptIsUnreadableThroughTheEndpoint(t *testing.T) {
	healthy := healthyReceipt()
	stored := healthy.IRI()

	tampered := healthy
	tampered.Revision = "0000000000000000000000000000000000000000"
	body := receiptTriples(tampered) // published under the OLD target

	st := publicationStore{storedTarget: stored, body: body, failDump: true}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: a tampered receipt was attested", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "recomputes to") {
		t.Fatalf("the refusal does not name the identity disagreement: %q", got.GetDetail())
	}

	// Control: the same store, untampered, must still verify. Without this the
	// test above would pass for a resolver that refuses everything.
	ok := publicationStore{storedTarget: stored, body: receiptTriples(healthy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(ok), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("the untampered control did not verify: %v %s", got.GetResolution(), got.GetDetail())
	}
}

// FALSIFIER 2. A pointer to a receipt that is not there is UNREADABLE; no
// pointer at all is ABSENT. Collapsing them lets a corrupt record read as
// never-published.
func TestDanglingAndAbsentStayMechanicallyDistinct(t *testing.T) {
	dangling := publicationStore{
		storedTarget: "https://globular.io/awareness/publication/receipt/sha256-deadbeef",
		body:         nil,
		failDump:     true,
	}
	got := resolveCurrentPublication(context.Background(), serverWith(dangling), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("a dangling pointer resolved %v, want UNREADABLE", got.GetResolution())
	}

	absent := publicationStore{storedTarget: "", failDump: true}
	got = resolveCurrentPublication(context.Background(), serverWith(absent), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT {
		t.Fatalf("a missing pointer resolved %v, want ABSENT", got.GetResolution())
	}
}

// FALSIFIER 3. The generation changes between the freshness read and the
// publication read. The authority must refuse to attest a coherent
// publication rather than pairing generation A with receipt B.
func TestAGenerationChangeMidCompositionRefusesToAttest(t *testing.T) {
	healthy := healthyReceipt()
	var reads int64
	st := publicationStore{
		storedTarget: healthy.IRI(),
		body:         receiptTriples(healthy),
		failDump:     true,
	}
	// Each freshness read reports a DIFFERENT live digest, which is exactly the
	// world a publication landing mid-call produces.
	st.fakeStore.graphFreshness = func(context.Context) seedmeta.Verification {
		n := atomic.AddInt64(&reads, 1)
		return seedmeta.Verification{
			State: seedmeta.FreshnessCurrent,
			Live:  seedmeta.Marker{Digest: strings.Repeat("a", 63) + string(rune('0'+n))},
		}
	}
	a := serverWith(st).graphAuthorityFor(context.Background(), pubTestDomain)
	pub := a.GetCurrentPublication()
	if pub.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an authority paired two generations", pub.GetResolution())
	}
	if !strings.Contains(pub.GetDetail(), "changed while this authority was being composed") {
		t.Fatalf("the refusal does not name the race: %q", pub.GetDetail())
	}

	// Control: a stable generation must still attest.
	stable := publicationStore{storedTarget: healthy.IRI(), body: receiptTriples(healthy), failDump: true}
	stable.fakeStore.graphFreshness = func(context.Context) seedmeta.Verification {
		return seedmeta.Verification{State: seedmeta.FreshnessCurrent, Live: seedmeta.Marker{Digest: strings.Repeat("b", 64)}}
	}
	a = serverWith(stable).graphAuthorityFor(context.Background(), pubTestDomain)
	if got := a.GetCurrentPublication().GetResolution(); got != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a stable generation failed to attest: %v %s", got, a.GetCurrentPublication().GetDetail())
	}
}

// FALSIFIER 4. The lookup is bounded: it never dumps the graph, it costs
// exactly two subject reads, and an authority nobody asked a publication
// question of costs zero.
func TestThePublicationLookupIsBounded(t *testing.T) {
	healthy := healthyReceipt()
	var describes int64
	st := publicationStore{
		storedTarget: healthy.IRI(),
		body:         receiptTriples(healthy),
		describes:    &describes,
		failDump:     true, // panics if the resolver reaches for the whole graph
	}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("bounded resolution failed: %v %s", got.GetResolution(), got.GetDetail())
	}
	if n := atomic.LoadInt64(&describes); n != 2 {
		t.Fatalf("the lookup cost %d Describe calls, want exactly 2 (pointer, then receipt)", n)
	}

	// An RPC that asks no publication question must do no publication work.
	var idle int64
	quiet := publicationStore{
		storedTarget: healthy.IRI(),
		body:         receiptTriples(healthy),
		describes:    &idle,
		failDump:     true,
	}
	a := serverWith(quiet).graphAuthorityFor(context.Background(), "")
	if a.GetCurrentPublication().GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNSPECIFIED {
		t.Fatalf("an unasked authority resolved a publication: %v", a.GetCurrentPublication().GetResolution())
	}
	if n := atomic.LoadInt64(&idle); n != 0 {
		t.Fatalf("an unasked authority still made %d publication Describe calls", n)
	}
}
