package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/signing"
)

// newTaskForDesignGate builds an llm.Task for design-gate tests.
func newTaskForDesignGate(messages []llm.Message) *llm.Task {
	return &llm.Task{ID: "test-task", Type: "feature", Messages: messages}
}

func TestComputeVerdict_NoFindings(t *testing.T) {
	o := ComputeVerdict(nil)
	assert.Equal(t, "pass", o.Verdict)
	assert.Equal(t, "A", o.Grade)
	assert.Equal(t, 100, o.Score)
	assert.Equal(t, 0, o.FindingsCount)
	assert.Equal(t, 0, o.Corroborated)
}

func TestComputeVerdict_CriticalBlocks(t *testing.T) {
	o := ComputeVerdict([]Finding{{RuleID: "llm-sqli-1", Severity: "critical", Confidence: 0.9}})
	assert.Equal(t, "block", o.Verdict)
}

func TestComputeVerdict_HighBlocks(t *testing.T) {
	o := ComputeVerdict([]Finding{{RuleID: "det-1", Severity: "high", Confidence: 0.8}})
	assert.Equal(t, "block", o.Verdict)
}

func TestComputeVerdict_MediumWarns(t *testing.T) {
	o := ComputeVerdict([]Finding{{RuleID: "det-1", Severity: "medium", Confidence: 0.8}})
	assert.Equal(t, "warn", o.Verdict)
}

func TestComputeVerdict_LowOnlyIsPass(t *testing.T) {
	o := ComputeVerdict([]Finding{{RuleID: "det-1", Severity: "low", Confidence: 0.8}})
	assert.Equal(t, "pass", o.Verdict)
}

func TestComputeVerdict_LowScoreWarns(t *testing.T) {
	findings := make([]Finding, 20)
	for i := range findings {
		findings[i] = Finding{RuleID: "det-low", Severity: "low", Confidence: 1.0}
	}
	o := ComputeVerdict(findings)
	// 20 lows at confidence 1.0 → score 100-(20*3)=40 < 70 → warn even without medium+.
	assert.Equal(t, "warn", o.Verdict)
	assert.Less(t, o.Score, 70)
}

func TestComputeVerdict_CorroboratedCount(t *testing.T) {
	findings := []Finding{
		{RuleID: "det-sqli+llm", Severity: "critical", Confidence: 0.9},
		{RuleID: "llm-xss", Severity: "high", Confidence: 0.7},
		{RuleID: "det-xss+llm", Severity: "medium", Confidence: 0.6},
	}
	o := ComputeVerdict(findings)
	assert.Equal(t, 2, o.Corroborated)
	assert.Equal(t, 3, o.FindingsCount)
	assert.Equal(t, "block", o.Verdict)
}

func TestApplyAnalysisHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	applyAnalysisHeaders(w, AnalysisOutcome{
		Verdict: "block", Grade: "C", Score: 55, FindingsCount: 3, Corroborated: 1,
	})
	assert.Equal(t, "block", w.Header().Get("X-VigilAgent-Verdict"))
	assert.Equal(t, "C", w.Header().Get("X-VigilAgent-Grade"))
	assert.Equal(t, "55", w.Header().Get("X-VigilAgent-Score"))
	assert.Equal(t, "3", w.Header().Get("X-VigilAgent-Findings"))
	assert.Equal(t, "1", w.Header().Get("X-VigilAgent-Corroborated"))
}

// ─── Policy Engine ────────────────────────────────────────────────────────

func TestComputePolicy_Matrix(t *testing.T) {
	block := AnalysisOutcome{Verdict: "block", Grade: "F", Score: 40}
	warn := AnalysisOutcome{Verdict: "warn", Grade: "C", Score: 65}
	pass := AnalysisOutcome{Verdict: "pass", Grade: "A", Score: 95}

	tests := []struct {
		name string
		o    AnalysisOutcome
		mode EnforcementMode
		want PolicyDecision
	}{
		{"observe-pass", pass, ModeObserve, PolicyAllow},
		{"observe-warn", warn, ModeObserve, PolicyAllowWithNotice},
		{"observe-block advisory", block, ModeObserve, PolicyHoldForReview},
		{"balanced-pass", pass, ModeBalanced, PolicyAllow},
		{"balanced-warn", warn, ModeBalanced, PolicyAllowWithNotice},
		{"balanced-block holds", block, ModeBalanced, PolicyHoldForReview},
		{"strict-pass", pass, ModeStrict, PolicyAllow},
		{"strict-warn requires ack", warn, ModeStrict, PolicyRequireAck},
		{"strict-block blocks", block, ModeStrict, PolicyBlock},
		{"passthrough always allows", block, ModePassthrough, PolicyAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ComputePolicy(tt.o, tt.mode))
		})
	}
}

