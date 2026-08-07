package pipeline

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/confidence"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/skillengine"
)

func TestInferLanguage(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		code   string
		want   string
	}{
		{"go from code", "", "package main\nfunc main() {}", "go"},
		{"go from fmt", "", `fmt.Println("hello")`, "go"},
		{"python from code", "", "def foo():\n  pass", "python"},
		{"python from prompt", "build a python app", "", "python"},
		{"javascript from code", "", "const x = 1", "javascript"},
		{"javascript from prompt", "build a node app", "", "javascript"},
		{"typescript from code", "", "const x: string = 'hi'", "typescript"},
		{"typescript from prompt", "build a next app", "", "typescript"},
		{"rust from code", "", "fn main() {}", "rust"},
		{"rust from prompt", "build with cargo", "", "rust"},
		{"java from code", "", "public class Main {}", "java"},
		{"java from prompt", "build a spring app", "", "java"},
		{"unknown default", "build something", "", "unknown"},
		{"go takes priority over prompt", "python app", "package main\nfunc main() {}", "go"},
		{"python from import and colon", "", "import os\nx: int = 1", "python"},
		{"rust from fn main only", "", "fn main() {}", "rust"},
		{"java from public static void", "", "public static void main(String[] args) {}", "java"},
		{"typescript from interface", "", "const x: number = 1\ninterface Foo {}", "typescript"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferLanguage(tt.prompt, tt.code)
			if got != tt.want {
				t.Errorf("inferLanguage(%q, %q) = %q, want %q", tt.prompt, tt.code, got, tt.want)
			}
		})
	}
}

func TestLanguageToFileExt(t *testing.T) {
	tests := []struct {
		lang string
		want string
	}{
		{"python", "py"},
		{"javascript", "js"},
		{"typescript", "ts"},
		{"rust", "rs"},
		{"java", "java"},
		{"go", "go"},
		{"unknown", "go"},
		{"", "go"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			got := languageToFileExt(tt.lang)
			if got != tt.want {
				t.Errorf("languageToFileExt(%q) = %q, want %q", tt.lang, got, tt.want)
			}
		})
	}
}

func TestInferEntityFromPrompt(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   string
	}{
		{"payment", "build a payment system", "payment"},
		{"billing", "create billing service", "payment"},
		{"auth", "implement auth flow", "auth"},
		{"login", "build login page", "auth"},
		{"api", "design REST API", "api"},
		{"database", "optimize database queries", "database"},
		{"db", "create db schema", "database"},
		{"default", "build a tool", "service"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferEntityFromPrompt(tt.prompt)
			if got != tt.want {
				t.Errorf("inferEntityFromPrompt(%q) = %q, want %q", tt.prompt, got, tt.want)
			}
		})
	}
}

func TestParseReviewerOutput_VerdictPass(t *testing.T) {
	content := "VERDICT: pass\nNo issues found.\n- Add unit tests\n- Implement error handling"
	verdict, findings, suggestions := parseReviewerOutput(content)
	if verdict != "pass" {
		t.Errorf("verdict = %q, want pass", verdict)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want empty", findings)
	}
	if len(suggestions) != 2 {
		t.Errorf("suggestions = %v, want 2", suggestions)
	}
}

func TestParseReviewerOutput_VerdictFail(t *testing.T) {
	content := "verdict: fail\n- Critical SQL injection found\n- Missing auth check"
	verdict, findings, _ := parseReviewerOutput(content)
	if verdict != "fail" {
		t.Errorf("verdict = %q, want fail", verdict)
	}
	if len(findings) != 2 {
		t.Errorf("findings = %v, want 2 findings", findings)
	}
}

func TestParseReviewerOutput_VerdictWarn(t *testing.T) {
	content := "VERDICT: warn\n- Medium severity issue"
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "warn" {
		t.Errorf("verdict = %q, want warn", verdict)
	}
}

