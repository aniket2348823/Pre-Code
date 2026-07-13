package memory

import (
	"context"
	"sync"
	"testing"
)

func TestWorkingMemory_AddAndGet(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello"})
	wm.Add(Message{Role: "assistant", Content: "hi"})
	if wm.Count() != 2 {
		t.Errorf("expected 2 messages, got %d", wm.Count())
	}
	msgs := wm.Get()
	if len(msgs) != 2 {
		t.Errorf("expected 2, got %d", len(msgs))
	}
}

func TestWorkingMemory_Search(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "fix the auth bug"})
	wm.Add(Message{Role: "user", Content: "add login page"})
	wm.Add(Message{Role: "assistant", Content: "auth fixed"})
	results := wm.Search("auth", 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestWorkingMemory_SearchLimit(t *testing.T) {
	wm := NewWorkingMemory(0)
	for i := 0; i < 10; i++ {
		wm.Add(Message{Role: "user", Content: "test message"})
	}
	results := wm.Search("test", 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestWorkingMemory_Clear(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello"})
	wm.Clear()
	if wm.Count() != 0 {
		t.Error("expected 0 after clear")
	}
}

func TestWorkingMemory_TokenCount(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello", Tokens: 10})
	wm.Add(Message{Role: "assistant", Content: "hi", Tokens: 5})
	if wm.TokenCount() != 15 {
		t.Errorf("expected 15 tokens, got %d", wm.TokenCount())
	}
}

func TestWorkingMemory_Concurrent(t *testing.T) {
	wm := NewWorkingMemory(0)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			wm.Add(Message{Role: "user", Content: "test"})
			wm.Get()
			wm.Search("test", 10)
			wm.Count()
		}(i)
	}
	wg.Wait()
}

func TestProceduralStore_StoreAndGet(t *testing.T) {
	s := NewProceduralStore()
	wf := &Workflow{ID: "w1", Name: "deploy", Description: "deploy workflow"}
	if err := s.Store(context.Background(), wf); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), "w1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "deploy" {
		t.Errorf("expected deploy, got %s", got.Name)
	}
}

func TestProceduralStore_GetNotFound(t *testing.T) {
	s := NewProceduralStore()
	_, err := s.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent workflow")
	}
}

func TestProceduralStore_Search(t *testing.T) {
	s := NewProceduralStore()
	s.Store(context.Background(), &Workflow{ID: "w1", Name: "deploy app"})
	s.Store(context.Background(), &Workflow{ID: "w2", Name: "test app"})
	s.Store(context.Background(), &Workflow{ID: "w3", Name: "build lib"})
	results, _ := s.Search(context.Background(), "app", 10)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestProceduralStore_ListByUser(t *testing.T) {
	s := NewProceduralStore()
	s.Store(context.Background(), &Workflow{ID: "w1", UserID: "u1", Name: "w1"})
	s.Store(context.Background(), &Workflow{ID: "w2", UserID: "u2", Name: "w2"})
	results, _ := s.ListByUser(context.Background(), "u1", 10)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestNoOpEmbedder(t *testing.T) {
	e := NewNoOpEmbedder(1536)
	if e.Dimensions() != 1536 {
		t.Errorf("expected 1536, got %d", e.Dimensions())
	}
	vec, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 1536 {
		t.Errorf("expected 1536-dim vector, got %d", len(vec))
	}
	for _, v := range vec {
		if v != 0 {
			t.Error("noop embedder should return zeros")
		}
	}
}

func TestNoOpEmbedder_Name(t *testing.T) {
	e := NewNoOpEmbedder(100)
	if e.Name() != "noop" {
		t.Errorf("expected noop, got %s", e.Name())
	}
}
