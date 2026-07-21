package contract

import (
	"encoding/json"
	"testing"
)

func TestCreateOrgRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      CreateOrgRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid request",
			req:    CreateOrgRequest{Name: "Acme Corp", BillingEmail: "billing@acme.com"},
			hasErr: false,
		},
		{
			name:     "missing name",
			req:      CreateOrgRequest{BillingEmail: "billing@acme.com"},
			hasErr:   true,
			errField: "name",
		},
		{
			name:     "missing email",
			req:      CreateOrgRequest{Name: "Acme Corp"},
			hasErr:   true,
			errField: "billing_email",
		},
		{
			name:     "invalid email no @",
			req:      CreateOrgRequest{Name: "Acme", BillingEmail: "not-an-email"},
			hasErr:   true,
			errField: "billing_email",
		},
		{
			name:     "invalid email no domain dot",
			req:      CreateOrgRequest{Name: "Acme", BillingEmail: "user@localhost"},
			hasErr:   true,
			errField: "billing_email",
		},
		{
			name:   "valid email with subdomain",
			req:    CreateOrgRequest{Name: "Acme", BillingEmail: "admin@billing.acme.com"},
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

func TestInviteMemberRequest_Validate(t *testing.T) {
	tests := []struct {
		name     string
		req      InviteMemberRequest
		hasErr   bool
		errField string
	}{
		{
			name:   "valid request",
			req:    InviteMemberRequest{Email: "dev@acme.com", Role: RoleDeveloper},
			hasErr: false,
		},
		{
			name:     "missing email",
			req:      InviteMemberRequest{Role: RoleDeveloper},
			hasErr:   true,
			errField: "email",
		},
		{
			name:     "invalid email",
			req:      InviteMemberRequest{Email: "bad", Role: RoleDeveloper},
			hasErr:   true,
			errField: "email",
		},
		{
			name:     "invalid role",
			req:      InviteMemberRequest{Email: "dev@acme.com", Role: Role("root")},
			hasErr:   true,
			errField: "role",
		},
		{
			name:   "super_admin role is valid",
			req:    InviteMemberRequest{Email: "admin@acme.com", Role: RoleSuperAdmin},
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

func TestOrgMember_RolesMatchEnum(t *testing.T) {
	// Verify every role used in an OrgMember is a valid enum value.
	members := []OrgMember{
		{Role: RoleViewer},
		{Role: RoleDeveloper},
		{Role: RoleAdmin},
		{Role: RoleSuperAdmin},
	}
	for _, m := range members {
		if !m.Role.Valid() {
			t.Errorf("OrgMember role %q is invalid", m.Role)
		}
	}
}

func TestOrganization_JSONRoundTrip(t *testing.T) {
	now := Now()
	original := Organization{
		ID:           "org-1",
		Name:         "VigilAgent Inc.",
		BillingEmail: "billing@vigilagent.com",
		Tier:         TierTeam,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Organization
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.ID != "org-1" {
		t.Errorf("ID = %q, want org-1", decoded.ID)
	}
	if decoded.Tier != TierTeam {
		t.Errorf("Tier = %q, want team", decoded.Tier)
	}
	if decoded.BillingEmail != "billing@vigilagent.com" {
		t.Errorf("BillingEmail = %q, want billing@vigilagent.com", decoded.BillingEmail)
	}
}

func TestOrgMember_JSONRoundTrip(t *testing.T) {
	now := Now()
	original := OrgMember{
		ID:        "mem-1",
		UserID:    "user-42",
		Email:     "dev@acme.com",
		Role:      RoleDeveloper,
		Status:    "active",
		JoinedAt:  now,
		InvitedAt: now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded OrgMember
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Email != "dev@acme.com" {
		t.Errorf("Email = %q, want dev@acme.com", decoded.Email)
	}
	if decoded.Status != "active" {
		t.Errorf("Status = %q, want active", decoded.Status)
	}
}