func TestParseReviewerOutput_KeywordFallback_Fail(t *testing.T) {
	content := "Critical vulnerability found in authentication"
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "fail" {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

func TestParseReviewerOutput_KeywordFallback_Warn(t *testing.T) {
	content := "Warning: medium severity issue detected"
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "warn" {
		t.Errorf("verdict = %q, want warn", verdict)
	}
}

func TestParseReviewerOutput_KeywordFallback_Pass(t *testing.T) {
	content := "No critical issues found. Looks good overall."
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "pass" {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

func TestParseReviewerOutput_Suggestions(t *testing.T) {
	content := "- Add input validation\n- Implement rate limiting\n- Fix SQL injection"
	_, findings, suggestions := parseReviewerOutput(content)
	if len(suggestions) != 2 {
		t.Errorf("suggestions = %d, want 2 (Add, Implement)", len(suggestions))
	}
	if len(findings) != 1 {
		t.Errorf("findings = %d, want 1 (Fix SQL injection)", len(findings))
	}
}

func TestParseReviewerOutput_EmptyContent(t *testing.T) {
	verdict, findings, suggestions := parseReviewerOutput("")
	if verdict != "pass" {
		t.Errorf("verdict = %q, want pass", verdict)
	}
	if len(findings) != 0 {
		t.Errorf("findings = %v, want empty", findings)
	}
	if len(suggestions) != 0 {
		t.Errorf("suggestions = %v, want empty", suggestions)
	}
}

func TestParseReviewerOutput_BulletPrefixes(t *testing.T) {
	content := "• Issue one\n* Issue two\n- Issue three"
	_, findings, _ := parseReviewerOutput(content)
	if len(findings) != 3 {
		t.Errorf("findings = %d, want 3", len(findings))
	}
}

func TestParseReviewerOutput_VerdictReject(t *testing.T) {
	content := "verdict: reject\nSomething bad"
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "fail" {
		t.Errorf("verdict = %q, want fail (reject maps to fail)", verdict)
	}
}

func TestParseReviewerOutput_VerdictCaution(t *testing.T) {
	content := "verdict: caution\nSome warning"
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "warn" {
		t.Errorf("verdict = %q, want warn (caution maps to warn)", verdict)
	}
}

func TestBuildReviewerContext_NoFindings(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	ctx := szp.buildReviewerContext("main output", nil, "")
	if !strings.Contains(ctx, "MAIN LLM OUTPUT") {
		t.Error("missing MAIN LLM OUTPUT section")
	}
	if strings.Contains(ctx, "DETERMINISTIC ENGINE FINDINGS") {
		t.Error("should not contain findings section when empty")
	}
}

func TestBuildReviewerContext_WithFindings(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{Severity: scanner.SeverityCritical, Message: "critical issue", Line: 10, Snippet: "code", Fix: "fix it", Confidence: 0.95},
		{Severity: scanner.SeverityLow, Message: "low issue", Line: 20, Snippet: "other", Fix: "fix low", Confidence: 0.6},
	}
	ctx := szp.buildReviewerContext("main output", findings, "project ctx")
	if !strings.Contains(ctx, "DETERMINISTIC ENGINE FINDINGS") {
		t.Error("missing findings section")
	}
	if !strings.Contains(ctx, "CRITICAL") {
		t.Error("critical should appear in uppercase")
	}
	if !strings.Contains(ctx, "project ctx") {
		t.Error("missing project context")
	}
}

func TestBuildReviewerContext_MaxFindings(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := make([]scanner.Finding, 20)
	for i := range findings {
		findings[i] = scanner.Finding{
			Severity: scanner.SeverityMedium,
			Message:  "issue",
			Line:     i,
			Fix:      "fix",
		}
	}
	ctx := szp.buildReviewerContext("output", findings, "")
	if !strings.Contains(ctx, "truncated") {
		t.Error("expected truncation message for >10 findings")
	}
}

func TestBuildReviewerContext_NoProjectContext(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	ctx := szp.buildReviewerContext("output", nil, "")
	if strings.Contains(ctx, "PROJECT CONTEXT") {
		t.Error("should not contain project context section when empty")
	}
}

func TestAggregateEvidence_NoFindingsNoReviewers(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	evidence := szp.aggregateEvidence(nil, nil)
	if len(evidence) != 0 {
		t.Errorf("expected 0 evidence, got %d", len(evidence))
	}
}

func TestAggregateEvidence_CriticalFinding(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{Severity: scanner.SeverityCritical, Message: "critical"},
	}
	evidence := szp.aggregateEvidence(findings, nil)
	if len(evidence) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(evidence))
	}
	if evidence[0].Verdict != "fail" {
		t.Errorf("verdict = %q, want fail", evidence[0].Verdict)
	}
}

