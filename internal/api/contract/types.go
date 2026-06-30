// Package contract defines the API request/response types for VigilAgent.
// All types match the wire format specified in doc 04-api-contract.
package contract

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Identifiers
// ---------------------------------------------------------------------------

// ID is the canonical identifier type (UUID v4).
type ID = uuid.UUID

// NewID generates a new random UUID.
func NewID() ID { return uuid.New() }

// ParseID parses a UUID string.
func ParseID(s string) (ID, error) { return uuid.Parse(s) }

// ---------------------------------------------------------------------------
// Pagination (cursor-based per API contract §5)
// ---------------------------------------------------------------------------

// DefaultPageLimit is the default number of items per page.
const DefaultPageLimit = 20

// MaxPageLimit is the maximum allowed page size.
const MaxPageLimit = 100

// PageRequest holds cursor-based pagination parameters from the client.
type PageRequest struct {
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// EffectiveLimit returns the limit clamped to [1, MaxPageLimit] with a default.
func (p PageRequest) EffectiveLimit() int {
	if p.Limit <= 0 {
		return DefaultPageLimit
	}
	if p.Limit > MaxPageLimit {
		return MaxPageLimit
	}
	return p.Limit
}

// PageResponse holds pagination metadata in list responses.
type PageResponse struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// ---------------------------------------------------------------------------
// Timestamps
// ---------------------------------------------------------------------------

// Timestamp is an RFC 3339 time wrapper that serialises consistently.
type Timestamp struct {
	time.Time
}

// Now returns a Timestamp set to the current UTC time.
func Now() Timestamp { return Timestamp{time.Now().UTC()} }

// MarshalJSON implements json.Marshaler.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.Time.Format(time.RFC3339))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("invalid timestamp %q: %w", s, err)
	}
	t.Time = parsed
	return nil
}

// TimestampFromTime wraps a stdlib time.Time in a Timestamp.
func TimestampFromTime(t time.Time) Timestamp {
	return Timestamp{t.UTC()}
}

// ---------------------------------------------------------------------------
// TaskStatus — agent state machine per doc 02 §2.2
// ---------------------------------------------------------------------------

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskStatusPending     TaskStatus = "pending"
	TaskStatusPlanning    TaskStatus = "planning"
	TaskStatusExecuting   TaskStatus = "executing"
	TaskStatusWaitingHITL TaskStatus = "waiting_hitl"
	TaskStatusReviewing   TaskStatus = "reviewing"
	TaskStatusCompleted   TaskStatus = "completed"
	TaskStatusFailed      TaskStatus = "failed"
	TaskStatusCancelled   TaskStatus = "cancelled"
)

// AllTaskStatuses returns every valid status value.
func AllTaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskStatusPending, TaskStatusPlanning, TaskStatusExecuting,
		TaskStatusWaitingHITL, TaskStatusReviewing, TaskStatusCompleted,
		TaskStatusFailed, TaskStatusCancelled,
	}
}

// Valid returns true when the status is one of the known values.
func (s TaskStatus) Valid() bool {
	for _, v := range AllTaskStatuses() {
		if s == v {
			return true
		}
	}
	return false
}

// IsTerminal returns true for statuses that represent a final state.
func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed || s == TaskStatusCancelled
}

// ---------------------------------------------------------------------------
// Complexity — 5-factor scoring per doc 06 §3
// ---------------------------------------------------------------------------

// Complexity classifies a task's difficulty for model routing.
type Complexity string

const (
	ComplexitySimple   Complexity = "simple"
	ComplexityModerate Complexity = "moderate"
	ComplexityComplex  Complexity = "complex"
	ComplexityCritical Complexity = "critical"
)

// AllComplexities returns every valid complexity value.
func AllComplexities() []Complexity {
	return []Complexity{
		ComplexitySimple, ComplexityModerate, ComplexityComplex, ComplexityCritical,
	}
}

// Valid returns true when the complexity is one of the known values.
func (c Complexity) Valid() bool {
	for _, v := range AllComplexities() {
		if c == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Role — RBAC per doc 02 §3.2
// ---------------------------------------------------------------------------

// Role represents a user's access level within an organization.
type Role string

const (
	RoleViewer     Role = "viewer"
	RoleDeveloper  Role = "developer"
	RoleAdmin      Role = "admin"
	RoleSuperAdmin Role = "super_admin"
)

// AllRoles returns every valid role value.
func AllRoles() []Role {
	return []Role{RoleViewer, RoleDeveloper, RoleAdmin, RoleSuperAdmin}
}

// Valid returns true when the role is one of the known values.
func (r Role) Valid() bool {
	for _, v := range AllRoles() {
		if r == v {
			return true
		}
	}
	return false
}

// AtLeast returns true when the role has privileges >= the given level.
func (r Role) AtLeast(other Role) bool {
	return roleLevel(r) >= roleLevel(other)
}

func roleLevel(r Role) int {
	switch r {
	case RoleViewer:
		return 1
	case RoleDeveloper:
		return 2
	case RoleAdmin:
		return 3
	case RoleSuperAdmin:
		return 4
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Tier — pricing tiers per doc 00 master prompt
// ---------------------------------------------------------------------------

// Tier represents a subscription pricing tier.
type Tier string

const (
	TierFree       Tier = "free"
	TierPro        Tier = "pro"
	TierTeam       Tier = "team"
	TierEnterprise Tier = "enterprise"
)

// AllTiers returns every valid tier value.
func AllTiers() []Tier {
	return []Tier{TierFree, TierPro, TierTeam, TierEnterprise}
}

// Valid returns true when the tier is one of the known values.
func (t Tier) Valid() bool {
	for _, v := range AllTiers() {
		if t == v {
			return true
		}
	}
	return false
}

// MonthlyTaskLimit returns the monthly task cap per the Master doc.
// Reconciliation report C2 resolved: monthly limits (not daily).
func (t Tier) MonthlyTaskLimit() int {
	switch t {
	case TierFree:
		return 100
	case TierPro:
		return 2000
	case TierTeam:
		return 5000
	case TierEnterprise:
		return -1 // unlimited / custom
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// MemoryType — memory layer classification per doc 02 §2.4
// ---------------------------------------------------------------------------

// MemoryType classifies a memory result by its storage layer.
type MemoryType string

const (
	MemoryTypeEpisodic   MemoryType = "episodic"
	MemoryTypeSemantic   MemoryType = "semantic"
	MemoryTypeProcedural MemoryType = "procedural"
)

// AllMemoryTypes returns every valid memory type.
func AllMemoryTypes() []MemoryType {
	return []MemoryType{MemoryTypeEpisodic, MemoryTypeSemantic, MemoryTypeProcedural}
}

// Valid returns true when the memory type is one of the known values.
func (m MemoryType) Valid() bool {
	for _, v := range AllMemoryTypes() {
		if m == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

// FieldError describes a single validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationErrors collects zero or more field-level errors.
type ValidationErrors []FieldError

// Add appends a field error.
func (v *ValidationErrors) Add(field, message string) {
	*v = append(*v, FieldError{Field: field, Message: message})
}

// HasErrors returns true when at least one error has been recorded.
func (v ValidationErrors) HasErrors() bool { return len(v) > 0 }

// ToMap converts errors into a map suitable for AppError.Details.
func (v ValidationErrors) ToMap() map[string]any {
	if len(v) == 0 {
		return nil
	}
	m := make(map[string]any, len(v))
	for _, fe := range v {
		m[fe.Field] = fe.Message
	}
	return m
}
