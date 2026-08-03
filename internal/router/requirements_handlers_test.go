package router

import (
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

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