func TestAggregateEvidence_HighFinding(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{Severity: scanner.SeverityHigh, Message: "high"},
	}
	evidence := szp.aggregateEvidence(findings, nil)
	if evidence[0].Verdict != "fail" {
		t.Errorf("verdict = %q, want fail", evidence[0].Verdict)
	}
}

func TestAggregateEvidence_MediumFinding(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{Severity: scanner.SeverityMedium, Message: "medium"},
	}
	evidence := szp.aggregateEvidence(findings, nil)
	if evidence[0].Verdict != "warn" {
		t.Errorf("verdict = %q, want warn", evidence[0].Verdict)
	}
}

func TestAggregateEvidence_LowFinding(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{Severity: scanner.SeverityLow, Message: "low"},
	}
	evidence := szp.aggregateEvidence(findings, nil)
	if evidence[0].Verdict != "pass" {
		t.Errorf("verdict = %q, want pass", evidence[0].Verdict)
	}
}

func TestAggregateEvidence_InfoFinding(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{Severity: scanner.SeverityInfo, Message: "info"},
	}
	evidence := szp.aggregateEvidence(findings, nil)
	if evidence[0].Verdict != "pass" {
		t.Errorf("verdict = %q, want pass", evidence[0].Verdict)
	}
}

func TestAggregateEvidence_ReviewerWithWeight(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	reviewers := []ReviewerOutput{
		{Name: "security", Role: "Security Architect", Verdict: "fail", Findings: []string{"issue"}},
	}
	evidence := szp.aggregateEvidence(nil, reviewers)
	if len(evidence) != 1 {
		t.Fatalf("expected 1 evidence, got %d", len(evidence))
	}
	if evidence[0].Weight != 0.85 {
		t.Errorf("weight = %f, want 0.85 for security reviewer", evidence[0].Weight)
	}
}

func TestAggregateEvidence_ReviewerUnknownWeight(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	reviewers := []ReviewerOutput{
		{Name: "unknown_reviewer", Role: "Unknown", Verdict: "pass", Findings: []string{}},
	}
	evidence := szp.aggregateEvidence(nil, reviewers)
	if evidence[0].Weight != 0.6 {
		t.Errorf("weight = %f, want 0.6 default", evidence[0].Weight)
	}
}

func TestAggregateEvidence_AllReviewerWeights(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	expectedWeights := map[string]float64{
		"security":     0.85,
		"architecture": 0.70,
		"compliance":   0.75,
		"cost":         0.50,
		"red_team":     0.90,
	}
	for name, expected := range expectedWeights {
		reviewers := []ReviewerOutput{
			{Name: name, Role: name, Verdict: "pass", Findings: []string{}},
		}
		evidence := szp.aggregateEvidence(nil, reviewers)
		if evidence[0].Weight != expected {
			t.Errorf("%s weight = %f, want %f", name, evidence[0].Weight, expected)
		}
	}
}

