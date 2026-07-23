package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	defaultListenAddr = ":8080"
	defaultAPIURL     = "https://api.github.com"
)

// Config contains the immutable process configuration for the GitHub App.
type Config struct {
	AppID         string
	PrivateKeyPEM []byte
	WebhookSecret []byte
	ListenAddr    string
	GitHubAPIURL  string
}

// Load reads and validates process configuration from the environment.
func Load() (Config, error) {
	appID := strings.TrimSpace(os.Getenv("SENSEI_GITHUB_APP_ID"))
	if appID == "" {
		return Config{}, errors.New("SENSEI_GITHUB_APP_ID is required")
	}

	privateKey, err := loadPrivateKey()
	if err != nil {
		return Config{}, err
	}

	webhookSecret, err := loadWebhookSecret()
	if err != nil {
		return Config{}, err
	}

	listenAddr := strings.TrimSpace(os.Getenv("SENSEI_GITHUB_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}

	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SENSEI_GITHUB_API_URL")), "/")
	if apiURL == "" {
		apiURL = defaultAPIURL
	}

	return Config{
		AppID:         appID,
		PrivateKeyPEM: privateKey,
		WebhookSecret: webhookSecret,
		ListenAddr:    listenAddr,
		GitHubAPIURL:  apiURL,
	}, nil
}

func loadPrivateKey() ([]byte, error) {
	if value := os.Getenv("SENSEI_GITHUB_PRIVATE_KEY"); strings.TrimSpace(value) != "" {
		// Environment managers commonly preserve literal \n sequences.
		return []byte(strings.ReplaceAll(value, `\n`, "\n")), nil
	}

	path := strings.TrimSpace(os.Getenv("SENSEI_GITHUB_PRIVATE_KEY_FILE"))
	if path == "" {
		return nil, errors.New("SENSEI_GITHUB_PRIVATE_KEY or SENSEI_GITHUB_PRIVATE_KEY_FILE is required")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	return content, nil
}

func loadWebhookSecret() ([]byte, error) {
	if value := os.Getenv("SENSEI_GITHUB_WEBHOOK_SECRET"); value != "" {
		return []byte(value), nil
	}

	path := strings.TrimSpace(os.Getenv("SENSEI_GITHUB_WEBHOOK_SECRET_FILE"))
	if path == "" {
		return nil, errors.New("SENSEI_GITHUB_WEBHOOK_SECRET or SENSEI_GITHUB_WEBHOOK_SECRET_FILE is required")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App webhook secret: %w", err)
	}
	content = bytes.TrimSpace(content)
	if len(content) == 0 {
		return nil, errors.New("GitHub App webhook secret file is empty")
	}
	return content, nil
}
