package contract

// ---------------------------------------------------------------------------
// Project resource types — API contract §2.3
// ---------------------------------------------------------------------------

// CreateProjectRequest is the body for POST /v1/projects.
type CreateProjectRequest struct {
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	RepositoryURL string           `json:"repository_url,omitempty"`
	Language      string           `json:"language,omitempty"`
	Settings      *ProjectSettings `json:"settings,omitempty"`
}

// Validate checks required fields.
func (r *CreateProjectRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Name == "" {
		errs.Add("name", "name is required")
	}
	return errs
}

// CreateProjectResponse wraps the created project.
type CreateProjectResponse struct {
	Project Project `json:"project"`
}

// Project is the full project entity.
type Project struct {
	ID            string           `json:"id"`
	UserID        string           `json:"user_id"`
	Name          string           `json:"name"`
	Description   string           `json:"description,omitempty"`
	RepositoryURL string           `json:"repository_url,omitempty"`
	Language      string           `json:"language,omitempty"`
	Settings      *ProjectSettings `json:"settings,omitempty"`
	CreatedAt     Timestamp        `json:"created_at"`
	UpdatedAt     Timestamp        `json:"updated_at"`
}

// ProjectSettings holds per-project configuration.
type ProjectSettings struct {
	DefaultModel string  `json:"default_model,omitempty"`
	BudgetLimit  float64 `json:"budget_limit,omitempty"`
	Conventions  string  `json:"conventions,omitempty"`
}

// UpdateProjectRequest is the body for PATCH /v1/projects/{id}.
type UpdateProjectRequest struct {
	Name          *string          `json:"name,omitempty"`
	Description   *string          `json:"description,omitempty"`
	RepositoryURL *string          `json:"repository_url,omitempty"`
	Language      *string          `json:"language,omitempty"`
	Settings      *ProjectSettings `json:"settings,omitempty"`
}

// Validate checks that at least one field is set.
func (r *UpdateProjectRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Name == nil && r.Description == nil && r.RepositoryURL == nil &&
		r.Language == nil && r.Settings == nil {
		errs.Add("body", "at least one field must be provided")
	}
	if r.Name != nil && *r.Name == "" {
		errs.Add("name", "name must not be empty")
	}
	return errs
}

// UpdateProjectResponse wraps the updated project.
type UpdateProjectResponse struct {
	Project Project `json:"project"`
}

// ListProjectsRequest holds query parameters for GET /v1/projects.
type ListProjectsRequest struct {
	PageRequest
}

// ListProjectsResponse is the response for GET /v1/projects.
type ListProjectsResponse struct {
	Projects []Project    `json:"projects"`
	Page     PageResponse `json:"page"`
}
