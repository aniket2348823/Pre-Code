package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/database"
)


func TestNewSkillRepository(t *testing.T) {
	t.Run("creates repo with pool", func(t *testing.T) {
		conn := database.NewConn(nil)
		r := NewSkillRepository(conn)
		require.NotNil(t, r)
		assert.Equal(t, conn, r.pool)
	})

	t.Run("creates repo with nil conn", func(t *testing.T) {
		r := NewSkillRepository(nil)
		require.NotNil(t, r)
		assert.Nil(t, r.pool)
	})
}

func TestSkill_Struct(t *testing.T) {
	now := time.Now()
	s := Skill{
		ID:          "skill-1",
		Name:        "code-linter",
		Description: "Lints code for issues",
		Author:      "vigilteam",
		Version:     "1.0.0",
		Category:    "quality",
		Downloads:   1500,
		Rating:      4.8,
		RatingCount: 42,
		Permissions: []string{"file_read", "terminal"},
		Manifest:    []byte(`{"name":"code-linter"}`),
		IsVerified:  true,
		IsPublished: true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	assert.Equal(t, "skill-1", s.ID)
	assert.Equal(t, "code-linter", s.Name)
	assert.Equal(t, "Lints code for issues", s.Description)
	assert.Equal(t, "vigilteam", s.Author)
	assert.Equal(t, "1.0.0", s.Version)
	assert.Equal(t, "quality", s.Category)
	assert.Equal(t, 1500, s.Downloads)
	assert.Equal(t, 4.8, s.Rating)
	assert.Equal(t, 42, s.RatingCount)
	assert.Equal(t, []string{"file_read", "terminal"}, s.Permissions)
	assert.Equal(t, []byte(`{"name":"code-linter"}`), s.Manifest)
	assert.True(t, s.IsVerified)
	assert.True(t, s.IsPublished)
}

func TestSkill_Struct_ZeroValues(t *testing.T) {
	s := Skill{}
	assert.Empty(t, s.ID)
	assert.Empty(t, s.Name)
	assert.Empty(t, s.Description)
	assert.Empty(t, s.Author)
	assert.Empty(t, s.Version)
	assert.Empty(t, s.Category)
	assert.Equal(t, 0, s.Downloads)
	assert.Equal(t, 0.0, s.Rating)
	assert.Equal(t, 0, s.RatingCount)
	assert.Nil(t, s.Permissions)
	assert.Nil(t, s.Manifest)
	assert.False(t, s.IsVerified)
	assert.False(t, s.IsPublished)
	assert.True(t, s.CreatedAt.IsZero())
	assert.True(t, s.UpdatedAt.IsZero())
}

func TestSkillInstallation_Struct(t *testing.T) {
	now := time.Now()
	si := SkillInstallation{
		ID:          "inst-1",
		SkillID:     "skill-1",
		UserID:      "user-1",
		ProjectID:   "proj-1",
		Status:      "active",
		Config:      []byte(`{"enabled":true}`),
		InstalledAt: now,
	}

	assert.Equal(t, "inst-1", si.ID)
	assert.Equal(t, "skill-1", si.SkillID)
	assert.Equal(t, "user-1", si.UserID)
	assert.Equal(t, "proj-1", si.ProjectID)
	assert.Equal(t, "active", si.Status)
	assert.Equal(t, []byte(`{"enabled":true}`), si.Config)
}

func TestSkillRating_Struct(t *testing.T) {
	now := time.Now()
	sr := SkillRating{
		ID:        "rating-1",
		SkillID:   "skill-1",
		UserID:    "user-1",
		Rating:    5,
		Review:    "Excellent skill",
		CreatedAt: now,
	}

	assert.Equal(t, "rating-1", sr.ID)
	assert.Equal(t, "skill-1", sr.SkillID)
	assert.Equal(t, "user-1", sr.UserID)
	assert.Equal(t, 5, sr.Rating)
	assert.Equal(t, "Excellent skill", sr.Review)
}

func TestSkillRating_Struct_ZeroValues(t *testing.T) {
	sr := SkillRating{}
	assert.Empty(t, sr.ID)
	assert.Empty(t, sr.SkillID)
	assert.Empty(t, sr.UserID)
	assert.Equal(t, 0, sr.Rating)
	assert.Empty(t, sr.Review)
	assert.True(t, sr.CreatedAt.IsZero())
}

func TestSkillInstallation_Struct_ZeroValues(t *testing.T) {
	si := SkillInstallation{}
	assert.Empty(t, si.ID)
	assert.Empty(t, si.SkillID)
	assert.Empty(t, si.UserID)
	assert.Empty(t, si.ProjectID)
	assert.Empty(t, si.Status)
	assert.Nil(t, si.Config)
	assert.True(t, si.InstalledAt.IsZero())
}

func TestSkillRepository_Create_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_ = r.Create(nil, &Skill{})
	})
}

func TestSkillRepository_FindByID_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_, _ = r.FindByID(nil, "nonexistent")
	})
}

func TestSkillRepository_List_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_, _, _ = r.List(nil, "", "downloads", 0, 10)
	})
}

func TestSkillRepository_Update_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_ = r.Update(nil, "id", "n", "d", "1.0", "cat")
	})
}

func TestSkillRepository_Delete_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_ = r.Delete(nil, "id")
	})
}

func TestSkillRepository_IncrementDownloads_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_ = r.IncrementDownloads(nil, "id")
	})
}

func TestSkillRepository_AddRating_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_ = r.AddRating(nil, &SkillRating{})
	})
}

func TestSkillRepository_ListRatings_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_, _, _ = r.ListRatings(nil, "skill-1", 0, 10)
	})
}

func TestSkillRepository_Install_NilPool(t *testing.T) {
	r := NewSkillRepository(nil)
	assert.Panics(t, func() {
		_ = r.Install(nil, &SkillInstallation{})
	})
}

func TestSkill_WithNilPermissions(t *testing.T) {
	s := Skill{Permissions: nil}
	assert.Nil(t, s.Permissions)
}

func TestSkill_WithEmptyPermissions(t *testing.T) {
	s := Skill{Permissions: []string{}}
	assert.Empty(t, s.Permissions)
}

func TestSkill_WithNilManifest(t *testing.T) {
	s := Skill{Manifest: nil}
	assert.Nil(t, s.Manifest)
}
