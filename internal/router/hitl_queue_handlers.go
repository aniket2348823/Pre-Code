package router

import (
	"net/http"

	mw "github.com/vigilagent/vigilagent/internal/middleware"
)


// listHITLCheckpointsHandler returns all pending HITL checkpoints for the authenticated user.
func (r *Router) listHITLCheckpointsHandler(w http.ResponseWriter, req *http.Request) {
	if r.hitlHandler == nil {
		r.hitlHandler = mw.NewHITLHandler(r.hitlQueue)
	}
	r.hitlHandler.ListPendingHandler(w, req)
}

// decideHITLHandler processes a human decision on a checkpoint.
func (r *Router) decideHITLHandler(w http.ResponseWriter, req *http.Request) {
	if r.hitlHandler == nil {
		r.hitlHandler = mw.NewHITLHandler(r.hitlQueue)
	}
	r.hitlHandler.DecideHandler(w, req)
}

// hitlStatusHandler returns the status of a specific checkpoint.
func (r *Router) hitlStatusHandler(w http.ResponseWriter, req *http.Request) {
	if r.hitlHandler == nil {
		r.hitlHandler = mw.NewHITLHandler(r.hitlQueue)
	}
	r.hitlHandler.StatusHandler(w, req)
}
