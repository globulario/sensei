// SPDX-License-Identifier: AGPL-3.0-only

package graphgeneration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/closure"
	"github.com/globulario/sensei/golang/seedmeta"
)

const (
	domainA = "github.com/globulario/services"
	domainB = "github.com/globulario/sensei-code"
)

// slice builds the N-Triples a domain owns: one subject per identity, each
// tagged with the owning repo the way the extractor tags real output.
func slice(domain string, identities ...string) string {
	var b strings.Builder
	for _, id := range identities {
		subject := fmt.Sprintf("<urn:sensei:%s>", id)
		fmt.Fprintf(&b, "%s <%srepo> %q .\n", subject, seedmeta.NamespaceIRI, domain)
		fmt.Fprintf(&b, "%s <urn:sensei:title> %q .\n", subject, id)
	}
	return b.String()
}

// String-taking wrappers: the fixtures are easier to read as text, and the
// conversion is not what these tests are about.
func sliceDigest(nt, domain string) string { return SliceDigest([]byte(nt), domain) }

func partition(nt, domain string) (owned, rest []byte) { return PartitionByDomain([]byte(nt), domain) }

func compose(prev *Set, gen Generation, m seedmeta.Marker, tx []byte,
	built string, rep *closure.Report, nt string, registered []string) *Set {
	return Compose(prev, gen, m, tx, built, rep, []byte(nt), registered)
}

func provenReport(domain string, markerDigest string) *closure.Report {
	return &closure.Report{
		Domain:              domain,
		CertifiedSourceRoot: "/repo/" + DomainSlug(domain) + "/docs/awareness",
		MarkerDigest:        markerDigest,
		ExpectedToProject:   2,
		Projected:           2,
		ClosureProven:       true,
	}
}

func marker(digest string, triples int64) seedmeta.Marker {
	return seedmeta.Marker{Digest: digest, IRI: "urn:sensei:seed:" + digest, TripleCount: triples}
}

func generationOf(m seedmeta.Marker, built string) Generation {
	return Generation{
		MarkerDigest:    m.Digest,
		MarkerIRI:       m.IRI,
		TripleCount:     m.TripleCount,
		PublishedUnix:   1700000000,
		PublishedDomain: built,
	}
}

// TestBuildingOneDomainKeepsEveryOtherDomainAuthoritative is issue #176's
// regression, expressed at the level where the decision is actually made.
//
// Both build orders, twice over, with the assertion that matters: after any
// single-domain publication, EVERY registered domain still holds a proven
// closure report bound to the publication that is actually live.
func TestBuildingOneDomainKeepsEveryOtherDomainAuthoritative(t *testing.T) {
	for _, order := range [][]string{{domainA, domainB}, {domainB, domainA}} {
		t.Run(strings.Join(order, "_then_"), func(t *testing.T) {
			dir := t.TempDir()
			store := "http://127.0.0.1:7878/store?default"
			registered := []string{domainA, domainB}

			sliceA := slice(domainA, "a.one", "a.two")
			sliceB := slice(domainB, "b.one", "b.two")
			graph := sliceA + sliceB

			// Genesis: both domains published and proven at generation g0.
			g0 := marker("00000000000000000000000000000000000000000000000000000000000000aa", 8)
			set := &Set{
				Generation: generationOf(g0, domainA),
				Marker:     g0,
				Domains: map[string]DomainProof{
					domainA: {Report: provenReport(domainA, g0.Digest), SliceDigest: sliceDigest(graph, domainA)},
					domainB: {Report: provenReport(domainB, g0.Digest), SliceDigest: sliceDigest(graph, domainB)},
				},
			}
			if err := Write(dir, store, set); err != nil {
				t.Fatalf("write genesis: %v", err)
			}

			// Now republish each domain in turn. Content is unchanged; only the
			// whole-graph marker moves, exactly as it does when a rebuild is a
			// no-op on triples.
			for i, built := range order {
				prev, err := Load(dir)
				if err != nil {
					t.Fatalf("load before building %s: %v", built, err)
				}
				next := marker(fmt.Sprintf("%063dbb", i+1), 8)
				composed := compose(prev, generationOf(next, built), next, nil,
					built, provenReport(built, next.Digest), graph, registered)
				if err := Write(dir, store, composed); err != nil {
					t.Fatalf("write after building %s: %v", built, err)
				}

				got, err := Load(dir)
				if err != nil {
					t.Fatalf("load after building %s: %v", built, err)
				}
				for _, domain := range registered {
					proof, ok := got.ProofFor(domain)
					if !ok {
						t.Fatalf("after building %s: %s has no proof at all", built, domain)
					}
					if proof.CarryForwardRefusal != "" {
						t.Fatalf("after building %s: %s refused carry-forward: %s", built, domain, proof.CarryForwardRefusal)
					}
					if !proof.Proven() {
						t.Fatalf("after building %s: %s is no longer proven", built, domain)
					}
					if proof.Report.MarkerDigest != next.Digest {
						t.Fatalf("after building %s: %s report cites publication %s but the live generation is %s",
							built, domain, proof.Report.MarkerDigest, next.Digest)
					}
				}
				// The carried domain keeps its own certified source root: it was
				// re-stamped, not overwritten with the builder's.
				other := domainA
				if built == domainA {
					other = domainB
				}
				carried, _ := got.ProofFor(other)
				if want := "/repo/" + DomainSlug(other) + "/docs/awareness"; carried.Report.CertifiedSourceRoot != want {
					t.Fatalf("carried proof for %s lost its source root: got %q want %q",
						other, carried.Report.CertifiedSourceRoot, want)
				}
			}
		})
	}
}

