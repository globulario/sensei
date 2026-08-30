// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// FALSIFIER 5. The caller's publication question must survive EVERY degraded
// branch, including the earliest one.
//
// The scope-degraded path was repaired first; this nil-store return dropped the
// same predicate one branch earlier, and none of the previous falsifiers
// reached it. A degraded response must say "you asked and I cannot answer",
// never "nobody asked".
func TestANilStoreStillPreservesTheCallersPublicationQuestion(t *testing.T) {
	s := newServer(nil)
	s.homeDomain = "github.com/globulario/sensei"

	asked := s.degradedPreflightResponse("t", nil, time.Now(), pubTestDomain)
	pub := asked.GetAuthority().GetCurrentPublication()
	if pub.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: a supplied question was reported as unasked", pub.GetResolution())
	}
	if pub.GetRequestedDomain() != pubTestDomain {
		t.Fatalf("requested_domain = %q, want %q", pub.GetRequestedDomain(), pubTestDomain)
	}
	if !strings.Contains(pub.GetDetail(), "store is unavailable") {
		t.Fatalf("the refusal does not name the degradation: %q", pub.GetDetail())
	}

	// Control: nobody asked, so UNSPECIFIED remains correct.
	unasked := s.degradedPreflightResponse("t", nil, time.Now(), "")
	if got := unasked.GetAuthority().GetCurrentPublication().GetResolution(); got != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNSPECIFIED {
		t.Fatalf("an unasked degraded response reported %v, want UNSPECIFIED", got)
	}
}

// FALSIFIER 6. A v1 receipt cannot serve a field the v1 identity does not hash.
//
// The frozen v1 algorithm omits source_path, so a backfilled v1 receipt still
// verifies while exposing an unauthenticated path -- the present-and-unhashed
// defect v2 removed, returning through the version-agnostic parser.
func TestAV1ReceiptCarryingAV2OnlyFieldIsUnreadable(t *testing.T) {
	legacy := publication.Receipt{ // no Version: this is v1
		Domain: pubTestDomain, Revision: "f6b4755", Tree: "ad916f77",
		State: publication.CleanExact, SourceDigest: "cff0d611",
	}
	stored := legacy.IRI()

	backfilled := legacy
	backfilled.SourcePath = "docs/attacker-controlled"
	if backfilled.IRI() != stored {
		t.Fatal("the specimen is wrong: v1 identity must be unchanged by source_path, or this proves nothing")
	}

	body := receiptTriples(legacy)
	body = append(body, store.Triple{
		Predicate: "https://globular.io/awareness#publicationSourcePath",
		Object:    "docs/attacker-controlled",
	})
	st := publicationStore{storedTarget: stored, body: body, failDump: true}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an unauthenticated path was served as verified", got.GetResolution())
	}

	// Control: the same v1 receipt without the smuggled field verifies.
	clean := publicationStore{storedTarget: stored, body: receiptTriples(legacy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(clean), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a clean v1 receipt failed to verify: %v %s", got.GetResolution(), got.GetDetail())
	}
}

// FALSIFIER 7. Two current-publication targets mean the question has no single
// answer. Describe has no defined row order, so keeping the last one let the
// same graph attest either receipt.
func TestTwoCurrentPublicationTargetsAreUnreadable(t *testing.T) {
	a := healthyReceipt()
	b := a
	b.Revision = "0000000000000000000000000000000000000000"

	st := ambiguousPointerStore{targets: []string{a.IRI(), b.IRI()}, bodies: map[string][]store.Triple{
		a.IRI(): receiptTriples(a),
		b.IRI(): receiptTriples(b),
	}}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an ambiguous pointer attested one of two receipts", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "distinct IRI target") {
		t.Fatalf("the refusal does not name the ambiguity: %q", got.GetDetail())
	}

	// Control: one target still verifies.
	single := ambiguousPointerStore{targets: []string{a.IRI()}, bodies: map[string][]store.Triple{a.IRI(): receiptTriples(a)}}
	if got := resolveCurrentPublication(context.Background(), serverWith(single), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a single target failed to verify: %v %s", got.GetResolution(), got.GetDetail())
	}
}

type ambiguousPointerStore struct {
	fakeStore
	targets []string
	bodies  map[string][]store.Triple
}

func (p ambiguousPointerStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	if iri == publication.PointerIRI(pubTestDomain) {
		var out []store.Triple
		for _, t := range p.targets {
			out = append(out, store.Triple{
				Predicate:   publication.CurrentPublicationPredicate,
				Object:      t,
				ObjectIsIRI: true,
			})
		}
		return out, nil
	}
	if body, ok := p.bodies[iri]; ok {
		return body, nil
	}
	return p.fakeStore.Describe(ctx, iri)
}

