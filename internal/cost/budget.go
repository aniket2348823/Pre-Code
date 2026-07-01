package cost

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BudgetManager tracks and enforces cost budgets at org and task levels.
type BudgetManager struct {
	pool        *pgxpool.Pool
	mu          sync.RWMutex
	orgBudgets  map[string]float64
	taskBudgets map[string]float64
	usage       map[string]float64
	defaultOrg  float64
	defaultTask float64
}

// NewBudgetManager creates a new budget manager with default limits.
func NewBudgetManager(pool *pgxpool.Pool, defaultOrgBudget, defaultTaskBudget float64) *BudgetManager {
	return &BudgetManager{
		pool:        pool,
		orgBudgets:  make(map[string]float64),
		taskBudgets: make(map[string]float64),
		usage:       make(map[string]float64),
		defaultOrg:  defaultOrgBudget,
		defaultTask: defaultTaskBudget,
	}
}

// SetOrgBudget sets the budget limit for an organization.
func (m *BudgetManager) SetOrgBudget(orgID string, budget float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.orgBudgets[orgID] = budget
}

// SetTaskBudget sets the budget limit for a specific task.
func (m *BudgetManager) SetTaskBudget(taskID string, budget float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.taskBudgets[taskID] = budget
}

// GetOrgBudget returns the budget for an organization.
func (m *BudgetManager) GetOrgBudget(orgID string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if b, ok := m.orgBudgets[orgID]; ok {
		return b
	}
	return m.defaultOrg
}

// GetTaskBudget returns the budget for a specific task.
func (m *BudgetManager) GetTaskBudget(taskID string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if b, ok := m.taskBudgets[taskID]; ok {
		return b
	}
	return m.defaultTask
}

// CheckBudget returns an error if the cost would exceed the budget.
func (m *BudgetManager) CheckBudget(ctx context.Context, orgID, taskID string, proposedCost float64) error {
	// Check org budget
	orgBudget := m.GetOrgBudget(orgID)
	if orgBudget > 0 {
		orgUsage := m.getOrgUsage(orgID)
		if orgUsage+proposedCost > orgBudget {
			slog.Warn("org budget exceeded",
				"org_id", orgID,
				"usage", orgUsage,
				"budget", orgBudget,
				"proposed", proposedCost,
			)
			return &BudgetExceededError{
				Type:     "org",
				ID:       orgID,
				Usage:    orgUsage,
				Budget:   orgBudget,
				Proposed: proposedCost,
			}
		}
	}

	// Check task budget
	taskBudget := m.GetTaskBudget(taskID)
	if taskBudget > 0 {
		taskUsage := m.getTaskUsage(taskID)
		if taskUsage+proposedCost > taskBudget {
			slog.Warn("task budget exceeded",
				"task_id", taskID,
				"usage", taskUsage,
				"budget", taskBudget,
				"proposed", proposedCost,
			)
			return &BudgetExceededError{
				Type:     "task",
				ID:       taskID,
				Usage:    taskUsage,
				Budget:   taskBudget,
				Proposed: proposedCost,
			}
		}
	}

	return nil
}

// RecordCost records cost against an org and task.
func (m *BudgetManager) RecordCost(orgID, taskID string, cost float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage["org:"+orgID] += cost
	m.usage["task:"+taskID] += cost
}

// GetUsage returns the total usage for an org.
func (m *BudgetManager) GetUsage(orgID string) float64 {
	return m.getOrgUsage(orgID)
}

func (m *BudgetManager) getOrgUsage(orgID string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usage["org:"+orgID]
}

func (m *BudgetManager) getTaskUsage(taskID string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usage["task:"+taskID]
}

// BudgetExceededError is returned when a cost would exceed the budget.
type BudgetExceededError struct {
	Type     string  // "org" or "task"
	ID       string
	Usage    float64
	Budget   float64
	Proposed float64
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("%s budget exceeded for %s: usage=$%.4f, budget=$%.4f, proposed=$%.4f",
		e.Type, e.ID, e.Usage, e.Budget, e.Proposed)
}

// ResetUsage resets all usage counters (call periodically).
func (m *BudgetManager) ResetUsage() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = make(map[string]float64)
}

// GetSnapshot returns a snapshot of all budget data.
func (m *BudgetManager) GetSnapshot() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := map[string]interface{}{
		"usage":       make(map[string]float64),
		"org_budgets": make(map[string]float64),
		"updated_at":  time.Now(),
	}

	usage := snapshot["usage"].(map[string]float64)
	for k, v := range m.usage {
		usage[k] = v
	}

	orgBudgets := snapshot["org_budgets"].(map[string]float64)
	for k, v := range m.orgBudgets {
		orgBudgets[k] = v
	}

	return snapshot
}
