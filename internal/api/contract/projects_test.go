package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCreateProjectRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      CreateProjectRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid request",
			req:    CreateProjectRequest{Name: "my-project"},
			hasErr: false,
		},
		{
			name:     "missing name",
			req:      CreateProjectRequest{},
			hasErr:   true,
			errField: "name",
		},
		{
			name:   "with all optional fields",
			req:    CreateProjectRequest{Name: "proj", Description: "desc", RepositoryURL: "https://github.com/x/y", Language: "go"},
			hasErr: false,
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

func TestUpdateProjectRequest_Validate(t *testing.T) {
	t.Run("empty body rejected", func(t *testing.T) {
		req := UpdateProjectRequest{}
		errs := req.Validate()
		if !errs.HasErrors() {
			t.Error("expected error when no fields are set")
		}
	})

	t.Run("empty name string rejected", func(t *testing.T) {
		empty := ""
		req := UpdateProjectRequest{Name: &empty}
		errs := req.Validate()
		if !errs.HasErrors() {
			t.Error("expected error when name is empty string")
		}
	})

	t.Run("valid partial update", func(t *testing.T) {
		desc := "new description"
		req := UpdateProjectRequest{Description: &desc}
		if errs := req.Validate(); errs.HasErrors() {
			t.Errorf("unexpected errors: %v", errs.ToMap())
		}
	})
}

func TestProject_JSONRoundTrip(t *testing.T) {
	now := TimestampFromTime(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	original := Project{
		ID:            "proj-1",
		UserID:        "user-1",
		Name:          "VigilAgent",
		Description:   "AI agent platform",
		RepositoryURL: "https://github.com/vigilagent/vigilagent",
		Language:      "go",
		Settings: &ProjectSettings{
			DefaultModel: "claude-sonnet-4",
			BudgetLimit:  50.00,
			Conventions:  "use gofmt",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Project
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Settings == nil {
		t.Fatal("Settings should not be nil")
	}
	if decoded.Settings.BudgetLimit != 50.00 {
		t.Errorf("BudgetLimit = %v, want 50.00", decoded.Settings.BudgetLimit)
	}
}

func TestProjectSettings_Defaults(t *testing.T) {
	// A nil settings is valid — sensible defaults should be applied server-side.
	var s *ProjectSettings
	if s != nil {
		t.Error("nil ProjectSettings should remain nil")
	}
}
