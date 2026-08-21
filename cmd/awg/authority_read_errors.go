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

// backendUnreachable reports whether an RPC failed because NOTHING ANSWERED —
// a transport failure — as opposed to a backend that answered and declined.
//
// The distinction is finer than the status code, and the code alone gets it
// wrong. A running graph server whose store is unavailable returns
// codes.Unavailable itself (golang/server/metadata.go), and so does a transport
// that never reached one. Routing on the code would send an operator to start a
// server that is already running, which is the same wrong-repair defect this
// predicate exists to prevent, one layer in.
//
// So the code narrows the candidates and the transport signature decides. gRPC
// phrases a failure to reach a peer distinctly ("connection refused", "no such
// host", "transport:", "last connection error"), while a service-level refusal
// carries the server's own message.
//
// It is deliberately conservative: an Unavailable that does not look like a
// transport failure is treated as a reachable backend declining, because
// "something is there and unhappy" is the safer thing to report about an
// endpoint you cannot classify.
//
// Callers that only PHRASE the failure should use formatReadSurfaceError, which
// stays deliberately broader: for prose, "the backend is unreachable, this is
// not an empty result" is true enough of both worlds. This predicate is for
// callers that must ROUTE on the difference (#231).
func backendUnreachable(err error) bool {
	if err == nil {
		return false
	}
	detail := strings.ToLower(err.Error())
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded:
			return looksLikeTransportFailure(detail)
		default:
			return false
		}
	}
	return looksLikeTransportFailure(detail)
}

func looksLikeTransportFailure(lowerDetail string) bool {
	for _, marker := range []string{
		"connection refused",
		"connection error",
		"no such host",
		"transport:",
		"i/o timeout",
		"context deadline exceeded while awaiting", // gRPC's dial-timeout phrasing
	} {
		if strings.Contains(lowerDetail, marker) {
			return true
		}
	}
	return false
}
