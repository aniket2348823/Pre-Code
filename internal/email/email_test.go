package email

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

func TestTokenGenerator_GenerateToken(t *testing.T) {
	tg := &TokenGenerator{}

	token, err := tg.GenerateToken(16)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if len(token) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("expected token length 32, got %d", len(token))
	}

	// Test that tokens are unique
	token2, err := tg.GenerateToken(16)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	if token == token2 {
		t.Error("expected unique tokens")
	}
}

func TestInMemoryTokenStore_StoreAndGet(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "user-123",
		Email:     "test@example.com",
		Token:     "test-token-123",
		ExpiresAt: time.Now().Add(time.Hour),
		Purpose:   "verify",
	}

	if err := store.Store(ctx, vt); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	got, ok := store.Get(ctx, "test-token-123")
	if !ok {
		t.Fatal("expected token to exist")
	}
	if got.UserID != "user-123" {
		t.Errorf("expected user-123, got %s", got.UserID)
	}
}

func TestInMemoryTokenStore_GetExpired(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "user-123",
		Email:     "test@example.com",
		Token:     "expired-token",
		ExpiresAt: time.Now().Add(-time.Hour),
		Purpose:   "verify",
	}

	if err := store.Store(ctx, vt); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	_, ok := store.Get(ctx, "expired-token")
	if ok {
		t.Error("expected expired token to not be returned")
	}
}

func TestInMemoryTokenStore_Delete(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "user-123",
		Email:     "test@example.com",
		Token:     "token-to-delete",
		ExpiresAt: time.Now().Add(time.Hour),
		Purpose:   "verify",
	}

	if err := store.Store(ctx, vt); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if err := store.Delete(ctx, "token-to-delete"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, ok := store.Get(ctx, "token-to-delete")
	if ok {
		t.Error("expected token to be deleted")
	}
}

func TestInMemoryTokenStore_Cleanup(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	vt := &VerificationToken{
		UserID:    "user-123",
		Email:     "test@example.com",
		Token:     "cleanup-token",
		ExpiresAt: time.Now().Add(-time.Hour),
		Purpose:   "verify",
	}

	if err := store.Store(ctx, vt); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	store.Cleanup(ctx, 10*time.Millisecond)

	time.Sleep(50 * time.Millisecond)

	_, ok := store.Get(ctx, "cleanup-token")
	if ok {
		t.Error("expected expired token to be cleaned up")
	}
}

