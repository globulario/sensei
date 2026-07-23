package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInstallationTokenExchangeAndCache(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/app/installations/77/access_tokens" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		authorization := r.Header.Get("Authorization")
		if !strings.HasPrefix(authorization, "Bearer ") || len(strings.Split(strings.TrimPrefix(authorization, "Bearer "), ".")) != 3 {
			t.Fatalf("invalid JWT authorization header: %q", authorization)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"installation-token","expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	auth, err := NewAuthenticator("12345", privateKeyPEM, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	auth.now = func() time.Time { return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC) }

	for i := 0; i < 2; i++ {
		token, err := auth.InstallationToken(context.Background(), 77)
		if err != nil {
			t.Fatal(err)
		}
		if token != "installation-token" {
			t.Fatalf("token = %q", token)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("token exchange calls = %d, want 1", calls.Load())
	}
}