func TestComputePolicy_ScannerUnavailableFailsClosed(t *testing.T) {
	o := AnalysisOutcome{Verdict: "pass", Grade: "A", Score: 100, ScannersUnavailable: true}
	assert.Equal(t, PolicyScannerUnavailable, ComputePolicy(o, ModeStrict))
	assert.Equal(t, PolicyAllow, ComputePolicy(o, ModeObserve), "advisory modes never fail closed")
	assert.Equal(t, PolicyAllow, ComputePolicy(o, ModeBalanced))
}

func TestEnforcePolicy(t *testing.T) {
	// strict + block → nothing released, HTTP 451
	release, status, reason := enforcePolicy(PolicyBlock, ModeStrict)
	assert.False(t, release)
	assert.Equal(t, http.StatusUnavailableForLegalReasons, status)
	assert.NotEmpty(t, reason)

	// scanner outage fails closed with HTTP 503 in strict mode
	release, status, reason = enforcePolicy(PolicyScannerUnavailable, ModeStrict)
	assert.False(t, release)
	assert.Equal(t, http.StatusServiceUnavailable, status)
	assert.NotEmpty(t, reason)

	// everything else releases
	for _, tc := range []struct {
		policy PolicyDecision
		mode   EnforcementMode
	}{
		{PolicyAllow, ModeStrict},
		{PolicyRequireAck, ModeStrict},
		{PolicyHoldForReview, ModeBalanced},
		{PolicyHoldForReview, ModeObserve},
		{PolicyBlock, ModeObserve}, // observe never blocks
	} {
		release, _, _ := enforcePolicy(tc.policy, tc.mode)
		assert.True(t, release, "expected release for %s in %s", tc.policy, tc.mode)
	}
}

func TestModelAllowed(t *testing.T) {
	s := &ProxyServer{cfg: Config{AllowedModels: "gpt-4o*,claude-3-5-sonnet-*"}}
	assert.True(t, s.modelAllowed("gpt-4o-mini"))
	assert.True(t, s.modelAllowed("gpt-4o"))
	assert.True(t, s.modelAllowed("claude-3-5-sonnet-20241022"))
	assert.False(t, s.modelAllowed("gemini-2.5-pro"))

	// empty allowlist = allow all
	s2 := &ProxyServer{cfg: Config{}}
	assert.True(t, s2.modelAllowed("anything"))
	assert.True(t, s2.modelAllowed(""))
}

// ─── Streaming Gate ───────────────────────────────────────────────────────

func TestStreamingGate_BalancedHoldsCode(t *testing.T) {
	g := newStreamingGate(ModeBalanced)
	live := g.push("Here is the fix:\n```go\n")
	assert.Contains(t, live, "Here is the fix:")
	assert.NotContains(t, live, "```")
	assert.Equal(t, "", g.push("rows, err := db.Query(ctx, sql)\n"), "code must be held")
	live = g.push("```\nApply it.")
	assert.NotContains(t, live, "db.Query")
	assert.Contains(t, live, "Apply it.")

	release, redacted := g.finish(PolicyHoldForReview, ModeBalanced)
	assert.True(t, redacted)
	assert.Contains(t, release, "code withheld")
}

func TestStreamingGate_BalancedReleasesClean(t *testing.T) {
	g := newStreamingGate(ModeBalanced)
	g.push("```go\nx := 1\n```")
	release, redacted := g.finish(PolicyAllow, ModeBalanced)
	assert.False(t, redacted)
	assert.Contains(t, release, "x := 1")
}

func TestStreamingGate_StrictHoldsAll(t *testing.T) {
	g := newStreamingGate(ModeStrict)
	assert.Equal(t, "", g.push("hello "))
	assert.Equal(t, "", g.push("world"))
	release, redacted := g.finish(PolicyAllow, ModeStrict)
	assert.False(t, redacted)
	assert.Equal(t, "hello world", release)

	g2 := newStreamingGate(ModeStrict)
	g2.push("secret code")
	release, redacted = g2.finish(PolicyBlock, ModeStrict)
	assert.True(t, redacted)
	assert.Equal(t, "", release)
}

