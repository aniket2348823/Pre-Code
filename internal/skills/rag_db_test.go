package skills

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/internal/repository"
)

// --- Mock Infrastructure ---

// mockTx implements pgx.Tx for testing without a real database.
type mockTx struct {
	queryFunc func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	execFunc  func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFunc != nil {
		return m.queryFunc(ctx, sql, args...)
	}
	return &mockRows{}, nil
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return nil
}

func (m *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) { return m, nil }
func (m *mockTx) Commit(ctx context.Context) error          { return nil }
func (m *mockTx) Rollback(ctx context.Context) error        { return nil }
func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (m *mockTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }
func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *mockTx) Conn() *pgx.Conn { return nil }

// mockRows implements pgx.Rows.
type mockRows struct {
	current  int
	data     [][]any
	fields   []pgconn.FieldDescription
	closed   bool
	err      error
	scanFunc func(dest ...any) error
}

func (r *mockRows) Next() bool {
	if r.current < len(r.data) {
		r.current++
		return true
	}
	return false
}

func (r *mockRows) Scan(dest ...any) error {
	if r.scanFunc != nil {
		return r.scanFunc(dest...)
	}
	if r.current == 0 || r.current > len(r.data) {
		return fmt.Errorf("no current row")
	}
	row := r.data[r.current-1]
	for i, d := range dest {
		if i < len(row) {
			setValue(d, row[i])
		}
	}
	return nil
}

func (r *mockRows) Close()                                       { r.closed = true }
func (r *mockRows) Err() error                                   { return r.err }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 1") }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return r.fields }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }

// setValue uses reflect to assign a value to a destination pointer.
func setValue(dest, src any) {
	if dest == nil || src == nil {
		return
	}
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return
	}
	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(dv.Elem().Type()) {
		dv.Elem().Set(sv)
	} else if sv.Type().ConvertibleTo(dv.Elem().Type()) {
		dv.Elem().Set(sv.Convert(dv.Elem().Type()))
	}
}

// newMockConn creates a *database.Conn with nil pool using reflect.
// Safe to use as long as all queries go through a Tx injected via context.
func newMockConn() *database.Conn {
	conn := reflect.New(reflect.TypeOf(database.Conn{})).Elem()
	return conn.Addr().Interface().(*database.Conn)
}

// newMockRAGEngine creates a RAGEngine with mock DB and mock embedder.
func newMockRAGEngine() (*RAGEngine, *mockEmbedder) {
	embedder := &mockEmbedder{}
	return &RAGEngine{
		pool:     newMockConn(),
		embedder: embedder,
	}, embedder
}

// mockEmbedder implements the Embedder interface.
type mockEmbedder struct {
	embedFunc func(ctx context.Context, text string) ([]float32, error)
	dims      int
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	vec := make([]float32, 1536)
	vec[0] = 0.5
	return vec, nil
}

func (m *mockEmbedder) Dimensions() int {
	if m.dims > 0 {
		return m.dims
	}
	return 1536
}

func (m *mockEmbedder) Name() string { return "mock-embedder" }

// --- RAGEngine DB Function Tests ---

func TestNewRAGEngine(t *testing.T) {
	embedder := &mockEmbedder{}
	engine := NewRAGEngine(nil, embedder)
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if engine.embedder != embedder {
		t.Error("expected embedder to be set")
	}
}

func TestIndexSkill_Success(t *testing.T) {
	engine, _ := newMockRAGEngine()
	ctx := database.WithTx(context.Background(), &mockTx{})
	skill := repository.Skill{
		ID:          "skill-1",
		Name:        "test-skill",
		Description: "A test skill",
		Category:    "security",
		Permissions: []string{"read"},
	}

	err := engine.IndexSkill(ctx, skill)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestIndexSkill_EmbedError(t *testing.T) {
	engine, embedder := newMockRAGEngine()
	embedder.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, fmt.Errorf("embed failed")
	}
	ctx := database.WithTx(context.Background(), &mockTx{})
	skill := repository.Skill{ID: "skill-1", Name: "test"}

	err := engine.IndexSkill(ctx, skill)
	if err == nil {
		t.Error("expected error from embed failure")
	}
}

