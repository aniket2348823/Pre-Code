package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/repository"
)

func ensureIntegrationTest(t *testing.T) {
	t.Helper()
	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("INTEGRATION_TEST not set to \"1\", skipping integration test")
	}
}

func createTestOrg(t *testing.T, conn *database.Conn) string {
	t.Helper()
	ctx := context.Background()
	orgRepo := repository.NewOrganizationRepository(conn)
	org := &repository.Organization{
		Name:    fmt.Sprintf("Test Org %d", time.Now().UnixNano()),
		Slug:    fmt.Sprintf("test-org-%d", time.Now().UnixNano()),
		OwnerID: "a0000000-0000-0000-0000-000000000001",
		Plan:    "free",
	}
	if err := orgRepo.Create(ctx, org); err != nil {
		t.Fatalf("createTestOrg: %v", err)
	}
	t.Cleanup(func() {
		orgRepo.Delete(ctx, org.ID)
	})
	return org.ID
}

// ---------------------------------------------------------------------------
// TestUserCRUD_Integration
// ---------------------------------------------------------------------------

func TestUserCRUD_Integration(t *testing.T) {
	ensureIntegrationTest(t)

	db := SetupTestDB(t)
	defer db.Close()

	conn := database.NewConn(db.Pool)
	userRepo := repository.NewUserRepository(conn)
	ctx := context.Background()

	t.Run("create and find", func(t *testing.T) {
		u := &repository.User{
			Email:        fmt.Sprintf("user-crud-%d@example.com", time.Now().UnixNano()),
			PasswordHash: "test_hash_123",
			Name:         "CRUD User",
			Role:         "user",
		}
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if u.ID == "" {
			t.Fatal("expected non-empty user ID")
		}

		found, err := userRepo.FindByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if found.Email != u.Email {
			t.Errorf("email mismatch: got %q, want %q", found.Email, u.Email)
		}
		if found.Name != "CRUD User" {
			t.Errorf("name mismatch: got %q, want %q", found.Name, "CRUD User")
		}

		found2, err := userRepo.FindByEmail(ctx, u.Email)
		if err != nil {
			t.Fatalf("FindByEmail: %v", err)
		}
		if found2.ID != u.ID {
			t.Errorf("FindByEmail returned wrong user: got %q, want %q", found2.ID, u.ID)
		}

		userRepo.Delete(ctx, u.ID)
	})

	t.Run("update profile", func(t *testing.T) {
		u := &repository.User{
			Email:        fmt.Sprintf("user-update-%d@example.com", time.Now().UnixNano()),
			PasswordHash: "test_hash_123",
			Name:         "Original Name",
			Role:         "user",
		}
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		defer userRepo.Delete(ctx, u.ID)

		if err := userRepo.UpdateProfile(ctx, u.ID, "Updated Name", "https://avatar.test/new.png"); err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}

		found, err := userRepo.FindByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if found.Name != "Updated Name" {
			t.Errorf("expected updated name, got %q", found.Name)
		}
		if found.AvatarURL != "https://avatar.test/new.png" {
			t.Errorf("expected avatar URL, got %q", found.AvatarURL)
		}
	})

	t.Run("update password", func(t *testing.T) {
		u := &repository.User{
			Email:        fmt.Sprintf("user-pw-%d@example.com", time.Now().UnixNano()),
			PasswordHash: "old_hash",
			Name:         "PW User",
			Role:         "user",
		}
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		defer userRepo.Delete(ctx, u.ID)

		if err := userRepo.UpdatePassword(ctx, u.ID, "new_hash_456"); err != nil {
			t.Fatalf("UpdatePassword: %v", err)
		}

		found, err := userRepo.FindByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if found.PasswordHash != "new_hash_456" {
			t.Errorf("expected updated password hash, got %q", found.PasswordHash)
		}
	})

	t.Run("list and count", func(t *testing.T) {
		var ids []string
		for i := 0; i < 3; i++ {
			u := &repository.User{
				Email:        fmt.Sprintf("user-list-%d-%d@example.com", time.Now().UnixNano(), i),
				PasswordHash: "hash",
				Name:         fmt.Sprintf("List User %d", i),
				Role:         "user",
			}
			if err := userRepo.Create(ctx, u); err != nil {
				t.Fatalf("Create: %v", err)
			}
			ids = append(ids, u.ID)
		}
		defer func() {
			for _, id := range ids {
				userRepo.Delete(ctx, id)
			}
		}()

		count, err := userRepo.Count(ctx)
		if err != nil {
			t.Fatalf("Count: %v", err)
		}
		if count < 3 {
			t.Errorf("expected at least 3 users, got %d", count)
		}

		users, err := userRepo.List(ctx, 0, 100)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(users) < 3 {
			t.Errorf("expected at least 3 users in list, got %d", len(users))
		}
	})

	t.Run("find nonexistent returns error", func(t *testing.T) {
		_, err := userRepo.FindByID(ctx, "00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent user")
		}
	})
}

// ---------------------------------------------------------------------------
// TestProjectCRUD_Integration
// ---------------------------------------------------------------------------