// FALSIFIER 8. A pointer edge that exists but names no IRI is UNREADABLE.
//
// Excluding a literal-valued edge and reporting the empty result as ABSENT
// discards the only evidence that a pointer was ever written. A start gate
// allowed to bootstrap on absence would fail OPEN over malformed stored state,
// which is worse than the dangling case already repaired.
func TestAMalformedPointerIsUnreadableRatherThanAbsent(t *testing.T) {
	st := literalPointerStore{}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: a malformed pointer read as never-published", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "currentPublication edge") {
		t.Fatalf("the refusal does not name the malformed edge: %q", got.GetDetail())
	}

	// Control: a genuinely absent pointer is still ABSENT.
	absent := publicationStore{storedTarget: "", failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(absent), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_ABSENT {
		t.Fatalf("a genuinely missing pointer reported %v, want ABSENT", got.GetResolution())
	}
}

type literalPointerStore struct{ fakeStore }

func (p literalPointerStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	if iri == publication.PointerIRI(pubTestDomain) {
		// The predicate is present; its object is a literal, not an IRI.
		return []store.Triple{{
			Predicate:   publication.CurrentPublicationPredicate,
			Object:      "not-an-iri",
			ObjectIsIRI: false,
		}}, nil
	}
	return p.fakeStore.Describe(ctx, iri)
}

// FALSIFIER 9. Two distinct values for one identity-bearing receipt field mean
// the receipt has no single identity.
//
// The pointer-ambiguity repair counted pointer targets and left the receipt
// BODY with the same last-row-wins defect: SELECT has no ordering, so the value
// matching the stored IRI could win under one ordering and lose under another,
// making VERIFIED depend on row order.
func TestAReceiptWithTwoValuesForOneFieldIsUnreadable(t *testing.T) {
	healthy := healthyReceipt()
	body := receiptTriples(healthy)
	body = append(body, store.Triple{
		Predicate: "https://globular.io/awareness#publicationSourceRevision",
		Object:    "0000000000000000000000000000000000000000",
	})
	st := publicationStore{storedTarget: healthy.IRI(), body: body, failDump: true}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an ambiguous receipt field was attested", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "distinct values") {
		t.Fatalf("the refusal does not name the ambiguity: %q", got.GetDetail())
	}

	// Control: the unambiguous body verifies.
	clean := publicationStore{storedTarget: healthy.IRI(), body: receiptTriples(healthy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(clean), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("an unambiguous receipt failed: %v %s", got.GetResolution(), got.GetDetail())
	}
}

// FALSIFIER 10. The source state is a closed vocabulary, read by membership.
//
// A self-consistent receipt carrying an unrecognised state would otherwise
// verify and be projected as VERIFIED, presenting semantics this server cannot
// interpret as an attestation it can.
func TestAReceiptWithAnUnknownSourceStateIsUnreadable(t *testing.T) {
	odd := healthyReceipt()
	odd.State = publication.SourceState("PROBABLY_FINE")
	st := publicationStore{storedTarget: odd.IRI(), body: receiptTriples(odd), failDump: true}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an undefined source state was attested", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "source state") {
		t.Fatalf("the refusal does not name the state: %q", got.GetDetail())
	}
	// Control: every member of the vocabulary is accepted.
	for _, ok := range []publication.SourceState{publication.CleanExact, publication.Dirty, publication.Unknown} {
		r := healthyReceipt()
		r.State = ok
		st := publicationStore{storedTarget: r.IRI(), body: receiptTriples(r), failDump: true}
		if got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
			t.Fatalf("state %q was refused: %v %s", ok, got.GetResolution(), got.GetDetail())
		}
	}
}

// FALSIFIER 11. THE POINTER PROPERTY, not the two instances that exposed it.
//
// Falsifier 8 proved a literal-ONLY pointer is unreadable, and falsifier 7
// proved two IRI targets are unreadable. Neither reached the mixed case: a
// valid IRI edge alongside a malformed one, where the malformed edge
// contributes no target and the distinct-target count still reads 1.
//
// The invariant is that a publication pointer consists of EXACTLY ONE
// well-formed currentPublication edge. Attesting the readable half of an
// ambiguous pointer is choosing which half to believe.
func TestAPointerMixingValidAndMalformedEdgesIsUnreadable(t *testing.T) {
	healthy := healthyReceipt()
	st := mixedPointerStore{
		valid:  healthy.IRI(),
		bodies: map[string][]store.Triple{healthy.IRI(): receiptTriples(healthy)},
	}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: a pointer with a malformed edge attested its readable half", got.GetResolution())
	}

	// Control: the same pointer with ONLY the well-formed edge verifies, so the
	// test cannot pass by refusing everything.
	single := publicationStore{storedTarget: healthy.IRI(), body: receiptTriples(healthy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(single), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a single well-formed edge failed: %v %s", got.GetResolution(), got.GetDetail())
	}
}

type mixedPointerStore struct {
	fakeStore
	valid  string
	bodies map[string][]store.Triple
}

