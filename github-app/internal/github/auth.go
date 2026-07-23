package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const apiVersion = "2026-03-10"

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// Authenticator exchanges a short-lived app JWT for installation tokens.
type Authenticator struct {
	appID      string
	privateKey *rsa.PrivateKey
	baseURL    string
	httpClient *http.Client
	now        func() time.Time

	mu     sync.Mutex
	tokens map[int64]cachedToken
}

func NewAuthenticator(appID string, privateKeyPEM []byte, baseURL string, httpClient *http.Client) (*Authenticator, error) {
	if strings.TrimSpace(appID) == "" {
		return nil, errors.New("GitHub App ID is required")
	}
	privateKey, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Authenticator{
		appID:      appID,
		privateKey: privateKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		now:        time.Now,
		tokens:     make(map[int64]cachedToken),
	}, nil
}

func (a *Authenticator) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	if installationID <= 0 {
		return "", errors.New("installation ID must be positive")
	}

	now := a.now()
	a.mu.Lock()
	if token, ok := a.tokens[installationID]; ok && now.Add(time.Minute).Before(token.expiresAt) {
		a.mu.Unlock()
		return token.value, nil
	}
	a.mu.Unlock()

	jwt, err := a.appJWT(now)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", a.baseURL, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", fmt.Errorf("create installation token request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "sensei-github-app")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange installation token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return "", fmt.Errorf("exchange installation token: GitHub returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode installation token: %w", err)
	}
	if payload.Token == "" || payload.ExpiresAt.IsZero() {
		return "", errors.New("GitHub returned an incomplete installation token")
	}

	a.mu.Lock()
	a.tokens[installationID] = cachedToken{value: payload.Token, expiresAt: payload.ExpiresAt}
	a.mu.Unlock()
	return payload.Token, nil
}

func (a *Authenticator) appJWT(now time.Time) (string, error) {
	header, err := encodeJWTPart(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := encodeJWTPart(struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}{
		IssuedAt:  now.Add(-60 * time.Second).Unix(),
		ExpiresAt: now.Add(9 * time.Minute).Unix(),
		Issuer:    a.appID,
	})
	if err != nil {
		return "", err
	}

	unsigned := header + "." + claims
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign GitHub App JWT: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func encodeJWTPart(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode JWT: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func parsePrivateKey(content []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, errors.New("decode GitHub App private key: no PEM block found")
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("GitHub App private key is not RSA")
	}
	return key, nil
}
