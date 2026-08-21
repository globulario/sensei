// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func formatReadSurfaceError(surface string, err error) string {
	if err == nil {
		return ""
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return fmt.Sprintf("%s unavailable: awareness-graph backend is unreachable; this is not an empty/no-results response: %s", surface, st.Message())
		}
	}
	detail := strings.ToLower(err.Error())
	if strings.Contains(detail, "connection refused") || strings.Contains(detail, "deadline exceeded") {
		return fmt.Sprintf("%s unavailable: awareness-graph backend is unreachable; this is not an empty/no-results response: %v", surface, err)
	}
	return err.Error()
}

// backendUnreachable reports whether an RPC failed because nothing answered,
// as opposed to a backend that answered and declined.
//
// It is the same classification formatReadSurfaceError makes, exposed as a
// predicate for callers that must ROUTE on it rather than phrase it. gRPC dials
// lazily, so an endpoint nobody is serving does not fail at connect time — it
// fails on the first call, with Unavailable — which is why a caller checking
// only the dial sees nothing wrong and reports whatever the composed absence
// looks like instead (#231).
func backendUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return true
		}
	}
	detail := strings.ToLower(err.Error())
	return strings.Contains(detail, "connection refused") || strings.Contains(detail, "deadline exceeded")
}
