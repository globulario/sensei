// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"context"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// TokenEnv is the environment variable holding the bearer token a Sensei client
// sends to an auth-enabled server. Empty/unset means "no token" — correct for
// the trusted-network self-host default where the server also has auth off.
const TokenEnv = "SENSEI_TOKEN"

// LegacyTokenEnv is the pre-rename compatibility fallback.
const LegacyTokenEnv = "AWG_TOKEN"

// TokenFromEnv returns the trimmed bearer token from $SENSEI_TOKEN, falling back
// to legacy $AWG_TOKEN when unset.
func TokenFromEnv() string {
	if v := strings.TrimSpace(os.Getenv(TokenEnv)); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(LegacyTokenEnv))
}

// bearerPerRPC attaches "authorization: Bearer <token>" to every RPC. It permits
// plaintext transport (RequireTransportSecurity=false) so self-host on a trusted
// network — or behind a TLS-terminating proxy — works without requiring mTLS on
// the app hop. Put TLS in front for untrusted networks.
type bearerPerRPC struct{ token string }

func (b bearerPerRPC) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

func (b bearerPerRPC) RequireTransportSecurity() bool { return false }

// BearerToken returns a per-RPC credential for token, or nil when token is empty
// (so callers can append it unconditionally).
func BearerToken(token string) credentials.PerRPCCredentials {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return bearerPerRPC{token: strings.TrimSpace(token)}
}

// DialConn opens a raw gRPC connection with insecure transport plus the bearer
// token from $SENSEI_TOKEN, or legacy $AWG_TOKEN, when set. Shared by the CLI
// commands and the MCP bridge so token handling lives in exactly one place.
func DialConn(addr string) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if cred := BearerToken(TokenFromEnv()); cred != nil {
		opts = append(opts, grpc.WithPerRPCCredentials(cred))
	}
	return grpc.NewClient(addr, opts...)
}
