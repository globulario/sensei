// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=server.auth
// @awareness file_role=grpc_auth_interceptor
package main

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Opt-in bearer-token auth for the self-host service mode. When the server is
// started with a token (--auth-token / $AWG_TOKEN), every AwarenessGraph RPC
// must present "authorization: Bearer <token>". With no token the server is
// open — the correct default for a trusted-network self-host or local dev.
//
// The gRPC health and reflection services are exempt so liveness probes and
// tooling keep working without the secret.

// parseBearer extracts the token from an "authorization" header value. It
// accepts a case-insensitive "Bearer " scheme and a bare token. Pure/testable.
func parseBearer(header string) string {
	header = strings.TrimSpace(header)
	if i := strings.IndexByte(header, ' '); i > 0 {
		if strings.EqualFold(header[:i], "bearer") {
			return strings.TrimSpace(header[i+1:])
		}
	}
	return header
}

// isAuthExemptMethod reports whether a full gRPC method should bypass auth.
func isAuthExemptMethod(fullMethod string) bool {
	return strings.HasPrefix(fullMethod, "/grpc.health.") ||
		strings.HasPrefix(fullMethod, "/grpc.reflection.")
}

// checkBearer validates the incoming context's bearer token against want using
// a constant-time comparison. Returns a codes.Unauthenticated status on any
// mismatch or absence.
func checkBearer(ctx context.Context, want string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing request metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return status.Error(codes.Unauthenticated, "missing bearer token")
	}
	got := parseBearer(vals[0])
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return nil
}

// bearerUnaryInterceptor enforces the token on unary RPCs (health/reflection
// exempt).
func bearerUnaryInterceptor(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if isAuthExemptMethod(info.FullMethod) {
			return handler(ctx, req)
		}
		if err := checkBearer(ctx, token); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// bearerStreamInterceptor enforces the token on streaming RPCs.
func bearerStreamInterceptor(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if isAuthExemptMethod(info.FullMethod) {
			return handler(srv, ss)
		}
		if err := checkBearer(ss.Context(), token); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}
