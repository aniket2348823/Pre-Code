package query

import (
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/pkg/pagination"
)

type TestItem struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
}

func TestProcessList(t *testing.T) {
	items := []TestItem{
		{ID: "1", Name: "Alpha", Status: "active", CreatedAt: time.Now().Add(-10 * time.Minute)},
		{ID: "2", Name: "Beta", Status: "inactive", CreatedAt: time.Now().Add(-5 * time.Minute)},
		{ID: "3", Name: "Gamma", Status: "active", CreatedAt: time.Now()},
	}

	t.Run("filter by status", func(t *testing.T) {
		f := Filter{Status: "active"}
		s := Sort{Field: "created_at", Order: "asc"}
		pag := pagination.Params{Limit: 10}

		res, meta := ProcessList(items, f, s, pag)
		if len(res) != 2 {
			t.Errorf("expected 2 items, got %d", len(res))
		}
		if res[0].Name != "Alpha" || res[1].Name != "Gamma" {
			t.Errorf("incorrect items filtered/sorted: %v", res)
		}
		if meta.Total != 2 {
			t.Errorf("expected total 2, got %d", meta.Total)
		}
	})

	t.Run("sort by name desc", func(t *testing.T) {
		f := Filter{}
		s := Sort{Field: "name", Order: "desc"}
		pag := pagination.Params{Limit: 10}

		res, _ := ProcessList(items, f, s, pag)
		if len(res) != 3 {
			t.Fatalf("expected 3 items, got %d", len(res))
		}
		if res[0].Name != "Gamma" || res[1].Name != "Beta" || res[2].Name != "Alpha" {
			t.Errorf("incorrect sort order: %v", res)
		}
	})

	t.Run("paginate with limit and cursor", func(t *testing.T) {
		f := Filter{}
		s := Sort{Field: "name", Order: "asc"} // Alpha, Beta, Gamma
		pag1 := pagination.Params{Limit: 2}

		res1, meta1 := ProcessList(items, f, s, pag1)
		if len(res1) != 2 {
			t.Fatalf("expected 2 items, got %d", len(res1))
		}
		if res1[0].Name != "Alpha" || res1[1].Name != "Beta" {
			t.Errorf("expected Alpha and Beta, got %v", res1)
		}
		if !meta1.HasMore {
			t.Error("expected has_more to be true")
		}
		if meta1.NextCursor == "" {
			t.Error("expected next_cursor to be set")
		}

		// Use the next cursor
		pag2 := pagination.Params{Limit: 2, Cursor: meta1.NextCursor}
		res2, meta2 := ProcessList(items, f, s, pag2)
		if len(res2) != 1 {
			t.Fatalf("expected 1 item, got %d", len(res2))
		}
		if res2[0].Name != "Gamma" {
			t.Errorf("expected Gamma, got %v", res2)
		}
		if meta2.HasMore {
			t.Error("expected has_more to be false")
		}
	})
}
