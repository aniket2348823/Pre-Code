package secrets

import (
	"os"
	"testing"
	"time"
)

func TestEnvSecretsManager_Get(t *testing.T) {
	os.Setenv("TEST_SECRET_FOO", "bar123")
	defer os.Unsetenv("TEST_SECRET_FOO")

	m := NewEnvSecretsManager("TEST_SECRET")
	val, err := m.Get("FOO")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "bar123" {
		t.Errorf("expected bar123, got %s", val)
	}
}

func TestEnvSecretsManager_GetMissing(t *testing.T) {
	m := NewEnvSecretsManager("NONEXISTENT_PREFIX_XYZ")
	_, err := m.Get("NOPE")
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestEnvSecretsManager_Rotate(t *testing.T) {
	os.Setenv("ROTATE_TEST_SECRET", "old_value")
	defer os.Unsetenv("ROTATE_TEST_SECRET")

	m := NewEnvSecretsManager("ROTATE_TEST")
	newVal, err := m.Rotate("SECRET")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newVal == "" {
		t.Fatal("expected non-empty rotated value")
	}
	if newVal == "old_value" {
		t.Error("rotated value should differ from old value")
	}
	envVal := os.Getenv("ROTATE_TEST_SECRET")
	if envVal != newVal {
		t.Errorf("env not updated: expected %s, got %s", newVal, envVal)
	}
}

func TestEnvSecretsManager_List(t *testing.T) {
	os.Setenv("LIST_TEST_A", "1")
	os.Setenv("LIST_TEST_B", "2")
	defer os.Unsetenv("LIST_TEST_A")
	defer os.Unsetenv("LIST_TEST_B")

	m := NewEnvSecretsManager("LIST_TEST")
	m.Rotate("A")
	m.Rotate("B")

	metas, err := m.List()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("expected 2 secrets, got %d", len(metas))
	}
	for _, meta := range metas {
		if meta.Version != 1 {
			t.Errorf("expected version 1 for %s, got %d", meta.Name, meta.Version)
		}
		if meta.LastRotatedAt.IsZero() {
			t.Errorf("expected non-zero rotation time for %s", meta.Name)
		}
	}
}

func TestEnvSecretsManager_RotateIncrementsVersion(t *testing.T) {
	os.Setenv("VER_TEST_SECRET", "initial")
	defer os.Unsetenv("VER_TEST_SECRET")

	m := NewEnvSecretsManager("VER_TEST")
	m.Rotate("SECRET")
	m.Rotate("SECRET")
	m.Rotate("SECRET")

	metas, _ := m.List()
	for _, meta := range metas {
		if meta.Name == "SECRET" && meta.Version != 3 {
			t.Errorf("expected version 3, got %d", meta.Version)
		}
	}
}

func TestSecretMetadata_NeedsRotation(t *testing.T) {
	tests := []struct {
		name     string
		days     int
		lastRot  time.Time
		expected bool
	}{
		{"no rotation set", 0, time.Now(), false},
		{"recently rotated", 90, time.Now().Add(-30 * 24 * time.Hour), false},
		{"overdue", 90, time.Now().Add(-100 * 24 * time.Hour), true},
		{"exactly due", 90, time.Now().Add(-90 * 24 * time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := SecretMetadata{
				Name:          "test",
				Version:       1,
				LastRotatedAt: tt.lastRot,
				RotationDays:  tt.days,
			}
			if got := m.NeedsRotation(); got != tt.expected {
				t.Errorf("NeedsRotation() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRotationScheduler_StartStop(t *testing.T) {
	os.Setenv("SCHED_TEST", "val")
	defer os.Unsetenv("SCHED_TEST")

	m := NewEnvSecretsManager("SCHED_TEST")
	sched := NewRotationScheduler(m, 50*time.Millisecond, 90)
	sched.Start()
	// Let it tick a few times.
	time.Sleep(200 * time.Millisecond)
	sched.Stop()
	// No panic = pass.
}

func TestJWTKeyRotator_CurrentKey(t *testing.T) {
	os.Setenv("JWT_ROTATE_TEST_KEY", "initial_key_value")
	defer os.Unsetenv("JWT_ROTATE_TEST_KEY")

	m := NewEnvSecretsManager("JWT_ROTATE_TEST")
	r := NewJWTKeyRotator(m, "KEY", 90)

	key, err := r.CurrentKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) == 0 {
		t.Fatal("expected non-empty key")
	}
	// Second call should return cached key.
	key2, err := r.CurrentKey()
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if string(key) != string(key2) {
		t.Errorf("expected same key, got different")
	}
}

func TestJWTKeyRotator_NeedsRotation(t *testing.T) {
	os.Setenv("JWT_NEED_ROT_KEY", "initial")
	defer os.Unsetenv("JWT_NEED_ROT_KEY")

	m := NewEnvSecretsManager("JWT_NEED_ROT")
	r := NewJWTKeyRotator(m, "KEY", 90)

	// Not yet rotated — doesn't need rotation.
	if r.NeedsRotation() {
		t.Error("should not need rotation before first rotate")
	}

	r.CurrentKey() // triggers rotate

	// After rotation with 90 day window — should not need rotation.
	if r.NeedsRotation() {
		t.Error("should not need rotation immediately after rotate")
	}
}

func TestEnvSecretsManager_NoPrefix(t *testing.T) {
	os.Setenv("BARE_SECRET", "bare_value")
	defer os.Unsetenv("BARE_SECRET")

	m := NewEnvSecretsManager("")
	val, err := m.Get("BARE_SECRET")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "bare_value" {
		t.Errorf("expected bare_value, got %s", val)
	}
}
