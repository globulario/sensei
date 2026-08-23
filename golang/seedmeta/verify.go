// SPDX-License-Identifier: AGPL-3.0-only

package seedmeta

import (
	"context"
	"fmt"
	"strconv"

	"github.com/globulario/sensei/golang/store"
)

type FreshnessState int

const (
	FreshnessUnknown FreshnessState = iota
	FreshnessCurrent
	FreshnessStale
	FreshnessEmpty
	FreshnessCheckError
)

func (s FreshnessState) String() string {
	switch s {
	case FreshnessCurrent:
		return "current"
	case FreshnessStale:
		return "stale"
	case FreshnessUnknown:
		return "unknown"
	case FreshnessEmpty:
		return "empty"
	case FreshnessCheckError:
		return "check_error"
	default:
		return "unspecified"
	}
}

// ContentState records whether the live store's CONTENT was compared against
// the expected artifact digest, and what that comparison found. It is separate
// from FreshnessState because the two answer different questions: freshness
// asks whether the live store carries the expected identity, integrity asks
// whether it still holds the bytes that identity was computed over.
type ContentState int

const (
	// ContentUnchecked means no content comparison was attempted or possible.
	// It is the state of every VerifyLiveStore result: that check compares the
	// marker and the triple count, never the content.
	ContentUnchecked ContentState = iota
	ContentMatch
	ContentMismatch
	ContentCheckError
)

func (s ContentState) String() string {
	switch s {
	case ContentMatch:
		return "match"
	case ContentMismatch:
		return "mismatch"
	case ContentCheckError:
		return "check_error"
	case ContentUnchecked:
		return "unchecked"
	default:
		return "unspecified"
	}
}

type Verification struct {
	State           FreshnessState
	Expected        Marker
	Live            Marker
	LiveTripleCount int64
	MarkerPresent   bool
	SeedBuildCount  int64
	Detail          string

	// Content, ContentDigest and ContentDetail are populated only by
	// VerifyLiveContent. A zero ContentState means the content was never
	// compared — never that it matched.
	Content       ContentState
	ContentDigest string
	ContentDetail string
}

// ContentProven reports whether this verification carries a recomputed
// content digest that matched the expected artifact. A count-and-marker
// verification is never content-proven, however current it is.
func (v Verification) ContentProven() bool {
	return v.State == FreshnessCurrent && v.Content == ContentMatch
}

// ContentDumper is the optional store capability a full-content integrity
// check needs: a complete N-Triples serialization of the live dataset.
type ContentDumper interface {
	DumpNTriples(context.Context) ([]byte, error)
}

type VerifierStore interface {
	Describe(context.Context, string) ([]store.Triple, error)
	CountTriples(context.Context) (int64, error)
	CountByClass(context.Context, string) (int64, error)
}

func VerifyLiveStore(ctx context.Context, s VerifierStore, expected Marker) Verification {
	ver := Verification{State: FreshnessUnknown, Expected: expected}
	if expected.Digest == "" || expected.IRI == "" {
		ver.Detail = "expected graph marker missing digest or IRI"
		return ver
	}
	if expected.TripleCount <= 0 {
		ver.Detail = "expected graph marker missing triple count"
		return ver
	}

	n, err := s.CountTriples(ctx)
	if err != nil {
		ver.State = FreshnessCheckError
		ver.Detail = fmt.Sprintf("count live triples: %v", err)
		return ver
	}
	ver.LiveTripleCount = n
	if n == 0 {
		ver.State = FreshnessEmpty
		ver.Detail = "live store is empty"
		return ver
	}

	if seedBuildCount, err := s.CountByClass(ctx, markerClassIRI); err == nil {
		ver.SeedBuildCount = seedBuildCount
	}

	triples, err := s.Describe(ctx, expected.IRI)
	if err != nil {
		ver.State = FreshnessCheckError
		ver.Detail = fmt.Sprintf("describe live marker %s: %v", expected.IRI, err)
		return ver
	}
	if len(triples) == 0 {
		ver.State = FreshnessStale
		ver.Detail = fmt.Sprintf("live store missing expected graph marker %s", expected.Digest)
		return ver
	}
	ver.MarkerPresent = true
	ver.Live = markerFromTriples(expected.IRI, triples)
	if ver.Live.Digest == "" {
		ver.State = FreshnessUnknown
		ver.Detail = "live graph marker missing digest literal"
		return ver
	}
	if ver.Live.Digest != expected.Digest {
		ver.State = FreshnessStale
		ver.Detail = fmt.Sprintf("live graph digest %s != expected %s", ver.Live.Digest, expected.Digest)
		return ver
	}
	if ver.Live.TripleCount <= 0 {
		ver.State = FreshnessUnknown
		ver.Detail = "live graph marker missing triple count"
		return ver
	}
	if ver.Live.TripleCount != expected.TripleCount {
		ver.State = FreshnessStale
		ver.Detail = fmt.Sprintf("live graph marker triple count %d != expected %d", ver.Live.TripleCount, expected.TripleCount)
		return ver
	}
	if ver.LiveTripleCount != expected.TripleCount {
		ver.State = FreshnessStale
		ver.Detail = fmt.Sprintf("live triple count %d != expected %d", ver.LiveTripleCount, expected.TripleCount)
		return ver
	}
	ver.State = FreshnessCurrent
	ver.Detail = liveCountAndMarkerDetail(expected)
	return ver
}

