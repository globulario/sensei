package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsMountedSecrets(t *testing.T) {
	clearConfigEnvironment(t)
	dir := t.TempDir()
	privateKeyPath := filepath.Join(dir, "private-key.pem")
	webhookSecretPath := filepath.Join(dir, "webhook-secret.txt")
	if err := os.WriteFile(privateKeyPath, []byte("private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(webhookSecretPath, []byte("webhook-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SENSEI_GITHUB_APP_ID", "123")
	t.Setenv("SENSEI_GITHUB_PRIVATE_KEY_FILE", privateKeyPath)
	t.Setenv("SENSEI_GITHUB_WEBHOOK_SECRET_FILE", webhookSecretPath)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.PrivateKeyPEM) != "private-key" {
		t.Fatalf("private key = %q", cfg.PrivateKeyPEM)
	}
	if string(cfg.WebhookSecret) != "webhook-secret" {
		t.Fatalf("webhook secret = %q", cfg.WebhookSecret)
	}
	if cfg.ListenAddr != defaultListenAddr || cfg.GitHubAPIURL != defaultAPIURL {
		t.Fatalf("defaults = %q, %q", cfg.ListenAddr, cfg.GitHubAPIURL)
	}
}

func TestLoadRejectsEmptyMountedWebhookSecret(t *testing.T) {
	clearConfigEnvironment(t)
	dir := t.TempDir()
	webhookSecretPath := filepath.Join(dir, "webhook-secret.txt")
	if err := os.WriteFile(webhookSecretPath, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SENSEI_GITHUB_APP_ID", "123")
	t.Setenv("SENSEI_GITHUB_PRIVATE_KEY", "private-key")
	t.Setenv("SENSEI_GITHUB_WEBHOOK_SECRET_FILE", webhookSecretPath)

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted an empty mounted webhook secret")
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"SENSEI_GITHUB_APP_ID",
		"SENSEI_GITHUB_PRIVATE_KEY",
		"SENSEI_GITHUB_PRIVATE_KEY_FILE",
		"SENSEI_GITHUB_WEBHOOK_SECRET",
		"SENSEI_GITHUB_WEBHOOK_SECRET_FILE",
		"SENSEI_GITHUB_LISTEN_ADDR",
		"SENSEI_GITHUB_API_URL",
	} {
		t.Setenv(name, "")
	}
}
