package skills

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/repository"
)

// Embedder defines the interface for text embedding.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// SearchQuery represents a parsed and enriched search query.
type SearchQuery struct {
	Raw          string
	Expanded     []string
	Language     string
	Category     string
	MinRating    float64
	MinDownloads int
	SortBy       string
	Limit        int
	Offset       int
}

// SearchResult represents a single search result with hybrid scoring.
type SearchResult struct {
	Skill       repository.Skill `json:"skill"`
	Score       float64          `json:"score"`
	VectorScore float64          `json:"vector_score,omitempty"`
	BM25Score   float64          `json:"bm25_score,omitempty"`
	MetaScore   float64          `json:"meta_score,omitempty"`
	Highlights  []string         `json:"highlights,omitempty"`
}

// RAGResponse is the full RAG response with metadata.
type RAGResponse struct {
	Results    []SearchResult `json:"results"`
	Total      int            `json:"total"`
	Query      string         `json:"query"`
	ExpandedTo []string       `json:"expanded_to,omitempty"`
	Provider   string         `json:"provider"`
	LatencyMs  int64          `json:"latency_ms"`
}

// CategoryCount represents a skill category with its count.
type CategoryCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// scoredResult is used internally during hybrid search merging.
type scoredResult struct {
	skill       repository.Skill
	score       float64
	vectorScore float64
	bm25Score   float64
	metaScore   float64
}

// RAGEngine is a world-class Retrieval-Augmented Generation engine for the skill marketplace.
type RAGEngine struct {
	pool    *database.Conn
	embedder Embedder
}

// NewRAGEngine creates a new RAG engine for skill marketplace search.
func NewRAGEngine(pool *database.Conn, embedder Embedder) *RAGEngine {
	return &RAGEngine{
		pool:     pool,
		embedder: embedder,
	}
}

// HybridSearch performs world-class hybrid search combining vector, BM25, and metadata scoring.
func (r *RAGEngine) HybridSearch(ctx context.Context, query SearchQuery) (*RAGResponse, error) {
	start := time.Now()

	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		query.Limit = 100
	}

	// Step 1: Query expansion
	expanded := r.expandQuery(query.Raw)

	// Step 2: Generate embedding
	embedding, err := r.embedder.Embed(ctx, query.Raw)
	if err != nil {
		slog.Warn("RAG: embedding failed, falling back to BM25 only", "error", err)
		embedding = make([]float32, r.embedder.Dimensions())
	}
	pgVec := pgvector.NewVector(embedding)

	// Step 3: Parallel hybrid search
	vectorCh := make(chan []scoredResult, 1)
	bm25Ch := make(chan []scoredResult, 1)

	go func() {
		results, err := r.vectorSearch(ctx, pgVec, query, 50)
		if err != nil {
			slog.Warn("RAG: vector search failed", "error", err)
			vectorCh <- nil
			return
		}
		vectorCh <- results
	}()

	go func() {
		results, err := r.bm25Search(ctx, query.Raw, query, 50)
		if err != nil {
			slog.Warn("RAG: BM25 search failed", "error", err)
			bm25Ch <- nil
			return
		}
		bm25Ch <- results
	}()

	vectorResults := <-vectorCh
	bm25Results := <-bm25Ch

	// Step 4: Reciprocal Rank Fusion
	merged := r.reciprocalRankFusion(vectorResults, bm25Results)

	// Step 5: Rerank by metadata
	reranked := r.rerankByMetadata(merged)

	// Step 6: Pagination
	total := len(reranked)
	startIdx := query.Offset
	if startIdx >= total {
		startIdx = total
	}
	end := startIdx + query.Limit
	if end > total {
		end = total
	}
	paged := reranked[startIdx:end]

	// Convert to SearchResult
	results := make([]SearchResult, len(paged))
	for i, sr := range paged {
		results[i] = SearchResult{
			Skill:       sr.skill,
			Score:       sr.score,
			VectorScore: sr.vectorScore,
			BM25Score:   sr.bm25Score,
			MetaScore:   sr.metaScore,
		}
	}

	latency := time.Since(start).Milliseconds()

	return &RAGResponse{
		Results:    results,
		Total:      total,
		Query:      query.Raw,
		ExpandedTo: expanded,
		Provider:   r.embedder.Name(),
		LatencyMs:  latency,
	}, nil
}

