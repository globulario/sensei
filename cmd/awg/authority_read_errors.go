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
