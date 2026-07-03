package memory

import (
	"context"
	"errors"
	"testing"
)

// fakeEmbedder returns a fixed, non-zero vector so tests can assert that the
// manager actually routes text through the configured embedder.
type fakeEmbedder struct {
	dims int
	vec  []float32
	err  error
}

func (f *fakeEmbedder) Name() string    { return "fake" }
func (f *fakeEmbedder) Dimensions() int { return f.dims }
func (f *fakeEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vec, nil
}

func nonZero(v []float32) bool {
	for _, x := range v {
		if x != 0 {
			return true
		}
	}
	return false
}

func TestManagerEmbed_UsesConfiguredEmbedder(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3, 0.4}
	m := NewManagerWithEmbedder(nil, &fakeEmbedder{dims: 4, vec: want})

	got := m.embed(context.Background(), "add error handling to UserService")

	if len(got.Slice()) != len(want) {
		t.Fatalf("dimension mismatch: got %d, want %d", len(got.Slice()), len(want))
	}
	if !nonZero(got.Slice()) {
		t.Fatal("expected a non-zero embedding, got zero vector")
	}
	for i, x := range got.Slice() {
		if x != want[i] {
			t.Fatalf("element %d: got %v, want %v", i, x, want[i])
		}
	}
}

func TestManagerEmbed_NoOpFallsBackToZeroVector(t *testing.T) {
	m := NewManager(nil) // defaults to NoOpEmbedder(1536)

	got := m.embed(context.Background(), "anything")

	if len(got.Slice()) != 1536 {
		t.Fatalf("expected 1536 dims, got %d", len(got.Slice()))
	}
	if nonZero(got.Slice()) {
		t.Fatal("NoOp embedder should yield a zero vector")
	}
}

func TestManagerEmbed_ErrorFallsBackToZeroVector(t *testing.T) {
	m := NewManagerWithEmbedder(nil, &fakeEmbedder{dims: 8, err: errors.New("api down")})

	got := m.embed(context.Background(), "query")

	if len(got.Slice()) != 8 {
		t.Fatalf("expected 8 dims on fallback, got %d", len(got.Slice()))
	}
	if nonZero(got.Slice()) {
		t.Fatal("embedder error should degrade to a zero vector, not fail")
	}
}

func TestManagerEmbed_DimensionMismatchFallsBack(t *testing.T) {
	// Embedder claims 4 dims but returns 3: manager must not pass through a
	// malformed vector (which would break a vector(N) DB column).
	m := NewManagerWithEmbedder(nil, &fakeEmbedder{dims: 4, vec: []float32{1, 2, 3}})

	got := m.embed(context.Background(), "query")

	if len(got.Slice()) != 4 {
		t.Fatalf("expected 4 dims, got %d", len(got.Slice()))
	}
	if nonZero(got.Slice()) {
		t.Fatal("dimension mismatch should degrade to a zero vector")
	}
}