// expandQuery generates semantic variants of the search query using whole-word matching.
func (r *RAGEngine) expandQuery(query string) []string {
	expanded := []string{query}

	synonyms := map[string][]string{
		"auth":     {"authentication", "login", "jwt", "oauth", "session"},
		"validate": {"validation", "schema", "check", "verify", "sanitize"},
		"scan":     {"lint", "analyze", "check", "detect"},
		"security": {"vulnerability", "cve", "owasp", "hardening"},
		"error":    {"exception", "panic", "recover", "handle"},
		"config":   {"configuration", "settings", "options", "env"},
		"test":     {"testing", "spec", "mock", "assert"},
		"deploy":   {"deployment", "ci", "cd", "pipeline"},
		"monitor":  {"observability", "metrics", "logging", "tracing"},
		"cache":    {"caching", "redis", "memcache", "ttl"},
	}

	words := strings.Fields(strings.ToLower(query))
	for _, word := range words {
		if len(word) < 3 {
			continue // skip short words ("a", "I", "go", "to") — too ambiguous for synonym expansion
		}
		if syns, ok := synonyms[word]; ok {
			for _, syn := range syns {
				if syn != word {
					// Use whole-word replacement: rebuild query with exact word replacement
					newWords := make([]string, len(words))
					copy(newWords, words)
					for i, w := range newWords {
						if w == word {
							newWords[i] = syn
							break // only replace first occurrence
						}
					}
					expanded = append(expanded, strings.Join(newWords, " "))
				}
			}
		}
	}

	return expanded
}