// A changed slice must never be re-stamped. This is the forbidden repair: it
// would put a fresh publication identity on a verdict computed against
// different content, which is the exact defect the closure report exists to
// catch.
func TestChangedSliceIsRefusedNotCarriedForward(t *testing.T) {
	before := slice(domainA, "a.one") + slice(domainB, "b.one")
	after := slice(domainA, "a.one") + slice(domainB, "b.one", "b.two")

	g0 := marker("00000000000000000000000000000000000000000000000000000000000000aa", 4)
	prev := &Set{
		Generation: generationOf(g0, domainA),
		Marker:     g0,
		Domains: map[string]DomainProof{
			domainA: {Report: provenReport(domainA, g0.Digest), SliceDigest: sliceDigest(before, domainA)},
			domainB: {Report: provenReport(domainB, g0.Digest), SliceDigest: sliceDigest(before, domainB)},
		},
	}
	g1 := marker("00000000000000000000000000000000000000000000000000000000000000bb", 6)
	next := compose(prev, generationOf(g1, domainA), g1, nil,
		domainA, provenReport(domainA, g1.Digest), after, []string{domainA, domainB})

	proof, ok := next.ProofFor(domainB)
	if !ok {
		t.Fatal("domain B vanished from the proof set")
	}
	if proof.Report != nil {
		t.Fatalf("domain B's proof was carried forward onto changed content: %+v", proof.Report)
	}
	if !strings.Contains(proof.CarryForwardRefusal, "changed") {
		t.Fatalf("refusal does not say why: %q", proof.CarryForwardRefusal)
	}
}

func TestMissingPriorProofIsRecordedAsRefusal(t *testing.T) {
	graph := slice(domainA, "a.one") + slice(domainB, "b.one")
	g1 := marker("00000000000000000000000000000000000000000000000000000000000000cc", 4)
	next := compose(&Set{Domains: map[string]DomainProof{}}, generationOf(g1, domainA), g1, nil,
		domainA, provenReport(domainA, g1.Digest), graph, []string{domainA, domainB})

	proof, _ := next.ProofFor(domainB)
	if proof.Report != nil {
		t.Fatal("a domain with no prior proof was reported as proven")
	}
	if proof.CarryForwardRefusal == "" {
		t.Fatal("absence was left silent instead of recorded")
	}
}