func TestVerificationService_GenerateAndValidate(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	token, err := vs.GenerateVerificationToken(context.Background(), "user-123", "test@example.com", "verify", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateVerificationToken failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	vt, ok := vs.ValidateToken(context.Background(), token)
	if !ok {
		t.Fatal("expected token to be valid")
	}
	if vt.UserID != "user-123" {
		t.Errorf("expected user-123, got %s", vt.UserID)
	}
	if vt.Purpose != "verify" {
		t.Errorf("expected verify, got %s", vt.Purpose)
	}
}

func TestVerificationService_InvalidateToken(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	token, err := vs.GenerateVerificationToken(context.Background(), "user-123", "test@example.com", "verify", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateVerificationToken failed: %v", err)
	}

	vs.InvalidateToken(context.Background(), token)

	_, ok := vs.ValidateToken(context.Background(), token)
	if ok {
		t.Error("expected token to be invalidated")
	}
}

func TestVerificationService_SendVerificationEmail(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	ctx := context.Background()
	err := vs.SendVerificationEmail(ctx, "user-123", "test@example.com", "https://example.com")
	if err != nil {
		t.Errorf("SendVerificationEmail failed: %v", err)
	}
}

func TestVerificationService_SendPasswordResetEmail(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	ctx := context.Background()
	err := vs.SendPasswordResetEmail(ctx, "user-123", "test@example.com", "https://example.com")
	if err != nil {
		t.Errorf("SendPasswordResetEmail failed: %v", err)
	}
}

func TestTemplateEngine_Render(t *testing.T) {
	te := NewTemplateEngine()

	data := TemplateData{
		UserName: "John",
		Action:   "verify",
		URL:      "https://example.com/verify",
		Expiry:   "24 hours",
	}

	html, err := te.Render("verification", data)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if html == "" {
		t.Error("expected non-empty HTML")
	}
}

func TestTemplateEngine_RenderNotFound(t *testing.T) {
	te := NewTemplateEngine()

	_, err := te.Render("nonexistent", TemplateData{})
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestNoOpSender_Send(t *testing.T) {
	sender := &NoOpSender{}
	msg := &Message{
		To:      []string{"test@example.com"},
		Subject: "Test",
		Body:    "Test body",
	}

	err := sender.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("NoOpSender.Send failed: %v", err)
	}
}

func TestJoinAddrs_Single(t *testing.T) {
	got := joinAddrs([]string{"a@example.com"})
	if got != "a@example.com" {
		t.Errorf("expected a@example.com, got %s", got)
	}
}

func TestJoinAddrs_Multiple(t *testing.T) {
	got := joinAddrs([]string{"a@ex.com", "b@ex.com", "c@ex.com"})
	if got != "a@ex.com, b@ex.com, c@ex.com" {
		t.Errorf("unexpected: %s", got)
	}
}

func TestJoinAddrs_Empty(t *testing.T) {
	got := joinAddrs([]string{})
	if got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestNewSMTPSender_NoAuth(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "localhost", Port: 587})
	if s.auth != nil {
		t.Error("expected nil auth when username empty")
	}
	if s.config.Host != "localhost" {
		t.Errorf("expected localhost, got %s", s.config.Host)
	}
}

func TestNewSMTPSender_WithAuth(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "smtp.ex.com", Port: 587, Username: "user", Password: "pass"})
	if s.auth == nil {
		t.Error("expected non-nil auth when username provided")
	}
}

func TestSMTPSender_Send_NoServer(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "localhost", Port: 19999, From: "test@ex.com", FromName: "Test"})
	err := s.Send(context.Background(), &Message{
		To:      []string{"recipient@ex.com"},
		Subject: "test",
		Body:    "hello",
	})
	if err == nil {
		t.Error("expected error sending to nonexistent server")
	}
}

func TestSMTPSender_Send_DefaultFrom(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "localhost", Port: 19999, From: "noreply@ex.com", FromName: "App"})
	msg := &Message{
		To:      []string{"to@ex.com"},
		Subject: "test",
		Body:    "hello",
	}
	_ = s.Send(context.Background(), msg)
	if msg.From == "" {
		t.Error("expected From to be set to default")
	}
	if !strings.Contains(msg.From, "noreply@ex.com") {
		t.Errorf("expected From to contain noreply@ex.com, got %s", msg.From)
	}
}

func TestSMTPSender_Send_WithCustomFrom(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "localhost", Port: 19999, From: "default@ex.com", FromName: "App"})
	msg := &Message{
		To:       []string{"to@ex.com"},
		Subject:  "test",
		Body:     "hello",
		From:     "custom@ex.com",
		HTMLBody: "<p>hi</p>",
	}
	_ = s.Send(context.Background(), msg)
	if msg.From != "custom@ex.com" {
		t.Errorf("expected custom From preserved, got %s", msg.From)
	}
}

func TestNewSendGridSender_Defaults(t *testing.T) {
	s := NewSendGridSender(SendGridConfig{})
	if s.cfg.FromEmail != "noreply@vigilagent.com" {
		t.Errorf("expected default from email, got %s", s.cfg.FromEmail)
	}
	if s.cfg.FromName != "VigilAgent" {
		t.Errorf("expected default from name, got %s", s.cfg.FromName)
	}
	if s.cfg.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", s.cfg.MaxRetries)
	}
	if s.cfg.RetryDelay != time.Second {
		t.Errorf("expected 1s delay, got %v", s.cfg.RetryDelay)
	}
}

