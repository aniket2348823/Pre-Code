package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/database"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
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
	return pool
}

func setupTestDBConn(t *testing.T) *database.Conn {
	t.Helper()
	return database.NewConn(setupTestDB(t))
}

func cleanupOrg(t *testing.T, pool *pgxpool.Pool, id string) {
	pool.Exec(context.Background(), "DELETE FROM organization_members WHERE organization_id = $1", id)
	pool.Exec(context.Background(), "DELETE FROM organizations WHERE id = $1", id)
}

func TestOrganizationRepository_Create(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)

	t.Run("creates org and returns id and timestamps", func(t *testing.T) {
		o := &Organization{
			Name:        "Test Org",
			Slug:        "test-org",
			Description: "A test org",
			OwnerID:     "00000000-0000-0000-0000-000000000001",
			Plan:        "free",
		}
		err := r.Create(context.Background(), o)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		defer cleanupOrg(t, conn.Pool(), o.ID)

		if o.ID == "" {
			t.Error("expected non-empty ID")
		}
		if o.CreatedAt.IsZero() {
			t.Error("expected non-zero CreatedAt")
		}
		if o.UpdatedAt.IsZero() {
			t.Error("expected non-zero UpdatedAt")
		}
	})

	t.Run("duplicate slug returns error", func(t *testing.T) {
		o1 := &Organization{Name: "Org A", Slug: "dup-slug", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
		r.Create(context.Background(), o1)
		defer cleanupOrg(t, conn.Pool(), o1.ID)

		o2 := &Organization{Name: "Org B", Slug: "dup-slug", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
		err := r.Create(context.Background(), o2)
		if err == nil {
			cleanupOrg(t, conn.Pool(), o2.ID)
			t.Fatal("expected error for duplicate slug")
		}
	})
}

func TestOrganizationRepository_FindByID(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)

	t.Run("finds existing org", func(t *testing.T) {
		o := &Organization{Name: "Find Me", Slug: "find-me", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
		r.Create(context.Background(), o)
		defer cleanupOrg(t, conn.Pool(), o.ID)

		found, err := r.FindByID(context.Background(), o.ID)
		if err != nil {
			t.Fatalf("FindByID failed: %v", err)
		}
		if found.Name != "Find Me" {
			t.Errorf("expected name 'Find Me', got %q", found.Name)
		}
		if found.Slug != "find-me" {
			t.Errorf("expected slug 'find-me', got %q", found.Slug)
		}
	})

	t.Run("returns error for non-existent id", func(t *testing.T) {
		_, err := r.FindByID(context.Background(), "00000000-0000-0000-0000-999999999999")
		if err == nil {
			t.Fatal("expected error for non-existent org")
		}
	})
}

func TestOrganizationRepository_FindBySlug(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)

	o := &Organization{Name: "Slug Test", Slug: "slug-test", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
	r.Create(context.Background(), o)
	defer cleanupOrg(t, conn.Pool(), o.ID)

	found, err := r.FindBySlug(context.Background(), "slug-test")
	if err != nil {
		t.Fatalf("FindBySlug failed: %v", err)
	}
	if found.ID != o.ID {
		t.Errorf("expected ID %s, got %s", o.ID, found.ID)
	}
}

func TestOrganizationRepository_Update(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)

	o := &Organization{Name: "Original", Slug: "update-test", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
	r.Create(context.Background(), o)
	defer cleanupOrg(t, conn.Pool(), o.ID)

	err := r.Update(context.Background(), o.ID, "Updated Name", "New desc", "pro", map[string]interface{}{"key": "val"})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	found, _ := r.FindByID(context.Background(), o.ID)
	if found.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", found.Name)
	}
	if found.Plan != "pro" {
		t.Errorf("expected plan 'pro', got %q", found.Plan)
	}
}

func TestOrganizationRepository_Delete(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)

	o := &Organization{Name: "Delete Me", Slug: "delete-test", OwnerID: "00000000-0000-0000-0000-000000000001", Plan: "free"}
	r.Create(context.Background(), o)

	err := r.Delete(context.Background(), o.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = r.FindByID(context.Background(), o.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestOrganizationRepository_ListByUser(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)
	ownerID := "00000000-0000-0000-0000-000000000099"

	o1 := &Organization{Name: "Owned Org", Slug: "owned-list", OwnerID: ownerID, Plan: "free"}
	r.Create(context.Background(), o1)
	defer cleanupOrg(t, conn.Pool(), o1.ID)

	orgs, err := r.ListByUser(context.Background(), ownerID)
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(orgs) == 0 {
		t.Fatal("expected at least 1 org")
	}
	found := false
	for _, o := range orgs {
		if o.ID == o1.ID {
			found = true
		}
	}
	if !found {
		t.Error("created org not found in list")
	}
}

func TestOrganizationRepository_Membership(t *testing.T) {
	conn := setupTestDBConn(t)
	defer conn.Close()
	r := NewOrganizationRepository(conn)
	ownerID := "00000000-0000-0000-0000-000000000001"
	memberID := "00000000-0000-0000-0000-000000000002"

	o := &Organization{Name: "Member Org", Slug: "member-test", OwnerID: ownerID, Plan: "free"}
	r.Create(context.Background(), o)
	defer cleanupOrg(t, conn.Pool(), o.ID)

	t.Run("owner is owner and member", func(t *testing.T) {
		isOwner, _ := r.IsOwner(context.Background(), o.ID, ownerID)
		if !isOwner {
			t.Error("expected owner to be owner")
		}
		isMember, _ := r.IsMember(context.Background(), o.ID, ownerID)
		if !isMember {
			t.Error("expected owner to be member")
		}
	})

	t.Run("non-member is not member", func(t *testing.T) {
		isMember, _ := r.IsMember(context.Background(), o.ID, memberID)
		if isMember {
			t.Error("expected non-member to not be member")
		}
	})

	t.Run("add and remove member", func(t *testing.T) {
		err := r.AddMember(context.Background(), o.ID, memberID, "member")
		if err != nil {
			t.Fatalf("AddMember failed: %v", err)
		}

		isMember, _ := r.IsMember(context.Background(), o.ID, memberID)
		if !isMember {
			t.Error("expected to be member after add")
		}

		err = r.RemoveMember(context.Background(), o.ID, memberID)
		if err != nil {
			t.Fatalf("RemoveMember failed: %v", err)
		}

		isMember, _ = r.IsMember(context.Background(), o.ID, memberID)
		if isMember {
			t.Error("expected not to be member after remove")
		}
	})

	t.Run("add member updates role on conflict", func(t *testing.T) {
		r.AddMember(context.Background(), o.ID, memberID, "member")
		err := r.AddMember(context.Background(), o.ID, memberID, "admin")
		if err != nil {
			t.Fatalf("AddMember update failed: %v", err)
		}
		r.RemoveMember(context.Background(), o.ID, memberID)
	})
}
