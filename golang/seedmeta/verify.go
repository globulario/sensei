// SPDX-License-Identifier: Apache-2.0

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

type Verification struct {
	State           FreshnessState
	Expected        Marker
	Live            Marker
	LiveTripleCount int64
	MarkerPresent   bool
	SeedBuildCount  int64
	Detail          string
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
	ver.Detail = "live store matches expected validated graph artifact"
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
