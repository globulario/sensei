// SPDX-License-Identifier: AGPL-3.0-only

package seedmeta

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/globulario/sensei/golang/store"
)

// fakeStore serves the verification surface from one in-memory N-Triples
// artifact, so a test can mutate the "live" dataset the way a SPARQL update
// would and observe what each check concludes.
type fakeStore struct {
	nt      []byte
	dumpErr error
}

func (f *fakeStore) lines() []string {
	var out []string
	for _, raw := range strings.Split(string(f.nt), "\n") {
		if line := strings.TrimSpace(raw); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (f *fakeStore) CountTriples(context.Context) (int64, error) {
	return int64(len(f.lines())), nil
}

func (f *fakeStore) CountByClass(context.Context, string) (int64, error) { return 1, nil }

func (f *fakeStore) Describe(_ context.Context, iri string) ([]store.Triple, error) {
	var out []store.Triple
	for _, line := range f.lines() {
		if !strings.HasPrefix(line, "<"+iri+"> ") {
			continue
		}
		subj, value, pred, ok := parseLiteralLine(line)
		if !ok || subj != iri {
			continue
		}
		out = append(out, store.Triple{Predicate: pred, Object: value})
	}
	return out, nil
}

// dumpableStore adds the content capability. fakeStore deliberately does not
// have it, so a test can prove an unverifiable backend is not reported proven.
type dumpableStore struct{ *fakeStore }

func (d dumpableStore) DumpNTriples(context.Context) ([]byte, error) {
	if d.dumpErr != nil {
		return nil, d.dumpErr
	}
	return d.nt, nil
}

func artifact(t *testing.T, triples ...string) ([]byte, Marker) {
	t.Helper()
	nt, marker := AppendMarker([]byte(strings.Join(triples, "\n") + "\n"))
	return nt, marker
}

func realTriples() []string {
	return []string{
		`<https://example.test/a> <https://example.test/p> <https://example.test/b> .`,
		`<https://example.test/c> <https://example.test/p> <https://example.test/d> .`,
		`<https://example.test/e> <https://example.test/p> <https://example.test/f> .`,
	}
}

// TestVerifyLiveStore_CurrentDetailDoesNotClaimContentMatch pins the #282
// finding: the count-and-marker check must describe the evidence it actually
// has. It compared a marker and a number; it did not compare the store.
func TestVerifyLiveStore_CurrentDetailDoesNotClaimContentMatch(t *testing.T) {
	nt, marker := artifact(t, realTriples()...)
	ver := VerifyLiveStore(context.Background(), &fakeStore{nt: nt}, marker)
	if ver.State != FreshnessCurrent {
		t.Fatalf("state=%s detail=%q, want current", ver.State, ver.Detail)
	}
	if ver.Content != ContentUnchecked {
		t.Fatalf("content=%s, want unchecked: VerifyLiveStore never compares content", ver.Content)
	}
	if ver.ContentProven() {
		t.Fatal("ContentProven() is true for a count-only verification")
	}
	if strings.Contains(ver.Detail, "live store matches expected validated graph artifact") {
		t.Fatalf("detail=%q still claims the store matches the artifact", ver.Detail)
	}
	if !strings.Contains(ver.Detail, "content not compared") {
		t.Fatalf("detail=%q does not say the content was left uncompared", ver.Detail)
	}
}

// TestVerifyLiveContent_CountPreservingMutationIsRefused is the reproduction
// from #282 as a test: delete one real triple, insert one unrelated triple,
// leave the marker and the count untouched. The count channel cannot see it.
func TestVerifyLiveContent_CountPreservingMutationIsRefused(t *testing.T) {
	nt, marker := artifact(t, realTriples()...)
	mutated := strings.Replace(string(nt),
		`<https://example.test/c> <https://example.test/p> <https://example.test/d> .`,
		`<https://example.test/x> <https://example.test/q> <https://example.test/y> .`, 1)
	if mutated == string(nt) {
		t.Fatal("test setup: mutation did not apply")
	}
	live := &fakeStore{nt: []byte(mutated)}

	if ver := VerifyLiveStore(context.Background(), live, marker); ver.State != FreshnessCurrent {
		t.Fatalf("count check state=%s, want current — the mutation is count-preserving by construction", ver.State)
	}

	ver := VerifyLiveContent(context.Background(), dumpableStore{live}, marker)
	if ver.State != FreshnessStale {
		t.Fatalf("content check state=%s detail=%q, want stale", ver.State, ver.Detail)
	}
	if ver.Content != ContentMismatch {
		t.Fatalf("content=%s, want mismatch", ver.Content)
	}
	if ver.ContentProven() {
		t.Fatal("a drifted store is reported content-proven")
	}
	if ver.ContentDigest == "" || ver.ContentDigest == marker.Digest {
		t.Fatalf("content digest=%q, want a recomputed digest differing from %s", ver.ContentDigest, marker.Digest)
	}
}

func TestVerifyLiveContent_UnmutatedStoreIsProven(t *testing.T) {
	nt, marker := artifact(t, realTriples()...)
	ver := VerifyLiveContent(context.Background(), dumpableStore{&fakeStore{nt: nt}}, marker)
	if !ver.ContentProven() {
		t.Fatalf("state=%s content=%s detail=%q, want a content-proven current store", ver.State, ver.Content, ver.Detail)
	}
	if ver.ContentDigest != marker.Digest {
		t.Fatalf("content digest=%s, want %s", ver.ContentDigest, marker.Digest)
	}
}

// A dump the store cannot produce is not evidence of a match. Fail closed.
func TestVerifyLiveContent_DumpErrorFailsClosed(t *testing.T) {
	nt, marker := artifact(t, realTriples()...)
	live := &fakeStore{nt: nt, dumpErr: errors.New("store unreachable")}
	ver := VerifyLiveContent(context.Background(), dumpableStore{live}, marker)
	if ver.State != FreshnessCheckError {
		t.Fatalf("state=%s, want check_error", ver.State)
	}
	if ver.Content != ContentCheckError || ver.ContentProven() {
		t.Fatalf("content=%s proven=%v, want check_error and not proven", ver.Content, ver.ContentProven())
	}
}

// A backend with no serialization capability leaves the content unchecked. It
// must not inherit "proven" from the count check that did pass.
func TestVerifyLiveContent_BackendWithoutDumpIsNotProven(t *testing.T) {
	nt, marker := artifact(t, realTriples()...)
	ver := VerifyLiveContent(context.Background(), &countOnlyStore{fakeStore{nt: nt}}, marker)
	if ver.State != FreshnessCurrent {
		t.Fatalf("state=%s, want current: the count check still passed", ver.State)
	}
	if ver.Content != ContentUnchecked || ver.ContentProven() {
		t.Fatalf("content=%s proven=%v, want unchecked and not proven", ver.Content, ver.ContentProven())
	}
}

// countOnlyStore satisfies VerifierStore without ContentDumper.
type countOnlyStore struct{ fakeStore }

// VerifyLiveContent refuses on the cheap channel before paying for a dump.
func TestVerifyLiveContent_StaleCountRefusesWithoutDumping(t *testing.T) {
	nt, marker := artifact(t, realTriples()...)
	extra := string(nt) + "<https://example.test/x> <https://example.test/q> <https://example.test/y> .\n"
	live := &fakeStore{nt: []byte(extra), dumpErr: errors.New("dump must not be attempted")}
	ver := VerifyLiveContent(context.Background(), dumpableStore{live}, marker)
	if ver.State != FreshnessStale {
		t.Fatalf("state=%s, want stale from the triple-count comparison", ver.State)
	}
	if ver.Content != ContentUnchecked {
		t.Fatalf("content=%s, want unchecked: no dump should have been read", ver.Content)
	}
}