func TestBuildSummary_EmptyReport(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	report := &ReviewReport{
		Duration: 100 * time.Millisecond,
	}
	summary := szp.buildSummary(report)
	if !strings.Contains(summary, "100ms") {
		t.Errorf("summary missing duration: %s", summary)
	}
	if !strings.Contains(summary, "0 findings") {
		t.Errorf("summary missing findings count: %s", summary)
	}
}

func TestBuildSummary_WithConfidence(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	report := &ReviewReport{
		Duration: 200 * time.Millisecond,
		Confidence: &confidence.Score{
			Grade:      "A+",
			Confidence: 0.98,
			Reason:     "all clear",
		},
	}
	summary := szp.buildSummary(report)
	if !strings.Contains(summary, "A+") {
		t.Errorf("summary missing grade: %s", summary)
	}
	if !strings.Contains(summary, "98%") {
		t.Errorf("summary missing confidence percentage: %s", summary)
	}
}

func TestBuildSummary_WithReviewers(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	report := &ReviewReport{
		Duration: 100 * time.Millisecond,
		Reviewers: []ReviewerOutput{
			{Verdict: "pass"},
			{Verdict: "fail"},
			{Verdict: "warn"},
		},
	}
	summary := szp.buildSummary(report)
	if !strings.Contains(summary, "1 passed") {
		t.Errorf("summary missing pass count: %s", summary)
	}
	if !strings.Contains(summary, "1 failed") {
		t.Errorf("summary missing fail count: %s", summary)
	}
	if !strings.Contains(summary, "1 warnings") {
		t.Errorf("summary missing warn count: %s", summary)
	}
}

func TestBuildSummary_WithFindings(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	report := &ReviewReport{
		Duration: 100 * time.Millisecond,
		DeterministicFindings: []scanner.Finding{
			{Severity: scanner.SeverityCritical},
			{Severity: scanner.SeverityHigh},
			{Severity: scanner.SeverityLow},
		},
	}
	summary := szp.buildSummary(report)
	if !strings.Contains(summary, "3 findings") {
		t.Errorf("summary missing total findings: %s", summary)
	}
	if !strings.Contains(summary, "1 critical") {
		t.Errorf("summary missing critical count: %s", summary)
	}
	if !strings.Contains(summary, "1 high") {
		t.Errorf("summary missing high count: %s", summary)
	}
}

func TestBuildSummary_WithRetries(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	report := &ReviewReport{
		Duration: 100 * time.Millisecond,
		Retries:  2,
	}
	summary := szp.buildSummary(report)
	if !strings.Contains(summary, "2") {
		t.Errorf("summary missing retry count: %s", summary)
	}
}

func TestBuildSummary_WithSkills(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	report := &ReviewReport{
		Duration: 100 * time.Millisecond,
		Skills:   make([]*skillengine.Skill, 3),
	}
	summary := szp.buildSummary(report)
	if !strings.Contains(summary, "3") {
		t.Errorf("summary missing skill count: %s", summary)
	}
}

func TestReviewerWeightMap_AllExpectedKeys(t *testing.T) {
	expectedKeys := []string{"security", "architecture", "compliance", "cost", "red_team"}
	for _, key := range expectedKeys {
		if _, ok := reviewerWeightMap[key]; !ok {
			t.Errorf("missing key %q in reviewerWeightMap", key)
		}
	}
}

func TestParseReviewerOutput_VerdictWithContext(t *testing.T) {
	content := "Review Results:\n\nverdict: pass\n\nAll checks passed.\n- Ensure tests are added\n- Use table-driven tests"
	verdict, _, suggestions := parseReviewerOutput(content)
	if verdict != "pass" {
		t.Errorf("verdict = %q, want pass", verdict)
	}
	if len(suggestions) != 2 {
		t.Errorf("suggestions = %d, want 2", len(suggestions))
	}
}

func TestParseReviewerOutput_NoNegativeSignals(t *testing.T) {
	content := "Critical issue: SQL injection vulnerability found in user query handler"
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "fail" {
		t.Errorf("verdict = %q, want fail", verdict)
	}
}

