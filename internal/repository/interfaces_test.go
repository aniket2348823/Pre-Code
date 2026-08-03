package repository

import "testing"

// Compile-time interface satisfaction checks.
// Redundant with interfaces.go but catches regressions if implementations move.

func TestCompileTime_UserRepositoryInterface(t *testing.T) {
	var _ UserRepositoryInterface = (*UserRepository)(nil)
}

func TestCompileTime_OrganizationRepositoryInterface(t *testing.T) {
	var _ OrganizationRepositoryInterface = (*OrganizationRepository)(nil)
}

func TestCompileTime_ProjectRepositoryInterface(t *testing.T) {
	var _ ProjectRepositoryInterface = (*ProjectRepository)(nil)
}

func TestCompileTime_AgentRepositoryInterface(t *testing.T) {
	var _ AgentRepositoryInterface = (*AgentRepository)(nil)
}

func TestCompileTime_SessionRepositoryInterface(t *testing.T) {
	var _ SessionRepositoryInterface = (*SessionRepository)(nil)
}

func TestCompileTime_EventRepositoryInterface(t *testing.T) {
	var _ EventRepositoryInterface = (*EventRepository)(nil)
}

func TestCompileTime_APIKeyRepositoryInterface(t *testing.T) {
	var _ APIKeyRepositoryInterface = (*APIKeyRepository)(nil)
}

func TestCompileTime_TaskRepositoryInterface(t *testing.T) {
	var _ TaskRepositoryInterface = (*TaskRepository)(nil)
}

func TestCompileTime_SkillRepositoryInterface(t *testing.T) {
	var _ SkillRepositoryInterface = (*SkillRepository)(nil)
}

func TestCompileTime_AlertRepositoryInterface(t *testing.T) {
	var _ AlertRepositoryInterface = (*AlertRepository)(nil)
}