// A reader must observe one whole generation or the other. Writing a new
// generation must not disturb the one currently pointed at.
func TestReaderNeverObservesAHalfUpdatedSet(t *testing.T) {
	dir := t.TempDir()
	store := "http://127.0.0.1:7878/store?default"
	graph := slice(domainA, "a.one")

	g0 := marker("00000000000000000000000000000000000000000000000000000000000000aa", 2)
	if err := Write(dir, store, &Set{
		Generation: generationOf(g0, domainA),
		Marker:     g0,
		Domains:    map[string]DomainProof{domainA: {Report: provenReport(domainA, g0.Digest), SliceDigest: sliceDigest(graph, domainA)}},
	}); err != nil {
		t.Fatalf("write g0: %v", err)
	}

	// Simulate a build that laid down a generation directory and then died
	// before the pointer swap.
	g1 := marker("00000000000000000000000000000000000000000000000000000000000000bb", 2)
	orphan := GenerationDir(dir, g1.Digest)
	if err := os.MkdirAll(filepath.Join(orphan, "domains"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, GenerationFileName), []byte("{ truncated"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load after interrupted publication: %v", err)
	}
	if got.Marker.Digest != g0.Digest {
		t.Fatalf("reader followed an unpublished generation: got %s want %s", got.Marker.Digest, g0.Digest)
	}
	if !got.Domains[domainA].Proven() {
		t.Fatal("the previous generation stopped being proven because a later write was interrupted")
	}
}

func TestSliceDigestIgnoresOrderAndDuplicates(t *testing.T) {
	one := slice(domainA, "a.one", "a.two")
	lines := strings.Split(strings.TrimSpace(one), "\n")
	shuffled := strings.Join([]string{lines[3], lines[0], lines[1], lines[2], lines[0]}, "\n") + "\n"

	if sliceDigest(one, domainA) != sliceDigest(shuffled, domainA) {
		t.Fatal("slice digest depends on line order or duplication, so an unchanged slice can look changed")
	}
	if sliceDigest(one, domainA) == sliceDigest(slice(domainA, "a.one"), domainA) {
		t.Fatal("slice digest did not change when the slice did")
	}
}

func TestPartitionIsolatesOnlySolelyOwnedSubjects(t *testing.T) {
	shared := fmt.Sprintf("<urn:sensei:shared> <%srepo> %q .\n<urn:sensei:shared> <%srepo> %q .\n",
		seedmeta.NamespaceIRI, domainA, seedmeta.NamespaceIRI, domainB)
	graph := slice(domainA, "a.one") + shared

	owned, rest := partition(graph, domainA)
	if !strings.Contains(string(owned), "a.one") {
		t.Fatal("solely-owned subject was not claimed by its domain")
	}
	if strings.Contains(string(owned), "shared") {
		t.Fatal("a subject attributed to two domains was treated as solely owned")
	}
	if !strings.Contains(string(rest), "shared") {
		t.Fatal("a co-owned subject was dropped from the retained graph")
	}
}

func TestStoreIDIsStableAcrossEquivalentEndpoints(t *testing.T) {
	// The builder addresses the Graph Store endpoint and the server addresses
	// the SPARQL query endpoint. Same Oxigraph, so same proof set.
	a := StoreID("http://127.0.0.1:7878/store?default")
	b := StoreID("http://127.0.0.1:7878/query")
	c := StoreID("HTTP://127.0.0.1:7878/store/")
	if a != b || b != c {
		t.Fatalf("equivalent store endpoints resolved to different proof sets: %s %s %s", a, b, c)
	}
	if a == StoreID("http://127.0.0.1:7879/store") {
		t.Fatal("different stores share one proof set")
	}
}

// Regression for a defect the live two-domain run found in the first version of
// this package.
//
// SliceDigest used the same sole-ownership predicate as PartitionByDomain. A
// real store had 143 subjects co-owned by services and sensei-code; publishing
// sensei-code added its repo tag to those shared subjects, pushed them out of
// services' solely-owned set, and moved services' digest even though services'
// content was untouched. The carry-forward was then refused for a reason that
// was not true.
//
// A per-domain identity must not be a function of what other domains publish.
func TestSliceDigestIsUnaffectedByAnotherDomainClaimingASharedSubject(t *testing.T) {
	shared := fmt.Sprintf("<urn:sensei:shared> <%srepo> %q .\n<urn:sensei:shared> <urn:sensei:title> \"shared\" .\n",
		seedmeta.NamespaceIRI, domainA)
	beforeB := slice(domainA, "a.one") + shared
	// domainB now co-owns the shared subject. Nothing about A's content changed.
	afterB := beforeB + fmt.Sprintf("<urn:sensei:shared> <%srepo> %q .\n", seedmeta.NamespaceIRI, domainB) +
		slice(domainB, "b.one")

	if got, want := sliceDigest(afterB, domainA), sliceDigest(beforeB, domainA); got != want {
		t.Fatalf("another domain co-owning a shared subject changed %s's slice digest (%s -> %s)", domainA, want, got)
	}
	// But a real change to shared content must still be caught.
	mutated := afterB + "<urn:sensei:shared> <urn:sensei:extra> \"added\" .\n"
	if sliceDigest(mutated, domainA) == sliceDigest(afterB, domainA) {
		t.Fatal("a change to shared content did not move the slice digest")
	}
}

func TestRegisteredButUnpublishedDomainIsAbsentNotUnproven(t *testing.T) {
	graph := slice(domainA, "a.one")
	g1 := marker("00000000000000000000000000000000000000000000000000000000000000dd", 2)
	next := compose(&Set{Domains: map[string]DomainProof{}}, generationOf(g1, domainA), g1, nil,
		domainA, provenReport(domainA, g1.Digest), graph,
		[]string{domainA, domainB, "github.com/globulario/never-published"})

	if _, ok := next.ProofFor("github.com/globulario/never-published"); ok {
		t.Fatal("a registered domain that has published nothing was listed as unproven rather than absent")
	}
	if _, ok := next.ProofFor(domainB); ok {
		t.Fatalf("%s has no content in this graph and should not appear in the proof set", domainB)
	}
}

// Republishing byte-identical content reuses the generation directory. A
// domain that was refused last time and is proven this time must not leave its
// old refusal behind, or the verdict becomes a function of readdir order.
func TestRepublishingSameGenerationClearsTheOtherVerdictFile(t *testing.T) {
	dir := t.TempDir()
	store := "http://127.0.0.1:7878/store?default"
	g := marker("00000000000000000000000000000000000000000000000000000000000000ee", 4)

	if err := Write(dir, store, &Set{
		Generation: generationOf(g, domainA),
		Marker:     g,
		Domains: map[string]DomainProof{
			domainA: {Report: provenReport(domainA, g.Digest), SliceDigest: "x"},
			domainB: {SliceDigest: "y", CarryForwardRefusal: "no prior proof"},
		},
	}); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Same digest, but domain B is now proven.
	if err := Write(dir, store, &Set{
		Generation: generationOf(g, domainB),
		Marker:     g,
		Domains: map[string]DomainProof{
			domainA: {Report: provenReport(domainA, g.Digest), SliceDigest: "x"},
			domainB: {Report: provenReport(domainB, g.Digest), SliceDigest: "y"},
		},
	}); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	proof, _ := got.ProofFor(domainB)
	if proof.CarryForwardRefusal != "" {
		t.Fatalf("a stale refusal survived republication: %q", proof.CarryForwardRefusal)
	}
	if !proof.Proven() {
		t.Fatal("domain B is not proven after being republished with a proof")
	}
}

func TestLoopbackSpellingsResolveToOneProofSet(t *testing.T) {
	want := StoreID("http://127.0.0.1:7878/store?default")
	for _, u := range []string{
		"http://localhost:7878/query",
		"HTTP://LocalHost:7878/store",
		"http://[::1]:7878/query",
		"http://localhost.localdomain:7878/store?default",
	} {
		if got := StoreID(u); got != want {
			t.Fatalf("%s resolved to a different proof set (%s != %s)", u, got, want)
		}
	}
	if StoreID("http://localhost:7879/query") == want {
		t.Fatal("a different port shares one proof set")
	}
	if StoreID("http://example.internal:7878/query") == want {
		t.Fatal("a remote host collapsed into loopback")
	}
}
