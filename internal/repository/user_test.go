package repository

import (
	"testing"
)

// TestUserRepositoryInterface verifies UserRepository satisfies UserRepositoryInterface.
func TestUserRepositoryInterface(t *testing.T) {
	var _ UserRepositoryInterface = (*UserRepository)(nil)
}
