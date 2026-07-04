package skills

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.skills) != 0 {
		t.Errorf("expected empty registry, got %d skills", len(r.skills))
	}
}

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	skill := &Skill{
		ID:           "skill-1",
		IsPublished:  true,
		Manifest:     Manifest{Name: "test-skill", Version: "1.0.0", Description: "A test", Category: "security"},
		InstallCount: 10,
		CreatedAt:    time.Now(),
	}

	r.Register(skill)

	got, ok := r.Get("skill-1")
	if !ok {
		t.Fatal("expected to find skill-1")
	}
	if got.Manifest.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", got.Manifest.Name)
	}
}

func TestGet_NotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestList_PublishedOnly(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "pub1", IsPublished: true, Manifest: Manifest{Category: "security"}})
	r.Register(&Skill{ID: "draft1", IsPublished: false, Manifest: Manifest{Category: "security"}})
	r.Register(&Skill{ID: "pub2", IsPublished: true, Manifest: Manifest{Category: "utility"}})

	result := r.List("", 10)
	if len(result) != 2 {
		t.Errorf("expected 2 published skills, got %d", len(result))
	}
}

func TestList_FilterByCategory(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Category: "security"}})
	r.Register(&Skill{ID: "s2", IsPublished: true, Manifest: Manifest{Category: "utility"}})
	r.Register(&Skill{ID: "s3", IsPublished: true, Manifest: Manifest{Category: "security"}})

	result := r.List("security", 10)
	if len(result) != 2 {
		t.Errorf("expected 2 security skills, got %d", len(result))
	}
}

func TestList_Limit(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 5; i++ {
		r.Register(&Skill{ID: string(rune('a' + i)), IsPublished: true, Manifest: Manifest{Category: "cat"}})
	}

	result := r.List("", 3)
	if len(result) != 3 {
		t.Errorf("expected 3 skills with limit, got %d", len(result))
	}
}

func TestSearch(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "auth-scanner", DisplayName: "Auth Scanner", Description: "Scans authentication"}})
	r.Register(&Skill{ID: "s2", IsPublished: true, Manifest: Manifest{Name: "sql-fixer", DisplayName: "SQL Fixer", Description: "Fixes SQL injection"}})
	r.Register(&Skill{ID: "s3", IsPublished: false, Manifest: Manifest{Name: "auth-helper", Description: "Hidden auth tool"}})

	result := r.Search("auth", 10)
	// Should find s1 (published) but not s3 (unpublished)
	if len(result) != 1 {
		t.Errorf("expected 1 published auth skill, got %d", len(result))
	}
}

func TestSearch_MatchDescription(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "tool-x", Description: "SQL injection scanner"}})

	result := r.Search("sql", 10)
	if len(result) != 1 {
		t.Errorf("expected 1 match, got %d", len(result))
	}
}

func TestSearch_Limit(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 5; i++ {
		r.Register(&Skill{ID: string(rune('a' + i)), IsPublished: true, Manifest: Manifest{Name: "auth-tool", Description: "auth tool"}})
	}

	result := r.Search("auth", 2)
	if len(result) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(result))
	}
}

func TestValidateManifest(t *testing.T) {
	valid := &Manifest{Name: "test", Version: "1.0", Description: "desc", Category: "cat"}
	if err := ValidateManifest(valid); err != nil {
		t.Errorf("expected valid manifest, got %v", err)
	}

	tests := []struct {
		name    string
		manifest *Manifest
	}{
		{"empty name", &Manifest{Version: "1.0", Description: "desc", Category: "cat"}},
		{"empty version", &Manifest{Name: "test", Description: "desc", Category: "cat"}},
		{"empty description", &Manifest{Name: "test", Version: "1.0", Category: "cat"}},
		{"empty category", &Manifest{Name: "test", Version: "1.0", Description: "desc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateManifest(tt.manifest); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	data := `{"name":"test-skill","version":"1.0.0","description":"A test skill","category":"security","entry_point":"main.go"}`
	os.WriteFile(path, []byte(data), 0644)

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if manifest.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", manifest.Name)
	}
	if manifest.Category != "security" {
		t.Errorf("expected category 'security', got %q", manifest.Category)
	}
}

func TestLoadManifest_NotFound(t *testing.T) {
	_, err := LoadManifest("/nonexistent/manifest.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := LoadManifest(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGetSkillDir(t *testing.T) {
	got := GetSkillDir("/skills", "my-skill")
	expected := filepath.Join("/skills", "my-skill")
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
