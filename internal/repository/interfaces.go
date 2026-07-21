package repository

import (
	"context"
	"time"
)

// UserRepositoryInterface defines the interface for user data access.
type UserRepositoryInterface interface {
	Create(ctx context.Context, user *User) error
	FindByID(ctx context.Context, id string) (*User, error)
	FindByEmail(ctx context.Context, email string) (*User, error)
	UpdateProfile(ctx context.Context, userID, name, avatarURL string) error
	UpdatePassword(ctx context.Context, userID, passwordHash string) error
	UpdateEmailVerified(ctx context.Context, userID string) error
	UpdateLastLogin(ctx context.Context, userID string) error
	UpdateRole(ctx context.Context, userID, role string) error
	Delete(ctx context.Context, userID string) error
	Count(ctx context.Context) (int, error)
	CountActive24h(ctx context.Context) (int, error)
	List(ctx context.Context, offset, limit int) ([]User, error)
}

// OrganizationRepositoryInterface defines the interface for organization data access.
type OrganizationRepositoryInterface interface {
	Create(ctx context.Context, org *Organization) error
	FindByID(ctx context.Context, id string) (*Organization, error)
	ListByUser(ctx context.Context, userID string) ([]Organization, error)
	Update(ctx context.Context, id, name, description, plan string, settings map[string]interface{}) error
	Delete(ctx context.Context, id string) error
	IsMember(ctx context.Context, orgID, userID string) (bool, error)
	IsOwner(ctx context.Context, orgID, userID string) (bool, error)
	AddMember(ctx context.Context, orgID, userID, role string) error
}

// ProjectRepositoryInterface defines the interface for project data access.
type ProjectRepositoryInterface interface {
	Create(ctx context.Context, project *Project) error
	FindByID(ctx context.Context, id string) (*Project, error)
	ListByOrg(ctx context.Context, orgID string) ([]Project, error)
	Update(ctx context.Context, id, name, description, status string) error
	Delete(ctx context.Context, id string) error
}

// AgentRepositoryInterface defines the interface for agent data access.
type AgentRepositoryInterface interface {
	Create(ctx context.Context, agent *Agent) error
	FindByID(ctx context.Context, id string) (*Agent, error)
	ListByProject(ctx context.Context, projectID string) ([]Agent, error)
	Update(ctx context.Context, id, name, description, status string, config map[string]interface{}) error
	Delete(ctx context.Context, id string) error
}

// SessionRepositoryInterface defines the interface for session data access.
type SessionRepositoryInterface interface {
	Create(ctx context.Context, session *Session) error
	FindByID(ctx context.Context, id string) (*Session, error)
	ListByAgent(ctx context.Context, agentID string) ([]Session, error)
	Update(ctx context.Context, id, status string) error
	EndSession(ctx context.Context, id string) error
}

// EventRepositoryInterface defines the interface for event data access.
type EventRepositoryInterface interface {
	Create(ctx context.Context, event *Event) error
	BatchCreate(ctx context.Context, events []Event) error
	GetCostByOrg(ctx context.Context, orgID string, from, to time.Time) (*CostSummary, error)
	GetTokensByOrg(ctx context.Context, orgID string, from, to time.Time) (*TokenSummary, error)
	GetSessionStatsByOrg(ctx context.Context, orgID string) (*SessionStats, error)
	GetTopAgentsByOrg(ctx context.Context, orgID string, limit int) ([]TopAgent, error)
	GetRecentActivity(ctx context.Context, orgID string, limit int) ([]DashboardActivity, error)
}

// APIKeyRepositoryInterface defines the interface for API key data access.
type APIKeyRepositoryInterface interface {
	Create(ctx context.Context, key *APIKey) error
	FindByHash(ctx context.Context, hash string) (*APIKey, error)
	ListByUser(ctx context.Context, userID string) ([]APIKey, error)
	Delete(ctx context.Context, id, userID string) error
}

// TaskRepositoryInterface defines the interface for task data access.
type TaskRepositoryInterface interface {
	Create(ctx context.Context, task *Task) error
	FindByID(ctx context.Context, id string) (*Task, error)
	ListByProject(ctx context.Context, projectID string, offset, limit int) ([]Task, int, error)
	UpdateStatus(ctx context.Context, id, status string) error
	Complete(ctx context.Context, id, result, modelUsed, provider string, inputTokens, outputTokens, totalTokens int, cost float64) error
	Cancel(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// SkillRepositoryInterface defines the interface for skill data access.
type SkillRepositoryInterface interface {
	Create(ctx context.Context, skill *Skill) error
	FindByID(ctx context.Context, id string) (*Skill, error)
	List(ctx context.Context, category, sortBy string, offset, limit int) ([]Skill, int, error)
	Update(ctx context.Context, id, name, description, version, category string) error
	Delete(ctx context.Context, id string) error
	IncrementDownloads(ctx context.Context, id string) error
	AddRating(ctx context.Context, rating *SkillRating) error
	ListRatings(ctx context.Context, skillID string, offset, limit int) ([]SkillRating, int, error)
}

// AlertRepositoryInterface defines the interface for alert data access.
type AlertRepositoryInterface interface {
	Create(ctx context.Context, alert *Alert) error
	FindByID(ctx context.Context, id string) (*Alert, error)
	ListByUser(ctx context.Context, userID string) ([]Alert, error)
	Update(ctx context.Context, id, name, channel string, isActive bool) error
	Delete(ctx context.Context, id string) error
}

// Compile-time interface checks
var (
	_ UserRepositoryInterface         = (*UserRepository)(nil)
	_ OrganizationRepositoryInterface = (*OrganizationRepository)(nil)
	_ ProjectRepositoryInterface      = (*ProjectRepository)(nil)
	_ AgentRepositoryInterface        = (*AgentRepository)(nil)
	_ SessionRepositoryInterface      = (*SessionRepository)(nil)
	_ EventRepositoryInterface        = (*EventRepository)(nil)
	_ APIKeyRepositoryInterface       = (*APIKeyRepository)(nil)
	_ TaskRepositoryInterface         = (*TaskRepository)(nil)
	_ SkillRepositoryInterface        = (*SkillRepository)(nil)
	_ AlertRepositoryInterface        = (*AlertRepository)(nil)
)
