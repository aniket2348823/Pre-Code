package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestListSkillsRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      ListSkillsRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "empty is valid",
			req:    ListSkillsRequest{},
			hasErr: false,
		},
		{
			name:   "valid sort_by downloads",
			req:    ListSkillsRequest{SortBy: "downloads"},
			hasErr: false,
		},
		{
			name:   "valid sort_by rating",
			req:    ListSkillsRequest{SortBy: "rating"},
			hasErr: false,
		},
		{
			name:   "valid sort_by created_at",
			req:    ListSkillsRequest{SortBy: "created_at"},
			hasErr: false,
		},
		{
			name:   "valid sort_by name",
			req:    ListSkillsRequest{SortBy: "name"},
			hasErr: false,
		},
		{
			name:     "invalid sort_by",
			req:      ListSkillsRequest{SortBy: "popularity"},
			hasErr:   true,
			errField: "sort_by",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.req.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}

func TestInstallSkillRequest_Validate(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		req := InstallSkillRequest{SkillID: "skill-123"}
		if errs := req.Validate(); errs.HasErrors() {
			t.Errorf("unexpected errors: %v", errs.ToMap())
		}
	})

	t.Run("missing skill_id", func(t *testing.T) {
		req := InstallSkillRequest{}
		errs := req.Validate()
		if !errs.HasErrors() {
			t.Error("expected validation error for missing skill_id")
		}
	})
}

func TestSkillManifest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		manifest SkillManifest
		hasErr   bool
		errField string
	}{
		{
			name:     "valid manifest",
			manifest: SkillManifest{Name: "lint", Version: "1.0.0", Description: "Run linter", Author: "vigil"},
			hasErr:   false,
		},
		{
			name:     "missing name",
			manifest: SkillManifest{Version: "1.0.0", Description: "x", Author: "a"},
			hasErr:   true,
			errField: "name",
		},
		{
			name:     "missing version",
			manifest: SkillManifest{Name: "lint", Description: "x", Author: "a"},
			hasErr:   true,
			errField: "version",
		},
		{
			name:     "missing description",
			manifest: SkillManifest{Name: "lint", Version: "1.0.0", Author: "a"},
			hasErr:   true,
			errField: "description",
		},
		{
			name:     "missing author",
			manifest: SkillManifest{Name: "lint", Version: "1.0.0", Description: "x"},
			hasErr:   true,
			errField: "author",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.manifest.Validate()
			if tt.hasErr && !errs.HasErrors() {
				t.Errorf("expected validation error on field %q", tt.errField)
			}
			if !tt.hasErr && errs.HasErrors() {
				t.Errorf("unexpected errors: %v", errs.ToMap())
			}
		})
	}
}

func TestSkill_JSONRoundTrip(t *testing.T) {
	now := TimestampFromTime(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	original := Skill{
		ID:          "skill-1",
		Name:        "go-lint",
		Description: "Run golangci-lint on Go projects",
		Author:      "vigilagent",
		Version:     "1.2.0",
		Downloads:   4200,
		Rating:      4.8,
		Category:    "quality",
		Permissions: []string{"read_file", "run_command"},
		Verified:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Skill
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Name != "go-lint" {
		t.Errorf("Name = %q, want go-lint", decoded.Name)
	}
	if decoded.Downloads != 4200 {
		t.Errorf("Downloads = %d, want 4200", decoded.Downloads)
	}
	if decoded.Rating != 4.8 {
		t.Errorf("Rating = %v, want 4.8", decoded.Rating)
	}
	if !decoded.Verified {
		t.Error("Verified should be true")
	}
	if len(decoded.Permissions) != 2 {
		t.Errorf("Permissions len = %d, want 2", len(decoded.Permissions))
	}
}
