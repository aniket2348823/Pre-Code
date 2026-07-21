package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html/template"
	"log/slog"
	"net/smtp"
	"sync"
	"time"
)

// Sender defines the interface for sending emails.
type Sender interface {
	Send(ctx context.Context, msg *Message) error
}

// Message represents an email message.
type Message struct {
	To       []string
	Subject  string
	Body     string // plain text body
	HTMLBody string // HTML body (optional)
	From     string
}

// SMTPConfig holds SMTP server configuration.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
}

// SMTPSender implements Sender using SMTP.
type SMTPSender struct {
	config SMTPConfig
	auth   smtp.Auth
	mu     sync.Mutex
}

// NewSMTPSender creates a new SMTP email sender.
func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	return &SMTPSender{
		config: cfg,
		auth:   auth,
	}
}

// Send sends an email via SMTP.
func (s *SMTPSender) Send(ctx context.Context, msg *Message) error {
	if msg.From == "" {
		msg.From = fmt.Sprintf("%s <%s>", s.config.FromName, s.config.From)
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", msg.From))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", joinAddrs(msg.To)))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	buf.WriteString("MIME-Version: 1.0\r\n")

	if msg.HTMLBody != "" {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(msg.HTMLBody)
	} else {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(msg.Body)
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	err := smtp.SendMail(addr, s.auth, s.config.From, msg.To, buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	slog.Info("email sent", "to", msg.To, "subject", msg.Subject)
	return nil
}

func joinAddrs(addrs []string) string {
	result := ""
	for i, a := range addrs {
		if i > 0 {
			result += ", "
		}
		result += a
	}
	return result
}

// TokenGenerator generates cryptographically secure tokens.
type TokenGenerator struct{}

// GenerateToken creates a random hex token of the specified byte length.
func (tg *TokenGenerator) GenerateToken(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// TokenStore defines the interface for storing verification tokens.
type TokenStore interface {
	Store(ctx context.Context, vt *VerificationToken) error
	Get(ctx context.Context, token string) (*VerificationToken, bool)
	Delete(ctx context.Context, token string) error
	Cleanup(ctx context.Context, interval time.Duration)
}

// VerificationService manages email verification tokens.
type VerificationService struct {
	sender   Sender
	store    TokenStore // backed by Redis or in-memory
	tokenGen TokenGenerator
}

type VerificationToken struct {
	UserID    string
	Email     string
	Token     string
	ExpiresAt time.Time
	Purpose   string // "verify", "reset"
}

// NewVerificationService creates a new email verification service with in-memory token storage.
func NewVerificationService(sender Sender) *VerificationService {
	return &VerificationService{
		sender: sender,
		store:  NewInMemoryTokenStore(),
	}
}

// NewVerificationServiceWithRedis creates a new email verification service with Redis-backed token storage.
func NewVerificationServiceWithRedis(sender Sender, redisStore *RedisTokenStore) *VerificationService {
	return &VerificationService{
		sender: sender,
		store:  redisStore,
	}
}

// GenerateVerificationToken creates and stores a verification token.
func (vs *VerificationService) GenerateVerificationToken(userID, email, purpose string) (string, error) {
	token, err := vs.tokenGen.GenerateToken(32)
	if err != nil {
		return "", err
	}

	vt := &VerificationToken{
		UserID:    userID,
		Email:     email,
		Token:     token,
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Purpose:   purpose,
	}
	if err := vs.store.Store(context.Background(), vt); err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return token, nil
}

// ValidateToken checks if a token is valid and returns its data.
func (vs *VerificationService) ValidateToken(token string) (*VerificationToken, bool) {
	return vs.store.Get(context.Background(), token)
}

// InvalidateToken removes a token after use.
func (vs *VerificationService) InvalidateToken(token string) {
	_ = vs.store.Delete(context.Background(), token)
}

// SendVerificationEmail sends an email verification link.
func (vs *VerificationService) SendVerificationEmail(ctx context.Context, userID, email, baseURL string) error {
	token, err := vs.GenerateVerificationToken(userID, email, "verify")
	if err != nil {
		return err
	}

	verifyURL := fmt.Sprintf("%s/verify?token=%s", baseURL, token)

	msg := &Message{
		To:      []string{email},
		Subject: "Verify your VigilAgent account",
		Body:    fmt.Sprintf("Please verify your account by visiting: %s", verifyURL),
		HTMLBody: fmt.Sprintf(`
			<h2>Welcome to VigilAgent!</h2>
			<p>Please verify your account by clicking the link below:</p>
			<p><a href="%s" style="background:#4F46E5;color:white;padding:12px 24px;text-decoration:none;border-radius:6px;">Verify Email</a></p>
			<p>This link expires in 24 hours.</p>
			<p>If you didn't create an account, please ignore this email.</p>
		`, verifyURL),
	}

	return vs.sender.Send(ctx, msg)
}

// SendPasswordResetEmail sends a password reset link.
func (vs *VerificationService) SendPasswordResetEmail(ctx context.Context, userID, email, baseURL string) error {
	token, err := vs.GenerateVerificationToken(userID, email, "reset")
	if err != nil {
		return err
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", baseURL, token)

	msg := &Message{
		To:      []string{email},
		Subject: "Reset your VigilAgent password",
		Body:    fmt.Sprintf("Reset your password by visiting: %s", resetURL),
		HTMLBody: fmt.Sprintf(`
			<h2>Password Reset Request</h2>
			<p>You requested a password reset. Click the link below to set a new password:</p>
			<p><a href="%s" style="background:#DC2626;color:white;padding:12px 24px;text-decoration:none;border-radius:6px;">Reset Password</a></p>
			<p>This link expires in 1 hour.</p>
			<p>If you didn't request this, please ignore this email.</p>
		`, resetURL),
	}

	return vs.sender.Send(ctx, msg)
}

// Cleanup is a no-op for Redis-backed stores (Redis handles expiry via TTL).
// For in-memory stores, this runs periodic cleanup.
func (vs *VerificationService) Cleanup(ctx context.Context, interval time.Duration) {
	vs.store.Cleanup(ctx, interval)
}

// InMemoryTokenStore is the default in-memory implementation of TokenStore.
type InMemoryTokenStore struct {
	tokens map[string]*VerificationToken
	mu     sync.RWMutex
}

// NewInMemoryTokenStore creates a new in-memory token store.
func NewInMemoryTokenStore() *InMemoryTokenStore {
	return &InMemoryTokenStore{
		tokens: make(map[string]*VerificationToken),
	}
}

func (s *InMemoryTokenStore) Store(_ context.Context, vt *VerificationToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[vt.Token] = vt
	return nil
}

func (s *InMemoryTokenStore) Get(_ context.Context, token string) (*VerificationToken, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	vt, exists := s.tokens[token]
	if !exists {
		return nil, false
	}
	if time.Now().After(vt.ExpiresAt) {
		delete(s.tokens, token)
		return nil, false
	}
	return vt, true
}

func (s *InMemoryTokenStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
	return nil
}

func (s *InMemoryTokenStore) Cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for token, vt := range s.tokens {
				if now.After(vt.ExpiresAt) {
					delete(s.tokens, token)
				}
			}
			s.mu.Unlock()
		}
	}
}

