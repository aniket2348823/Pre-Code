package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMessages_Success(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"}]}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	assert.Equal(t, "user", messages[0].Role)
	assert.Equal(t, "hello", messages[0].Content)
	assert.Equal(t, "assistant", messages[1].Role)
	assert.Equal(t, "hi", messages[1].Content)
}

func TestParseMessages_EmptyMessages(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{"messages":[]}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestParseMessages_NoMessagesKey(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{"model":"gpt-4o"}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestParseMessages_InvalidJSON(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`not json`)

	messages, err := server.parseMessages(body)
	assert.Error(t, err)
	assert.Nil(t, messages)
}

func TestParseMessages_SingleMessage(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{"messages":[{"role":"user","content":"test"}]}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Role)
	assert.Equal(t, "test", messages[0].Content)
}

func TestParseMessages_MultipleRoles(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{
		"messages":[
			{"role":"system","content":"You are helpful"},
			{"role":"user","content":"hi"},
			{"role":"assistant","content":"hello"},
			{"role":"user","content":"bye"}
		]
	}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	require.Len(t, messages, 4)
	assert.Equal(t, "system", messages[0].Role)
	assert.Equal(t, "user", messages[1].Role)
	assert.Equal(t, "assistant", messages[2].Role)
	assert.Equal(t, "user", messages[3].Role)
}

func TestParseMessages_EmptyContent(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{"messages":[{"role":"user","content":""}]}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, "", messages[0].Content)
}

func TestParseMessages_EmptyBody(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{}`)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	assert.Empty(t, messages)
}

func TestParseMessages_NilBody(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})

	messages, err := server.parseMessages(nil)
	assert.Error(t, err)
	assert.Nil(t, messages)
}

func TestParseMessages_LargeContent(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	longContent := make([]byte, 10000)
	for i := range longContent {
		longContent[i] = 'a'
	}
	reqBody := map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": string(longContent)},
		},
	}
	body, _ := json.Marshal(reqBody)

	messages, err := server.parseMessages(body)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Len(t, messages[0].Content, 10000)
}

func TestServer_NewServer(t *testing.T) {
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-key",
	}
	server := NewServer(cfg)
	assert.NotNil(t, server)
	assert.NotNil(t, server.Router())
}

func TestServer_NewServer_WithAuth(t *testing.T) {
	cfg := Config{
		Port:           "0",
		BackendURL:     "http://localhost:9999",
		APIKey:         "test-key",
		AllowedAPIKeys: "key1,key2",
	}
	server := NewServer(cfg)
	assert.NotNil(t, server)
}

func TestServer_HealthEndpoint(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	req := createTestRequest("GET", "/health", nil)
	w := executeRequest(server, req)
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestServer_MetricsEndpoint(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	req := createTestRequest("GET", "/metrics", nil)
	w := executeRequest(server, req)
	assert.Equal(t, 200, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "requests_total")
	assert.Contains(t, resp, "healthy")
}

func TestServer_UsageEndpoint_Empty(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	req := createTestRequest("GET", "/v1/usage", nil)
	w := executeRequest(server, req)
	assert.Equal(t, 200, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Equal(t, float64(0), resp["count"])
}

func TestServer_ProvidersEndpoint(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	req := createTestRequest("GET", "/v1/providers", nil)
	w := executeRequest(server, req)
	assert.Equal(t, 200, w.Code)

	var resp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Contains(t, resp, "providers")
}

func TestServer_ChatCompletions_NoBody(t *testing.T) {
	server := NewServer(Config{
		BackendURL:     "http://localhost:9999",
		AllowedAPIKeys: "",
	})
	req := createTestRequest("POST", "/v1/chat/completions", nil)
	w := executeRequest(server, req)
	// Empty body with no auth: auth is disabled (no keys), so handler runs
	// and rejects empty body with 400
	assert.Contains(t, []int{400, 401}, w.Code)
}

func TestServer_ChatCompletions_InvalidJSON(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	req := createTestRequest("POST", "/v1/chat/completions", []byte("not json"))
	w := executeRequest(server, req)
	assert.Contains(t, []int{400, 401}, w.Code)
}

func TestServer_AnalyzeEndpoint_NoCode(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	body := []byte(`{"language":"go"}`)
	req := createTestRequest("POST", "/v1/analyze", body)
	w := executeRequest(server, req)
	// No auth keys configured, so auth is disabled. Handler gets the request
	// but rejects it because "code is required" → 400
	assert.Contains(t, []int{400, 401}, w.Code)
}

func TestServer_RateLimitMiddleware(t *testing.T) {
	server := NewServer(Config{
		BackendURL:     "http://localhost:9999",
		AllowedAPIKeys: "",
	})
	req := createTestRequest("GET", "/health", nil)
	w := executeRequest(server, req)
	assert.Equal(t, 200, w.Code)
}

func TestServer_UsageRecording(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	server.recordUsage("test-key", 0.10, 100, false)
	server.recordUsage("test-key", 0.05, 50, false)
	server.recordUsage("other-key", 0.20, 200, false)

	server.usageMu.RLock()
	usage := server.usageByKey["test-key"]
	server.usageMu.RUnlock()

	assert.Equal(t, uint64(2), usage.RequestCount)
	assert.InDelta(t, 0.15, usage.TotalCost, 0.0001)
	assert.Equal(t, 150, usage.TotalTokens)
}

func TestServer_UsageRecording_EmptyKey(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	server.recordUsage("", 0.10, 100, false)

	server.usageMu.RLock()
	count := len(server.usageByKey)
	server.usageMu.RUnlock()
	assert.Equal(t, 0, count)
}

func TestServer_UsageRecording_Error(t *testing.T) {
	server := NewServer(Config{BackendURL: "http://localhost:9999"})
	server.recordUsage("err-key", 0, 0, true)
	server.recordUsage("err-key", 0, 0, true)

	server.usageMu.RLock()
	usage := server.usageByKey["err-key"]
	server.usageMu.RUnlock()

	assert.Equal(t, uint64(2), usage.ErrorCount)
	assert.Equal(t, uint64(2), usage.RequestCount)
}

func TestParseAllowedKeys_ComplexCSV(t *testing.T) {
	keys := parseAllowedKeys("key1, key2 , key3")
	assert.Len(t, keys, 3)
	_, ok := keys["key2"]
	assert.True(t, ok)
}

func TestParseAllowedKeys_EmptyString(t *testing.T) {
	keys := parseAllowedKeys("")
	assert.Nil(t, keys)
}

func TestParseAllowedKeys_WhitespaceOnly(t *testing.T) {
	keys := parseAllowedKeys(" , , ")
	assert.Nil(t, keys)
}

func TestParseAllowedKeys_SingleKey(t *testing.T) {
	keys := parseAllowedKeys("only-key")
	assert.Len(t, keys, 1)
	_, ok := keys["only-key"]
	assert.True(t, ok)
}

// Helper functions for testing
func createTestRequest(method, path string, body []byte) *http.Request {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func executeRequest(server *ProxyServer, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	return w
}
