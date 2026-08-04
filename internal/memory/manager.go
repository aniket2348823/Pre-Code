package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/pgvector/pgvector-go"
	"github.com/vigilagent/vigilagent/internal/database"
)

// Manager coordinates all memory layers for cascading recall.
type Manager struct {
	episodic   *EpisodicStore
	semantic   *SemanticStore
	procedural *ProceduralStore
	working    atomic.Pointer[WorkingMemory]
	embedder   Embedder
	pool       *database.Conn
	mu         sync.RWMutex
}

// NewManager creates a new memory manager with all layers.
//
// It defaults to a NoOpEmbedder (zero vectors) so semantic recall degrades
// gracefully when no embedding provider is configured. Use
// NewManagerWithEmbedder to enable real semantic search.
func NewManager(pool *database.Conn) *Manager {
	return NewManagerWithEmbedder(pool, NewNoOpEmbedder(1536))
}

// NewManagerWithEmbedder creates a memory manager that embeds queries and
// stored content using the given Embedder, enabling real vector search.
// It validates embedding dimensions at startup to prevent silent data corruption.
func NewManagerWithEmbedder(pool *database.Conn, embedder Embedder) *Manager {
	if embedder == nil {
		embedder = NewNoOpEmbedder(1536)
	}

	// Validate embedding dimension matches table schema at startup.
	if pool != nil {
		var dbDim int
		err := pool.QueryRow(context.Background(),
			"SELECT vector_dims(embedding) FROM memory_patterns LIMIT 1").Scan(&dbDim)
		if err == nil && dbDim != embedder.Dimensions() {
			slog.Warn("EMBEDDING DIMENSION MISMATCH",
				"embedder", embedder.Name(),
				"embedder_dims", embedder.Dimensions(),
				"db_dims", dbDim,
				"action", "falling back to NoOpEmbedder to prevent data corruption",
			)
			embedder = NewNoOpEmbedder(dbDim)
		} else if err != nil {
			slog.Warn("could not verify embedding dimensions (table may not exist yet)", "error", err)
		}
	}

	return &Manager{
		episodic:   NewEpisodicStore(pool),
		semantic:   NewSemanticStore(pool),
		procedural: NewProceduralStore(),
		embedder:   embedder,
		pool:       pool,
	}
}

func (m *Manager) initWorkingMemory() {
	m.working.Store(NewWorkingMemory(30 * time.Minute))
}



// Recall performs cascading memory recall: working -> episodic -> semantic.
func (m *Manager) Recall(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	var results []MemoryResult

	// Layer 1: Check working memory (current session)
	working := m.working.Load()
	workingMsgs := working.Search(query, limit)
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

	// Embed the query once and reuse across vector-backed layers.
	queryVecRaw, embedErr := m.embedder.Embed(ctx, query)
	if embedErr != nil {
		slog.Warn("failed to embed query", "error", embedErr)
		queryVecRaw = make([]float32, m.embedder.Dimensions())
	}
	queryVec := pgvector.NewVector(queryVecRaw)

	// Guard: if the query vector is all zeros (NoOpEmbedder), skip vector search
	// to avoid pgvector returning garbage results or crashing on NaN similarity.
	isZeroVector := true
	for _, v := range queryVecRaw {
		if v != 0 {
			isZeroVector = false
			break
		}
	}
	if isZeroVector {
		slog.Debug("semantic recall skipped: zero-vector embedder (no embedding provider configured)")
		return results, nil
	}

	// Layer 2: Check episodic memory (past interactions)
	if m.episodic != nil {
		episodes, err := m.episodic.Search(ctx, "", queryVec, limit-len(results))
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
		patterns, err := m.semantic.Search(ctx, "", queryVec, limit-len(results))
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
		Embedding: func() pgvector.Vector {
			v, err := m.embedder.Embed(ctx, title+"\n"+content)
			if err != nil {
				slog.Warn("failed to embed episode, using zero vector", "error", err)
				return pgvector.NewVector(make([]float32, m.embedder.Dimensions()))
			}
			return pgvector.NewVector(v)
		}(),
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
		Embedding: func() pgvector.Vector {
			v, err := m.embedder.Embed(ctx, name+"\n"+description)
			if err != nil {
				slog.Warn("failed to embed pattern, using zero vector", "error", err)
				return pgvector.NewVector(make([]float32, m.embedder.Dimensions()))
			}
			return pgvector.NewVector(v)
		}(),
	}
	return m.semantic.Store(ctx, pattern)
}

// AddWorkingMessage adds a message to working memory.
func (m *Manager) AddWorkingMessage(role, content string, tokens int) {
	working := m.working.Load()
	if working != nil {
		working.Add(Message{Role: role, Content: content, Tokens: tokens})
	}
}

// EnableRedisBacking configures working memory to persist to Redis
// so messages survive server restarts. Call after creating the manager.
func (m *Manager) EnableRedisBacking(rds *redis.Client, sessionID string) {
	if rds == nil || sessionID == "" {
		return
	}
	m.working.Store(NewRedisBackedWorkingMemory(rds, sessionID, 24*time.Hour))
	slog.Info("working memory: Redis-backed", "session_id", sessionID)
}

// GetWorkingMessages returns all working memory messages.
func (m *Manager) GetWorkingMessages() []Message {
	working := m.working.Load()
	if working != nil {
		return working.Get()
	}
	return nil
}

// ClearWorkingMemory clears the working memory.
func (m *Manager) ClearWorkingMemory() {
	working := m.working.Load()
	if working != nil {
		working.Clear()
	}
}

// WorkingMemory returns the current working memory instance.
func (m *Manager) WorkingMemory() *WorkingMemory {
	return m.working.Load()
}

// WorkingCount returns the number of messages in working memory.
func (m *Manager) WorkingCount() int {
	working := m.working.Load()
	if working != nil {
		return working.Count()
	}
	return 0
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
