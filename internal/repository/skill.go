package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/vigilagent/vigilagent/internal/database"
)

// Skill represents a skill record in the database.
type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	Version     string    `json:"version"`
	Category    string    `json:"category,omitempty"`
	Downloads   int       `json:"downloads"`
	Rating      float64   `json:"rating"`
	RatingCount int       `json:"rating_count"`
	Permissions []string  `json:"permissions,omitempty"`
	Manifest    []byte    `json:"-"`
	IsVerified  bool      `json:"verified"`
	IsPublished bool      `json:"published"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SkillInstallation represents an installed skill.
type SkillInstallation struct {
	ID        string    `json:"id"`
	SkillID   string    `json:"skill_id"`
	UserID    string    `json:"user_id"`
	ProjectID string    `json:"project_id,omitempty"`
	Status    string    `json:"status"`
	Config    []byte    `json:"-"`
	InstalledAt time.Time `json:"installed_at"`
}

// SkillRating represents a user rating for a skill.
type SkillRating struct {
	ID        string    `json:"id"`
	SkillID   string    `json:"skill_id"`
	UserID    string    `json:"user_id"`
	Rating    int       `json:"rating"`
	Review    string    `json:"review,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SkillRepository handles database operations for skills.
type SkillRepository struct {
	pool *database.Conn
}

// NewSkillRepository creates a new skill repository.
func NewSkillRepository(pool *database.Conn) *SkillRepository {
	return &SkillRepository{pool: pool}
}

// Create inserts a new skill into the database.
func (r *SkillRepository) Create(ctx context.Context, skill *Skill) error {
	query := `
		INSERT INTO skills (name, description, author, version, category, permissions, manifest)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, downloads, rating, rating_count, is_verified, is_published, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		skill.Name, skill.Description, skill.Author, skill.Version,
		skill.Category, skill.Permissions, skill.Manifest,
	).Scan(&skill.ID, &skill.Downloads, &skill.Rating, &skill.RatingCount,
		&skill.IsVerified, &skill.IsPublished, &skill.CreatedAt, &skill.UpdatedAt)
}

// FindByID retrieves a skill by ID.
func (r *SkillRepository) FindByID(ctx context.Context, id string) (*Skill, error) {
	query := `
		SELECT id, name, description, author, version, category, downloads, rating,
		       rating_count, permissions, manifest, is_verified, is_published, created_at, updated_at
		FROM skills WHERE id = $1
	`
	skill := &Skill{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&skill.ID, &skill.Name, &skill.Description, &skill.Author, &skill.Version,
		&skill.Category, &skill.Downloads, &skill.Rating, &skill.RatingCount,
		&skill.Permissions, &skill.Manifest, &skill.IsVerified, &skill.IsPublished,
		&skill.CreatedAt, &skill.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("skill not found")
		}
		return nil, fmt.Errorf("failed to find skill: %w", err)
	}
	return skill, nil
}

// List lists skills with optional filtering.
func (r *SkillRepository) List(ctx context.Context, category, sortBy string, offset, limit int) ([]Skill, int, error) {
	where := "WHERE is_published = true"
	args := []interface{}{}
	argIdx := 1

	if category != "" {
		where += fmt.Sprintf(" AND category = $%d", argIdx)
		args = append(args, category)
		argIdx++
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM skills %s", where)
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count skills: %w", err)
	}

	orderClause := "ORDER BY created_at DESC"
	switch sortBy {
	case "downloads":
		orderClause = "ORDER BY downloads DESC"
	case "rating":
		orderClause = "ORDER BY rating DESC"
	case "name":
		orderClause = "ORDER BY name ASC"
	}

	query := fmt.Sprintf(`
		SELECT id, name, description, author, version, category, downloads, rating,
		       rating_count, permissions, is_verified, is_published, created_at, updated_at
		FROM skills %s
		%s
		LIMIT $%d OFFSET $%d
	`, where, orderClause, argIdx, argIdx+1)

	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list skills: %w", err)
	}
	defer rows.Close()

	var skills []Skill
	for rows.Next() {
		var s Skill
		if err := rows.Scan(
			&s.ID, &s.Name, &s.Description, &s.Author, &s.Version,
			&s.Category, &s.Downloads, &s.Rating, &s.RatingCount,
			&s.Permissions, &s.IsVerified, &s.IsPublished,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan skill: %w", err)
		}
		skills = append(skills, s)
	}
	return skills, total, nil
}

// Update updates a skill's fields.
func (r *SkillRepository) Update(ctx context.Context, id, name, description, version, category string) error {
	query := `UPDATE skills SET name=$2, description=$3, version=$4, category=$5, updated_at=NOW() WHERE id=$1`
	_, err := r.pool.Exec(ctx, query, id, name, description, version, category)
	return err
}

// Delete removes a skill by ID.
func (r *SkillRepository) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM skills WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// IncrementDownloads increments the download counter.
func (r *SkillRepository) IncrementDownloads(ctx context.Context, id string) error {
	query := `UPDATE skills SET downloads = downloads + 1, updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, id)
	return err
}

// AddRating adds a rating to a skill.
func (r *SkillRepository) AddRating(ctx context.Context, rating *SkillRating) error {
	query := `
		INSERT INTO skill_ratings (skill_id, user_id, rating, review)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`
	err := r.pool.QueryRow(ctx, query,
		rating.SkillID, rating.UserID, rating.Rating, rating.Review,
	).Scan(&rating.ID, &rating.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to add rating: %w", err)
	}

	// Update aggregate rating
	updateQuery := `
		UPDATE skills SET
			rating = (SELECT COALESCE(AVG(rating), 0) FROM skill_ratings WHERE skill_id = $1),
			rating_count = (SELECT COUNT(*) FROM skill_ratings WHERE skill_id = $1),
			updated_at = NOW()
		WHERE id = $1
	`
	_, _ = r.pool.Exec(ctx, updateQuery, rating.SkillID)
	return nil
}

// ListRatings lists ratings for a skill.
func (r *SkillRepository) ListRatings(ctx context.Context, skillID string, offset, limit int) ([]SkillRating, int, error) {
	countQuery := `SELECT COUNT(*) FROM skill_ratings WHERE skill_id = $1`
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, skillID).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT id, skill_id, user_id, rating, review, created_at
		FROM skill_ratings WHERE skill_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, skillID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var ratings []SkillRating
	for rows.Next() {
		var sr SkillRating
		if err := rows.Scan(&sr.ID, &sr.SkillID, &sr.UserID, &sr.Rating, &sr.Review, &sr.CreatedAt); err != nil {
			return nil, 0, err
		}
		ratings = append(ratings, sr)
	}
	return ratings, total, nil
}

// Install creates a skill installation record.
func (r *SkillRepository) Install(ctx context.Context, inst *SkillInstallation) error {
	query := `
		INSERT INTO skill_installations (skill_id, user_id, project_id, status, config)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, installed_at
	`
	return r.pool.QueryRow(ctx, query,
		inst.SkillID, inst.UserID, inst.ProjectID, inst.Status, inst.Config,
	).Scan(&inst.ID, &inst.InstalledAt)
}