func TestIndexSkill_ExecError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("db exec failed")
		},
	}
	ctx := database.WithTx(context.Background(), tx)
	skill := repository.Skill{ID: "skill-1", Name: "test"}

	err := engine.IndexSkill(ctx, skill)
	if err == nil {
		t.Error("expected error from exec failure")
	}
}

func TestSuggestSkills_Success(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"auth-scanner"},
					{"auth-helper"},
				},
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	suggestions, err := engine.SuggestSkills(ctx, "auth", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 2 {
		t.Errorf("expected 2 suggestions, got %d", len(suggestions))
	}
	if suggestions[0] != "auth-scanner" {
		t.Errorf("expected 'auth-scanner', got %q", suggestions[0])
	}
}

func TestSuggestSkills_EmptyPartial(t *testing.T) {
	engine, _ := newMockRAGEngine()
	ctx := context.Background()

	suggestions, err := engine.SuggestSkills(ctx, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if suggestions != nil {
		t.Errorf("expected nil for empty partial, got %v", suggestions)
	}
}

func TestSuggestSkills_DefaultLimit(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: [][]interface{}{{"result"}}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	suggestions, err := engine.SuggestSkills(ctx, "auth", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion, got %d", len(suggestions))
	}
}

func TestSuggestSkills_FallbackQuery(t *testing.T) {
	engine, _ := newMockRAGEngine()
	callCount := 0
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("pg_trgm not available")
			}
			return &mockRows{data: [][]interface{}{{"fallback-result"}}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	suggestions, err := engine.SuggestSkills(ctx, "auth", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion from fallback, got %d", len(suggestions))
	}
}

func TestSuggestSkills_BothQueriesFail(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	_, err := engine.SuggestSkills(ctx, "auth", 10)
	if err == nil {
		t.Error("expected error when both queries fail")
	}
}

func TestSuggestSkills_ScanError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data:     [][]interface{}{{"ok"}, {"bad"}},
				scanFunc: func(dest ...any) error { return fmt.Errorf("scan error") },
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	suggestions, err := engine.SuggestSkills(ctx, "auth", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Scan errors are skipped per-row
	if len(suggestions) != 0 {
		t.Errorf("expected 0 suggestions due to scan errors, got %d", len(suggestions))
	}
}

func TestGetTrending_Success(t *testing.T) {
	engine, _ := newMockRAGEngine()
	now := time.Now()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"skill-1", "auth-tool", "Auth scanner", "author1", "1.0.0", "security", 100, 4.5, 10, []string{"read"}, []byte("{}"), true, true, now, now},
				},
				scanFunc: func(dest ...any) error {
					if len(dest) == 15 {
						*dest[0].(*string) = "skill-1"
						*dest[1].(*string) = "auth-tool"
						*dest[2].(*string) = "Auth scanner"
						*dest[3].(*string) = "author1"
						*dest[4].(*string) = "1.0.0"
						*dest[5].(*string) = "security"
						*dest[6].(*int) = 100
						*dest[7].(*float64) = 4.5
						*dest[8].(*int) = 10
						*dest[9].(*[]string) = []string{"read"}
						*dest[10].(*[]byte) = []byte("{}")
						*dest[11].(*bool) = true
						*dest[12].(*bool) = true
						*dest[13].(*time.Time) = now
						*dest[14].(*time.Time) = now
					}
					return nil
				},
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	skills, err := engine.GetTrending(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "auth-tool" {
		t.Errorf("expected name 'auth-tool', got %q", skills[0].Name)
	}
}

func TestGetTrending_DefaultLimit(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: [][]interface{}{}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	skills, err := engine.GetTrending(ctx, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestGetTrending_QueryError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	_, err := engine.GetTrending(ctx, 10)
	if err == nil {
		t.Error("expected error")
	}
}

func TestGetTrending_ScanError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data:     [][]interface{}{{"bad"}},
				scanFunc: func(dest ...any) error { return fmt.Errorf("scan fail") },
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	skills, err := engine.GetTrending(ctx, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills after scan error, got %d", len(skills))
	}
}

