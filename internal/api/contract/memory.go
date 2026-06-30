package contract

// ---------------------------------------------------------------------------
// Memory resource types — API contract §2.4
// ---------------------------------------------------------------------------

// DefaultMemorySearchLimit is the default number of memory results.
const DefaultMemorySearchLimit = 10

// SearchMemoryRequest is the body for POST /v1/memory/search.
type SearchMemoryRequest struct {
	Query     string       `json:"query"`
	ProjectID string       `json:"project_id,omitempty"`
	Limit     int          `json:"limit,omitempty"`
	Types     []MemoryType `json:"types,omitempty"`
}

// Validate checks required fields.
func (r *SearchMemoryRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Query == "" {
		errs.Add("query", "query is required")
	}
	if r.Limit < 0 {
		errs.Add("limit", "limit must be non-negative")
	}
	for i, mt := range r.Types {
		if !mt.Valid() {
			errs.Add("types", "invalid memory type at index "+itoa(i))
		}
	}
	return errs
}

// EffectiveLimit returns the search limit, clamped to sensible bounds.
func (r *SearchMemoryRequest) EffectiveLimit() int {
	if r.Limit <= 0 {
		return DefaultMemorySearchLimit
	}
	if r.Limit > MaxPageLimit {
		return MaxPageLimit
	}
	return r.Limit
}

// SearchMemoryResponse is the response for POST /v1/memory/search.
type SearchMemoryResponse struct {
	Results []MemoryResult `json:"results"`
	Page    PageResponse   `json:"page"`
}

// MemoryResult is a single memory search hit.
type MemoryResult struct {
	ID             string     `json:"id"`
	Type           MemoryType `json:"type"`
	Content        string     `json:"content"`
	RelevanceScore float64    `json:"relevance_score"`
	ProjectID      string     `json:"project_id,omitempty"`
	CreatedAt      Timestamp  `json:"created_at"`
}

// CreateMemoryRequest is the body for POST /v1/memory.
type CreateMemoryRequest struct {
	Type      MemoryType     `json:"type"`
	Content   string         `json:"content"`
	ProjectID string         `json:"project_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Validate checks required fields.
func (r *CreateMemoryRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Content == "" {
		errs.Add("content", "content is required")
	}
	if !r.Type.Valid() {
		errs.Add("type", "type must be one of: episodic, semantic, procedural")
	}
	return errs
}

// CreateMemoryResponse wraps the created memory episode.
type CreateMemoryResponse struct {
	Memory MemoryEpisode `json:"memory"`
}

// MemoryEpisode is the full memory entity.
type MemoryEpisode struct {
	ID        string         `json:"id"`
	Type      MemoryType     `json:"type"`
	Content   string         `json:"content"`
	ProjectID string         `json:"project_id,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt Timestamp      `json:"created_at"`
	UpdatedAt Timestamp      `json:"updated_at"`
}

// itoa is a minimal int-to-string for error messages (avoids strconv import).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