func (p mixedPointerStore) Describe(ctx context.Context, iri string) ([]store.Triple, error) {
	if iri == publication.PointerIRI(pubTestDomain) {
		return []store.Triple{
			{Predicate: publication.CurrentPublicationPredicate, Object: p.valid, ObjectIsIRI: true},
			{Predicate: publication.CurrentPublicationPredicate, Object: "garbage", ObjectIsIRI: false},
		}, nil
	}
	if body, ok := p.bodies[iri]; ok {
		return body, nil
	}
	return p.fakeStore.Describe(ctx, iri)
}

// FALSIFIER 12. Term kind is part of the value.
//
// Every publication field is published as a LITERAL. The resolver discarded
// Triple.ObjectIsIRI and re-rendered each object as a quoted literal, so an
// IRI-valued field was normalised into a well-formed literal and verified --
// the digest was computed over the normalisation rather than over what the
// store actually holds. Reproduced before it was repaired.
func TestAnIRIValuedReceiptFieldIsUnreadable(t *testing.T) {
	healthy := healthyReceipt()
	var body []store.Triple
	for _, tr := range receiptTriples(healthy) {
		if tr.Predicate == "https://globular.io/awareness#publicationSourceRevision" {
			tr.ObjectIsIRI = true // same text, stored as a different kind of term
		}
		body = append(body, tr)
	}
	st := publicationStore{storedTarget: healthy.IRI(), body: body, failDump: true}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an IRI term was normalised into a literal and verified", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "IRI term") {
		t.Fatalf("the refusal does not name the term kind: %q", got.GetDetail())
	}

	// Control: literal-valued fields still verify, and rdf:type stays an IRI
	// without tripping the rule -- it is not a publication field.
	clean := publicationStore{storedTarget: healthy.IRI(), body: receiptTriples(healthy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(clean), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a correctly stored receipt failed: %v %s", got.GetResolution(), got.GetDetail())
	}
}

// FALSIFIER 13. THE FIELD-AUTHENTICATION PROPERTY, generalised from v1's
// SourcePath.
//
// An unrecognised aw:publication... predicate parses past the term-kind and
// multiplicity checks, is then ignored by the parser, and never participates in
// the identity -- so the receipt verifies while carrying a field nothing
// authenticates. That is the v1 SourcePath defect stated over the namespace
// instead of over the single field that was caught.
//
// Reproduced as VERIFIED before this repair.
func TestAnUndefinedPublicationFieldIsUnreadable(t *testing.T) {
	healthy := healthyReceipt()
	body := append(receiptTriples(healthy), store.Triple{
		Predicate: "https://globular.io/awareness#publicationMystery",
		Object:    "x",
	})
	st := publicationStore{storedTarget: healthy.IRI(), body: body, failDump: true}
	got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain)
	if got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_UNREADABLE {
		t.Fatalf("resolution = %v, want UNREADABLE: an unauthenticated field rode inside a verified receipt", got.GetResolution())
	}
	if !strings.Contains(got.GetDetail(), "does not define") {
		t.Fatalf("the refusal does not name the undefined field: %q", got.GetDetail())
	}

	// Control 1: the clean receipt still verifies.
	clean := publicationStore{storedTarget: healthy.IRI(), body: receiptTriples(healthy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(clean), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a clean receipt failed: %v %s", got.GetResolution(), got.GetDetail())
	}

	// Control 2: NON-publication metadata is a different category and must NOT
	// be rejected. rdf:type and rdfs:label are ordinary metadata, not attested
	// content, and refusing every unfamiliar triple would refuse receipts over
	// facts the identity never claimed to cover.
	withMeta := append(receiptTriples(healthy),
		store.Triple{Predicate: "http://www.w3.org/2000/01/rdf-schema#comment", Object: "harmless"},
		store.Triple{Predicate: "https://globular.io/awareness#authoredIn", Object: "generated:somewhere"},
	)
	meta := publicationStore{storedTarget: healthy.IRI(), body: withMeta, failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(meta), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("ordinary metadata was rejected as an unauthenticated field: %v %s", got.GetResolution(), got.GetDetail())
	}
}

// A v1 receipt must not be judged against v2's vocabulary, or the frozen
// historical shape would stop verifying.
func TestEachVersionIsJudgedAgainstItsOwnVocabulary(t *testing.T) {
	legacy := publication.Receipt{
		Domain: pubTestDomain, Revision: "f6b4755", Tree: "ad916f77",
		State: publication.CleanExact, SourceDigest: "cff0d611",
		SourceRoot: "/tmp/build/docs/awareness",
	}
	st := publicationStore{storedTarget: legacy.IRI(), body: receiptTriples(legacy), failDump: true}
	if got := resolveCurrentPublication(context.Background(), serverWith(st), pubTestDomain); got.GetResolution() != awarenesspb.PublicationResolution_PUBLICATION_RESOLUTION_VERIFIED {
		t.Fatalf("a v1 receipt carrying v1's own SourceRoot was refused: %v %s", got.GetResolution(), got.GetDetail())
	}
}