func TestGetByCategory_Success(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"security", 5},
					{"utility", 3},
				},
				scanFunc: func(dest ...any) error {
					if len(dest) == 2 {
						*dest[0].(*string) = "security"
						*dest[1].(*int) = 5
					}
					return nil
				},
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	categories, err := engine.GetByCategory(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(categories))
	}
	if categories[0].Category != "security" || categories[0].Count != 5 {
		t.Errorf("unexpected first category: %+v", categories[0])
	}
}

func TestGetByCategory_QueryError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	_, err := engine.GetByCategory(ctx)
	if err == nil {
		t.Error("expected error")
	}
}

func TestGetByCategory_ScanError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data:     [][]interface{}{{"cat", 1}},
				scanFunc: func(dest ...any) error { return fmt.Errorf("scan fail") },
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	categories, err := engine.GetByCategory(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(categories) != 0 {
		t.Errorf("expected 0 categories after scan error, got %d", len(categories))
	}
}

func TestReindexAll_Success(t *testing.T) {
	engine, _ := newMockRAGEngine()
	now := time.Now()
	callCount := 0
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"skill-1", "auth-tool", "Auth scanner", "author1", "1.0.0", "security", 100, 4.5, 10, []string{"read"}, []byte("{}"), true, true, now, now},
				},
				scanFunc: func(dest ...any) error {
					if len(dest) == 15 {
						*dest[0].(*string) = "skill-1"
						*dest[1].(*string) = "auth-tool"
						*dest[2].(*string) = "Auth scanner"
						*dest[3].(*string) = "author1"
						*dest[4].(*string) = "1.0.0"
						*dest[5].(*string) = "security"
						*dest[6].(*int) = 100
						*dest[7].(*float64) = 4.5
						*dest[8].(*int) = 10
						*dest[9].(*[]string) = []string{"read"}
						*dest[10].(*[]byte) = []byte("{}")
						*dest[11].(*bool) = true
						*dest[12].(*bool) = true
						*dest[13].(*time.Time) = now
						*dest[14].(*time.Time) = now
					}
					return nil
				},
			}, nil
		},
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			callCount++
			return pgconn.NewCommandTag("OK"), nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	count, err := engine.ReindexAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 indexed, got %d", count)
	}
}

func TestReindexAll_EmptyResult(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: [][]interface{}{}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	count, err := engine.ReindexAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 indexed, got %d", count)
	}
}

func TestReindexAll_QueryError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return nil, fmt.Errorf("db error")
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	_, err := engine.ReindexAll(ctx)
	if err == nil {
		t.Error("expected error")
	}
}

func TestReindexAll_IndexSkillError(t *testing.T) {
	engine, embedder := newMockRAGEngine()
	embedder.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, fmt.Errorf("embed fail")
	}
	now := time.Now()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"skill-1", "auth-tool", "Auth scanner", "author1", "1.0.0", "security", 100, 4.5, 10, []string{"read"}, []byte("{}"), true, true, now, now},
				},
				scanFunc: func(dest ...any) error {
					if len(dest) == 15 {
						*dest[0].(*string) = "skill-1"
						*dest[1].(*string) = "auth-tool"
						*dest[2].(*string) = "Auth scanner"
						*dest[3].(*string) = "author1"
						*dest[4].(*string) = "1.0.0"
						*dest[5].(*string) = "security"
						*dest[6].(*int) = 100
						*dest[7].(*float64) = 4.5
						*dest[8].(*int) = 10
						*dest[9].(*[]string) = []string{"read"}
						*dest[10].(*[]byte) = []byte("{}")
						*dest[11].(*bool) = true
						*dest[12].(*bool) = true
						*dest[13].(*time.Time) = now
						*dest[14].(*time.Time) = now
					}
					return nil
				},
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	count, err := engine.ReindexAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// IndexSkill fails but ReindexAll continues
	if count != 0 {
		t.Errorf("expected 0 indexed after IndexSkill error, got %d", count)
	}
}

func TestReindexAll_ScanError(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data:     [][]interface{}{{"bad"}},
				scanFunc: func(dest ...any) error { return fmt.Errorf("scan fail") },
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	count, err := engine.ReindexAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 after scan error, got %d", count)
	}
}

