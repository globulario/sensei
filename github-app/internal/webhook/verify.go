package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

const signaturePrefix = "sha256="

var ErrInvalidSignature = errors.New("invalid GitHub webhook signature")

// VerifySignature validates GitHub's X-Hub-Signature-256 header in constant time.
func VerifySignature(secret, body []byte, signature string) error {
	if len(secret) == 0 || len(body) == 0 {
		return ErrInvalidSignature
	}
	if !strings.HasPrefix(signature, signaturePrefix) {
		return ErrInvalidSignature
	}

	provided, err := hex.DecodeString(strings.TrimPrefix(signature, signaturePrefix))
	if err != nil {
		return ErrInvalidSignature
	}

	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(provided, expected) {
		return ErrInvalidSignature
	}
	return nil
}