// liveCountAndMarkerDetail states exactly what VerifyLiveStore compared.
//
// It used to read "live store matches expected validated graph artifact",
// which is a stronger claim than a marker lookup and a triple count can
// support: a mutation that deletes one triple and inserts another keeps the
// count and the marker intact, and was reported as a match (#282). The
// detail now names its own evidence, so a reader can tell a count comparison
// from an integrity proof.
func liveCountAndMarkerDetail(expected Marker) string {
	return fmt.Sprintf("live graph marker %s and triple count %d match the expected artifact; store content not compared",
		shortDigest(expected.Digest), expected.TripleCount)
}

func shortDigest(digest string) string {
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

// VerifyLiveContent is VerifyLiveStore plus the integrity check the count
// cannot perform: it serializes the live store and recomputes the canonical
// artifact digest over it, exactly as the build path computed the expected
// digest in the first place (AppendMarker over canonicalized, marker-stripped
// N-Triples). Any drift in the dataset changes that digest.
//
// It costs a full serialization of the store, so it belongs on the load,
// publication and operator-diagnostic paths — not on a per-request one. The
// cheap checks run first and refuse before any dump is read.
func VerifyLiveContent(ctx context.Context, s VerifierStore, expected Marker) Verification {
	ver := VerifyLiveStore(ctx, s, expected)
	if ver.State != FreshnessCurrent {
		return ver
	}
	dumper, ok := s.(ContentDumper)
	if !ok {
		ver.Content = ContentUnchecked
		ver.ContentDetail = "store backend cannot serialize its content for an integrity check"
		return ver
	}
	dump, err := dumper.DumpNTriples(ctx)
	if err != nil {
		ver.State = FreshnessCheckError
		ver.Content = ContentCheckError
		ver.ContentDetail = fmt.Sprintf("dump live store for integrity check: %v", err)
		ver.Detail = ver.ContentDetail
		return ver
	}
	_, recomputed := AppendMarker(dump)
	ver.ContentDigest = recomputed.Digest
	if recomputed.Digest != expected.Digest {
		ver.State = FreshnessStale
		ver.Content = ContentMismatch
		ver.ContentDetail = fmt.Sprintf("live store content digest %s != expected %s", shortDigest(recomputed.Digest), shortDigest(expected.Digest))
		ver.Detail = fmt.Sprintf("live store drifted with its triple count intact: marker %s and count %d still match, but the content recomputes to %s",
			shortDigest(expected.Digest), expected.TripleCount, shortDigest(recomputed.Digest))
		return ver
	}
	ver.Content = ContentMatch
	ver.ContentDetail = fmt.Sprintf("live store content digest recomputes to %s", shortDigest(recomputed.Digest))
	ver.Detail = fmt.Sprintf("live store content recomputes to the expected validated graph artifact %s (%d triples)",
		shortDigest(expected.Digest), expected.TripleCount)
	return ver
}

func markerFromTriples(iri string, triples []store.Triple) Marker {
	marker := Marker{IRI: iri}
	for _, t := range triples {
		switch t.Predicate {
		case markerDigestIRI:
			marker.Digest = t.Object
		case markerTripleCountIRI:
			if n, err := strconv.ParseInt(t.Object, 10, 64); err == nil && n >= 0 {
				marker.TripleCount = n
			}
		}
	}
	return marker
}
