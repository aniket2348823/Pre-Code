package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/confidence"
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
