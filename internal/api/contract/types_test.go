package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTaskStatus_Valid(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		valid  bool
	}{
		{"pending is valid", TaskStatusPending, true},
		{"planning is valid", TaskStatusPlanning, true},
		{"executing is valid", TaskStatusExecuting, true},
		{"waiting_hitl is valid", TaskStatusWaitingHITL, true},
		{"reviewing is valid", TaskStatusReviewing, true},
		{"completed is valid", TaskStatusCompleted, true},
		{"failed is valid", TaskStatusFailed, true},
		{"cancelled is valid", TaskStatusCancelled, true},
		{"garbage is invalid", TaskStatus("nope"), false},
		{"empty is invalid", TaskStatus(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.valid {
				t.Errorf("TaskStatus(%q).Valid() = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestTaskStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		terminal bool
	}{
		{TaskStatusPending, false},
		{TaskStatusPlanning, false},
		{TaskStatusExecuting, false},
		{TaskStatusWaitingHITL, false},
		{TaskStatusReviewing, false},
		{TaskStatusCompleted, true},
		{TaskStatusFailed, true},
		{TaskStatusCancelled, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.terminal {
				t.Errorf("TaskStatus(%q).IsTerminal() = %v, want %v", tt.status, got, tt.terminal)
			}
		})
	}
}

func TestAllTaskStatuses_Count(t *testing.T) {
	if got := len(AllTaskStatuses()); got != 8 {
		t.Errorf("AllTaskStatuses() has %d entries, want 8", got)
	}
}

func TestComplexity_Valid(t *testing.T) {
	tests := []struct {
		c     Complexity
		valid bool
	}{
		{ComplexitySimple, true},
		{ComplexityModerate, true},
		{ComplexityComplex, true},
		{ComplexityCritical, true},
		{Complexity("easy"), false},
		{Complexity(""), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.c), func(t *testing.T) {
			if got := tt.c.Valid(); got != tt.valid {
				t.Errorf("Complexity(%q).Valid() = %v, want %v", tt.c, got, tt.valid)
			}
		})
	}
}

func TestRole_Valid(t *testing.T) {
	for _, r := range AllRoles() {
		if !r.Valid() {
			t.Errorf("Role(%q).Valid() = false, expected true", r)
		}
	}
	if Role("root").Valid() {
		t.Error("Role(root).Valid() = true, expected false")
	}
}

func TestRole_AtLeast(t *testing.T) {
	tests := []struct {
		role    Role
		minimum Role
		ok      bool
	}{
		{RoleSuperAdmin, RoleSuperAdmin, true},
		{RoleSuperAdmin, RoleViewer, true},
		{RoleAdmin, RoleDeveloper, true},
		{RoleDeveloper, RoleAdmin, false},
		{RoleViewer, RoleDeveloper, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.role)+">="+string(tt.minimum), func(t *testing.T) {
			if got := tt.role.AtLeast(tt.minimum); got != tt.ok {
				t.Errorf("Role(%q).AtLeast(%q) = %v, want %v", tt.role, tt.minimum, got, tt.ok)
			}
		})
	}
}

func TestTier_Valid(t *testing.T) {
	for _, tier := range AllTiers() {
		if !tier.Valid() {
			t.Errorf("Tier(%q).Valid() = false, expected true", tier)
		}
	}
	if Tier("unlimited").Valid() {
		t.Error("Tier(unlimited).Valid() = true, expected false")
	}
}

func TestTier_MonthlyTaskLimit(t *testing.T) {
	tests := []struct {
		tier Tier
		want int
	}{
		{TierFree, 100},
		{TierPro, 2000},
		{TierTeam, 5000},
		{TierEnterprise, -1},
	}
	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			if got := tt.tier.MonthlyTaskLimit(); got != tt.want {
				t.Errorf("Tier(%q).MonthlyTaskLimit() = %d, want %d", tt.tier, got, tt.want)
			}
		})
	}
}

func TestMemoryType_Valid(t *testing.T) {
	for _, mt := range AllMemoryTypes() {
		if !mt.Valid() {
			t.Errorf("MemoryType(%q).Valid() = false, expected true", mt)
		}
	}
	if MemoryType("working").Valid() {
		t.Error("MemoryType(working) should be invalid — working memory is not stored via API")
	}
}

func TestPageRequest_EffectiveLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{"zero uses default", 0, DefaultPageLimit},
		{"negative uses default", -5, DefaultPageLimit},
		{"valid limit", 50, 50},
		{"above max capped", 200, MaxPageLimit},
		{"exact max", MaxPageLimit, MaxPageLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := PageRequest{Limit: tt.limit}
			if got := p.EffectiveLimit(); got != tt.want {
				t.Errorf("EffectiveLimit() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTimestamp_JSONRoundTrip(t *testing.T) {
	original := TimestampFromTime(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC))
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Timestamp
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !original.Time.Equal(decoded.Time) {
		t.Errorf("round-trip mismatch: got %v, want %v", decoded.Time, original.Time)
	}
}

func TestTimestamp_InvalidJSON(t *testing.T) {
	var ts Timestamp
	if err := json.Unmarshal([]byte(`"not-a-date"`), &ts); err == nil {
		t.Error("expected error on invalid timestamp, got nil")
	}
}

func TestValidationErrors(t *testing.T) {
	var errs ValidationErrors
	if errs.HasErrors() {
		t.Error("empty errors should not have errors")
	}

	errs.Add("name", "required")
	errs.Add("email", "invalid format")
	if !errs.HasErrors() {
		t.Error("should have errors after Add")
	}

	m := errs.ToMap()
	if m["name"] != "required" {
		t.Errorf("name error = %v, want 'required'", m["name"])
	}
	if m["email"] != "invalid format" {
		t.Errorf("email error = %v, want 'invalid format'", m["email"])
	}
}

func TestValidationErrors_EmptyMap(t *testing.T) {
	var errs ValidationErrors
	if m := errs.ToMap(); m != nil {
		t.Errorf("empty errors should return nil map, got %v", m)
	}
}