func TestParseReviewerOutput_NegativeSignals(t *testing.T) {
	content := "No critical issues found. No high severity issues. The code looks good."
	verdict, _, _ := parseReviewerOutput(content)
	if verdict != "pass" {
		t.Errorf("verdict = %q, want pass", verdict)
	}
}

// ─── Single-call 5-role reviewers ─────────────────────────────────────────

// mockReviewProvider returns fixed content and counts how many times Chat was called.
type mockReviewProvider struct {
	content   string
	callCount int32
}

func (m *mockReviewProvider) Chat(_ context.Context, _ *llm.ChatRequest) (*llm.ChatResponse, error) {
	atomic.AddInt32(&m.callCount, 1)
	return &llm.ChatResponse{Content: m.content, Model: "mock-model", Cost: 0.001}, nil
}

func (m *mockReviewProvider) Stream(_ context.Context, _ *llm.ChatRequest) (<-chan *llm.ChatChunk, error) {
	ch := make(chan *llm.ChatChunk, 1)
	ch <- &llm.ChatChunk{Content: m.content, Finish: true}
	close(ch)
	return ch, nil
}

func (m *mockReviewProvider) HealthCheck(_ context.Context) error { return nil }

func (m *mockReviewProvider) Name() string { return "mock" }

func mockCalls(m *mockReviewProvider) int { return int(atomic.LoadInt32(&m.callCount)) }

// newHealthyRouter builds a ModelRouter with the mock provider marked healthy.
func newHealthyRouter(t *testing.T, mock *mockReviewProvider) *llm.ModelRouter {
	t.Helper()
	router := llm.NewModelRouter(&llm.RouterConfig{DefaultModel: "gpt-4o-mini"})
	router.RegisterProvider("openai", mock)
	router.GetHealthMonitor().RecordSuccess("openai", time.Millisecond)
	return router
}

// fiveRoleContract is the strict JSON contract the single-call reviewer must
// return — one entry per role, with line-anchored findings.
const fiveRoleContract = `{"roles": [
  {"role": "security", "verdict": "fail", "findings": [
    {"severity": "critical", "line_start": 3, "line_end": 3, "message": "SQL injection", "replacement": "db.Query(ctx, sql, id)", "confidence": 0.9}
  ]},
  {"role": "architecture", "verdict": "pass", "findings": []},
  {"role": "compliance", "verdict": "warn", "findings": [
    {"severity": "medium", "line_start": 8, "line_end": 8, "message": "Missing audit log", "replacement": "", "confidence": 0.6}
  ]},
  {"role": "cost", "verdict": "pass", "findings": []},
  {"role": "red_team", "verdict": "fail", "findings": [
    {"severity": "high", "line_start": 12, "line_end": 13, "message": "Privilege escalation", "replacement": "if user.Role == admin", "confidence": 0.8}
  ]}
]}`

func TestParseSingleCallReviewContent(t *testing.T) {
	outputs, suggestions, ok := parseSingleCallReviewContent(fiveRoleContract)
	if !ok {
		t.Fatal("expected parse success")
	}
	if len(outputs) != 5 {
		t.Fatalf("expected 5 role outputs, got %d", len(outputs))
	}
	if outputs[0].Name != "security" || outputs[0].Verdict != "fail" {
		t.Errorf("security output mismatch: %+v", outputs[0])
	}
	if outputs[2].Name != "compliance" || outputs[2].Verdict != "warn" {
		t.Errorf("compliance output mismatch: %+v", outputs[2])
	}
	if len(suggestions) != 3 {
		t.Fatalf("expected 3 suggestions, got %d", len(suggestions))
	}
	var rt *Suggestion
	for i := range suggestions {
		if suggestions[i].Role == "red_team" {
			rt = &suggestions[i]
		}
	}
	if rt == nil {
		t.Fatal("missing red_team suggestion")
	}
	if rt.LineStart != 12 || rt.LineEnd != 13 || rt.Replacement != "if user.Role == admin" {
		t.Errorf("red_team suggestion mismatch: %+v", *rt)
	}
}

