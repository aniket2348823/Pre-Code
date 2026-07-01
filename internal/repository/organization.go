package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Organization represents an organization record in the database.
type Organization struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Slug        string                 `json:"slug"`
	Description string                 `json:"description,omitempty"`
	OwnerID     string                 `json:"owner_id"`
	Plan        string                 `json:"plan"`
	Settings    map[string]interface{} `json:"settings,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// OrgMember represents a membership record.
type OrgMember struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
}

// OrganizationRepository handles database operations for organizations.
type OrganizationRepository struct {
	pool *pgxpool.Pool
}

// NewOrganizationRepository creates a new organization repository.
func NewOrganizationRepository(pool *pgxpool.Pool) *OrganizationRepository {
	return &OrganizationRepository{pool: pool}
}

// Create inserts a new organization and adds the owner as admin member.
func (r *OrganizationRepository) Create(ctx context.Context, org *Organization) error {
	query := `
		INSERT INTO organizations (name, slug, description, owner_id, plan, settings)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		org.Name, org.Slug, org.Description, org.OwnerID, org.Plan, org.Settings,
	).Scan(&org.ID, &org.CreatedAt, &org.UpdatedAt)
}

// FindByID retrieves an organization by ID.
func (r *OrganizationRepository) FindByID(ctx context.Context, id string) (*Organization, error) {
	query := `
		SELECT id, name, slug, description, owner_id, plan, settings, created_at, updated_at
		FROM organizations WHERE id = $1
	`
	org := &Organization{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&org.ID, &org.Name, &org.Slug, &org.Description,
		&org.OwnerID, &org.Plan, &org.Settings,
		&org.CreatedAt, &org.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("organization not found")
		}
		return nil, fmt.Errorf("failed to find organization: %w", err)
	}
	return org, nil
}

// FindBySlug retrieves an organization by slug.
func (r *OrganizationRepository) FindBySlug(ctx context.Context, slug string) (*Organization, error) {
	query := `
		SELECT id, name, slug, description, owner_id, plan, settings, created_at, updated_at
		FROM organizations WHERE slug = $1
	`
	org := &Organization{}
	err := r.pool.QueryRow(ctx, query, slug).Scan(
		&org.ID, &org.Name, &org.Slug, &org.Description,
		&org.OwnerID, &org.Plan, &org.Settings,
		&org.CreatedAt, &org.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("organization not found")
		}
		return nil, fmt.Errorf("failed to find organization: %w", err)
	}
	return org, nil
}

// Update updates organization fields.
func (r *OrganizationRepository) Update(ctx context.Context, id, name, description, plan string, settings map[string]interface{}) error {
	query := `
		UPDATE organizations
		SET name = $2, description = $3, plan = $4, settings = $5, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.pool.Exec(ctx, query, id, name, description, plan, settings)
	return err
}

// Delete removes an organization by ID.
func (r *OrganizationRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	return err
}

// ListByUser returns all organizations a user is a member of (owner or member).
func (r *OrganizationRepository) ListByUser(ctx context.Context, userID string) ([]Organization, error) {
	query := `
		SELECT o.id, o.name, o.slug, o.description, o.owner_id, o.plan, o.settings, o.created_at, o.updated_at
		FROM organizations o
		LEFT JOIN organization_members m ON o.id = m.organization_id
		WHERE o.owner_id = $1 OR m.user_id = $1
		ORDER BY o.created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}
	defer rows.Close()

	var orgs []Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(
			&org.ID, &org.Name, &org.Slug, &org.Description,
			&org.OwnerID, &org.Plan, &org.Settings,
			&org.CreatedAt, &org.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan organization: %w", err)
		}
		orgs = append(orgs, org)
	}
	return orgs, rows.Err()
}

// AddMember adds a user to an organization.
func (r *OrganizationRepository) AddMember(ctx context.Context, orgID, userID, role string) error {
	query := `
		INSERT INTO organization_members (organization_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET role = $3
	`
	_, err := r.pool.Exec(ctx, query, orgID, userID, role)
	return err
}

// RemoveMember removes a user from an organization.
func (r *OrganizationRepository) RemoveMember(ctx context.Context, orgID, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM organization_members WHERE organization_id = $1 AND user_id = $2`,
	orgID, userID)
	return err
}

// IsMember checks if a user is a member (or owner) of an organization.
func (r *OrganizationRepository) IsMember(ctx context.Context, orgID, userID string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM organizations WHERE id = $1 AND owner_id = $2
			UNION
			SELECT 1 FROM organization_members WHERE organization_id = $1 AND user_id = $2
		)
	`
	var exists bool
	err := r.pool.QueryRow(ctx, query, orgID, userID).Scan(&exists)
	return exists, err
}

// IsOwner checks if a user is the owner of an organization.
func (r *OrganizationRepository) IsOwner(ctx context.Context, orgID, userID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND owner_id = $2)`,
		orgID, userID,
	).Scan(&exists)
	return exists, err
}