// vectorSearch performs pgvector cosine similarity search.
func (r *RAGEngine) vectorSearch(ctx context.Context, embedding pgvector.Vector, query SearchQuery, limit int) ([]scoredResult, error) {
	where := "WHERE s.is_published = true"
	args := []interface{}{embedding}
	argIdx := 2

	if query.Language != "" {
		where += fmt.Sprintf(" AND s.category = $%d", argIdx)
		args = append(args, query.Language)
		argIdx++
	}
	if query.MinRating > 0 {
		where += fmt.Sprintf(" AND s.rating >= $%d", argIdx)
		args = append(args, query.MinRating)
		argIdx++
	}

	searchQuery := fmt.Sprintf(`
		SELECT s.id, s.name, s.description, s.author, s.version, s.category,
		       s.downloads, s.rating, s.rating_count, s.permissions, s.manifest,
		       s.is_verified, s.is_published, s.created_at, s.updated_at,
		       1 - (COALESCE(se.embedding, $1) <=> $1) as similarity
		FROM skills s
		LEFT JOIN skill_embeddings se ON se.skill_id = s.id
		%s
		ORDER BY COALESCE(se.embedding, $1) <=> $1
		LIMIT $%d
	`, where, argIdx)

	args = append(args, limit)
	rows, err := r.pool.Query(ctx, searchQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []scoredResult
	for rows.Next() {
		var sr scoredResult
		var similarity float64
		if err := rows.Scan(
			&sr.skill.ID, &sr.skill.Name, &sr.skill.Description, &sr.skill.Author,
			&sr.skill.Version, &sr.skill.Category, &sr.skill.Downloads, &sr.skill.Rating,
			&sr.skill.RatingCount, &sr.skill.Permissions, &sr.skill.Manifest,
			&sr.skill.IsVerified, &sr.skill.IsPublished, &sr.skill.CreatedAt, &sr.skill.UpdatedAt,
			&similarity,
		); err != nil {
			continue
		}
		sr.vectorScore = similarity
		results = append(results, sr)
	}
	return results, rows.Err()
}

// bm25Search performs PostgreSQL full-text search with ranking.
func (r *RAGEngine) bm25Search(ctx context.Context, query string, sq SearchQuery, limit int) ([]scoredResult, error) {
	tsQuery := buildTsQuery(query)

	where := "WHERE is_published = true AND to_tsvector('english', name || ' ' || COALESCE(description, '') || ' ' || COALESCE(category, '')) @@ to_tsquery('english', $1)"
	args := []interface{}{tsQuery}
	argIdx := 2

	if sq.Language != "" {
		where += fmt.Sprintf(" AND category = $%d", argIdx)
		args = append(args, sq.Language)
		argIdx++
	}
	if sq.MinRating > 0 {
		where += fmt.Sprintf(" AND rating >= $%d", argIdx)
		args = append(args, sq.MinRating)
		argIdx++
	}

	searchQuery := fmt.Sprintf(`
		SELECT id, name, description, author, version, category, downloads, rating,
		       rating_count, permissions, manifest, is_verified, is_published, created_at, updated_at,
		       ts_rank(to_tsvector('english', name || ' ' || COALESCE(description, '') || ' ' || COALESCE(category, '')),
		              to_tsquery('english', $1)) AS rank
		FROM skills
		%s
		ORDER BY rank DESC
		LIMIT $%d
	`, where, argIdx)

	args = append(args, limit)
	rows, err := r.pool.Query(ctx, searchQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("BM25 search failed: %w", err)
	}
	defer rows.Close()

	var results []scoredResult
	for rows.Next() {
		var sr scoredResult
		var rank float64
		if err := rows.Scan(
			&sr.skill.ID, &sr.skill.Name, &sr.skill.Description, &sr.skill.Author,
			&sr.skill.Version, &sr.skill.Category, &sr.skill.Downloads, &sr.skill.Rating,
			&sr.skill.RatingCount, &sr.skill.Permissions, &sr.skill.Manifest,
			&sr.skill.IsVerified, &sr.skill.IsPublished, &sr.skill.CreatedAt, &sr.skill.UpdatedAt,
			&rank,
		); err != nil {
			continue
		}
		sr.bm25Score = rank
		results = append(results, sr)
	}
	return results, rows.Err()
}

// reciprocalRankFusion merges vector and BM25 results using RRF.
func (r *RAGEngine) reciprocalRankFusion(vectorResults, bm25Results []scoredResult) []scoredResult {
	skillMap := make(map[string]*scoredResult)
	const k = 60.0

	for rank, sr := range vectorResults {
		id := sr.skill.ID
		if existing, ok := skillMap[id]; ok {
			existing.vectorScore = 1.0 / (k + float64(rank+1))
		} else {
			sr.vectorScore = 1.0 / (k + float64(rank+1))
			skillMap[id] = &sr
		}
	}

	for rank, sr := range bm25Results {
		id := sr.skill.ID
		if existing, ok := skillMap[id]; ok {
			existing.bm25Score = 1.0 / (k + float64(rank+1))
		} else {
			sr.bm25Score = 1.0 / (k + float64(rank+1))
			skillMap[id] = &sr
		}
	}

	results := make([]scoredResult, 0, len(skillMap))
	for _, sr := range skillMap {
		sr.score = sr.vectorScore + sr.bm25Score
		results = append(results, *sr)
	}

	return results
}

// rerankByMetadata applies metadata-based reranking.
func (r *RAGEngine) rerankByMetadata(results []scoredResult) []scoredResult {
	maxDownloads := 1.0
	maxRating := 1.0
	for _, sr := range results {
		if float64(sr.skill.Downloads) > maxDownloads {
			maxDownloads = float64(sr.skill.Downloads)
		}
		if sr.skill.Rating > maxRating {
			maxRating = sr.skill.Rating
		}
	}

	for i := range results {
		downloadNorm := float64(results[i].skill.Downloads) / maxDownloads
		ratingNorm := results[i].skill.Rating / maxRating

		recencyBonus := 0.0
		if !results[i].skill.UpdatedAt.IsZero() {
			hoursSinceUpdate := time.Since(results[i].skill.UpdatedAt).Hours()
			if hoursSinceUpdate < 24*7 {
				recencyBonus = 0.1
			} else if hoursSinceUpdate < 24*30 {
				recencyBonus = 0.05
			}
		}

		verifiedBonus := 0.0
		if results[i].skill.IsVerified {
			verifiedBonus = 0.05
		}

		results[i].metaScore = downloadNorm*0.3 + ratingNorm*0.3 + recencyBonus + verifiedBonus
		results[i].score = results[i].score*0.5 + results[i].metaScore*0.3 +
			(results[i].vectorScore+results[i].bm25Score)*0.2
	}

	// Sort by final score descending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}

// IndexSkill stores a skill embedding for future search.
func (r *RAGEngine) IndexSkill(ctx context.Context, skill repository.Skill) error {
	text := fmt.Sprintf("%s %s %s %v", skill.Name, skill.Description, skill.Category, skill.Permissions)
	embedding, err := r.embedder.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("failed to embed skill: %w", err)
	}

	pgVec := pgvector.NewVector(embedding)

	query := `
		INSERT INTO skill_embeddings (skill_id, embedding, content_text, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (skill_id) DO UPDATE SET
			embedding = EXCLUDED.embedding,
			content_text = EXCLUDED.content_text,
			updated_at = NOW()
	`
	_, err = r.pool.Exec(ctx, query, skill.ID, pgVec, text)
	if err != nil {
		return fmt.Errorf("failed to index skill: %w", err)
	}

	slog.Info("skill indexed", "skill_id", skill.ID, "name", skill.Name)
	return nil
}

// ReindexAll re-indexes all published skills.
func (r *RAGEngine) ReindexAll(ctx context.Context) (int, error) {
	query := `
		SELECT id, name, description, author, version, category, downloads, rating,
		       rating_count, permissions, manifest, is_verified, is_published, created_at, updated_at
		FROM skills WHERE is_published = true
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to list skills for reindex: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var s repository.Skill
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Description, &s.Author, &s.Version,
			&s.Category, &s.Downloads, &s.Rating, &s.RatingCount,
			&s.Permissions, &s.Manifest, &s.IsVerified, &s.IsPublished,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			continue
		}
		if err := r.IndexSkill(ctx, s); err != nil {
			slog.Warn("failed to index skill", "skill_id", s.ID, "error", err)
			continue
		}
		count++
	}

	slog.Info("reindex complete", "indexed", count)
	return count, nil
}

// SuggestSkills returns autocomplete suggestions using pg_trgm trigram similarity.
func (r *RAGEngine) SuggestSkills(ctx context.Context, partial string, limit int) ([]string, error) {
	if partial == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT DISTINCT name FROM skills
		WHERE is_published = true
		AND (name % $1 OR name ILIKE $2)
		ORDER BY similarity(name, $1) DESC, name
		LIMIT $3
	`
	rows, err := r.pool.Query(ctx, query, partial, "%"+partial+"%", limit)
	if err != nil {
		// Fallback to ILIKE if pg_trgm not available
		query = `
			SELECT DISTINCT name FROM skills
			WHERE is_published = true AND name ILIKE $1
			ORDER BY name LIMIT $2
		`
		rows, err = r.pool.Query(ctx, query, "%"+partial+"%", limit)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var suggestions []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		suggestions = append(suggestions, name)
	}
	return suggestions, rows.Err()
}

// GetTrending returns trending skills based on recent downloads and ratings.
func (r *RAGEngine) GetTrending(ctx context.Context, limit int) ([]repository.Skill, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT id, name, description, author, version, category, downloads, rating,
		       rating_count, permissions, manifest, is_verified, is_published, created_at, updated_at
		FROM skills
		WHERE is_published = true
		AND updated_at > NOW() - INTERVAL '30 days'
		ORDER BY (downloads * 0.7 + rating * rating_count * 0.3) DESC
		LIMIT $1
	`
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []repository.Skill
	for rows.Next() {
		var s repository.Skill
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Description, &s.Author, &s.Version,
			&s.Category, &s.Downloads, &s.Rating, &s.RatingCount,
			&s.Permissions, &s.Manifest, &s.IsVerified, &s.IsPublished,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			continue
		}
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

// GetByCategory returns skills grouped by category with counts.
func (r *RAGEngine) GetByCategory(ctx context.Context) ([]CategoryCount, error) {
	query := `
		SELECT category, COUNT(*) as count
		FROM skills
		WHERE is_published = true AND category != ''
		GROUP BY category
		ORDER BY count DESC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []CategoryCount
	for rows.Next() {
		var cc CategoryCount
		if err := rows.Scan(&cc.Category, &cc.Count); err != nil {
			continue
		}
		categories = append(categories, cc)
	}
	return categories, rows.Err()
}

// buildTsQuery converts user input to a tsquery-compatible string.
func buildTsQuery(query string) string {
	words := strings.Fields(query)
	var tsWords []string
	for _, word := range words {
		word = strings.ReplaceAll(word, "'", "")
		word = strings.ReplaceAll(word, ":", "")
		if word != "" {
			tsWords = append(tsWords, word+":*")
		}
	}
	return strings.Join(tsWords, " & ")
}

// EnsurePgTrgmExtension enables the pg_trgm extension for fuzzy search.
func EnsurePgTrgmExtension(ctx context.Context, pool *database.Conn) error {
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm")
	return err
}

// EnsureRequiredTables creates all required tables for the RAG system.
// NOTE: In production, use migration files instead of this function.
func EnsureRequiredTables(ctx context.Context, pool *database.Conn) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS skill_embeddings (
			id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
			skill_id UUID NOT NULL UNIQUE REFERENCES skills(id) ON DELETE CASCADE,
			embedding vector(1536),
			content_text TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create skill_embeddings table: %w", err)
	}

	// Try HNSW first, fall back to IVFFlat
	_, err = pool.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_skill_embeddings_hnsw
		ON skill_embeddings USING hnsw (embedding vector_cosine_ops)
		WITH (m = 16, ef_construction = 64)
	`)
	if err != nil {
		slog.Warn("HNSW index creation failed, trying IVFFlat", "error", err)
		_, _ = pool.Exec(ctx, `
			CREATE INDEX IF NOT EXISTS idx_skill_embeddings_ivfflat
			ON skill_embeddings USING ivfflat (embedding vector_cosine_ops)
			WITH (lists = 100)
		`)
	}

	return nil
}
