package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/globulario/sensei-github-app/internal/app"
	"github.com/globulario/sensei-github-app/internal/config"
	githubapi "github.com/globulario/sensei-github-app/internal/github"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		target := "http://127.0.0.1:8080/readyz"
		if len(os.Args) > 2 {
			target = os.Args[2]
		}
		if err := runHealthcheck(target); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"version", version,
		"commit", commit,
		"build_time", buildTime,
	)
	if err := run(logger); err != nil {
		logger.Error("Sensei GitHub App stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	auth, err := githubapi.NewAuthenticator(cfg.AppID, cfg.PrivateKeyPEM, cfg.GitHubAPIURL, httpClient)
	if err != nil {
		return err
	}
	githubClient, err := githubapi.NewClient(auth, cfg.GitHubAPIURL, httpClient)
	if err != nil {
		return err
	}
	handler, err := app.NewHandler(cfg.WebhookSecret, githubClient, logger)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      45 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("Sensei GitHub App listening", "address", cfg.ListenAddr, "app_id", cfg.AppID)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runHealthcheck(target string) error {
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get(target)
	if err != nil {
		return fmt.Errorf("healthcheck request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned %s", response.Status)
	}
	return nil
}
