package contract

import "strings"

// ---------------------------------------------------------------------------
// Organization / Team resource types — API contract §2.7
// ---------------------------------------------------------------------------

// CreateOrgRequest is the body for POST /v1/organizations.
type CreateOrgRequest struct {
	Name         string `json:"name"`
	BillingEmail string `json:"billing_email"`
}

// Validate checks required fields.
func (r *CreateOrgRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Name == "" {
		errs.Add("name", "name is required")
	}
	if r.BillingEmail == "" {
		errs.Add("billing_email", "billing_email is required")
	} else if !isValidEmail(r.BillingEmail) {
		errs.Add("billing_email", "billing_email must be a valid email address")
	}
	return errs
}

// CreateOrgResponse wraps the created organization.
type CreateOrgResponse struct {
	Organization Organization `json:"organization"`
}

// Organization is the full organization entity.
type Organization struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	BillingEmail string    `json:"billing_email"`
	Tier         Tier      `json:"tier"`
	CreatedAt    Timestamp `json:"created_at"`
	UpdatedAt    Timestamp `json:"updated_at"`
}

// InviteMemberRequest is the body for POST /v1/organizations/{id}/members.
type InviteMemberRequest struct {
	Email string `json:"email"`
	Role  Role   `json:"role"`
}

// Validate checks required fields and enum values.
func (r *InviteMemberRequest) Validate() ValidationErrors {
	var errs ValidationErrors
	if r.Email == "" {
		errs.Add("email", "email is required")
	} else if !isValidEmail(r.Email) {
		errs.Add("email", "email must be a valid email address")
	}
	if !r.Role.Valid() {
		errs.Add("role", "role must be one of: viewer, developer, admin, super_admin")
	}
	return errs
}

// InviteMemberResponse wraps the created member.
type InviteMemberResponse struct {
	Member OrgMember `json:"member"`
}

// OrgMember is a member within an organization.
type OrgMember struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      Role      `json:"role"`
	Status    string    `json:"status"` // "invited", "active", "deactivated"
	JoinedAt  Timestamp `json:"joined_at"`
	InvitedAt Timestamp `json:"invited_at"`
}

// ListMembersRequest holds query parameters for GET /v1/organizations/{id}/members.
type ListMembersRequest struct {
	PageRequest
}

// ListMembersResponse is the response for GET /v1/organizations/{id}/members.
type ListMembersResponse struct {
	Members []OrgMember  `json:"members"`
	Page    PageResponse `json:"page"`
}

// isValidEmail is a lightweight email check (contains @ and a dot in the domain).
func isValidEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	if at < 1 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".")
}