func TestProjectCRUD_Integration(t *testing.T) {
	ensureIntegrationTest(t)

	db := SetupTestDB(t)
	defer db.Close()

	conn := database.NewConn(db.Pool)
	orgID := createTestOrg(t, conn)
	projectRepo := repository.NewProjectRepository(conn)
	ctx := context.Background()

	t.Run("create and find", func(t *testing.T) {
		p := &repository.Project{
			OrgID:       orgID,
			Name:        "Integration Project",
			Description: "Test description",
			Status:      "active",
		}
		if err := projectRepo.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if p.ID == "" {
			t.Fatal("expected non-empty project ID")
		}

		found, err := projectRepo.FindByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if found.Name != "Integration Project" {
			t.Errorf("name mismatch: got %q, want %q", found.Name, "Integration Project")
		}
		if found.OrgID != orgID {
			t.Errorf("org_id mismatch: got %q, want %q", found.OrgID, orgID)
		}

		projectRepo.Delete(ctx, p.ID)
	})

	t.Run("list by org", func(t *testing.T) {
		var ids []string
		for i := 0; i < 2; i++ {
			p := &repository.Project{
				OrgID:  orgID,
				Name:   fmt.Sprintf("List Project %d", i),
				Status: "active",
			}
			if err := projectRepo.Create(ctx, p); err != nil {
				t.Fatalf("Create: %v", err)
			}
			ids = append(ids, p.ID)
		}
		defer func() {
			for _, id := range ids {
				projectRepo.Delete(ctx, id)
			}
		}()

		projects, err := projectRepo.ListByOrg(ctx, orgID)
		if err != nil {
			t.Fatalf("ListByOrg: %v", err)
		}
		if len(projects) < 2 {
			t.Errorf("expected at least 2 projects, got %d", len(projects))
		}
	})

	t.Run("update project", func(t *testing.T) {
		p := &repository.Project{
			OrgID:  orgID,
			Name:   "Update Project",
			Status: "active",
		}
		if err := projectRepo.Create(ctx, p); err != nil {
			t.Fatalf("Create: %v", err)
		}
		defer projectRepo.Delete(ctx, p.ID)

		if err := projectRepo.Update(ctx, p.ID, "Updated Name", "new desc", "active"); err != nil {
			t.Fatalf("Update: %v", err)
		}

		found, err := projectRepo.FindByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if found.Name != "Updated Name" {
			t.Errorf("expected updated name, got %q", found.Name)
		}
		if found.Description != "new desc" {
			t.Errorf("expected updated description, got %q", found.Description)
		}
	})

	t.Run("update nonexistent returns error", func(t *testing.T) {
		err := projectRepo.Update(ctx, "00000000-0000-0000-0000-000000000000", "Nope", "", "active")
		if err == nil {
			t.Error("expected error updating nonexistent project")
		}
	})

	t.Run("find nonexistent returns error", func(t *testing.T) {
		_, err := projectRepo.FindByID(ctx, "00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Error("expected error for nonexistent project")
		}
	})
}

// ---------------------------------------------------------------------------
// TestConnectionRetry_Integration
// ---------------------------------------------------------------------------

func TestConnectionRetry_Integration(t *testing.T) {
	ensureIntegrationTest(t)

	db := SetupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	t.Run("retry query succeeds on healthy connection", func(t *testing.T) {
		cfg := database.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
			JitterRatio: 0.1,
		}

		var attempts int
		_, err := database.RetryQuery(ctx, cfg, func(ctx context.Context) (pgx.Rows, error) {
			attempts++
			return db.Pool.Query(ctx, "SELECT 1")
		})
		if err != nil {
			t.Fatalf("RetryQuery: %v", err)
		}
		if attempts != 1 {
			t.Errorf("expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("retry exec succeeds on healthy connection", func(t *testing.T) {
		cfg := database.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    100 * time.Millisecond,
			JitterRatio: 0.1,
		}

		var attempts int
		_, err := database.RetryExec(ctx, cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
			attempts++
			return db.Pool.Exec(ctx, "SELECT 1")
		})
		if err != nil {
			t.Fatalf("RetryExec: %v", err)
		}
		if attempts != 1 {
			t.Errorf("expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("retry gives up on non-retryable error", func(t *testing.T) {
		cfg := database.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    50 * time.Millisecond,
			JitterRatio: 0,
		}

		var attempts int
		_, err := database.RetryQuery(ctx, cfg, func(ctx context.Context) (pgx.Rows, error) {
			attempts++
			return db.Pool.Query(ctx, "SELECT * FROM nonexistent_table_xyz")
		})
		if err == nil {
			t.Error("expected error for invalid query")
		}
		if attempts != 1 {
			t.Errorf("non-retryable error should not retry: expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("retry succeeds after transient failure", func(t *testing.T) {
		cfg := database.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   10 * time.Millisecond,
			MaxDelay:    50 * time.Millisecond,
			JitterRatio: 0,
		}

		var attempts int
		_, err := database.RetryQuery(ctx, cfg, func(ctx context.Context) (pgx.Rows, error) {
			attempts++
			if attempts == 1 {
				return nil, context.DeadlineExceeded
			}
			return db.Pool.Query(ctx, "SELECT 1")
		})
		if err != nil {
			t.Fatalf("RetryQuery: %v", err)
		}
		if attempts != 2 {
			t.Errorf("expected 2 attempts, got %d", attempts)
		}
	})
}