func TestNewSendGridSender_WithConfig(t *testing.T) {
	s := NewSendGridSender(SendGridConfig{
		APIKey:     "key",
		FromEmail:  "me@ex.com",
		FromName:   "Me",
		MaxRetries: 5,
		RetryDelay: 2 * time.Second,
	})
	if s.cfg.MaxRetries != 5 {
		t.Errorf("expected 5 retries, got %d", s.cfg.MaxRetries)
	}
	if s.cfg.RetryDelay != 2*time.Second {
		t.Errorf("expected 2s delay, got %v", s.cfg.RetryDelay)
	}
}

func TestSendGridSender_Send_NoServer(t *testing.T) {
	s := NewSendGridSender(SendGridConfig{
		APIKey:     "test-key",
		FromEmail:  "from@ex.com",
		FromName:   "Test",
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})
	err := s.Send(context.Background(), &Message{
		To:      []string{"to@ex.com"},
		Subject: "test",
		Body:    "hello",
	})
	if err == nil {
		t.Error("expected error sending to nonexistent server")
	}
}

func TestSendGridSender_Send_EmptyTo(t *testing.T) {
	s := NewSendGridSender(SendGridConfig{
		APIKey:     "test-key",
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})
	err := s.Send(context.Background(), &Message{
		Subject: "test",
		Body:    "hello",
	})
	if err == nil {
		t.Error("expected error sending with empty To")
	}
}

func TestTokenGenerator_GenerateToken_DifferentSizes(t *testing.T) {
	tg := &TokenGenerator{}
	for _, size := range []int{8, 16, 32, 64} {
		token, err := tg.GenerateToken(size)
		if err != nil {
			t.Fatalf("GenerateToken(%d) failed: %v", size, err)
		}
		expectedLen := size * 2
		if len(token) != expectedLen {
			t.Errorf("GenerateToken(%d): expected len %d, got %d", size, expectedLen, len(token))
		}
	}
}

func TestInMemoryTokenStore_GetNotExist(t *testing.T) {
	store := NewInMemoryTokenStore()
	_, ok := store.Get(context.Background(), "nonexistent")
	if ok {
		t.Error("expected false for nonexistent token")
	}
}

func TestVerificationService_ValidateToken_Invalid(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)
	_, ok := vs.ValidateToken(context.Background(), "bad-token")
	if ok {
		t.Error("expected false for invalid token")
	}
}

