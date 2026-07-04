// Package skillengine implements the skill extraction engine: it converts
// validated findings into reusable skills with trigger → fix → verification →
// evidence, and maintains a skill registry with usage-based ranking. This is
// the core moat — over time, the skill library grows and the system becomes
// faster and more consistent.
package skillengine

import (
	"sort"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/util"
)

// Skill represents a validated security pattern extracted from findings.
type Skill struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Trigger      string    `json:"trigger"`      // what pattern detects this issue
	Fix          string    `json:"fix"`          // the recommended fix
	Verification string    `json:"verification"` // how to verify the fix works
	Evidence     []string  `json:"evidence"`     // proof that the fix is correct
	Confidence   float64   `json:"confidence"`   // 0.0–1.0 confidence score
	UsageCount   int       `json:"usage_count"`
	SuccessRate  float64   `json:"success_rate"` // 0.0–1.0
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Finding is a validated security finding that can become a skill.
type Finding struct {
	Severity  string   `json:"severity"`
	Message   string   `json:"message"`
	Filename  string   `json:"filename"`
	Line      int      `json:"line"`
	Fix       string   `json:"fix"`
	Analyzers []string `json:"analyzers"`
	Confidence float64 `json:"confidence"`
}

// SkillRank holds ranking metrics for a skill.
type SkillRank struct {
	SkillID     string  `json:"skill_id"`
	UsageCount  int     `json:"usage_count"`
	SuccessRate float64 `json:"success_rate"`
	Score       float64 `json:"score"` // composite ranking score
}

// Engine extracts and ranks skills from findings.
type Engine struct {
	mu     sync.RWMutex
	skills map[string]*Skill
	// Trigger → skill index for fast lookup.
	triggerIndex map[string]*Skill
}

// NewEngine creates an empty skill engine.
func NewEngine() *Engine {
	return &Engine{
		skills:       make(map[string]*Skill),
		triggerIndex: make(map[string]*Skill),
	}
}

// ExtractFromFinding attempts to convert a finding into a skill.
// If a matching skill already exists, it updates the existing skill's metrics.
// Returns the skill and whether it was newly created.
func (e *Engine) ExtractFromFinding(f Finding) (*Skill, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Generate a deterministic trigger key from the finding
	trigger := normalizeTrigger(f.Message)

	// Check if we already have a skill for this trigger
	if existing, ok := e.triggerIndex[trigger]; ok {
		existing.UsageCount++
		existing.UpdatedAt = time.Now()
		return existing, false
	}

	// Create new skill
	skill := &Skill{
		ID:           generateSkillID(trigger),
		Name:         f.Message,
		Trigger:      trigger,
		Fix:          f.Fix,
		Verification: "verify: " + f.Fix,
		Evidence:     f.Analyzers,
		Confidence:   f.Confidence,
		UsageCount:   1,
		SuccessRate:  0.0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	e.skills[skill.ID] = skill
	e.triggerIndex[trigger] = skill
	return skill, true
}

// RecordOutcome records whether a skill's fix was accepted or rejected.
func (e *Engine) RecordOutcome(skillID string, accepted bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	skill, ok := e.skills[skillID]
	if !ok {
		return
	}
	// Update success rate with exponential moving average
	alpha := 0.1
	if accepted {
		skill.SuccessRate = skill.SuccessRate*(1-alpha) + alpha
	} else {
		skill.SuccessRate = skill.SuccessRate * (1 - alpha)
	}
	// Update confidence based on success rate
	// Higher success rate → higher confidence
	weight := 0.7
	// If UsageCount is low, trust the original confidence more
	if skill.UsageCount < 5 {
		weight = 0.3
	}
	skill.Confidence = skill.Confidence*(1-weight) + skill.SuccessRate*weight
	skill.UpdatedAt = time.Now()
}

// FindByTrigger looks up a skill by its trigger pattern.
func (e *Engine) FindByTrigger(trigger string) (*Skill, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s, ok := e.triggerIndex[normalizeTrigger(trigger)]
	return s, ok
}

// GetAllSkills returns all skills sorted by composite score (descending).
func (e *Engine) GetAllSkills() []*Skill {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]*Skill, 0, len(e.skills))
	for _, s := range e.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return e.scoreSkill(out[i]) > e.scoreSkill(out[j])
	})
	return out
}

// Rank returns ranked skills with composite scores.
func (e *Engine) Rank() []SkillRank {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]SkillRank, 0, len(e.skills))
	for _, s := range e.skills {
		out = append(out, SkillRank{
			SkillID:     s.ID,
			UsageCount:  s.UsageCount,
			SuccessRate: s.SuccessRate,
			Score:       e.scoreSkill(s),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// Count returns the number of skills.
func (e *Engine) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.skills)
}

// scoreSkill computes a composite ranking score.
// Formula: 0.4 * confidence + 0.3 * min(usage/100, 1.0) + 0.3 * success_rate
func (e *Engine) scoreSkill(s *Skill) float64 {
	usageScore := float64(s.UsageCount) / 100.0
	if usageScore > 1.0 {
		usageScore = 1.0
	}
	return 0.4*s.Confidence + 0.3*usageScore + 0.3*s.SuccessRate
}

// normalizeTrigger creates a canonical trigger key from a message.
func normalizeTrigger(msg string) string {
	// Simple normalization: lowercase, trim, collapse whitespace
	out := make([]byte, 0, len(msg))
	prevSpace := false
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c >= 'A' && c <= 'Z' {
			c = c + 32
			out = append(out, c)
			prevSpace = false
		} else if c == ' ' || c == '\t' || c == '\n' {
			if !prevSpace {
				out = append(out, ' ')
				prevSpace = true
			}
		} else {
			out = append(out, c)
			prevSpace = false
		}
	}
	return string(out)
}

// generateSkillID creates a deterministic skill ID from a trigger.
func generateSkillID(trigger string) string {
	// Simple hash-like ID from trigger
	h := 0
	for i := 0; i < len(trigger); i++ {
		h = h*31 + int(trigger[i])
	}
	if h < 0 {
		h = -h
	}
	return "skill-" + util.Itoa(h)
}