func TestHybridSearch_Success(t *testing.T) {
	engine, _ := newMockRAGEngine()
	now := time.Now()

	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"skill-1", "auth-tool", "Auth scanner", "author1", "1.0.0", "security", 100, 4.5, 10, []string{"read"}, []byte("{}"), true, true, now, now, 0.9},
				},
				scanFunc: func(dest ...any) error {
					for i := range dest {
						switch d := dest[i].(type) {
						case *string:
							*d = "test"
						case *int:
							*d = 0
						case *float64:
							*d = 0.9
						case *bool:
							*d = true
						case *[]string:
							*d = []string{"read"}
						case *[]byte:
							*d = []byte("{}")
						case *time.Time:
							*d = now
						}
					}
					return nil
				},
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	query := SearchQuery{
		Raw:   "auth",
		Limit: 10,
	}
	resp, err := engine.HybridSearch(ctx, query)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Query != "auth" {
		t.Errorf("expected query 'auth', got %q", resp.Query)
	}
	if resp.Provider != "mock-embedder" {
		t.Errorf("expected provider 'mock-embedder', got %q", resp.Provider)
	}
}

func TestHybridSearch_DefaultLimit(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: [][]interface{}{}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	resp, err := engine.HybridSearch(ctx, SearchQuery{Raw: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestHybridSearch_CapsLimit(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: [][]interface{}{}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	resp, err := engine.HybridSearch(ctx, SearchQuery{Raw: "test", Limit: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestHybridSearch_EmbedFailGraceful(t *testing.T) {
	engine, embedder := newMockRAGEngine()
	embedder.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return nil, fmt.Errorf("embed failed")
	}
	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{data: [][]interface{}{}}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	resp, err := engine.HybridSearch(ctx, SearchQuery{Raw: "test", Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response even with embed failure")
	}
}

func TestHybridSearch_OffsetPagination(t *testing.T) {
	engine, _ := newMockRAGEngine()
	now := time.Now()

	tx := &mockTx{
		queryFunc: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			return &mockRows{
				data: [][]interface{}{
					{"skill-1", "test", "desc", "auth", "author", "1.0.0", "cat", 10, 4.0, 5, []string{}, []byte{}, true, true, now, now, 0.9},
				},
				scanFunc: func(dest ...any) error {
					for i := range dest {
						switch d := dest[i].(type) {
						case *string:
							*d = "test"
						case *int:
							*d = 0
						case *float64:
							*d = 0.9
						case *bool:
							*d = true
						case *[]string:
							*d = []string{}
						case *[]byte:
							*d = []byte{}
						case *time.Time:
							*d = now
						}
					}
					return nil
				},
			}, nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	resp, err := engine.HybridSearch(ctx, SearchQuery{Raw: "test", Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestEnsurePgTrgmExtension(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			if sql != "CREATE EXTENSION IF NOT EXISTS pg_trgm" {
				t.Errorf("unexpected SQL: %s", sql)
			}
			return pgconn.NewCommandTag("CREATE EXTENSION"), nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	err := EnsurePgTrgmExtension(ctx, engine.pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRequiredTables(t *testing.T) {
	engine, _ := newMockRAGEngine()
	tx := &mockTx{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("CREATE TABLE"), nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	err := EnsureRequiredTables(ctx, engine.pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRequiredTables_HNSWError_FallbackIVFFlat(t *testing.T) {
	engine, _ := newMockRAGEngine()
	callCount := 0
	tx := &mockTx{
		execFunc: func(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			callCount++
			if callCount == 1 {
				// First call: CREATE TABLE succeeds
				return pgconn.NewCommandTag("CREATE TABLE"), nil
			}
			if callCount == 2 {
				// Second call: HNSW index fails
				return pgconn.CommandTag{}, fmt.Errorf("HNSW not supported")
			}
			// Third call: IVFFlat fallback
			return pgconn.NewCommandTag("CREATE INDEX"), nil
		},
	}
	ctx := database.WithTx(context.Background(), tx)

	err := EnsureRequiredTables(ctx, engine.pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
