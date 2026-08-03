package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistry_NotNil(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
}

func TestRegistry_RegisterOverwrite(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", Manifest: Manifest{Name: "v1"}, IsPublished: true})
	r.Register(&Skill{ID: "s1", Manifest: Manifest{Name: "v2"}, IsPublished: true})
	got, ok := r.Get("s1")
	require.True(t, ok)
	assert.Equal(t, "v2", got.Manifest.Name)
}

func TestRegistry_List_SortedByInstalls(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "low", IsPublished: true, InstallCount: 5, Manifest: Manifest{Category: "cat"}})
	r.Register(&Skill{ID: "high", IsPublished: true, InstallCount: 100, Manifest: Manifest{Category: "cat"}})
	r.Register(&Skill{ID: "mid", IsPublished: true, InstallCount: 50, Manifest: Manifest{Category: "cat"}})

	result := r.List("", 10)
	require.Len(t, result, 3)
	assert.Equal(t, "high", result[0].ID)
	assert.Equal(t, "mid", result[1].ID)
	assert.Equal(t, "low", result[2].ID)
}

func TestRegistry_List_LimitZero(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Category: "cat"}})
	r.Register(&Skill{ID: "s2", IsPublished: true, Manifest: Manifest{Category: "cat"}})
	result := r.List("", 0)
	assert.Len(t, result, 2, "limit 0 means no limit")
}

func TestRegistry_Search_DisplayName(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "tool", DisplayName: "Auth Helper"}})
	result := r.Search("auth", 10)
	assert.Len(t, result, 1)
}

func TestRegistry_Search_CaseInsensitive(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "Auth-Scanner"}})
	result := r.Search("AUTH", 10)
	assert.Len(t, result, 1)
}

func TestRegistry_Search_EmptyQuery(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "x"}})
	result := r.Search("", 10)
	assert.Len(t, result, 1, "empty query should match everything")
}

func TestValidateManifest_AllMissing(t *testing.T) {
	m := &Manifest{}
	err := ValidateManifest(m)
	assert.Error(t, err)
}

func TestLoadManifest_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))
	_, err := LoadManifest(path)
	assert.Error(t, err)
}

func TestGetSkillDir_NestedPath(t *testing.T) {
	got := GetSkillDir("/a/b/c", "skill")
	assert.Equal(t, filepath.Join("/a/b/c", "skill"), got)
}

func TestManifest_AuthorInfo(t *testing.T) {
	m := Manifest{
		Name:   "test",
		Author: AuthorInfo{Name: "tester", Email: "t@e.com", URL: "https://example.com"},
	}
	assert.Equal(t, "tester", m.Author.Name)
	assert.Equal(t, "t@e.com", m.Author.Email)
	assert.Equal(t, "https://example.com", m.Author.URL)
}

func TestManifest_Requirements(t *testing.T) {
	m := Manifest{
		Name: "test",
		Requires: Requirements{
			PlatformVersion: ">=1.0",
			Permissions:     []string{"read"},
			Tools:           []string{"git"},
			Runtime:         "go",
		},
	}
	assert.Equal(t, ">=1.0", m.Requires.PlatformVersion)
	assert.Contains(t, m.Requires.Permissions, "read")
}

func TestSkill_PublishedAt_NilByDefault(t *testing.T) {
	s := Skill{ID: "s1"}
	assert.Nil(t, s.PublishedAt)
}

func TestSkill_PublishedAt_Set(t *testing.T) {
	now := time.Now()
	s := Skill{ID: "s1", PublishedAt: &now}
	require.NotNil(t, s.PublishedAt)
	assert.Equal(t, now, *s.PublishedAt)
}
