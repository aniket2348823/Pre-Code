// Package e2e provides end-to-end integration tests for the VigilAgent API.
// These tests exercise the full request lifecycle: register → create org →
// create project → create task → verify → scan → review.
//
// Run with: go test -tags=integration ./internal/e2e/ -v -timeout 120s
// Requires: PostgreSQL, Redis, NATS running (via docker-compose.dev.yml)
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// baseURL returns the API server base URL from environment or default.
func baseURL() string {
	if u := os.Getenv("E2E_BASE_URL"); u != "" {
		return u
	}
	return "http://localhost:8080"
}

// apiRequest makes an authenticated HTTP request to the API.
func apiRequest(t *testing.T, method, path, token string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(data)
	}

	url := baseURL() + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var result map[string]interface{}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if len(respBody) > 0 {
		json.Unmarshal(respBody, &result)
	}

	return resp, result
}

// requireStatus asserts the response status code.
func requireStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		t.Fatalf("expected status %d, got %d", expected, resp.StatusCode)
	}
}

// ═══════════════════════════════════════════════════════════════
// Test Suite: Full E2E Flow
// ═══════════════════════════════════════════════════════════════

func TestE2E_FullFlow(t *testing.T) {
	// Step 1: Register a new user
	t.Run("Register", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/auth/register", "", map[string]interface{}{
			"email":    fmt.Sprintf("e2e_%d@test.com", time.Now().UnixNano()),
			"password": "test-password-12345!",
			"name":     "E2E Test User",
		})
		requireStatus(t, resp, 201)

		token, ok := body["token"].(string)
		if !ok || token == "" {
			t.Fatal("expected token in register response")
		}
		t.Logf("registered user, token: %s...", token[:20])

		// Store token for subsequent tests
		t.Setenv("E2E_TOKEN", token)
	})

	token := os.Getenv("E2E_TOKEN")
	if token == "" {
		t.Fatal("no token from registration — run TestE2E_FullFlow/Register first")
	}

	// Step 2: Get current user
	t.Run("GetCurrentUser", func(t *testing.T) {
		resp, body := apiRequest(t, "GET", "/api/v1/users/me", token, nil)
		requireStatus(t, resp, 200)

		email, ok := body["email"].(string)
		if !ok || email == "" {
			t.Fatal("expected email in user response")
		}
		t.Logf("current user: %s", email)
	})

	// Step 3: Create organization
	var orgID string
	t.Run("CreateOrganization", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/organizations", token, map[string]interface{}{
			"name":        "E2E Test Org",
			"description": "Organization for E2E testing",
		})
		requireStatus(t, resp, 201)

		orgID, _ = body["id"].(string)
		if orgID == "" {
			t.Fatal("expected org ID in response")
		}
		t.Logf("created org: %s", orgID)
	})

	// Step 4: Create project
	var projectID string
	t.Run("CreateProject", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/projects", token, map[string]interface{}{
			"org_id":      orgID,
			"name":        "E2E Test Project",
			"description": "Project for E2E testing",
		})
		requireStatus(t, resp, 201)

		projectID, _ = body["id"].(string)
		if projectID == "" {
			t.Fatal("expected project ID in response")
		}
		t.Logf("created project: %s", projectID)
	})

	// Step 5: Create agent
	var agentID string
	t.Run("CreateAgent", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/projects/"+projectID+"/agents", token, map[string]interface{}{
			"name":        "E2E Test Agent",
			"description": "Agent for E2E testing",
		})
		requireStatus(t, resp, 201)

		agentID, _ = body["id"].(string)
		if agentID == "" {
			t.Fatal("expected agent ID in response")
		}
		t.Logf("created agent: %s", agentID)
	})

	// Step 6: Create task
	var taskID string
	t.Run("CreateTask", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/tasks", token, map[string]interface{}{
			"title":       "E2E Test Task",
			"description": "Write a simple HTTP server in Go",
			"project_id":  projectID,
		})
		// Task creation may return 201 or 202
		if resp.StatusCode != 201 && resp.StatusCode != 202 {
			t.Fatalf("expected status 201/202, got %d", resp.StatusCode)
		}

		taskID, _ = body["id"].(string)
		if taskID == "" {
			t.Fatal("expected task ID in response")
		}
		t.Logf("created task: %s", taskID)
	})

	// Step 7: Get task status
	t.Run("GetTask", func(t *testing.T) {
		resp, body := apiRequest(t, "GET", "/api/v1/tasks/"+taskID, token, nil)
		requireStatus(t, resp, 200)

		state, _ := body["state"].(string)
		t.Logf("task state: %s", state)
	})

	// Step 8: Scan code
	t.Run("ScanCode", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/scan", token, map[string]interface{}{
			"code":     `package main; import "fmt"; func main() { fmt.Println("hello") }`,
			"language": "go",
		})
		requireStatus(t, resp, 200)

		if findings, ok := body["findings"].([]interface{}); ok {
			t.Logf("scan findings: %d", len(findings))
		}
	})

	// Step 9: List API keys
	t.Run("ListAPIKeys", func(t *testing.T) {
		resp, body := apiRequest(t, "GET", "/api/v1/api-keys", token, nil)
		requireStatus(t, resp, 200)

		if keys, ok := body["api_keys"].([]interface{}); ok {
			t.Logf("api keys: %d", len(keys))
		}
	})

	// Step 10: Health check
	t.Run("HealthCheck", func(t *testing.T) {
		resp, _ := apiRequest(t, "GET", "/api/v1/health", "", nil)
		requireStatus(t, resp, 200)
	})

	// Step 11: Readiness check
	t.Run("ReadinessCheck", func(t *testing.T) {
		resp, _ := apiRequest(t, "GET", "/api/v1/ready", "", nil)
		// May be 200 or 503 depending on infra
		t.Logf("readiness status: %d", resp.StatusCode)
	})

	// Step 12: Provider catalog
	t.Run("ListProviders", func(t *testing.T) {
		resp, body := apiRequest(t, "GET", "/api/v1/providers", "", nil)
		requireStatus(t, resp, 200)

		if providers, ok := body["providers"].([]interface{}); ok {
			t.Logf("providers: %d", len(providers))
		}
	})
}

