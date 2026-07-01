package database

import (
	"testing"
)

func TestMigrationVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVer int
		wantOK  bool
	}{
		{
			name:    "standard migration",
			input:   "000001_init_schema.up.sql",
			wantVer: 1,
			wantOK:  true,
		},
		{
			name:    "five-digit version",
			input:   "000123_add_users.up.sql",
			wantVer: 123,
			wantOK:  true,
		},
		{
			name:    "down migration",
			input:   "000001_init_schema.down.sql",
			wantVer: 1,
			wantOK:  true,
		},
		{
			name:    "no underscore",
			input:   "migration.sql",
			wantVer: 0,
			wantOK:  false,
		},
		{
			name:    "no numeric prefix",
			input:   "abc_init.up.sql",
			wantVer: 0,
			wantOK:  false,
		},
		{
			name:    "empty string",
			input:   "",
			wantVer: 0,
			wantOK:  false,
		},
		{
			name:    "single digit",
			input:   "1_test.up.sql",
			wantVer: 1,
			wantOK:  true,
		},
		{
			name:    "zero version",
			input:   "000000_init.up.sql",
			wantVer: 0,
			wantOK:  true,
		},
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
	// Verify that version extraction produces correct sort order
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