func TestParseSingleCallReviewContent_Malformed(t *testing.T) {
	for _, content := range []string{"", "garbage", "no json here", `{"roles": []}`, `[1,2,3]`} {
		if _, _, ok := parseSingleCallReviewContent(content); ok {
			t.Errorf("expected parse failure for %q", content)
		}
	}
}

func TestParseSingleCallReviewContent_RoleNormalization(t *testing.T) {
	content := `{"roles": [
	  {"role": "Principal Security Architect", "verdict": "fail", "findings": [{"severity": "high", "line_start": 1, "line_end": 1, "message": "x"}]},
	  {"role": "Red Team", "verdict": "warn", "findings": []},
	  {"role": "hr", "verdict": "fail", "findings": []}
	]}`
	outputs, suggestions, ok := parseSingleCallReviewContent(content)
	if !ok {
		t.Fatal("expected parse success")
	}
	if len(outputs) != 2 {
		t.Fatalf("expected 2 recognized roles (hr dropped), got %d", len(outputs))
	}
	if outputs[0].Name != "security" || outputs[1].Name != "red_team" {
		t.Errorf("role normalization failed: %+v", outputs)
	}
	if len(suggestions) != 1 || suggestions[0].Role != "security" {
		t.Errorf("suggestion role attribution failed: %+v", suggestions)
	}
}

func TestRunSingleCallReviewers_SingleCall(t *testing.T) {
	mock := &mockReviewProvider{content: fiveRoleContract}
	router := newHealthyRouter(t, mock)
	szp := &ShiftZeroPipeline{llmRouter: router}

	outputs, suggestions := szp.runSingleCallReviewers(context.Background(), "reviewer context", "original prompt")

	if mockCalls(mock) != 1 {
		t.Errorf("expected exactly 1 LLM call for all 5 roles, got %d", mockCalls(mock))
	}
	if len(outputs) != 5 {
		t.Fatalf("expected 5 reviewer outputs, got %d", len(outputs))
	}
	if len(suggestions) != 3 {
		t.Errorf("expected 3 suggestions, got %d", len(suggestions))
	}
	// Every suggestion must be attributed to its owning reviewer output.
	total := 0
	for _, o := range outputs {
		total += len(o.LineSuggestions)
	}
	if total != 3 {
		t.Errorf("expected 3 line suggestions attached to reviewer outputs, got %d", total)
	}
}

func TestRunSingleCallReviewers_FallbackOnMalformed(t *testing.T) {
	mock := &mockReviewProvider{content: "the model ignored the JSON contract"}
	router := newHealthyRouter(t, mock)
	szp := &ShiftZeroPipeline{llmRouter: router}

	outputs, _ := szp.runSingleCallReviewers(context.Background(), "ctx", "prompt")
	if len(outputs) != 5 {
		t.Fatalf("expected fallback to 5 parallel reviewers, got %d", len(outputs))
	}
}

func TestRunSingleCallReviewers_NilRouter(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	outputs, _ := szp.runSingleCallReviewers(context.Background(), "ctx", "prompt")
	if len(outputs) != 5 {
		t.Fatalf("expected 5 error-verdict reviewers with nil router, got %d", len(outputs))
	}
	for _, o := range outputs {
		if o.Verdict != "error" {
			t.Errorf("expected error verdict with nil router, got %q", o.Verdict)
		}
	}
}

// ─── Suggestions ──────────────────────────────────────────────────────────