func TestVerificationService_GenerateVerificationToken_Purpose(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	token, err := vs.GenerateVerificationToken(context.Background(), "u1", "e@x.com", "reset", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	vt, ok := vs.ValidateToken(context.Background(), token)
	if !ok {
		t.Fatal("expected valid token")
	}
	if vt.Purpose != "reset" {
		t.Errorf("expected reset, got %s", vt.Purpose)
	}
	if vt.Email != "e@x.com" {
		t.Errorf("expected e@x.com, got %s", vt.Email)
	}
}

func TestVerificationService_Cleanup(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	vs.store.Store(context.Background(), &VerificationToken{
		Token:     "expired",
		ExpiresAt: time.Now().Add(-time.Hour),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	vs.Cleanup(ctx, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	_, ok := vs.ValidateToken(context.Background(), "expired")
	if ok {
		t.Error("expected expired token to be cleaned up")
	}
}

func TestSendGridSender_Send_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth header")
		}
		w.WriteHeader(202)
	}))
	defer srv.Close()

	s := NewSendGridSender(SendGridConfig{
		APIKey:     "test-key",
		FromEmail:  "from@ex.com",
		FromName:   "Test",
		MaxRetries: 1,
		RetryDelay: 10 * time.Millisecond,
	})

	u, _ := url.Parse(srv.URL)
	s.client.Transport = &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(req)
	}}

	err := s.Send(context.Background(), &Message{
		To:      []string{"to@ex.com"},
		Subject: "test",
		Body:    "hello",
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestSendGridSender_Send_RetryOnFailure(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(500)
		fmt.Fprint(w, "error")
	}))
	defer srv.Close()

	s := NewSendGridSender(SendGridConfig{
		APIKey:     "test-key",
		FromEmail:  "from@ex.com",
		FromName:   "Test",
		MaxRetries: 3,
		RetryDelay: 10 * time.Millisecond,
	})

	u, _ := url.Parse(srv.URL)
	s.client.Transport = &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(req)
	}}

	err := s.Send(context.Background(), &Message{
		To:      []string{"to@ex.com"},
		Subject: "test",
		Body:    "hello",
	})
	if err == nil {
		t.Error("expected error after retries")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestSendGridSender_Send_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	s := NewSendGridSender(SendGridConfig{
		APIKey:     "test-key",
		FromEmail:  "from@ex.com",
		MaxRetries: 3,
		RetryDelay: 5 * time.Second,
	})

	u, _ := url.Parse(srv.URL)
	s.client.Transport = &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(req)
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := s.Send(ctx, &Message{
		To:      []string{"to@ex.com"},
		Subject: "test",
		Body:    "hello",
	})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestSendGridSender_Send_HTMLContent(t *testing.T) {
	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		receivedBody = string(body[:n])
		w.WriteHeader(202)
	}))
	defer srv.Close()

	s := NewSendGridSender(SendGridConfig{
		APIKey:     "test-key",
		FromEmail:  "from@ex.com",
		MaxRetries: 1,
	})

	u, _ := url.Parse(srv.URL)
	s.client.Transport = &roundTripperFunc{fn: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(req)
	}}

	err := s.Send(context.Background(), &Message{
		To:       []string{"to@ex.com"},
		Subject:  "test",
		Body:     "plain text",
		HTMLBody: "<p>html content</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(receivedBody, "text/html") {
		t.Error("expected HTML content type in payload")
	}
	if !strings.Contains(receivedBody, "html content") {
		t.Error("expected HTML body in payload")
	}
}

type roundTripperFunc struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f *roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}

func TestNoOpSender_Send_VerifyFields(t *testing.T) {
	sender := &NoOpSender{}
	msg := &Message{
		To:       []string{"a@x.com", "b@x.com"},
		Subject:  "Test Subject",
		Body:     "Test Body",
		HTMLBody: "<p>HTML</p>",
	}
	err := sender.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("NoOpSender.Send failed: %v", err)
	}
}

func TestTemplateEngine_Render_PasswordReset(t *testing.T) {
	te := NewTemplateEngine()
	html, err := te.Render("password_reset", TemplateData{
		UserName: "Alice",
		URL:      "https://example.com/reset?token=abc",
		Expiry:   "1 hour",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Alice") {
		t.Error("expected UserName in output")
	}
	if !strings.Contains(html, "reset?token=abc") {
		t.Error("expected URL in output")
	}
}

func TestVerificationService_SendVerificationEmail_BadSender(t *testing.T) {
	sender := &failingSender{err: fmt.Errorf("smtp down")}
	vs := NewVerificationService(sender)
	err := vs.SendVerificationEmail(context.Background(), "u1", "e@x.com", "https://ex.com")
	if err == nil {
		t.Error("expected error from failing sender")
	}
}

func TestVerificationService_SendPasswordResetEmail_BadSender(t *testing.T) {
	sender := &failingSender{err: fmt.Errorf("smtp down")}
	vs := NewVerificationService(sender)
	err := vs.SendPasswordResetEmail(context.Background(), "u1", "e@x.com", "https://ex.com")
	if err == nil {
		t.Error("expected error from failing sender")
	}
}

func TestInMemoryTokenStore_StoreOverwrite(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx := context.Background()

	vt1 := &VerificationToken{Token: "t1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)}
	store.Store(ctx, vt1)

	vt2 := &VerificationToken{Token: "t1", UserID: "u2", ExpiresAt: time.Now().Add(time.Hour)}
	store.Store(ctx, vt2)

	got, ok := store.Get(ctx, "t1")
	if !ok {
		t.Fatal("expected token")
	}
	if got.UserID != "u2" {
		t.Errorf("expected overwrite to u2, got %s", got.UserID)
	}
}

type failingSender struct {
	err error
}

func (f *failingSender) Send(_ context.Context, _ *Message) error {
	return f.err
}

// --- New tests below ---

func parsePort(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort failed: %v", err)
	}
	var port int
	for _, c := range portStr {
		if c >= '0' && c <= '9' {
			port = port*10 + int(c-'0')
		}
	}
	return port
}

func testSMTPServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start test SMTP server: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleSMTPConn(conn)
		}
	}()

	return ln.Addr().String(), func() { ln.Close() }
}

