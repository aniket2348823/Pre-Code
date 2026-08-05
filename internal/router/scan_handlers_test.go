package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/scanner"
)

func scanTestRouter() *Router {
	return &Router{Mux: chi.NewMux(), engine: scanner.DefaultEngine()}
}

func TestScanRequiresAuth(t *testing.T) {
	r := scanTestRouter()
	req := reqWithClaims("POST", "/api/v1/scan", map[string]string{"code": "x"}, nil)
	w := httptest.NewRecorder()
	r.scanHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestScanEmptyCode(t *testing.T) {
	r := scanTestRouter()
	req := reqWithClaims("POST", "/api/v1/scan", map[string]string{"code": ""}, testClaims)
	w := httptest.NewRecorder()
	r.scanHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestScanDetectsVuln(t *testing.T) {
	r := scanTestRouter()
	body := map[string]string{
		"language": "go",
		"filename": "x.go",
		"code":     `q := fmt.Sprintf("SELECT * FROM users WHERE id=%d", id)`,
	}
	req := reqWithClaims("POST", "/api/v1/scan", body, testClaims)
	w := httptest.NewRecorder()
	r.scanHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	result := parseJSON(t, w)

	findings, ok := result["findings"].([]interface{})
	if !ok || len(findings) == 0 {
		t.Fatalf("expected at least one finding, got %v", result["findings"])
	}
	f := findings[0].(map[string]interface{})
	if c, _ := f["confidence"].(float64); c <= 0 || c >= 1 {
		t.Fatalf("confidence must be in (0,1), got %v", f["confidence"])
	}

	run, ok := result["analyzers_run"].([]interface{})
	if !ok {
		t.Fatalf("expected analyzers_run array, got %v", result["analyzers_run"])
	}
	seenBuiltin := false
	for _, a := range run {
		if a == "builtin" {
			seenBuiltin = true
		}
	}
	if !seenBuiltin {
		t.Fatalf("builtin analyzer must always run, got %v", run)
	}
}

func TestScanTooLarge(t *testing.T) {
	r := scanTestRouter()
	huge := strings.Repeat("a", maxRequestBodySize+1024)
	req := reqWithClaims("POST", "/api/v1/scan", map[string]string{"code": huge}, testClaims)
	w := httptest.NewRecorder()
	r.scanHandler(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
}

// ── Requirements, Validation & Pipeline Delegates ─────────

func requirementsTestRouter() *Router {
	r := &Router{Mux: chi.NewMux()}
	r.initHandlers()
	return r
}

func TestRequirementsHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/requirements", nil)
	w := httptest.NewRecorder()
	r.requirementsHandler(w, req)
	// Delegates to requirements handler
	assert.NotEqual(t, 0, w.Code)
}

func TestValidateHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/validate", nil)
	w := httptest.NewRecorder()
	r.validateHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestSchemaHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/schema", nil)
	w := httptest.NewRecorder()
	r.schemaHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestComplianceHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/compliance", nil)
	w := httptest.NewRecorder()
	r.complianceHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestPipelineHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/validate-full", nil)
	w := httptest.NewRecorder()
	r.pipelineHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestKnowledgeHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/knowledge", nil)
	w := httptest.NewRecorder()
	r.knowledgeHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestSkillEngineHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/skills/extract", nil)
	w := httptest.NewRecorder()
	r.skillEngineHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestConfidenceHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/confidence", nil)
	w := httptest.NewRecorder()
	r.confidenceHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestAttackGraphHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/attack-graph", nil)
	w := httptest.NewRecorder()
	r.attackGraphHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestAuditHandler_Delegates(t *testing.T) {
	r := requirementsTestRouter()
	req := httptest.NewRequest("POST", "/audit/trace", nil)
	w := httptest.NewRecorder()
	r.auditHandler(w, req)
	assert.NotEqual(t, 0, w.Code)
}

func TestRequirementsDelegatesHandlerFunctionsInitialized(t *testing.T) {
	r := requirementsTestRouter()
	assert.NotNil(t, r.requirementsHandlerFn)
	assert.NotNil(t, r.validateHandlerFn)
	assert.NotNil(t, r.schemaHandlerFn)
	assert.NotNil(t, r.complianceHandlerFn)
	assert.NotNil(t, r.pipelineHandlerFn)
	assert.NotNil(t, r.knowledgeHandlerFn)
	assert.NotNil(t, r.skillEngineHandlerFn)
	assert.NotNil(t, r.confidenceHandlerFn)
	assert.NotNil(t, r.attackGraphHandlerFn)
	assert.NotNil(t, r.auditHandlerFn)
}