func TestBuildSuggestions_DeterministicOnly(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{RuleID: "secrets-001", Severity: scanner.SeverityCritical, Message: "hardcoded secret", Line: 4, Fix: "use env var", Confidence: 0.95},
		{RuleID: "secrets-002", Severity: scanner.SeverityHigh, Message: "no line number", Line: 0, Fix: "n/a"},
	}
	suggestions := szp.buildSuggestions(findings, nil)
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion (line-less finding skipped), got %d", len(suggestions))
	}
	s := suggestions[0]
	if s.Role != "deterministic" || s.LineStart != 4 || s.LineEnd != 4 || s.Replacement != "use env var" {
		t.Errorf("deterministic suggestion mismatch: %+v", s)
	}
}

func TestBuildSuggestions_Corroboration(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	findings := []scanner.Finding{
		{RuleID: "sqli-001", Severity: scanner.SeverityCritical, Message: "SQL injection", Line: 5, Confidence: 0.9},
	}
	reviewers := []ReviewerOutput{
		{
			Name: "security",
			LineSuggestions: []Suggestion{
				{ID: "security:5:5", Role: "security", Severity: "critical", LineStart: 5, LineEnd: 5, Message: "SQL injection", Replacement: "param query", Confidence: 0.7},
			},
		},
	}
	suggestions := szp.buildSuggestions(findings, reviewers)
	if len(suggestions) != 2 {
		t.Fatalf("expected 2 suggestions (1 deterministic + 1 LLM), got %d", len(suggestions))
	}
	var llm *Suggestion
	for i := range suggestions {
		if suggestions[i].Role == "security" {
			llm = &suggestions[i]
		}
	}
	if llm == nil {
		t.Fatal("missing LLM suggestion")
	}
	if !llm.Corroborated {
		t.Error("expected corroborated=true when deterministic engine flagged line 5")
	}
	if llm.Confidence != 0.85 {
		t.Errorf("expected corroboration boost to 0.85, got %v", llm.Confidence)
	}
}

func TestBuildSuggestions_Deduplicates(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	reviewers := []ReviewerOutput{
		{Name: "security", LineSuggestions: []Suggestion{
			{ID: "security:3:3", Role: "security", LineStart: 3, LineEnd: 3, Message: "a"},
			{ID: "security:3:3", Role: "security", LineStart: 3, LineEnd: 3, Message: "b"},
		}},
	}
	suggestions := szp.buildSuggestions(nil, reviewers)
	if len(suggestions) != 1 {
		t.Errorf("expected dedup to 1 suggestion, got %d", len(suggestions))
	}
}