// NoOpSender is a placeholder that logs emails instead of sending them.
type NoOpSender struct{}

func (n *NoOpSender) Send(ctx context.Context, msg *Message) error {
	slog.Info("email (no-op)", "to", msg.To, "subject", msg.Subject)
	return nil
}

// TemplateData holds data for email templates.
type TemplateData struct {
	UserName string
	Action   string
	URL      string
	Expiry   string
}

// TemplateEngine renders email templates.
type TemplateEngine struct {
	templates map[string]*template.Template
}

// NewTemplateEngine creates a new template engine with built-in templates.
func NewTemplateEngine() *TemplateEngine {
	te := &TemplateEngine{
		templates: make(map[string]*template.Template),
	}
	te.templates["verification"] = template.Must(template.New("verification").Parse(`
		<h2>Welcome to VigilAgent, {{.UserName}}!</h2>
		<p>Please verify your email address by clicking the link below:</p>
		<p><a href="{{.URL}}" style="background:#4F46E5;color:white;padding:12px 24px;text-decoration:none;border-radius:6px;">Verify Email</a></p>
		<p>This link expires in {{.Expiry}}.</p>
	`))
	te.templates["password_reset"] = template.Must(template.New("password_reset").Parse(`
		<h2>Password Reset</h2>
		<p>Hi {{.UserName}},</p>
		<p>Click below to reset your password:</p>
		<p><a href="{{.URL}}" style="background:#DC2626;color:white;padding:12px 24px;text-decoration:none;border-radius:6px;">Reset Password</a></p>
		<p>This link expires in {{.Expiry}}.</p>
	`))
	return te
}

// Render renders a template with the given data.
func (te *TemplateEngine) Render(name string, data TemplateData) (string, error) {
	tmpl, ok := te.templates[name]
	if !ok {
		return "", fmt.Errorf("template %q not found", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return buf.String(), nil
}
