package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/pgvector/pgvector-go"
	"github.com/vigilagent/vigilagent/internal/database"
)

// Content from manager.go
// Manager coordinates all memory layers for cascading recall.
type Manager struct {
	episodic   *EpisodicStore
	semantic   *SemanticStore
	procedural *ProceduralStore
	working    atomic.Pointer[WorkingMemory]
	embedder   Embedder
	pool       *database.Conn
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
		// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
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

	m := &Manager{
		episodic:   NewEpisodicStore(pool),
		semantic:   NewSemanticStore(pool),
		procedural: NewProceduralStore(),
		embedder:   embedder,
		pool:       pool,
	}
	// Initialize working memory in the constructor so Recall() can never
	// dereference a nil atomic pointer (which would panic on the first recall).
	m.initWorkingMemory()
	return m
}

func (m *Manager) initWorkingMemory() {
	m.working.Store(NewWorkingMemory(30 * time.Minute))
}

// Recall performs cascading memory recall: working -> episodic -> semantic.
func (m *Manager) Recall(ctx context.Context, query string, limit int) ([]MemoryResult, error) {
	var results []MemoryResult

	// Layer 1: Check working memory (current session)
	working := m.working.Load()
	if working != nil {
		workingMsgs := working.Search(query, limit)
		for _, msg := range workingMsgs {
			results = append(results, MemoryResult{
				Type:     "working",
				Content:  msg.Content,
				Score:    0.9,
				Metadata: map[string]interface{}{"role": msg.Role},
			})
		}
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

// Content from episodic.go
// EpisodicStore manages episodic memory in PostgreSQL.
type EpisodicStore struct {
	pool *database.Conn
}

// NewEpisodicStore creates a new episodic memory store.
func NewEpisodicStore(pool *database.Conn) *EpisodicStore {
	return &EpisodicStore{pool: pool}
}

// EpisodicMemory represents a past interaction or decision.
type EpisodicMemory struct {
	ID          string          `json:"id"`
	UserID      string          `json:"user_id"`
	ProjectID   string          `json:"project_id,omitempty"`
	EpisodeType string          `json:"episode_type"`
	Title       string          `json:"title"`
	Content     string          `json:"content"`
	Summary     string          `json:"summary,omitempty"`
	TaskID      string          `json:"task_id,omitempty"`
	SessionID   string          `json:"session_id,omitempty"`
	Importance  float64         `json:"importance"`
	AccessCount int             `json:"access_count"`
	Tags        []string        `json:"tags,omitempty"`
	Embedding   pgvector.Vector `json:"-"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Similarity  float64         `json:"similarity,omitempty"`
}

// Store saves a new episodic memory.
func (s *EpisodicStore) Store(ctx context.Context, mem *EpisodicMemory) error {
	query := `
		INSERT INTO memory_episodes (user_id, project_id, episode_type, title, content, summary, task_id, session_id, importance, tags, embedding, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
		RETURNING id, created_at
	`
	return s.pool.QueryRow(ctx, query,
		mem.UserID, mem.ProjectID, mem.EpisodeType, mem.Title, mem.Content,
		mem.Summary, mem.TaskID, mem.SessionID, mem.Importance, mem.Tags, mem.Embedding,
	).Scan(&mem.ID, &mem.CreatedAt)
}

// Search finds episodic memories by semantic similarity.
func (s *EpisodicStore) Search(ctx context.Context, userID string, embedding pgvector.Vector, limit int) ([]EpisodicMemory, error) {
	query := `
		SELECT id, user_id, project_id, episode_type, title, content, summary,
		       importance, access_count, tags, created_at,
		       1 - (embedding <=> $1) as similarity
		FROM memory_episodes
		WHERE ($2 = '' OR user_id = $2) AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY embedding <=> $1
		LIMIT $3
	`
	rows, err := s.pool.Query(ctx, query, embedding, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("episodic search failed: %w", err)
	}
	defer rows.Close()

	var results []EpisodicMemory
	for rows.Next() {
		var mem EpisodicMemory
		if err := rows.Scan(&mem.ID, &mem.UserID, &mem.ProjectID, &mem.EpisodeType,
			&mem.Title, &mem.Content, &mem.Summary, &mem.Importance,
			&mem.AccessCount, &mem.Tags, &mem.CreatedAt, &mem.Similarity); err != nil {
			continue
		}
		results = append(results, mem)
	}
	return results, rows.Err()
}

// ListByUser returns all episodic memories for a user.
func (s *EpisodicStore) ListByUser(ctx context.Context, userID string, limit int) ([]EpisodicMemory, error) {
	query := `
		SELECT id, user_id, episode_type, title, content, importance, access_count, created_at
		FROM memory_episodes
		WHERE user_id = $1 AND (expires_at IS NULL OR expires_at > NOW())
		ORDER BY importance DESC, created_at DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []EpisodicMemory
	for rows.Next() {
		var mem EpisodicMemory
		if err := rows.Scan(&mem.ID, &mem.UserID, &mem.EpisodeType, &mem.Title,
			&mem.Content, &mem.Importance, &mem.AccessCount, &mem.CreatedAt); err != nil {
			continue
		}
		results = append(results, mem)
	}
	return results, rows.Err()
}

// Content from semantic.go
// SemanticStore manages semantic memory (codebase patterns) using pgvector.
type SemanticStore struct {
	pool *database.Conn
}

// NewSemanticStore creates a new semantic memory store.
func NewSemanticStore(pool *database.Conn) *SemanticStore {
	return &SemanticStore{pool: pool}
}

// Pattern represents a codebase pattern.
type Pattern struct {
	ID               string          `json:"id"`
	UserID           string          `json:"user_id"`
	ProjectID        string          `json:"project_id"`
	PatternType      string          `json:"pattern_type"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Examples         []string        `json:"examples,omitempty"`
	Confidence       float64         `json:"confidence"`
	ObservationCount int             `json:"observation_count"`
	Embedding        pgvector.Vector `json:"-"`
	FilePatterns     []string        `json:"file_patterns,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

// Store saves a new pattern to semantic memory.
func (s *SemanticStore) Store(ctx context.Context, pattern *Pattern) error {
	query := `
		INSERT INTO memory_patterns (user_id, project_id, pattern_type, name, description, examples, confidence, observation_count, embedding, file_patterns, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, created_at
	`
	return s.pool.QueryRow(ctx, query,
		pattern.UserID, pattern.ProjectID, pattern.PatternType, pattern.Name,
		pattern.Description, pattern.Examples, pattern.Confidence,
		pattern.ObservationCount, pattern.Embedding, pattern.FilePatterns,
	).Scan(&pattern.ID, &pattern.CreatedAt)
}

// Search finds patterns by semantic similarity.
func (s *SemanticStore) Search(ctx context.Context, projectID string, embedding pgvector.Vector, limit int) ([]Pattern, error) {
	query := `
		SELECT id, user_id, project_id, pattern_type, name, description, confidence,
		       observation_count, file_patterns, created_at,
		       1 - (embedding <=> $1) as similarity
		FROM memory_patterns
		WHERE ($2 = '' OR project_id = $2)
		ORDER BY embedding <=> $1
		LIMIT $3
	`
	rows, err := s.pool.Query(ctx, query, embedding, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("semantic search failed: %w", err)
	}
	defer rows.Close()

	var results []Pattern
	for rows.Next() {
		var p Pattern
		if err := rows.Scan(&p.ID, &p.UserID, &p.ProjectID, &p.PatternType, &p.Name,
			&p.Description, &p.Confidence, &p.ObservationCount, &p.FilePatterns,
			&p.CreatedAt); err != nil {
			continue
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// ListByProject returns all patterns for a project.
func (s *SemanticStore) ListByProject(ctx context.Context, projectID string, limit int) ([]Pattern, error) {
	query := `
		SELECT id, pattern_type, name, description, confidence, observation_count, created_at
		FROM memory_patterns
		WHERE project_id = $1
		ORDER BY confidence DESC, observation_count DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, query, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Pattern
	for rows.Next() {
		var p Pattern
		if err := rows.Scan(&p.ID, &p.PatternType, &p.Name, &p.Description,
			&p.Confidence, &p.ObservationCount, &p.CreatedAt); err != nil {
			continue
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// Content from working.go
// WorkingMemory provides in-memory session-scoped context.
// When a Redis client is provided, messages are persisted to survive restarts.
type WorkingMemory struct {
	messages  []Message
	mu        sync.RWMutex
	sessionID string
	rds       *redis.Client
	ttl       time.Duration
}

// Message represents a conversation message in working memory.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Tokens    int       `json:"tokens"`
	Timestamp time.Time `json:"timestamp"`
}

// NewWorkingMemory creates a new working memory instance.
func NewWorkingMemory(_ time.Duration) *WorkingMemory {
	return &WorkingMemory{
		messages: make([]Message, 0),
	}
}

// NewRedisBackedWorkingMemory creates a working memory instance that persists
// messages to Redis so they survive server restarts.
func NewRedisBackedWorkingMemory(rds *redis.Client, sessionID string, ttl time.Duration) *WorkingMemory {
	wm := &WorkingMemory{
		messages:  make([]Message, 0),
		sessionID: sessionID,
		rds:       rds,
		ttl:       ttl,
	}
	// Restore messages from Redis on creation.
	if rds != nil && sessionID != "" {
		wm.loadFromRedis()
	}
	return wm
}

func workingMemoryKey(sessionID string) string {
	return fmt.Sprintf("vigilagent:working_memory:%s", sessionID)
}

// loadFromRedis restores messages from Redis.
func (wm *WorkingMemory) loadFromRedis() {
	if wm.rds == nil || wm.sessionID == "" {
		return
	}
	// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := wm.rds.Get(ctx, workingMemoryKey(wm.sessionID)).Bytes()
	if err != nil {
		return // key doesn't exist or Redis error — start empty
	}
	var msgs []Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		slog.Warn("failed to unmarshal working memory from Redis", "error", err)
		return
	}
	wm.messages = msgs
}

// persistToRedis saves current messages to Redis.
// Takes a snapshot under read lock to avoid data race with concurrent Add() calls.
func (wm *WorkingMemory) persistToRedis() {
	if wm.rds == nil || wm.sessionID == "" {
		return
	}
	// Snapshot under read lock to avoid race with concurrent writers.
	wm.mu.RLock()
	snapshot := make([]Message, len(wm.messages))
	copy(snapshot, wm.messages)
	wm.mu.RUnlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		slog.Warn("failed to marshal working memory", "error", err)
		return
	}
	// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := workingMemoryKey(wm.sessionID)
	ttl := wm.ttl
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	if err := wm.rds.Set(ctx, key, data, ttl).Err(); err != nil {
		slog.Warn("failed to persist working memory to Redis", "error", err)
	}
}

// Add appends a message to working memory and persists to Redis if configured.
func (wm *WorkingMemory) Add(msg Message) {
	wm.mu.Lock()
	msg.Timestamp = time.Now()
	wm.messages = append(wm.messages, msg)
	wm.mu.Unlock()

	// Persist to Redis after releasing the lock: persistToRedis takes an RLock,
	// which would deadlock while we hold the write lock. Doing it synchronously
	// (instead of a goroutine per message) also guarantees snapshot ordering —
	// concurrent background writers could otherwise persist an older snapshot last.
	if wm.rds != nil {
		wm.persistToRedis()
	}
}

// Get returns all messages in working memory.
func (wm *WorkingMemory) Get() []Message {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	result := make([]Message, len(wm.messages))
	copy(result, wm.messages)
	return result
}

// Search performs a simple text search in working memory.
func (wm *WorkingMemory) Search(query string, limit int) []Message {
	wm.mu.RLock()
	defer wm.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []Message
	for _, msg := range wm.messages {
		if len(results) >= limit {
			break
		}
		if strings.Contains(strings.ToLower(msg.Content), queryLower) {
			results = append(results, msg)
		}
	}
	return results
}

// Clear removes all messages from working memory.
func (wm *WorkingMemory) Clear() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.messages = wm.messages[:0]
}

// Count returns the number of messages.
func (wm *WorkingMemory) Count() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return len(wm.messages)
}

// TokenCount returns estimated token count.
func (wm *WorkingMemory) TokenCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	total := 0
	for _, msg := range wm.messages {
		total += msg.Tokens
	}
	return total
}

// ProceduralStore manages learned workflows.
type ProceduralStore struct {
	workflows map[string]*Workflow
	mu        sync.RWMutex
}

// Workflow represents a learned workflow pattern.
type Workflow struct {
	ID          string                 `json:"id"`
	UserID      string                 `json:"user_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Steps       []WorkflowStep         `json:"steps"`
	SuccessRate float64                `json:"success_rate"`
	UsageCount  int                    `json:"usage_count"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

// WorkflowStep represents a step in a workflow.
type WorkflowStep struct {
	Action      string                 `json:"action"`
	Description string                 `json:"description"`
	Tool        string                 `json:"tool,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
}

// NewProceduralStore creates a new procedural memory store.
func NewProceduralStore() *ProceduralStore {
	return &ProceduralStore{
		workflows: make(map[string]*Workflow),
	}
}

// Store saves a workflow.
func (s *ProceduralStore) Store(_ context.Context, wf *Workflow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	wf.CreatedAt = time.Now()
	s.workflows[wf.ID] = wf
	return nil
}

// Get retrieves a workflow by ID.
func (s *ProceduralStore) Get(_ context.Context, id string) (*Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	wf, ok := s.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", id)
	}
	return wf, nil
}

// Search finds workflows by name.
func (s *ProceduralStore) Search(_ context.Context, query string, limit int) ([]Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []Workflow
	for _, wf := range s.workflows {
		if len(results) >= limit {
			break
		}
		if strings.Contains(strings.ToLower(wf.Name), queryLower) || strings.Contains(strings.ToLower(wf.Description), queryLower) {
			results = append(results, *wf)
		}
	}
	return results, nil
}

// ListByUser returns all workflows for a user.
func (s *ProceduralStore) ListByUser(_ context.Context, userID string, limit int) ([]Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Workflow
	for _, wf := range s.workflows {
		if len(results) >= limit {
			break
		}
		if wf.UserID == userID {
			results = append(results, *wf)
		}
	}
	return results, nil
}

// Content from embedding.go
// Embedder defines the interface for text embedding providers.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// OpenAIEmbedder implements Embedder using OpenAI's text-embedding API.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	httpClient *http.Client
}

// NewOpenAIEmbedder creates a new OpenAI embedding provider.
func NewOpenAIEmbedder(apiKey string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		apiKey:     apiKey,
		model:      "text-embedding-3-small",
		dimensions: 1536,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *OpenAIEmbedder) Name() string    { return "openai" }
func (e *OpenAIEmbedder) Dimensions() int { return e.dimensions }

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := openAIEmbedRequest{
		Model: e.model,
		Input: []string{text},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var embedResp openAIEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return embedResp.Data[0].Embedding, nil
}

// NoOpEmbedder is a placeholder that returns zero vectors.
type NoOpEmbedder struct {
	dimensions int
}

func NewNoOpEmbedder(dimensions int) *NoOpEmbedder {
	return &NoOpEmbedder{dimensions: dimensions}
}

func (e *NoOpEmbedder) Name() string    { return "noop" }
func (e *NoOpEmbedder) Dimensions() int { return e.dimensions }

func (e *NoOpEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, e.dimensions), nil
}
