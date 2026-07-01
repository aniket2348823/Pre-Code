package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Manager coordinates all memory layers for cascading recall.
type Manager struct {
	episodic    *EpisodicStore
	semantic    *SemanticStore
	procedural  *ProceduralStore
	working     *WorkingMemory
	pool        *pgxpool.Pool
}

// NewManager creates a new memory manager with all layers.
func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{
		episodic:   NewEpisodicStore(pool),
		semantic:   NewSemanticStore(pool),
		procedural: NewProceduralStore(),
		working:    NewWorkingMemory(30 * time.Minute),
		pool:       pool,
	}
}

// Recall performs cascading memory recall: working -> episodic -> semantic.
func (m *Manager) Recall(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	var results []MemoryResult

	// Layer 1: Check working memory (current session)
	workingMsgs := m.working.Search(query, limit)
	for _, msg := range workingMsgs {
		results = append(results, MemoryResult{
			Type:     "working",
			Content:  msg.Content,
			Score:    0.9,
			Metadata: map[string]interface{}{"role": msg.Role},
		})
	}
	if len(results) >= limit {
		return results, nil
	}

	// Layer 2: Check episodic memory (past interactions)
	if m.episodic != nil {
		// Use a zero vector for text-based search (production would use embeddings)
		zeroVec := pgvector.NewVector(make([]float32, 1536))
		episodes, err := m.episodic.Search(ctx, "", zeroVec, limit-len(results))
		if err == nil {
			for _, ep := range episodes {
				results = append(results, MemoryResult{
					Type:     "episodic",
					Content:  ep.Content,
					Title:    ep.Title,
					Score:    ep.Importance,
					Metadata: map[string]interface{}{"type": ep.EpisodeType, "tags": ep.Tags},
				})
			}
		}
	}
	if len(results) >= limit {
		return results, nil
	}

	// Layer 3: Check semantic memory (codebase patterns)
	if m.semantic != nil {
		zeroVec := pgvector.NewVector(make([]float32, 1536))
		patterns, err := m.semantic.Search(ctx, "", zeroVec, limit-len(results))
		if err == nil {
			for _, p := range patterns {
				results = append(results, MemoryResult{
					Type:     "semantic",
					Content:  p.Description,
					Title:    p.Name,
					Score:    p.Confidence,
					Metadata: map[string]interface{}{"pattern_type": p.PatternType},
				})
			}
		}
	}

	return results, nil
}

// StoreEpisode stores a new episodic memory.
func (m *Manager) StoreEpisode(ctx context.Context, userID, episodeType, title, content string, importance float64) error {
	mem := &EpisodicMemory{
		UserID:      userID,
		EpisodeType: episodeType,
		Title:       title,
		Content:     content,
		Importance:  importance,
		Embedding:   pgvector.NewVector(make([]float32, 1536)), // Placeholder
	}
	return m.episodic.Store(ctx, mem)
}

// StorePattern stores a new semantic pattern.
func (m *Manager) StorePattern(ctx context.Context, userID, projectID, patternType, name, description string) error {
	pattern := &Pattern{
		UserID:      userID,
		ProjectID:   projectID,
		PatternType: patternType,
		Name:        name,
		Description: description,
		Confidence:  0.5,
		Embedding:   pgvector.NewVector(make([]float32, 1536)), // Placeholder
	}
	return m.semantic.Store(ctx, pattern)
}

// AddWorkingMessage adds a message to working memory.
func (m *Manager) AddWorkingMessage(role, content string, tokens int) {
	m.working.Add(Message{Role: role, Content: content, Tokens: tokens})
}

// GetWorkingMessages returns all working memory messages.
func (m *Manager) GetWorkingMessages() []Message {
	return m.working.Get()
}

// ClearWorkingMemory clears the working memory.
func (m *Manager) ClearWorkingMemory() {
	m.working.Clear()
}

// MemoryResult represents a unified memory recall result.
type MemoryResult struct {
	Type     string                 `json:"type"`
	Content  string                 `json:"content"`
	Title    string                 `json:"title,omitempty"`
	Score    float64                `json:"score"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// SearchMemory performs unified semantic search across all memory layers.
func (m *Manager) SearchMemory(ctx context.Context, query string, types []string, limit int, minRelevance float64) ([]MemoryResult, error) {
	results, err := m.Recall(ctx, query, limit*2) // Get extra to filter
	if err != nil {
		return nil, fmt.Errorf("memory recall failed: %w", err)
	}

	// Filter by type and relevance
	var filtered []MemoryResult
	for _, r := range results {
		if r.Score < minRelevance {
			continue
		}
		if len(types) > 0 {
			match := false
			for _, t := range types {
				if r.Type == t {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		filtered = append(filtered, r)
		if len(filtered) >= limit {
			break
		}
	}

	return filtered, nil
}
