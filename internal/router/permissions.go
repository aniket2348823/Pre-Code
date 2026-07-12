package router

import (
	"context"
	"errors"

	"github.com/vigilagent/vigilagent/internal/repository"
)

// Sentinel error for permission checks — always returns access denied to prevent enumeration.
var errAccessDenied = errors.New("access denied")

// requireOrgMember checks that the user is a member of the organization.
func (r *Router) requireOrgMember(ctx context.Context, orgID, userID string) error {
	member, err := r.orgs.IsMember(ctx, orgID, userID)
	if err != nil || !member {
		return errAccessDenied
	}
	return nil
}

// requireOrgMemberWithOrg checks membership and returns the organization.
// Returns errAccessDenied for both missing and unauthorized to prevent enumeration.
func (r *Router) requireOrgMemberWithOrg(ctx context.Context, orgID, userID string) (*repository.Organization, error) {
	org, err := r.orgs.FindByID(ctx, orgID)
	if err != nil {
		return nil, errAccessDenied
	}
	member, err := r.orgs.IsMember(ctx, orgID, userID)
	if err != nil || !member {
		return nil, errAccessDenied
	}
	return org, nil
}

// requireOrgOwner checks that the user is the owner of the organization.
func (r *Router) requireOrgOwner(ctx context.Context, orgID, userID string) error {
	owner, err := r.orgs.IsOwner(ctx, orgID, userID)
	if err != nil || !owner {
		return errAccessDenied
	}
	return nil
}

// requireProjectMember looks up a project by ID and verifies the user is a member
// of the project's organization. Returns the project on success.
// Returns errAccessDenied for both missing and unauthorized to prevent enumeration.
func (r *Router) requireProjectMember(ctx context.Context, projectID, userID string) (*repository.Project, error) {
	project, err := r.projects.FindByID(ctx, projectID)
	if err != nil {
		return nil, errAccessDenied
	}
	member, err := r.orgs.IsMember(ctx, project.OrgID, userID)
	if err != nil || !member {
		return nil, errAccessDenied
	}
	return project, nil
}

// requireAgentMember looks up an agent by ID, then its project, and verifies
// the user is a member of the project's organization.
// Returns errAccessDenied for both missing and unauthorized to prevent enumeration.
func (r *Router) requireAgentMember(ctx context.Context, agentID, userID string) (*repository.Agent, *repository.Project, error) {
	agent, err := r.agents.FindByID(ctx, agentID)
	if err != nil {
		return nil, nil, errAccessDenied
	}
	project, err := r.projects.FindByID(ctx, agent.ProjectID)
	if err != nil {
		return nil, nil, errAccessDenied
	}
	member, err := r.orgs.IsMember(ctx, project.OrgID, userID)
	if err != nil || !member {
		return nil, nil, errAccessDenied
	}
	return agent, project, nil
}

// requireSessionMember looks up a session by ID, then its project, and verifies
// the user is a member of the project's organization.
// Returns errAccessDenied for both missing and unauthorized to prevent enumeration.
func (r *Router) requireSessionMember(ctx context.Context, sessionID, userID string) (*repository.Session, *repository.Project, error) {
	session, err := r.sessions.FindByID(ctx, sessionID)
	if err != nil {
		return nil, nil, errAccessDenied
	}
	project, err := r.projects.FindByID(ctx, session.ProjectID)
	if err != nil {
		return nil, nil, errAccessDenied
	}
	member, err := r.orgs.IsMember(ctx, project.OrgID, userID)
	if err != nil || !member {
		return nil, nil, errAccessDenied
	}
	return session, project, nil
}

// requireTaskMember looks up a task by ID, then its project, and verifies
// the user is a member of the project's organization.
// Returns errAccessDenied for both missing and unauthorized to prevent enumeration.
func (r *Router) requireTaskMember(ctx context.Context, taskID, userID string) (*repository.Task, *repository.Project, error) {
	task, err := r.tasks.FindByID(ctx, taskID)
	if err != nil {
		return nil, nil, errAccessDenied
	}
	project, err := r.projects.FindByID(ctx, task.ProjectID)
	if err != nil {
		return nil, nil, errAccessDenied
	}
	member, err := r.orgs.IsMember(ctx, project.OrgID, userID)
	if err != nil || !member {
		return nil, nil, errAccessDenied
	}
	return task, project, nil
}
