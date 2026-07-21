package contract

// ---------------------------------------------------------------------------
// Skill / Marketplace resource types — API contract §2.5, doc 08
// ---------------------------------------------------------------------------

// ListSkillsRequest holds query parameters for GET /v1/skills.
type ListSkillsRequest struct {
	Query    string `json:"query,omitempty"`
	Category string `json:"category,omitempty"`
	SortBy   string `json:"sort_by,omitempty"` // "downloads", "rating", "created_at"
	PageRequest
}

// ValidSortFields are the allowed sort-by values.
var ValidSortFields = []string{"downloads", "rating", "created_at", "name"}

// Validate checks filter and sort values.
func (r *ListSkillsRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.SortBy != "" {
		found := false
		for _, f := range ValidSortFields {
			if r.SortBy == f {
				found = true
				break
			}
		}
		if !found {
			errs.Add("sort_by", "sort_by must be one of: downloads, rating, created_at, name")
		}
	}
	return errs
}

// ListSkillsResponse is the response for GET /v1/skills.
type ListSkillsResponse struct {
	Skills []Skill      `json:"skills"`
	Page   PageResponse `json:"page"`
}

// Skill is the public skill entity.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Downloads   int      `json:"downloads"`
	Rating      float64  `json:"rating"`
	Category    string   `json:"category,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Verified    bool     `json:"verified"`
	CreatedAt   Timestamp `json:"created_at"`
	UpdatedAt   Timestamp `json:"updated_at"`
}

// InstallSkillRequest is the body for POST /v1/skills/{id}/install.
type InstallSkillRequest struct {
	SkillID string         `json:"skill_id"`
	Version string         `json:"version,omitempty"`
	Config  map[string]any `json:"config,omitempty"`
}

// Validate checks required fields.
func (r *InstallSkillRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.SkillID == "" {
		errs.Add("skill_id", "skill_id is required")
	}
	return errs
}

// InstallSkillResponse wraps the installation result.
type InstallSkillResponse struct {
	InstallationID string `json:"installation_id"`
	Status         string `json:"status"`
}

// SkillManifest describes a skill package per doc 08 §2.
type SkillManifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	Permissions  []string          `json:"permissions,omitempty"`
	ConfigSchema map[string]any    `json:"config_schema,omitempty"`
	Dependencies map[string]string `json:"dependencies,omitempty"`
	MinGoVersion string            `json:"min_go_version,omitempty"`
}

// Validate checks required manifest fields.
func (m *SkillManifest) Validate() ValidationErrors {
	var errs ValidationErrors
	if m.Name == "" {
		errs.Add("name", "name is required")
	}
	if m.Version == "" {
		errs.Add("version", "version is required")
	}
	if m.Description == "" {
		errs.Add("description", "description is required")
	}
	if m.Author == "" {
		errs.Add("author", "author is required")
	}
	return errs
}
