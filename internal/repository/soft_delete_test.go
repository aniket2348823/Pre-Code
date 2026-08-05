//go:build ignore

package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/database"
)

func setupTestDBForSoftDelete(t *testing.T) (*pgxpool.Pool, string) {
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

	orgRepo := NewOrganizationRepository(database.NewConn(pool))
	o := &Organization{Name: "SoftDelete Test Org", Slug: "sd-test-org", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
	if err := orgRepo.Create(context.Background(), o); err != nil {
		pool.Close()
		t.Fatalf("failed to create test org: %v", err)
	}

	return pool, o.ID
}

func cleanupSoftDeleteTestData(pool *pgxpool.Pool, orgID string) {
	pool.Exec(context.Background(), "DELETE FROM tasks WHERE project_id IN (SELECT id FROM projects WHERE org_id = $1)", orgID)
	pool.Exec(context.Background(), "DELETE FROM sessions WHERE project_id IN (SELECT id FROM projects WHERE org_id = $1)", orgID)
	pool.Exec(context.Background(), "DELETE FROM agents WHERE project_id IN (SELECT id FROM projects WHERE org_id = $1)", orgID)
	pool.Exec(context.Background(), "DELETE FROM projects WHERE org_id = $1", orgID)
	pool.Exec(context.Background(), "DELETE FROM organization_members WHERE organization_id = $1", orgID)
	pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", orgID)
}

// --- User Soft Delete Tests ---

func TestUserRepository_SoftDelete(t *testing.T) {
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
	defer pool.Close()

	r := NewUserRepository(database.NewConn(pool))
	u := &User{Email: "softdelete-test@example.com", PasswordHash: "hash", Name: "SD User", Role: "user"}
	if err := r.Create(context.Background(), u); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer r.pool.Exec(context.Background(), "DELETE FROM users WHERE email = $1", "softdelete-test@example.com")

	t.Run("soft delete sets deleted_at", func(t *testing.T) {
		if err := r.SoftDelete(context.Background(), u.ID); err != nil {
			t.Fatalf("SoftDelete failed: %v", err)
		}
		_, err := r.FindByID(context.Background(), u.ID)
		if err == nil {
			t.Fatal("expected error after soft delete for FindByID")
		}
	})

	t.Run("restore clears deleted_at", func(t *testing.T) {
		if err := r.Restore(context.Background(), u.ID); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		found, err := r.FindByID(context.Background(), u.ID)
		if err != nil {
			t.Fatalf("FindByID after restore failed: %v", err)
		}
		if found.Name != "SD User" {
			t.Errorf("expected name 'SD User', got %q", found.Name)
		}
	})

	t.Run("soft delete excludes from List", func(t *testing.T) {
		r.SoftDelete(context.Background(), u.ID)
		users, err := r.List(context.Background(), 0, 100)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		for _, usr := range users {
			if usr.ID == u.ID {
				t.Error("soft-deleted user should not appear in List")
			}
		}
		r.Restore(context.Background(), u.ID)
	})

	t.Run("soft delete excludes from FindByEmail", func(t *testing.T) {
		r.SoftDelete(context.Background(), u.ID)
		_, err := r.FindByEmail(context.Background(), "softdelete-test@example.com")
		if err == nil {
			t.Error("expected error for soft-deleted user FindByEmail")
		}
		r.Restore(context.Background(), u.ID)
	})
}

// --- Project Soft Delete & Concurrency Tests ---

func TestProjectRepository_SoftDelete(t *testing.T) {
	pool, orgID := setupTestDBForSoftDelete(t)
	defer pool.Close()
	defer cleanupSoftDeleteTestData(pool, orgID)
	r := NewProjectRepository(database.NewConn(pool))

	p := &Project{OrgID: orgID, Name: "SD Project", Status: "active"}
	if err := r.Create(context.Background(), p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	t.Run("soft delete then restore", func(t *testing.T) {
		if err := r.SoftDelete(context.Background(), p.ID); err != nil {
			t.Fatalf("SoftDelete failed: %v", err)
		}
		_, err := r.FindByID(context.Background(), p.ID)
		if err == nil {
			t.Fatal("expected error after soft delete")
		}

		if err := r.Restore(context.Background(), p.ID); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		found, err := r.FindByID(context.Background(), p.ID)
		if err != nil {
			t.Fatalf("FindByID after restore failed: %v", err)
		}
		if found.Name != "SD Project" {
			t.Errorf("expected name 'SD Project', got %q", found.Name)
		}
	})

	t.Run("soft delete excludes from ListByOrg", func(t *testing.T) {
		r.SoftDelete(context.Background(), p.ID)
		projects, err := r.ListByOrg(context.Background(), orgID)
		if err != nil {
			t.Fatalf("ListByOrg failed: %v", err)
		}
		for _, proj := range projects {
			if proj.ID == p.ID {
				t.Error("soft-deleted project should not appear in ListByOrg")
			}
		}
		r.Restore(context.Background(), p.ID)
	})
}

func TestProjectRepository_OptimisticConcurrency(t *testing.T) {
	pool, orgID := setupTestDBForSoftDelete(t)
	defer pool.Close()
	defer cleanupSoftDeleteTestData(pool, orgID)
	r := NewProjectRepository(database.NewConn(pool))

	p := &Project{OrgID: orgID, Name: "Concurrency Project", Status: "active"}
	if err := r.Create(context.Background(), p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	t.Run("update succeeds with correct version", func(t *testing.T) {
		err := r.Update(context.Background(), p.ID, "Updated V1", "desc", "active", 1)
		if err != nil {
			t.Fatalf("Update with correct version failed: %v", err)
		}
		found, _ := r.FindByID(context.Background(), p.ID)
		if found.Version != 2 {
			t.Errorf("expected version 2 after update, got %d", found.Version)
		}
	})

	t.Run("update fails with stale version", func(t *testing.T) {
		err := r.Update(context.Background(), p.ID, "Stale Update", "desc", "active", 1)
		if err == nil {
			t.Fatal("expected error for stale version update")
		}
	})

	t.Run("update succeeds with new version", func(t *testing.T) {
		err := r.Update(context.Background(), p.ID, "Updated V2", "desc", "active", 2)
		if err != nil {
			t.Fatalf("Update with version 2 failed: %v", err)
		}
		found, _ := r.FindByID(context.Background(), p.ID)
		if found.Version != 3 {
			t.Errorf("expected version 3, got %d", found.Version)
		}
	})
}

// --- Agent Soft Delete & Concurrency Tests ---

func TestAgentRepository_SoftDelete(t *testing.T) {
	pool, orgID := setupTestDBForSoftDelete(t)
	defer pool.Close()
	defer cleanupSoftDeleteTestData(pool, orgID)
	projRepo := NewProjectRepository(database.NewConn(pool))
	agentRepo := NewAgentRepository(database.NewConn(pool))

	p := &Project{OrgID: orgID, Name: "Agent Test Project", Status: "active"}
	projRepo.Create(context.Background(), p)

	a := &Agent{ProjectID: p.ID, Name: "SD Agent", Status: "idle"}
	if err := agentRepo.Create(context.Background(), a); err != nil {
		t.Fatalf("Create agent failed: %v", err)
	}

	t.Run("soft delete then restore", func(t *testing.T) {
		if err := agentRepo.SoftDelete(context.Background(), a.ID); err != nil {
			t.Fatalf("SoftDelete failed: %v", err)
		}
		_, err := agentRepo.FindByID(context.Background(), a.ID)
		if err == nil {
			t.Fatal("expected error after soft delete")
		}

		if err := agentRepo.Restore(context.Background(), a.ID); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		found, err := agentRepo.FindByID(context.Background(), a.ID)
		if err != nil {
			t.Fatalf("FindByID after restore failed: %v", err)
		}
		if found.Name != "SD Agent" {
			t.Errorf("expected name 'SD Agent', got %q", found.Name)
		}
	})
}

func TestAgentRepository_OptimisticConcurrency(t *testing.T) {
	pool, orgID := setupTestDBForSoftDelete(t)
	defer pool.Close()
	defer cleanupSoftDeleteTestData(pool, orgID)
	projRepo := NewProjectRepository(database.NewConn(pool))
	agentRepo := NewAgentRepository(database.NewConn(pool))

	p := &Project{OrgID: orgID, Name: "Agent Concurrency Project", Status: "active"}
	projRepo.Create(context.Background(), p)

	a := &Agent{ProjectID: p.ID, Name: "Concurrency Agent", Status: "idle", Config: map[string]interface{}{"model": "gpt-4"}}
	if err := agentRepo.Create(context.Background(), a); err != nil {
		t.Fatalf("Create agent failed: %v", err)
	}

	t.Run("update succeeds with correct version", func(t *testing.T) {
		err := agentRepo.Update(context.Background(), a.ID, "Updated V1", "desc", "active", a.Config, 1)
		if err != nil {
			t.Fatalf("Update with correct version failed: %v", err)
		}
		found, _ := agentRepo.FindByID(context.Background(), a.ID)
		if found.Version != 2 {
			t.Errorf("expected version 2, got %d", found.Version)
		}
	})

	t.Run("update fails with stale version", func(t *testing.T) {
		err := agentRepo.Update(context.Background(), a.ID, "Stale", "desc", "active", a.Config, 1)
		if err == nil {
			t.Fatal("expected error for stale version")
		}
	})
}

// --- Session Soft Delete Tests ---

func TestSessionRepository_SoftDelete(t *testing.T) {
	pool, orgID := setupTestDBForSoftDelete(t)
	defer pool.Close()
	defer cleanupSoftDeleteTestData(pool, orgID)
	projRepo := NewProjectRepository(database.NewConn(pool))
	sessRepo := NewSessionRepository(database.NewConn(pool))

	p := &Project{OrgID: orgID, Name: "Session Test Project", Status: "active"}
	projRepo.Create(context.Background(), p)

	s := &Session{ProjectID: p.ID, Status: "active"}
	if err := sessRepo.Create(context.Background(), s); err != nil {
		t.Fatalf("Create session failed: %v", err)
	}

	t.Run("soft delete then restore", func(t *testing.T) {
		if err := sessRepo.SoftDelete(context.Background(), s.ID); err != nil {
			t.Fatalf("SoftDelete failed: %v", err)
		}
		_, err := sessRepo.FindByID(context.Background(), s.ID)
		if err == nil {
			t.Fatal("expected error after soft delete")
		}

		if err := sessRepo.Restore(context.Background(), s.ID); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		_, err = sessRepo.FindByID(context.Background(), s.ID)
		if err != nil {
			t.Fatalf("FindByID after restore failed: %v", err)
		}
	})
}

// --- Task Soft Delete Tests ---

func TestTaskRepository_SoftDelete(t *testing.T) {
	pool, orgID := setupTestDBForSoftDelete(t)
	defer pool.Close()
	defer cleanupSoftDeleteTestData(pool, orgID)
	projRepo := NewProjectRepository(database.NewConn(pool))
	taskRepo := NewTaskRepository(database.NewConn(pool))

	p := &Project{OrgID: orgID, Name: "Task Test Project", Status: "active"}
	projRepo.Create(context.Background(), p)

	task := &Task{ProjectID: p.ID, UserID: "00000000-0000-0000-0000-000000000001", Prompt: "test task", Status: "pending"}
	if err := taskRepo.Create(context.Background(), task); err != nil {
		t.Fatalf("Create task failed: %v", err)
	}

	t.Run("soft delete then restore", func(t *testing.T) {
		if err := taskRepo.SoftDelete(context.Background(), task.ID); err != nil {
			t.Fatalf("SoftDelete failed: %v", err)
		}
		_, err := taskRepo.FindByID(context.Background(), task.ID)
		if err == nil {
			t.Fatal("expected error after soft delete")
		}

		if err := taskRepo.Restore(context.Background(), task.ID); err != nil {
			t.Fatalf("Restore failed: %v", err)
		}
		found, err := taskRepo.FindByID(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("FindByID after restore failed: %v", err)
		}
		if found.Prompt != "test task" {
			t.Errorf("expected prompt 'test task', got %q", found.Prompt)
		}
	})

	t.Run("soft delete excludes from ListByProject", func(t *testing.T) {
		taskRepo.SoftDelete(context.Background(), task.ID)
		tasks, _, err := taskRepo.ListByProject(context.Background(), p.ID, 0, 100)
		if err != nil {
			t.Fatalf("ListByProject failed: %v", err)
		}
		for _, tk := range tasks {
			if tk.ID == task.ID {
				t.Error("soft-deleted task should not appear in ListByProject")
			}
		}
		taskRepo.Restore(context.Background(), task.ID)
	})
}
