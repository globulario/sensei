// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"context"
	"testing"
)

func TestBearerToken(t *testing.T) {
	if BearerToken("") != nil {
		t.Error("empty token must yield a nil credential (so callers append unconditionally)")
	}
	if BearerToken("   ") != nil {
		t.Error("whitespace-only token must yield nil")
	}
	cred := BearerToken("  secret  ")
	if cred == nil {
		t.Fatal("non-empty token must yield a credential")
	}
	if cred.RequireTransportSecurity() {
		t.Error("bearer cred must permit plaintext (self-host / TLS-terminating proxy)")
	}
	md, err := cred.GetRequestMetadata(context.Background())
	if err != nil {
		t.Fatalf("GetRequestMetadata: %v", err)
	}
	if md["authorization"] != "Bearer secret" {
		t.Errorf("authorization = %q, want %q (trimmed)", md["authorization"], "Bearer secret")
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv(LegacyTokenEnv, "")
	t.Setenv(TokenEnv, "  tok  ")
	if got := TokenFromEnv(); got != "tok" {
		t.Errorf("TokenFromEnv = %q, want trimmed 'tok'", got)
	}
	t.Setenv(TokenEnv, "")
	if got := TokenFromEnv(); got != "" {
		t.Errorf("unset/empty must be '', got %q", got)
	}
}

func TestTokenFromEnvLegacyFallback(t *testing.T) {
	t.Setenv(TokenEnv, "")
	t.Setenv(LegacyTokenEnv, "  legacy  ")
	if got := TokenFromEnv(); got != "legacy" {
		t.Errorf("TokenFromEnv legacy fallback = %q, want trimmed 'legacy'", got)
	}

	t.Setenv(TokenEnv, "  sensei  ")
	if got := TokenFromEnv(); got != "sensei" {
		t.Errorf("TokenFromEnv should prefer %s over %s, got %q", TokenEnv, LegacyTokenEnv, got)
	}
}