func handleSMTPConn(conn net.Conn) {
	defer conn.Close()
	conn.Write([]byte("220 localhost SMTP\r\n"))
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(string(buf[:n]))
		if strings.HasPrefix(strings.ToUpper(cmd), "QUIT") {
			conn.Write([]byte("221 Bye\r\n"))
			return
		}
		if strings.HasPrefix(strings.ToUpper(cmd), "DATA") {
			conn.Write([]byte("354 Start mail input\r\n"))
			for {
				n, err := conn.Read(buf)
				if err != nil {
					return
				}
				if strings.Contains(string(buf[:n]), "\r\n.\r\n") {
					break
				}
			}
			conn.Write([]byte("250 OK\r\n"))
		} else {
			conn.Write([]byte("250 OK\r\n"))
		}
	}
}

func TestSMTPSender_Send_Success(t *testing.T) {
	addr, stop := testSMTPServer(t)
	defer stop()
	port := parsePort(t, addr)

	s := NewSMTPSender(SMTPConfig{
		Host:     "127.0.0.1",
		Port:     port,
		Username: "",
		From:     "test@example.com",
		FromName: "Test",
	})

	msg := &Message{
		To:      []string{"recipient@example.com"},
		Subject: "Test Subject",
		Body:    "Test body content",
	}

	err := s.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send failed: %v", err)
	}
}

func TestSMTPSender_Send_WithAuthType(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{
		Host:     "127.0.0.1",
		Port:     587,
		Username: "user@example.com",
		Password: "password123",
		From:     "test@example.com",
		FromName: "Test",
	})
	if s.auth == nil {
		t.Error("expected non-nil auth")
	}

}

func TestSMTPSender_Send_MultipleRecipients(t *testing.T) {
	addr, stop := testSMTPServer(t)
	defer stop()
	port := parsePort(t, addr)

	s := NewSMTPSender(SMTPConfig{
		Host: "127.0.0.1",
		Port: port,
		From: "test@example.com",
	})

	msg := &Message{
		To:      []string{"a@example.com", "b@example.com"},
		Subject: "Multi-recipient test",
		Body:    "Hello everyone",
	}

	err := s.Send(context.Background(), msg)
	if err != nil {
		t.Errorf("Send with multiple recipients failed: %v", err)
	}
}

func TestInMemoryTokenStore_Cleanup_ContextDone(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		store.Cleanup(ctx, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Cleanup did not exit after context cancel")
	}
}

