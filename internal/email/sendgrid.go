package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// SendGridConfig holds SendGrid API configuration.
type SendGridConfig struct {
	APIKey      string
	FromEmail   string
	FromName    string
	Templates   map[string]string // template_id → template name
	MaxRetries  int
	RetryDelay  time.Duration
}

// SendGridSender implements Sender using the SendGrid API v3.
// The VerificationService wraps this and constructs the full message
// (with correct token URLs) before calling Send().
type SendGridSender struct {
	cfg    SendGridConfig
	client *http.Client
}

// NewSendGridSender creates a new SendGrid email sender.
func NewSendGridSender(cfg SendGridConfig) *SendGridSender {
	if cfg.APIKey == "" {
		slog.Warn("SendGrid API key is empty, emails will fail")
	}
	if cfg.FromEmail == "" {
		cfg.FromEmail = "noreply@vigilagent.com"
	}
	if cfg.FromName == "" {
		cfg.FromName = "VigilAgent"
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Second
	}
	return &SendGridSender{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// Send implements the Sender interface.
// The VerificationService constructs the full Message (with correct token URLs)
// before calling this method.
// Supports multipart: sends both plain text and HTML when both are provided.
func (s *SendGridSender) Send(ctx context.Context, msg *Message) error {
	return s.sendWithRetry(ctx, msg.To, msg.Subject, msg.Body, msg.HTMLBody, "")
}

// sendWithRetry sends an email with retry logic.
func (s *SendGridSender) sendWithRetry(ctx context.Context, to []string, subject, body, htmlBody string, templateName string) error {
	var lastErr error
	for attempt := 0; attempt < s.cfg.MaxRetries; attempt++ {
		if err := s.send(ctx, to, subject, body, htmlBody, templateName); err != nil {
			lastErr = err
			slog.Warn("sendgrid: email send failed, retrying",
				"attempt", attempt+1,
				"max_retries", s.cfg.MaxRetries,
				"error", err,
			)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(s.cfg.RetryDelay * time.Duration(attempt+1)):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to send email after %d attempts: %w", s.cfg.MaxRetries, lastErr)
}

// send sends a single email via SendGrid API.
// Supports multipart: sends both plain text and HTML when htmlBody is provided.
func (s *SendGridSender) send(ctx context.Context, to []string, subject, body, htmlBody string, templateName string) error {
	// Build content array — plain text first, then HTML if provided.
	content := []map[string]string{
		{
			"type":  "text/plain",
			"value": body,
		},
	}
	if htmlBody != "" {
		content = append(content, map[string]string{
			"type":  "text/html",
			"value": htmlBody,
		})
	}

	toAddresses := make([]map[string]string, len(to))
	for i, addr := range to {
		toAddresses[i] = map[string]string{"email": addr}
	}

	// Build SendGrid v3 mail send payload
	payload := map[string]interface{}{
		"personalizations": []map[string]interface{}{
			{
				"to":      toAddresses,
				"subject": subject,
			},
		},
		"from": map[string]string{
			"email": s.cfg.FromEmail,
			"name":  s.cfg.FromName,
		},
		"content": content,
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.sendgrid.com/v3/mail/send", bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sendgrid request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		slog.Info("sendgrid: email sent successfully", "to", to, "subject", subject)
		return nil
	}

	// Read error response for better diagnostics
	respBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("sendgrid API error (status %d): %s", resp.StatusCode, strings.ReplaceAll(string(respBody), s.cfg.APIKey, maskAPIKey(s.cfg.APIKey)))
}

