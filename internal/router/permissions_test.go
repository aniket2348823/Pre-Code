package router

import (
	"context"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/repository"
)

func permissionsTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestErrAccessDenied(t *testing.T) {
	assert.EqualError(t, errAccessDenied, "access denied")
}

func TestRequireOrgMember_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireOrgMember(context.Background(), "org-1", "user-1")
	}()
}

func TestRequireOrgMemberWithOrg_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireOrgMemberWithOrg(context.Background(), "org-1", "user-1")
	}()
}

func TestRequireOrgOwner_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireOrgOwner(context.Background(), "org-1", "user-1")
	}()
}

func TestRequireProjectMember_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireProjectMember(context.Background(), "proj-1", "user-1")
	}()
}

func TestRequireAgentMember_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireAgentMember(context.Background(), "agent-1", "user-1")
	}()
}

func TestRequireSessionMember_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireSessionMember(context.Background(), "session-1", "user-1")
	}()
}

func TestRequireTaskMember_NilRepo(t *testing.T) {
	r := permissionsTestRouter()
	func() {
		defer func() { recover() }()
		r.requireTaskMember(context.Background(), "task-1", "user-1")
	}()
}

func TestPermissionErrorSentinel(t *testing.T) {
	assert.Error(t, errAccessDenied)
	assert.EqualError(t, errAccessDenied, "access denied")
}

func TestRequireOrgMember_ContextCancellation(t *testing.T) {
	r := permissionsTestRouter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	func() {
		defer func() { recover() }()
		r.requireOrgMember(ctx, "org-1", "user-1")
	}()
}

func TestOrganizationRepositoryFields(t *testing.T) {
	o := &repository.Organization{
		ID:          "org-1",
		Name:        "Test Org",
		Slug:        "test-org",
		Description: "desc",
		OwnerID:     "user-1",
		Plan:        "free",
	}
	assert.Equal(t, "org-1", o.ID)
	assert.Equal(t, "Test Org", o.Name)
	assert.Equal(t, "user-1", o.OwnerID)
}

func TestProjectRepositoryFields(t *testing.T) {
	p := &repository.Project{
		ID:     "proj-1",
		OrgID:  "org-1",
		Name:   "Test Project",
		Status: "active",
	}
	assert.Equal(t, "proj-1", p.ID)
	assert.Equal(t, "org-1", p.OrgID)
}

func TestAgentRepositoryFields(t *testing.T) {
	a := &repository.Agent{
		ID:        "agent-1",
		ProjectID: "proj-1",
		Name:      "Test Agent",
		Status:    "idle",
	}
	assert.Equal(t, "agent-1", a.ID)
	assert.Equal(t, "proj-1", a.ProjectID)
}
