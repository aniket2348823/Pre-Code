package router

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
)

// Reproduces the live-server panic on POST /api-keys using the exact
// NewWithMiddleware wiring that server.New uses when CORS origins are set.
// Run with: go test -run 'TestReproPanicLive' -v ./internal/router/ -timeout 90s
func TestReproPanicLive(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:         "localhost",
		Port:         5432,
		User:         "vigilagent",
		Password:     "vigilagent_dev",
		Name:         "vigilagent",
		SSLMode:      "disable",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := database.NewPostgres(ctx, cfg)
	if err != nil {
		t.Fatalf("cannot connect to docker postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	appCfg := &config.Config{}
	appCfg.Auth.JWTSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	appCfg.Auth.JWTExpiration = time.Hour
	appCfg.Auth.APIKeyPrefix = "va_"
	appCfg.Server.Env = "development"
	appCfg.Database = *cfg
	appCfg.CORS.AllowedOrigins = []string{"http://localhost:5173"}

	jwtSvc := auth.NewJWT(&appCfg.Auth)
	apiKeySvc := auth.NewAPIKeyService("va_")
	conn := db.Conn()

	opts := Options{
		Config:     appCfg,
		DB:         db,
		JWT:        jwtSvc,
		APIKeys:    apiKeySvc,
		Users:      repository.NewUserRepository(conn),
		APIKeyRepo: repository.NewAPIKeyRepository(conn),
		Orgs:       repository.NewOrganizationRepository(conn),
		Projects:   repository.NewProjectRepository(conn),
		Agents:     repository.NewAgentRepository(conn),
		Sessions:   repository.NewSessionRepository(conn),
		Events:     repository.NewEventRepository(conn),
		Tasks:      repository.NewTaskRepository(conn),
		Skills:     repository.NewSkillRepository(conn),
		Alerts:     repository.NewAlertRepository(conn),
		Webhook:    webhook.NewEngine(db.Pool),
	}

	// Mirror server.go: NewWithMiddleware when CORS origins are configured.
	r := NewWithMiddleware(opts, &MiddlewareConfig{
		RequestID: true,
		Timeout:   0, // 0 disables TimeoutHandler so the raw panic stack surfaces
		CORS: &cors.Config{
			AllowOrigins: []string{"http://localhost:5173"},
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
			AllowHeaders: []string{"Content-Type", "Authorization"},
			MaxAge:       3600,
		},
	})

	// 1. Register a REAL user (valid UUID in the DB)
	regBody := bytes.NewBufferString(fmt.Sprintf(`{"email":"repro-%d@vigil.test","password":"Str0ngPass!2026","name":"Repro E2E"}`, time.Now().UnixNano()))
	regReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", regBody)
	regReq.Header.Set("Content-Type", "application/json")
	regRR := httptest.NewRecorder()
	r.ServeHTTP(regRR, regReq)
	t.Logf("register status=%d body=%s", regRR.Code, truncate(regRR.Body.String(), 200))
	if regRR.Code != http.StatusCreated && regRR.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", regRR.Code, regRR.Body.String())
	}
	var regResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(regRR.Body.Bytes(), &regResp)
	if regResp.Data.Token == "" {
		t.Fatal("no token from register")
	}

	// 2. POST /api-keys with the real user's JWT + valid CSRF token
	apiBody := bytes.NewBufferString(`{"name":"repro-key"}`)
	apiReq := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", apiBody)
	apiReq.Header.Set("Authorization", "Bearer "+regResp.Data.Token)
	apiReq.Header.Set("Content-Type", "application/json")
	apiReq.Header.Set("X-CSRF-Token", makeCSRFToken(t, r))
	apiRR := httptest.NewRecorder()
	r.ServeHTTP(apiRR, apiReq)
	t.Logf("api-keys status=%d body=%s", apiRR.Code, truncate(apiRR.Body.String(), 300))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// makeCSRFToken generates a valid CSRF token using the router's HMAC secret
// (the CSRF middleware signs with the JWT secret).
func makeCSRFToken(t *testing.T, r *Router) string {
	t.Helper()
	// NewWithMiddleware does not wire CSRF (matches the live server);
	// in that case no token is needed.
	if r.csrf == nil {
		return ""
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	tok := hex.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(r.cfg.Auth.JWTSecret))
	mac.Write([]byte(tok))
	return tok + "." + hex.EncodeToString(mac.Sum(nil))
}
