package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	secret := []byte("correct horse battery staple")
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if err := VerifySignature(secret, body, signature); err != nil {
		t.Fatalf("VerifySignature() error = %v", err)
	}
}

func TestVerifySignatureRejectsTampering(t *testing.T) {
	secret := []byte("secret")
	body := []byte("original")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if err := VerifySignature(secret, []byte("tampered"), signature); err == nil {
		t.Fatal("VerifySignature() accepted a tampered body")
	}
}