func TestStreamingGate_ObservePassesThrough(t *testing.T) {
	g := newStreamingGate(ModeObserve)
	assert.Equal(t, "abc```def", g.push("abc```def"))
	release, redacted := g.finish(PolicyBlock, ModeObserve)
	assert.Equal(t, "", release)
	assert.False(t, redacted)
}

func TestStreamingGate_FenceSplitAcrossChunks(t *testing.T) {
	g := newStreamingGate(ModeBalanced)
	g.push("```go")
	g.push("\ncode here\n")
	live := g.push("```")
	assert.Equal(t, "", live)
	release, _ := g.finish(PolicyAllow, ModeBalanced)
	assert.Contains(t, release, "code here")
}

// ─── Metrics ──────────────────────────────────────────────────────────────

func TestMetricsDecisions(t *testing.T) {
	server, key := newProvenanceTestServer(t)
	server.recordDecision("block", PolicyBlock)
	server.recordDecision("warn", PolicyAllowWithNotice)
	server.recordDecision("block", PolicyBlock)

	req := httptest.NewRequest("GET", "/metrics", nil)
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Decisions struct {
			Count uint64            `json:"count"`
			By    map[string]uint64 `json:"by_verdict_policy"`
		} `json:"decisions"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, uint64(3), resp.Decisions.Count)
	assert.Equal(t, uint64(2), resp.Decisions.By["block:block"])
	assert.Equal(t, uint64(1), resp.Decisions.By["warn:allow_with_notice"])
}

func TestRedactCodeBlocks(t *testing.T) {
	content := "Here is the fix:\n```go\nrows, err := db.Query(ctx, sql)\n```\nApply it.\n```bash\nrm -rf /tmp\n```"
	redacted := redactCodeBlocks(content)
	assert.False(t, strings.Contains(redacted, "db.Query"), "code must be withheld")
	assert.False(t, strings.Contains(redacted, "rm -rf"), "code must be withheld")
	assert.True(t, strings.Contains(redacted, "Here is the fix"), "prose must pass through")
	assert.True(t, strings.Contains(redacted, "Apply it."), "prose must pass through")
}

// ─── Provenance / Audit Service ───────────────────────────────────────────

// newProvenanceTestServer returns a proxy with provenance endpoints + auth key.
func newProvenanceTestServer(t *testing.T) (*ProxyServer, string) {
	t.Helper()
	cfg := Config{
		Port:             "0",
		BackendURL:       "http://localhost:9999",
		APIKey:           "test-backend",
		AllowedAPIKeys:   "proxy-key",
		ProvenanceSecret: "test-provenance-secret",
	}
	return NewServer(cfg), "proxy-key"
}

func TestProvenanceAttestGetVerify(t *testing.T) {
	server, key := newProvenanceTestServer(t)

	// 1. Attest → signed record
	attestBody := `{"provider":"openai","model":"gpt-4o-mini","decision":"allow","response_hash":"abc123"}`
	req := httptest.NewRequest("POST", "/v1/provenance/attest", strings.NewReader(attestBody))
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Record    signing.ProvenanceRecord `json:"record"`
		Signature string                   `json:"signature"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Record.ScanID)
	assert.NotEmpty(t, resp.Signature)
	assert.Equal(t, signing.ProvenanceVerified, resp.Record.ProvenanceStatus)

	// 2. GET by scan id
	req = httptest.NewRequest("GET", "/v1/provenance?scan_id="+resp.Record.ScanID, nil)
	req.Header.Set("X-API-Key", key)
	w = httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// 3. Verify with stored signature
	verifyBody, _ := json.Marshal(map[string]string{"scan_id": resp.Record.ScanID, "signature": resp.Signature})
	req = httptest.NewRequest("POST", "/v1/provenance/verify", strings.NewReader(string(verifyBody)))
	req.Header.Set("X-API-Key", key)
	w = httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var verdict struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &verdict))
	assert.True(t, verdict.Valid, "signature must verify")

	// 4. Tampered signature → invalid
	verifyBody, _ = json.Marshal(map[string]string{"scan_id": resp.Record.ScanID, "signature": "tampered"})
	req = httptest.NewRequest("POST", "/v1/provenance/verify", strings.NewReader(string(verifyBody)))
	req.Header.Set("X-API-Key", key)
	w = httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &verdict))
	assert.False(t, verdict.Valid, "tampered signature must fail")
	assert.NotEmpty(t, verdict.Reason)

	// 5. Unknown scan id
	verifyBody, _ = json.Marshal(map[string]string{"scan_id": "scan_nope", "signature": "x"})
	req = httptest.NewRequest("POST", "/v1/provenance/verify", strings.NewReader(string(verifyBody)))
	req.Header.Set("X-API-Key", key)
	w = httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &verdict))
	assert.False(t, verdict.Valid)
	assert.Equal(t, "unknown scan_id", verdict.Reason)
}