// TestRunSuggestionMode_ReturnsSuggestions runs the full pipeline in suggestion
// mode with failing reviewer verdicts and asserts: suggestions are returned and
// the auto-rewrite re-validation loop is skipped (Retries == 0).
func TestRunSuggestionMode_ReturnsSuggestions(t *testing.T) {
	mock := &mockReviewProvider{content: fiveRoleContract}
	router := newHealthyRouter(t, mock)
	szp := NewShiftZeroPipeline(router, nil, nil, nil, nil, nil, nil)
	szp.SuggestionMode = true

	report, err := szp.Run(context.Background(), &ReviewRequest{
		Code:     "package main\n\nfunc main() {}\n",
		Language: "go",
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(report.Suggestions) == 0 {
		t.Error("expected line-anchored suggestions in report")
	}
	if report.Retries != 0 {
		t.Errorf("suggestion mode must skip auto-rewrite loop, got %d retries", report.Retries)
	}
	if report.FinalOutput != report.MainLLMResponse {
		t.Error("suggestion mode must not rewrite the main response")
	}
	// Failing reviewer verdicts must still surface.
	hasFail := false
	for _, r := range report.Reviewers {
		if r.Verdict == "fail" {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected at least one failing reviewer verdict from the contract")
	}
}

func TestNewShiftZeroPipeline_NilComponents(t *testing.T) {
	p := NewShiftZeroPipeline(nil, nil, nil, nil, nil, nil, nil)
	if p == nil {
		t.Fatal("expected non-nil pipeline")
	}
	if p.engine == nil {
		t.Error("engine should be initialized")
	}
	if p.knowledge == nil {
		t.Error("knowledge should be initialized")
	}
	if p.skills == nil {
		t.Error("skills should be initialized")
	}
	if p.attackGraph == nil {
		t.Error("attackGraph should be initialized")
	}
	if p.confidence == nil {
		t.Error("confidence should be initialized")
	}
}

func TestRunMainLLM_NilRouter(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	_, err := szp.runMainLLM(nil, &ReviewRequest{Prompt: "test"})
	if err == nil {
		t.Error("expected error with nil router")
	}
}

func TestRunSingleReviewer_NilRouter(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	def := reviewerDef{name: "security", role: "Security Architect", instruction: "test"}
	output := szp.runSingleReviewer(nil, "code context", "prompt", def)
	if output.Verdict != "error" {
		t.Errorf("verdict = %q, want error", output.Verdict)
	}
	if len(output.Findings) != 1 || output.Findings[0] != "LLM router not configured" {
		t.Errorf("findings = %v, want [LLM router not configured]", output.Findings)
	}
}

func TestRunSecurityFix_NilRouter(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	_, err := szp.runSecurityFix(nil, "code", nil, nil)
	if err == nil {
		t.Error("expected error with nil router")
	}
}

func TestExtractSkills_EmptyFindings(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	skills := szp.extractSkills(nil, nil)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestValidateKnowledgeGraph_EmptyPrompt(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "", "")
	// Should return empty since "payment"/"auth"/etc not in prompt
	if result != "" {
		t.Errorf("expected empty result for generic prompt, got %q", result)
	}
}

func TestValidateKnowledgeGraph_PaymentRequiresControls(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "build a payment system", "code here")
	if !strings.Contains(result, "audit_log") {
		t.Errorf("expected audit_log warning for payment, got %q", result)
	}
	if !strings.Contains(result, "encryption") {
		t.Errorf("expected encryption warning for payment, got %q", result)
	}
}

func TestValidateKnowledgeGraph_AuthRequiresControls(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "build an auth system", "code here")
	if !strings.Contains(result, "session_management") {
		t.Errorf("expected session_management warning, got %q", result)
	}
}

func TestValidateKnowledgeGraph_AdminsRequiresControls(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "build admin panel", "code here")
	if !strings.Contains(result, "access_control") {
		t.Errorf("expected access_control warning, got %q", result)
	}
}

func TestValidateKnowledgeGraph_DatabaseRequiresControls(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "setup database", "code here")
	if !strings.Contains(result, "encryption_at_rest") {
		t.Errorf("expected encryption_at_rest warning, got %q", result)
	}
}

func TestValidateKnowledgeGraph_ApiRequiresControls(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "build an api", "code here")
	if !strings.Contains(result, "rate_limiting") {
		t.Errorf("expected rate_limiting warning, got %q", result)
	}
}

func TestValidateKnowledgeGraph_AllControlsDeclared(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "build payment system", "audit log, encryption, rate limiting, fraud detection implemented")
	if result != "" {
		t.Errorf("expected empty when all controls declared, got %q", result)
	}
}

func TestValidateKnowledgeGraph_AllControlsDeclaredWithDash(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	result := szp.validateKnowledgeGraph(nil, "build payment system", "audit-log, encryption, rate-limiting, fraud-detection implemented")
	if result != "" {
		t.Errorf("expected empty when all controls declared with dashes, got %q", result)
	}
}

func TestRunReviewersInParallel_NilRouter(t *testing.T) {
	szp := &ShiftZeroPipeline{}
	outputs := szp.runReviewersInParallel(nil, "context", "prompt")
	// Should return 5 reviewer outputs (one per reviewer def), all with error verdict
	if len(outputs) != 5 {
		t.Errorf("expected 5 outputs, got %d", len(outputs))
	}
	for _, o := range outputs {
		if o.Verdict != "error" {
			t.Errorf("reviewer %s verdict = %q, want error", o.Name, o.Verdict)
		}
	}
}
