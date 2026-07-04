package router

import "net/http"

// requirementsHandler delegates to the pre-built handler stored in the router.
func (r *Router) requirementsHandler(w http.ResponseWriter, req *http.Request) {
	r.requirementsHandlerFn.ServeHTTP(w, req)
}

// validateHandler delegates to the pre-built handler stored in the router.
func (r *Router) validateHandler(w http.ResponseWriter, req *http.Request) {
	r.validateHandlerFn.ServeHTTP(w, req)
}

// schemaHandler delegates to the pre-built schema validation handler.
func (r *Router) schemaHandler(w http.ResponseWriter, req *http.Request) {
	r.schemaHandlerFn.ServeHTTP(w, req)
}

// complianceHandler delegates to the pre-built compliance handler.
func (r *Router) complianceHandler(w http.ResponseWriter, req *http.Request) {
	r.complianceHandlerFn.ServeHTTP(w, req)
}

// pipelineHandler delegates to the pre-built unified validation pipeline handler.
func (r *Router) pipelineHandler(w http.ResponseWriter, req *http.Request) {
	r.pipelineHandlerFn.ServeHTTP(w, req)
}

// knowledgeHandler delegates to the pre-built knowledge graph handler.
func (r *Router) knowledgeHandler(w http.ResponseWriter, req *http.Request) {
	r.knowledgeHandlerFn.ServeHTTP(w, req)
}

// skillEngineHandler delegates to the pre-built skill extraction handler.
func (r *Router) skillEngineHandler(w http.ResponseWriter, req *http.Request) {
	r.skillEngineHandlerFn.ServeHTTP(w, req)
}

// confidenceHandler delegates to the pre-built confidence engine handler.
func (r *Router) confidenceHandler(w http.ResponseWriter, req *http.Request) {
	r.confidenceHandlerFn.ServeHTTP(w, req)
}

// attackGraphHandler delegates to the pre-built attack graph handler.
func (r *Router) attackGraphHandler(w http.ResponseWriter, req *http.Request) {
	r.attackGraphHandlerFn.ServeHTTP(w, req)
}

// auditHandler delegates to the pre-built audit trail handler.
func (r *Router) auditHandler(w http.ResponseWriter, req *http.Request) {
	r.auditHandlerFn.ServeHTTP(w, req)
}
