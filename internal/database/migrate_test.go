package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVer int
		wantOK  bool
	}{
		{"standard", "000001_init_schema.up.sql", 1, true},
		{"five-digit", "000123_add_users.up.sql", 123, true},
		{"down", "000001_init_schema.down.sql", 1, true},
		{"no_underscore", "migration.sql", 0, false},
		{"no_numeric_prefix", "abc_init.up.sql", 0, false},
		{"empty", "", 0, false},
		{"single_digit", "1_test.up.sql", 1, true},
		{"zero", "000000_init.up.sql", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVer, gotOK := migrationVersion(tt.input)
			if gotVer != tt.wantVer || gotOK != tt.wantOK {
				t.Errorf("migrationVersion(%q) = (%d, %v), want (%d, %v)",
					tt.input, gotVer, gotOK, tt.wantVer, tt.wantOK)
			}
		})
	}
}

func TestMigrationVersion_SortOrder(t *testing.T) {
	versions := []string{
		"000010_later.up.sql",
		"000001_first.up.sql",
		"000005_middle.up.sql",
	}
	expected := []int{10, 1, 5}
	for i, v := range versions {
		ver, ok := migrationVersion(v)
		if !ok {
			t.Fatalf("migrationVersion(%q) returned !ok", v)
		}
		if ver != expected[i] {
			t.Errorf("migrationVersion(%q) = %d, want %d", v, ver, expected[i])
		}
	}
}

func TestMigrate_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	err := Migrate(context.Background(), nil, dir)
	if err != nil {
		t.Fatalf("expected nil error for empty dir, got %v", err)
	}
}

func TestMigrate_NoMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a migration"), 0644)
	err := Migrate(context.Background(), nil, dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestMigrate_NonExistentDir(t *testing.T) {
	err := Migrate(context.Background(), nil, "/nonexistent/path/xyz")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got %v", err)
	}
}

func TestMigrate_BadGlobPattern(t *testing.T) {
	err := Migrate(context.Background(), nil, "[")
	if err == nil {
		t.Fatal("expected error for bad glob pattern")
	}
}
