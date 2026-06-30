package contract

// ---------------------------------------------------------------------------
// Health endpoint types — GET /v1/health
// ---------------------------------------------------------------------------

// ServiceStatus represents the health state of a service dependency.
type ServiceStatus string

const (
	ServiceHealthy  ServiceStatus = "healthy"
	ServiceDegraded ServiceStatus = "degraded"
	ServiceDown     ServiceStatus = "down"
)

// HealthResponse is the body returned by GET /v1/health.
type HealthResponse struct {
	Status   ServiceStatus            `json:"status"`
	Version  string                   `json:"version"`
	Uptime   string                   `json:"uptime"`
	Services map[string]ServiceHealth `json:"services"`
}

// ServiceHealth describes the health of one backend dependency.
type ServiceHealth struct {
	Status    ServiceStatus `json:"status"`
	LatencyMs int64         `json:"latency_ms"`
}

// OverallStatus derives the aggregate status from individual services.
// If any service is down, overall is down.
// If any service is degraded, overall is degraded.
// Otherwise healthy.
func (h HealthResponse) OverallStatus() ServiceStatus {
	hasDown := false
	hasDegraded := false
	for _, svc := range h.Services {
		switch svc.Status {
		case ServiceDown:
			hasDown = true
		case ServiceDegraded:
			hasDegraded = true
		}
	}
	if hasDown {
		return ServiceDown
	}
	if hasDegraded {
		return ServiceDegraded
	}
	return ServiceHealthy
}
