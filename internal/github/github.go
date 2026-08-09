// Package github implements GitHub webhook integration for VigilAgent.
// It receives pull_request events and triggers automated code reviews.
package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Event represents a parsed GitHub webhook event.
type Event struct {
	Action      string       `json:"action"`
	Number      int          `json:"number"`
	PullRequest *PullRequest `json:"pull_request,omitempty"`
	Repository  *Repository  `json:"repository,omitempty"`
	Sender      *User        `json:"sender,omitempty"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Head    Ref    `json:"head"`
	Base    Ref    `json:"base"`
	DiffURL string `json:"diff_url"`
}

// Ref represents a git ref (branch, commit, etc.).
type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// Repository represents a GitHub repository.
type Repository struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

// User represents a GitHub user.
type User struct {
	Login string `json:"login"`
}

// Config holds GitHub webhook configuration.
type Config struct {
	WebhookSecret string // HMAC secret for signature verification
	Enabled       bool
}

// Handler handles incoming GitHub webhooks.
type Handler struct {
	config     Config
	reviewFunc func(ctx context.Context, pr *PullRequest, repo *Repository) error
}

// NewHandler creates a new GitHub webhook handler.
func NewHandler(cfg Config, reviewFunc func(ctx context.Context, pr *PullRequest, repo *Repository) error) *Handler {
	return &Handler{
		config:     cfg,
		reviewFunc: reviewFunc,
	}
}

// ServeHTTP implements http.Handler for the GitHub webhook endpoint.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.config.Enabled {
		http.Error(w, `{"error":"github integration not configured"}`, http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Verify signature
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if sigHeader == "" {
		http.Error(w, `{"error":"missing signature"}`, http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if !h.verifySignature(body, sigHeader) {
		slog.Warn("github: invalid webhook signature")
		http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
		return
	}

	// Parse event
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "ping" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"msg":"pong"}`))
		return
	}

	if eventType != "pull_request" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"msg":"ignored event type"}`))
		return
	}

	var event Event
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// Only trigger on opened, synchronize (new commits pushed), or reopened
	switch event.Action {
	case "opened", "synchronize", "reopened":
		slog.Info("github: triggering PR review",
			"pr", event.PullRequest.Number,
			"repo", event.Repository.FullName,
			"action", event.Action,
		)

		// Trigger review asynchronously
		go func() {
			// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if err := h.reviewFunc(ctx, event.PullRequest, event.Repository); err != nil {
				slog.Error("github: PR review failed",
					"pr", event.PullRequest.Number,
					"error", err,
				)
			}
		}()

	default:
		slog.Debug("github: ignoring PR action", "action", event.Action)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"msg":"ok"}`))
}

// verifySignature checks the HMAC-SHA256 signature.
func (h *Handler) verifySignature(payload []byte, sigHeader string) bool {
	if !strings.HasPrefix(sigHeader, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHeader, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(h.config.WebhookSecret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}
