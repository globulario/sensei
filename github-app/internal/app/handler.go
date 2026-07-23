package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/globulario/sensei-github-app/internal/briefing"
	githubapi "github.com/globulario/sensei-github-app/internal/github"
	"github.com/globulario/sensei-github-app/internal/webhook"
)

const maxWebhookBody = 5 << 20

// GitHubClient is the narrow GitHub surface used by the webhook processor.
type GitHubClient interface {
	GetPullRequestIdentity(context.Context, int64, string, string, int) (githubapi.PullRequestIdentity, error)
	ListPullRequestFiles(context.Context, int64, string, string, int) ([]githubapi.PullRequestFile, error)
	UpsertIssueComment(context.Context, int64, string, string, int, string, string) error
	UpsertCheckRun(context.Context, int64, string, string, string, string, string, string) error
}

// Handler receives authenticated GitHub webhooks.
type Handler struct {
	webhookSecret []byte
	github        GitHubClient
	logger        *slog.Logger
}

func NewHandler(webhookSecret []byte, github GitHubClient, logger *slog.Logger) (*Handler, error) {
	if len(webhookSecret) == 0 {
		return nil, errors.New("webhook secret is required")
	}
	if github == nil {
		return nil, errors.New("GitHub client is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{webhookSecret: webhookSecret, github: github, logger: logger}, nil
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", plainStatus("ok\n"))
	mux.HandleFunc("GET /readyz", plainStatus("ready\n"))
	mux.HandleFunc("POST /webhooks/github", h.handleGitHubWebhook)
	return mux
}

func plainStatus(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}
}

func (h *Handler) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "invalid webhook body", http.StatusBadRequest)
		return
	}
	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	if err := webhook.VerifySignature(h.webhookSecret, body, r.Header.Get("X-Hub-Signature-256")); err != nil {
		h.logger.Warn("rejected GitHub webhook", "delivery", deliveryID, "error", err)
		http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
		return
	}

	eventName := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	switch eventName {
	case "ping":
		w.WriteHeader(http.StatusNoContent)
		return
	case "pull_request":
		if deliveryID == "" {
			http.Error(w, "missing GitHub delivery identity", http.StatusBadRequest)
			return
		}
		if err := h.handlePullRequest(r.Context(), body, deliveryID); err != nil {
			h.logger.Error("process pull request webhook", "delivery", deliveryID, "error", err)
			http.Error(w, "failed to process pull request webhook", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) handlePullRequest(ctx context.Context, body []byte, deliveryID string) error {
	var event webhook.PullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("decode pull request event: %w", err)
	}
	if !event.Supported() {
		return nil
	}
	if !event.Valid() {
		return errors.New("pull request event is missing immutable analysis identity")
	}

	identity, err := h.currentIdentity(ctx, event)
	if err != nil {
		return err
	}
	if !matchesEvent(event, identity) {
		h.logStaleEvent(event, identity, deliveryID, "before file collection")
		return nil
	}

	files, err := h.github.ListPullRequestFiles(
		ctx,
		event.Installation.ID,
		event.Repository.Owner.Login,
		event.Repository.Name,
		event.PullRequest.Number,
	)
	if err != nil {
		return err
	}

	identity, err = h.currentIdentity(ctx, event)
	if err != nil {
		return err
	}
	if !matchesEvent(event, identity) {
		h.logStaleEvent(event, identity, deliveryID, "after file collection")
		return nil
	}

	report := briefing.Build(briefing.Input{
		Repository: event.Repository.FullName,
		PRNumber:   event.PullRequest.Number,
		BaseSHA:    event.PullRequest.Base.SHA,
		HeadSHA:    event.PullRequest.Head.SHA,
		Files:      files,
	})

	checkErr := h.github.UpsertCheckRun(
		ctx,
		event.Installation.ID,
		event.Repository.Owner.Login,
		event.Repository.Name,
		event.PullRequest.Head.SHA,
		report.ExternalID,
		report.CheckSummary,
		report.CheckText,
	)
	commentErr := h.github.UpsertIssueComment(
		ctx,
		event.Installation.ID,
		event.Repository.Owner.Login,
		event.Repository.Name,
		event.PullRequest.Number,
		briefing.CommentMarker,
		report.CommentBody,
	)
	if err := errors.Join(checkErr, commentErr); err != nil {
		return fmt.Errorf("publish Sensei briefing: %w", err)
	}

	h.logger.Info(
		"published Sensei briefing",
		"delivery", deliveryID,
		"event_action", event.Action,
		"installation_id", event.Installation.ID,
		"repository", event.Repository.FullName,
		"pull_request", event.PullRequest.Number,
		"base_sha", event.PullRequest.Base.SHA,
		"head_sha", event.PullRequest.Head.SHA,
		"check_external_id", report.ExternalID,
		"changed_files", len(files),
	)
	return nil
}

func (h *Handler) currentIdentity(ctx context.Context, event webhook.PullRequestEvent) (githubapi.PullRequestIdentity, error) {
	identity, err := h.github.GetPullRequestIdentity(
		ctx,
		event.Installation.ID,
		event.Repository.Owner.Login,
		event.Repository.Name,
		event.PullRequest.Number,
	)
	if err != nil {
		return githubapi.PullRequestIdentity{}, err
	}
	return identity, nil
}

func matchesEvent(event webhook.PullRequestEvent, identity githubapi.PullRequestIdentity) bool {
	return event.PullRequest.Base.SHA == identity.BaseSHA && event.PullRequest.Head.SHA == identity.HeadSHA
}

func (h *Handler) logStaleEvent(event webhook.PullRequestEvent, identity githubapi.PullRequestIdentity, deliveryID, stage string) {
	h.logger.Info(
		"discarded stale pull request delivery",
		"delivery", deliveryID,
		"event_action", event.Action,
		"installation_id", event.Installation.ID,
		"repository", event.Repository.FullName,
		"pull_request", event.PullRequest.Number,
		"stage", stage,
		"event_base_sha", event.PullRequest.Base.SHA,
		"event_head_sha", event.PullRequest.Head.SHA,
		"current_base_sha", identity.BaseSHA,
		"current_head_sha", identity.HeadSHA,
	)
}
