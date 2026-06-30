package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func setupTestDBForProject(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// Create a test org to own projects
	orgRepo := NewOrganizationRepository(pool)
	o := &Organization{Name: "Project Test Org", Slug: "proj-test-org", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
	err = orgRepo.Create(context.Background(), o)
	if err != nil {
		pool.Close()
		t.Fatalf("failed to create test org: %v", err)
	}

	return pool, o.ID
}

func cleanupTestProjectOrg(pool *pgxpool.Pool, orgID string) {
	pool.Exec(context.Background(), "DELETE FROM projects WHERE org_id = $1", orgID)
	pool.Exec(context.Background(), "DELETE FROM organization_members WHERE organization_id = $1", orgID)
	pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID)
}

func TestProjectRepository_Create(t *testing.T) {
	pool, orgID := setupTestDBForProject(t)
	defer pool.Close()
	defer cleanupTestProjectOrg(pool, orgID)
	r := NewProjectRepository(pool)

	t.Run("creates project and returns id and timestamps", func(t *testing.T) {
		p := &Project{
			OrgID:       orgID,
			Name:        "Test Project",
			Description: "A test project",
			Status:      "active",
		}
		err := r.Create(context.Background(), p)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if p.ID == "" {
			t.Error("expected non-empty ID")
		}
		if p.CreatedAt.IsZero() {
			t.Error("expected non-zero CreatedAt")
		}
		if p.UpdatedAt.IsZero() {
			t.Error("expected non-zero UpdatedAt")
		}
	})
}

func TestProjectRepository_FindByID(t *testing.T) {
	pool, orgID := setupTestDBForProject(t)
	defer pool.Close()
	defer cleanupTestProjectOrg(pool, orgID)
	r := NewProjectRepository(pool)

	p := &Project{OrgID: orgID, Name: "Find Project", Status: "active"}
	r.Create(context.Background(), p)

	t.Run("finds existing project", func(t *testing.T) {
		found, err := r.FindByID(context.Background(), p.ID)
		if err != nil {
			t.Fatalf("FindByID failed: %v", err)
		}
		if found.Name != "Find Project" {
			t.Errorf("expected name 'Find Project', got %q", found.Name)
		}
		if found.OrgID != orgID {
			t.Errorf("expected org_id %s, got %s", orgID, found.OrgID)
		}
	})

	t.Run("returns error for non-existent id", func(t *testing.T) {
		_, err := r.FindByID(context.Background(), "00000000-0000-0000-0000-999999999999")
		if err == nil {
			t.Fatal("expected error for non-existent project")
		}
	})
}

func TestProjectRepository_Update(t *testing.T) {
	pool, orgID := setupTestDBForProject(t)
	defer pool.Close()
	defer cleanupTestProjectOrg(pool, orgID)
	r := NewProjectRepository(pool)

	p := &Project{OrgID: orgID, Name: "Original", Status: "active"}
	r.Create(context.Background(), p)

	err := r.Update(context.Background(), p.ID, "Updated", "new desc", "archived")
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	found, _ := r.FindByID(context.Background(), p.ID)
	if found.Name != "Updated" {
		t.Errorf("expected name 'Updated', got %q", found.Name)
	}
	if found.Status != "archived" {
		t.Errorf("expected status 'archived', got %q", found.Status)
	}
}

func TestProjectRepository_Delete(t *testing.T) {
	pool, orgID := setupTestDBForProject(t)
	defer pool.Close()
	defer cleanupTestProjectOrg(pool, orgID)
	r := NewProjectRepository(pool)

	p := &Project{OrgID: orgID, Name: "Delete Project", Status: "active"}
	r.Create(context.Background(), p)

	err := r.Delete(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = r.FindByID(context.Background(), p.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestProjectRepository_ListByOrg(t *testing.T) {
	pool, orgID := setupTestDBForProject(t)
	defer pool.Close()
	defer cleanupTestProjectOrg(pool, orgID)
	r := NewProjectRepository(pool)

	r.Create(context.Background(), &Project{OrgID: orgID, Name: "Project A", Status: "active"})
	r.Create(context.Background(), &Project{OrgID: orgID, Name: "Project B", Status: "active"})

	projects, err := r.ListByOrg(context.Background(), orgID)
	if err != nil {
		t.Fatalf("ListByOrg failed: %v", err)
	}
	if len(projects) < 2 {
		t.Errorf("expected at least 2 projects, got %d", len(projects))
	}
}