func TestInMemoryTokenStore_Cleanup_NoExpiredTokens(t *testing.T) {
	store := NewInMemoryTokenStore()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	store.Store(ctx, &VerificationToken{
		Token:     "valid-token",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	store.Cleanup(ctx, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	_, ok := store.Get(ctx, "valid-token")
	if !ok {
		t.Error("expected non-expired token to still exist")
	}
}

func TestVerificationService_Cleanup_ContextDone(t *testing.T) {
	sender := &NoOpSender{}
	vs := NewVerificationService(sender)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		vs.Cleanup(ctx, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Cleanup did not exit after context cancel")
	}
}

func TestTemplateEngine_Render_EmptyData(t *testing.T) {
	te := NewTemplateEngine()
	html, err := te.Render("verification", TemplateData{})
	if err != nil {
		t.Fatalf("Render with empty data failed: %v", err)
	}
	if html == "" {
		t.Error("expected non-empty HTML")
	}
}

// failingTokenStore always errors on Store, triggering GenerateVerificationToken error paths.
type failingTokenStore struct {
	err error
}

func (f *failingTokenStore) Store(_ context.Context, _ *VerificationToken) error {
	return f.err
}

func (f *failingTokenStore) Get(_ context.Context, _ string) (*VerificationToken, bool) {
	return nil, false
}

func (f *failingTokenStore) Delete(_ context.Context, _ string) error {
	return nil
}

func (f *failingTokenStore) Cleanup(_ context.Context, _ time.Duration) {}

func TestVerificationService_GenerateToken_StoreFailure(t *testing.T) {
	sender := &NoOpSender{}
	vs := &VerificationService{
		sender: sender,
		store:  &failingTokenStore{err: fmt.Errorf("store unavailable")},
	}

	_, err := vs.GenerateVerificationToken(context.Background(), "u1", "e@x.com", "verify", 24*time.Hour)
	if err == nil {
		t.Error("expected error from failing store")
	}
	if !strings.Contains(err.Error(), "store unavailable") {
		t.Errorf("expected store error in message, got: %v", err)
	}
}

func TestVerificationService_SendVerificationEmail_GenerateTokenFailure(t *testing.T) {
	sender := &NoOpSender{}
	vs := &VerificationService{
		sender: sender,
		store:  &failingTokenStore{err: fmt.Errorf("disk full")},
	}

	err := vs.SendVerificationEmail(context.Background(), "u1", "e@x.com", "https://ex.com")
	if err == nil {
		t.Error("expected error when token generation fails")
	}
}

func TestVerificationService_SendPasswordResetEmail_GenerateTokenFailure(t *testing.T) {
	sender := &NoOpSender{}
	vs := &VerificationService{
		sender: sender,
		store:  &failingTokenStore{err: fmt.Errorf("disk full")},
	}

	err := vs.SendPasswordResetEmail(context.Background(), "u1", "e@x.com", "https://ex.com")
	if err == nil {
		t.Error("expected error when token generation fails")
	}
}

func TestNewSMTPSender_AllFields(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{
		Host:     "smtp.test.com",
		Port:     465,
		Username: "user",
		Password: "pass",
		From:     "from@test.com",
		FromName: "From Name",
	})
	if s.config.Host != "smtp.test.com" {
		t.Errorf("expected smtp.test.com, got %s", s.config.Host)
	}
	if s.config.Port != 465 {
		t.Errorf("expected 465, got %d", s.config.Port)
	}
}

// --- Redis token store tests (requires live Redis on localhost:6379) ---

func redisAvailable() bool {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379", DialTimeout: 500 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := client.Ping(ctx).Err()
	client.Close()
	return err == nil
}

func TestNewVerificationServiceWithRedis(t *testing.T) {
	sender := &NoOpSender{}
	store := NewRedisTokenStore(redis.NewClient(&redis.Options{Addr: "localhost:6379"}), time.Hour)
	vs := NewVerificationServiceWithRedis(sender, store)
	if vs == nil {
		t.Fatal("expected non-nil VerificationService")
	}
	if vs.sender != sender {
		t.Error("expected sender to be set")
	}
}

func TestRedisTokenStore_StoreAndGet(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "user-redis-1",
		Email:     "redis@example.com",
		Token:     "redis-test-token",
		ExpiresAt: time.Now().Add(time.Hour),
		Purpose:   "verify",
	}

	if err := store.Store(ctx, vt); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	got, ok := store.Get(ctx, "redis-test-token")
	if !ok {
		t.Fatal("expected token to exist")
	}
	if got.UserID != "user-redis-1" {
		t.Errorf("expected user-redis-1, got %s", got.UserID)
	}
	if got.Email != "redis@example.com" {
		t.Errorf("expected redis@example.com, got %s", got.Email)
	}

	// cleanup
	client.Del(ctx, "email:token:redis-test-token")
}

func TestRedisTokenStore_GetNotFound(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	ctx := context.Background()

	_, ok := store.Get(ctx, "nonexistent-token")
	if ok {
		t.Error("expected false for nonexistent token")
	}
}

func TestRedisTokenStore_Delete(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "user-del",
		Email:     "del@example.com",
		Token:     "del-token",
		ExpiresAt: time.Now().Add(time.Hour),
		Purpose:   "verify",
	}

	store.Store(ctx, vt)

	if err := store.Delete(ctx, "del-token"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, ok := store.Get(ctx, "del-token")
	if ok {
		t.Error("expected token to be deleted")
	}
}

func TestRedisTokenStore_Cleanup(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		store.Cleanup(ctx, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Cleanup did not exit after context cancel")
	}
}

func TestRedisTokenStore_Key(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)

	got := store.key("abc123")
	expected := "email:token:abc123"
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestRedisTokenStore_GetExpired(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, 1*time.Second)
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "user-exp",
		Email:     "exp@example.com",
		Token:     "exp-token",
		ExpiresAt: time.Now().Add(2 * time.Second),
		Purpose:   "verify",
	}

	store.Store(ctx, vt)

	// Wait for TTL expiry
	time.Sleep(1500 * time.Millisecond)

	_, ok := store.Get(ctx, "exp-token")
	if ok {
		t.Error("expected expired token to not be returned")
	}
}

