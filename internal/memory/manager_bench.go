package memory

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkWorkingMemory_Add(b *testing.B) {
	wm := NewWorkingMemory(30 * time.Minute)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wm.Add(Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
			Tokens:  10,
		})
	}
}

func BenchmarkWorkingMemory_Search(b *testing.B) {
	wm := NewWorkingMemory(30 * time.Minute)
	for i := 0; i < 1000; i++ {
		wm.Add(Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d with keyword", i),
			Tokens:  10,
		})
	}

	b.Run("found", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			wm.Search("keyword", 10)
		}
	})

	b.Run("not_found", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			wm.Search("nonexistent", 10)
		}
	})

	b.Run("limit_1", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			wm.Search("keyword", 1)
		}
	})

	b.Run("limit_all", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			wm.Search("keyword", 1000)
		}
	})
}

func BenchmarkWorkingMemory_Get(b *testing.B) {
	wm := NewWorkingMemory(30 * time.Minute)
	for i := 0; i < 100; i++ {
		wm.Add(Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
			Tokens:  10,
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wm.Get()
	}
}

func BenchmarkWorkingMemory_Clear(b *testing.B) {
	for i := 0; i < b.N; i++ {
		wm := NewWorkingMemory(30 * time.Minute)
		for j := 0; j < 100; j++ {
			wm.Add(Message{
				Role:    "user",
				Content: fmt.Sprintf("message %d", j),
				Tokens:  10,
			})
		}
		b.ResetTimer()
		wm.Clear()
	}
}

func BenchmarkWorkingMemory_ConcurrentAdd(b *testing.B) {
	wm := NewWorkingMemory(30 * time.Minute)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			wm.Add(Message{
				Role:    "user",
				Content: fmt.Sprintf("concurrent message %d", i),
				Tokens:  10,
			})
			i++
		}
	})
}

func BenchmarkWorkingMemory_TokenCount(b *testing.B) {
	wm := NewWorkingMemory(30 * time.Minute)
	for i := 0; i < 100; i++ {
		wm.Add(Message{
			Role:    "user",
			Content: fmt.Sprintf("message %d", i),
			Tokens:  10 + i,
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wm.TokenCount()
	}
}

func BenchmarkProceduralStore_Store(b *testing.B) {
	store := NewProceduralStore()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Store(context.Background(), &Workflow{
			ID:     fmt.Sprintf("wf-%d", i),
			UserID: "user-1",
			Name:   fmt.Sprintf("workflow %d", i),
		})
	}
}

func BenchmarkProceduralStore_Get(b *testing.B) {
	store := NewProceduralStore()
	for i := 0; i < 100; i++ {
		_ = store.Store(context.Background(), &Workflow{
			ID:     fmt.Sprintf("wf-%d", i),
			UserID: "user-1",
			Name:   fmt.Sprintf("workflow %d", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Get(context.Background(), fmt.Sprintf("wf-%d", i%100))
	}
}

func BenchmarkProceduralStore_Search(b *testing.B) {
	store := NewProceduralStore()
	for i := 0; i < 100; i++ {
		_ = store.Store(context.Background(), &Workflow{
			ID:          fmt.Sprintf("wf-%d", i),
			UserID:      "user-1",
			Name:        fmt.Sprintf("deploy workflow %d", i),
			Description: fmt.Sprintf("automated deployment step %d", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search(context.Background(), "deploy", 10)
	}
}

func BenchmarkProceduralStore_ListByUser(b *testing.B) {
	store := NewProceduralStore()
	for i := 0; i < 100; i++ {
		uid := "user-1"
		if i%2 == 0 {
			uid = "user-2"
		}
		_ = store.Store(context.Background(), &Workflow{
			ID:     fmt.Sprintf("wf-%d", i),
			UserID: uid,
			Name:   fmt.Sprintf("workflow %d", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.ListByUser(context.Background(), "user-1", 50)
	}
}

func BenchmarkNoOpEmbedder_Embed(b *testing.B) {
	embedder := NewNoOpEmbedder(1536)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = embedder.Embed(ctx, "test query for embedding")
	}
}

func BenchmarkManager_AddWorkingMessage(b *testing.B) {
	mgr := &Manager{}
	mgr.initWorkingMemory()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.AddWorkingMessage("user", fmt.Sprintf("message %d", i), 10)
	}
}

func BenchmarkManager_GetWorkingMessages(b *testing.B) {
	mgr := &Manager{}
	mgr.initWorkingMemory()
	for i := 0; i < 100; i++ {
		mgr.AddWorkingMessage("user", fmt.Sprintf("message %d", i), 10)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.GetWorkingMessages()
	}
}

func BenchmarkManager_WorkingCount(b *testing.B) {
	mgr := &Manager{}
	mgr.initWorkingMemory()
	for i := 0; i < 100; i++ {
		mgr.AddWorkingMessage("user", fmt.Sprintf("message %d", i), 10)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mgr.WorkingCount()
	}
}

func BenchmarkManager_ClearWorkingMemory(b *testing.B) {
	for i := 0; i < b.N; i++ {
		mgr := &Manager{}
		mgr.initWorkingMemory()
		for j := 0; j < 100; j++ {
			mgr.AddWorkingMessage("user", fmt.Sprintf("message %d", j), 10)
		}
		b.ResetTimer()
		mgr.ClearWorkingMemory()
	}
}