func TestProvenanceVerifyFullRecord(t *testing.T) {
	server, key := newProvenanceTestServer(t)
	rec := signing.ProvenanceRecord{
		ScanID:           "scan_manual",
		Provider:         "anthropic",
		Model:            "claude-sonnet-4",
		ProvenanceStatus: signing.ProvenanceVerified,
		ResponseHash:     signing.HashContent("payload"),
		Decision:         "allow_with_notice",
		Timestamp:        time.Now().UTC(),
	}
	sig, err := signing.SignProvenance("test-provenance-secret", rec)
	assert.NoError(t, err)

	body, _ := json.Marshal(map[string]interface{}{"record": rec, "signature": sig})
	req := httptest.NewRequest("POST", "/v1/provenance/verify", strings.NewReader(string(body)))
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	var verdict struct {
		Valid bool `json:"valid"`
	}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &verdict))
	assert.True(t, verdict.Valid)
}

// ─── Design-Stage Gate ────────────────────────────────────────────────────

func TestApplyDesignGate_ConstrainsSecrets(t *testing.T) {
	s, _ := newProvenanceTestServer(t)
	task := newTaskForDesignGate([]llm.Message{{Role: "user", Content: "Design a config loader with password: \"hunter2secret\" hardcoded"}})
	findings, constrained := s.applyDesignGate(task)
	assert.True(t, constrained)
	assert.NotEmpty(t, findings)
	assert.Greater(t, len(task.Messages), 1, "a constraint system message must be appended")
	last := task.Messages[len(task.Messages)-1]
	assert.Equal(t, "system", last.Role)
	assert.True(t, strings.Contains(last.Content, "SECURE DESIGN CONSTRAINTS"))
}

func TestApplyDesignGate_CleanPasses(t *testing.T) {
	s, _ := newProvenanceTestServer(t)
	task := newTaskForDesignGate([]llm.Message{{Role: "user", Content: "Design a REST API for a todo list"}})
	findings, constrained := s.applyDesignGate(task)
	assert.False(t, constrained)
	assert.Empty(t, findings)
	assert.Len(t, task.Messages, 1, "no constraints appended for a clean prompt")
}

// ─── OpenAI Responses API ────────────────────────────────────────────────

func TestParseResponsesInput(t *testing.T) {
	// string form
	msgs, err := parseResponsesInput(json.RawMessage(`"hello"`))
	assert.NoError(t, err)
	assert.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)

	// array form
	msgs, err = parseResponsesInput(json.RawMessage(`[{"role":"user","content":"a"},{"role":"assistant","content":"b"}]`))
	assert.NoError(t, err)
	assert.Len(t, msgs, 2)

	// empty
	_, err = parseResponsesInput(json.RawMessage(`[]`))
	assert.Error(t, err)

	// malformed
	_, err = parseResponsesInput(json.RawMessage(`123`))
	assert.Error(t, err)
}

func TestResponsesEndpointRequiresInput(t *testing.T) {
	server, key := newProvenanceTestServer(t)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini"}`))
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestResponsesEndpointStreamingAttempts(t *testing.T) {
	// Streaming is supported; with no configured LLM provider the gateway must
	// fail with 502 (bad gateway), not 501 (unsupported) and not leak content.
	server, key := newProvenanceTestServer(t)
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","stream":true,"input":"hi"}`))
	req.Header.Set("X-API-Key", key)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusNotImplemented, w.Code)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}
