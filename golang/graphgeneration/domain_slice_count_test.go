// SPDX-License-Identifier: AGPL-3.0-only

package graphgeneration

import (
	"strings"
	"testing"
)

const sliceCountNT = `<https://globular.io/awareness#invariant/a> <https://globular.io/awareness#repo> "example.com/one" .
<https://globular.io/awareness#invariant/a> <http://www.w3.org/2000/01/rdf-schema#label> "A" .
<https://globular.io/awareness#invariant/a> <http://www.w3.org/2000/01/rdf-schema#label> "A" .
<https://globular.io/awareness#invariant/b> <https://globular.io/awareness#repo> "example.com/two" .
<https://globular.io/awareness#invariant/b> <http://www.w3.org/2000/01/rdf-schema#label> "B" .
`

// The count must describe exactly the content the digest describes; a count of
// one slice beside a digest of another is worse than no count at all.
func TestSliceTripleCountCountsTheSameLinesTheDigestCovers(t *testing.T) {
	if got := SliceTripleCount([]byte(sliceCountNT), "example.com/one"); got != 2 {
		t.Fatalf("count=%d, want 2 (the duplicate line is one triple)", got)
	}
	if got := SliceTripleCount([]byte(sliceCountNT), "example.com/two"); got != 2 {
		t.Fatalf("count=%d, want 2", got)
	}
	if got := SliceTripleCount([]byte(sliceCountNT), "example.com/absent"); got != 0 {
		t.Fatalf("count=%d, want 0 for a domain with no content", got)
	}
}

func TestMaterialShortfall(t *testing.T) {
	cases := []struct {
		name           string
		expected, live int64
		want           bool
	}{
		{"a domain that lost everything", 121338, 6, true},
		{"exactly half is not yet material", 100, 50, false},
		{"below half is material", 100, 49, true},
		{"live above expected is never a shortfall", 100, 140, false},
		{"equal is never a shortfall", 100, 100, false},
		{"no recorded expectation cannot be shortfallen", 0, 0, false},
		{"an unrecorded count is not a count of nothing", 0, 500, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaterialShortfall(tc.expected, tc.live); got != tc.want {
				t.Fatalf("MaterialShortfall(%d, %d)=%v, want %v", tc.expected, tc.live, got, tc.want)
			}
		})
	}
}

func TestRepairCommandNamesTheDomain(t *testing.T) {
	if got := RepairCommand("github.com/globulario/services"); got != "sensei build --repo github.com/globulario/services" {
		t.Fatalf("got %q", got)
	}
	if got := RepairCommand("  "); !strings.Contains(got, "<domain>") {
		t.Fatalf("an unknown domain must still name the shape of the repair; got %q", got)
	}
}

// The recorded count must survive publication and be readable by the server
// that later compares a live store against it — that round trip is the whole
// mechanism by which a vanished domain becomes noticeable.
func TestComposeRecordsSliceCountsAndTheyRoundTrip(t *testing.T) {
	dir := t.TempDir()
	storeURL := "http://127.0.0.1:7878/store?default"
	graph := slice(domainA, "a.one", "a.two") + slice(domainB, "b.one")
	m := marker("00000000000000000000000000000000000000000000000000000000000000cc", 6)

	set := compose(nil, generationOf(m, domainA), m, nil, domainA, provenReport(domainA, m.Digest), graph, []string{domainA, domainB})
	if got := set.Domains[domainA].SliceTripleCount; got != 4 {
		t.Fatalf("built domain count=%d, want 4", got)
	}
	if got := set.Domains[domainB].SliceTripleCount; got != 2 {
		t.Fatalf("carried domain count=%d, want 2 — counted from the published content, not carried blind", got)
	}

	if err := Write(dir, storeURL, set); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.Domains[domainA].SliceTripleCount; got != 4 {
		t.Fatalf("after round trip count=%d, want 4", got)
	}
	if got := loaded.Domains[domainB].SliceTripleCount; got != 2 {
		t.Fatalf("after round trip count=%d, want 2", got)
	}
}

// A proof set written before counts existed must not read as "this domain
// published nothing"; it reads as no expectation, which MaterialShortfall
// refuses to act on.
func TestAProofSetWithoutCountsRecordsNoExpectation(t *testing.T) {
	dir := t.TempDir()
	storeURL := "http://127.0.0.1:7878/store?default"
	m := marker("00000000000000000000000000000000000000000000000000000000000000dd", 2)
	if err := Write(dir, storeURL, &Set{
		Generation: generationOf(m, domainA),
		Marker:     m,
		Domains: map[string]DomainProof{
			domainA: {Report: provenReport(domainA, m.Digest), SliceDigest: "deadbeef"},
		},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := loaded.Domains[domainA].SliceTripleCount; got != 0 {
		t.Fatalf("count=%d, want 0", got)
	}
	if MaterialShortfall(loaded.Domains[domainA].SliceTripleCount, 0) {
		t.Fatal("a proof set that recorded no count must not accuse a live store of losing everything")
	}
}