func TestRedisTokenStore_StoreAndRetrieveAllFields(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	ctx := context.Background()

	vt := &VerificationToken{
		UserID:    "u-full",
		Email:     "full@test.com",
		Token:     "full-token",
		ExpiresAt: time.Now().Add(time.Hour),
		Purpose:   "reset",
	}

	if err := store.Store(ctx, vt); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get(ctx, "full-token")
	if !ok {
		t.Fatal("expected token")
	}
	if got.Purpose != "reset" {
		t.Errorf("expected reset, got %s", got.Purpose)
	}

	client.Del(ctx, "email:token:full-token")
}

func TestRedisTokenStore_VerificationService_Integration(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	ctx := context.Background()
	store := NewRedisTokenStore(client, time.Minute)
	sender := &NoOpSender{}
	vs := NewVerificationServiceWithRedis(sender, store)

	token, err := vs.GenerateVerificationToken(context.Background(), "user-integ", "integ@test.com", "verify", 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateVerificationToken failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	vt, ok := vs.ValidateToken(context.Background(), token)
	if !ok {
		t.Fatal("expected valid token")
	}
	if vt.UserID != "user-integ" {
		t.Errorf("expected user-integ, got %s", vt.UserID)
	}

	vs.InvalidateToken(context.Background(), token)
	_, ok = vs.ValidateToken(context.Background(), token)
	if ok {
		t.Error("expected invalidated token")
	}

	client.Del(ctx, "email:token:"+token)
}

func TestRedisTokenStore_VerificationService_SendVerificationEmail(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	sender := &NoOpSender{}
	vs := NewVerificationServiceWithRedis(sender, store)

	err := vs.SendVerificationEmail(context.Background(), "u1", "e@x.com", "https://ex.com")
	if err != nil {
		t.Errorf("SendVerificationEmail failed: %v", err)
	}
}

func TestRedisTokenStore_VerificationService_SendPasswordResetEmail(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	sender := &NoOpSender{}
	vs := NewVerificationServiceWithRedis(sender, store)

	err := vs.SendPasswordResetEmail(context.Background(), "u1", "e@x.com", "https://ex.com")
	if err != nil {
		t.Errorf("SendPasswordResetEmail failed: %v", err)
	}
}

func TestRedisTokenStore_VerificationService_Cleanup(t *testing.T) {
	if !redisAvailable() {
		t.Skip("Redis not available on localhost:6379")
	}

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	store := NewRedisTokenStore(client, time.Minute)
	sender := &NoOpSender{}
	vs := NewVerificationServiceWithRedis(sender, store)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		vs.Cleanup(ctx, 10*time.Millisecond)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Cleanup did not exit after context cancel")
	}
}
