package cachekeys

import "testing"

func TestGenerateDeterministic(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer token"}
	k1 := Generate("GET", "/api/users", headers)
	k2 := Generate("GET", "/api/users", headers)
	if k1 != k2 {
		t.Fatalf("same inputs should produce same key: %s vs %s", k1, k2)
	}
	if len(k1) != 16 {
		t.Fatalf("expected 16-char key, got %d", len(k1))
	}
}

func TestGenerateDifferentPaths(t *testing.T) {
	k1 := Generate("GET", "/api/users", nil)
	k2 := Generate("GET", "/api/orders", nil)
	if k1 == k2 {
		t.Fatal("different paths should produce different keys")
	}
}

func TestGenerateDifferentMethods(t *testing.T) {
	k1 := Generate("GET", "/api/data", nil)
	k2 := Generate("POST", "/api/data", nil)
	if k1 == k2 {
		t.Fatal("different methods should produce different keys")
	}
}

func TestGenerateHeadersSorted(t *testing.T) {
	k1 := Generate("GET", "/api", map[string]string{"A": "1", "B": "2"})
	k2 := Generate("GET", "/api", map[string]string{"B": "2", "A": "1"})
	if k1 != k2 {
		t.Fatal("header order should not affect key")
	}
}

func TestGenerateWithBody(t *testing.T) {
	k1 := GenerateWithBody("POST", "/api", `{"a":1}`, nil)
	k2 := GenerateWithBody("POST", "/api", `{"a":2}`, nil)
	if k1 == k2 {
		t.Fatal("different bodies should produce different keys")
	}
}

func TestGenerateWithBodySameData(t *testing.T) {
	k1 := GenerateWithBody("POST", "/api", "hello", nil)
	k2 := GenerateWithBody("POST", "/api", "hello", nil)
	if k1 != k2 {
		t.Fatal("same body should produce same key")
	}
}

func TestGenerateNilHeaders(t *testing.T) {
	k := Generate("GET", "/api", nil)
	if k == "" {
		t.Fatal("expected non-empty key")
	}
}