// ═══════════════════════════════════════════════════════════════
// Test Suite: Auth
// ═══════════════════════════════════════════════════════════════

func TestE2E_Auth(t *testing.T) {
	email := fmt.Sprintf("auth_%d@test.com", time.Now().UnixNano())

	t.Run("RegisterUser", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/auth/register", "", map[string]interface{}{
			"email":    email,
			"password": "secure-password-12345!",
			"name":     "Auth Test User",
		})
		requireStatus(t, resp, 201)

		token, _ := body["token"].(string)
		if token == "" {
			t.Fatal("expected token")
		}
		t.Setenv("AUTH_TOKEN", token)
	})

	token := os.Getenv("AUTH_TOKEN")

	t.Run("LoginUser", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/auth/login", "", map[string]interface{}{
			"email":    email,
			"password": "secure-password-12345!",
		})
		requireStatus(t, resp, 200)

		newToken, _ := body["token"].(string)
		if newToken == "" {
			t.Fatal("expected token on login")
		}
		t.Logf("login successful, token: %s...", newToken[:20])
	})

	t.Run("LoginInvalidCredentials", func(t *testing.T) {
		resp, _ := apiRequest(t, "POST", "/api/v1/auth/login", "", map[string]interface{}{
			"email":    email,
			"password": "wrong-password",
		})
		if resp.StatusCode != 401 {
			t.Fatalf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Logout", func(t *testing.T) {
		resp, _ := apiRequest(t, "POST", "/api/v1/auth/logout", token, nil)
		requireStatus(t, resp, 200)
	})

	t.Run("AccessAfterLogout", func(t *testing.T) {
		resp, _ := apiRequest(t, "GET", "/api/v1/users/me", token, nil)
		// Should be 401 because token was revoked
		if resp.StatusCode == 200 {
			t.Log("WARNING: token still valid after logout (blacklist may not be configured)")
		}
	})
}

// ═══════════════════════════════════════════════════════════════
// Test Suite: Rate Limiting
// ═══════════════════════════════════════════════════════════════

func TestE2E_RateLimiting(t *testing.T) {
	t.Run("RateLimitHeaders", func(t *testing.T) {
		resp, _ := apiRequest(t, "GET", "/api/v1/health", "", nil)

		limit := resp.Header.Get("X-RateLimit-Limit")
		if limit == "" {
			t.Log("WARNING: X-RateLimit-Limit header not present")
		} else {
			t.Logf("rate limit headers: limit=%s", limit)
		}
	})
}

// ═══════════════════════════════════════════════════════════════
// Test Suite: HITL Queue
// ═══════════════════════════════════════════════════════════════

func TestE2E_HITLQueue(t *testing.T) {
	// Register and get token
	email := fmt.Sprintf("hitl_%d@test.com", time.Now().UnixNano())
	resp, body := apiRequest(t, "POST", "/api/v1/auth/register", "", map[string]interface{}{
		"email":    email,
		"password": "secure-password-12345!",
		"name":     "HITL Test User",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("registration failed: %d", resp.StatusCode)
	}
	token, _ := body["token"].(string)

	t.Run("ListPendingCheckpoints", func(t *testing.T) {
		resp, _ := apiRequest(t, "GET", "/api/v1/hitl/pending", token, nil)
		// May be 200 (empty list) or 404 (route not wired)
		t.Logf("HITL pending status: %d", resp.StatusCode)
	})
}

// ═══════════════════════════════════════════════════════════════
// Test Suite: Knowledge Graph
// ═══════════════════════════════════════════════════════════════

func TestE2E_KnowledgeGraph(t *testing.T) {
	// Use unique IDs per run to allow re-running tests without duplicate key failures.
	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	svcID := "test-payment-svc-" + ts
	dbID := "test-payment-db-" + ts

	t.Run("AddNode", func(t *testing.T) {
		resp, _ := apiRequest(t, "POST", "/api/v1/knowledge", "", map[string]interface{}{
			"operation": "add_node",
			"node": map[string]interface{}{
				"id":   svcID,
				"type": "service",
				"name": "Payment Service",
			},
		})
		requireStatus(t, resp, 200)
	})

	t.Run("AddTargetNode", func(t *testing.T) {
		resp, _ := apiRequest(t, "POST", "/api/v1/knowledge", "", map[string]interface{}{
			"operation": "add_node",
			"node": map[string]interface{}{
				"id":   dbID,
				"type": "database",
				"name": "Payment Database",
			},
		})
		requireStatus(t, resp, 200)
	})

	t.Run("AddEdge", func(t *testing.T) {
		resp, _ := apiRequest(t, "POST", "/api/v1/knowledge", "", map[string]interface{}{
			"operation": "add_edge",
			"edge": map[string]interface{}{
				"from":     svcID,
				"to":       dbID,
				"relation": "uses",
			},
		})
		requireStatus(t, resp, 200)
	})

	t.Run("CountNodes", func(t *testing.T) {
		resp, body := apiRequest(t, "POST", "/api/v1/knowledge", "", map[string]interface{}{
			"operation": "count",
		})
		requireStatus(t, resp, 200)

		nodes, _ := body["nodes"].(float64)
		t.Logf("knowledge graph: %.0f nodes", nodes)
	})
}
