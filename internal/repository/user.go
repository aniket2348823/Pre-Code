package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User represents a user record in the database.
type User struct {
	ID          string     `json:"id"`
	Email       string     `json:"email"`
	PasswordHash string    `json:"-"`
	Name        string     `json:"name"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	Role        string     `json:"role"`
	IsActive    bool       `json:"is_active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// UserRepository handles database operations for users.
type UserRepository struct {
	pool *pgxpool.Pool
}

// NewUserRepository creates a new user repository.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

// Create inserts a new user into the database.
func (r *UserRepository) Create(ctx context.Context, user *User) error {
	query := `
		INSERT INTO users (email, password_hash, name, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`
	return r.pool.QueryRow(ctx, query,
		user.Email, user.PasswordHash, user.Name, user.Role,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

// FindByEmail retrieves a user by email address.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, name, avatar_url, role,
		       is_active, last_login_at, created_at, updated_at
		FROM users WHERE email = $1
	`
	user := &User{}
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name,
		&user.AvatarURL, &user.Role, &user.IsActive,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}
	return user, nil
}

// FindByID retrieves a user by ID.
func (r *UserRepository) FindByID(ctx context.Context, id string) (*User, error) {
	query := `
		SELECT id, email, password_hash, name, avatar_url, role,
		       is_active, last_login_at, created_at, updated_at
		FROM users WHERE id = $1
	`
	user := &User{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Name,
		&user.AvatarURL, &user.Role, &user.IsActive,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, fmt.Errorf("failed to find user: %w", err)
	}
	return user, nil
}

// UpdateLastLogin updates the last_login_at timestamp.
func (r *UserRepository) UpdateLastLogin(ctx context.Context, userID string) error {
	query := `UPDATE users SET last_login_at = NOW(), updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, userID)
	return err
}

// UpdateProfile updates user profile fields.
func (r *UserRepository) UpdateProfile(ctx context.Context, userID, name, avatarURL string) error {
	query := `UPDATE users SET name = $2, avatar_url = $3, updated_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, userID, name, avatarURL)
	return err
}
