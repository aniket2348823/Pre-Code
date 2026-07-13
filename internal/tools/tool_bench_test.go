package tools

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkRegister benchmarks single-threaded tool registration.
func BenchmarkRegister(b *testing.B) {
	r := NewToolRegistry()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
}

// BenchmarkGet_Hit benchmarks single-threaded Get for an existing tool.
func BenchmarkGet_Hit(b *testing.B) {
	r := NewToolRegistry()
	r.Register(&mockTool{name: "existing_tool"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get("existing_tool")
	}
}

// BenchmarkGet_Miss benchmarks single-threaded Get for a missing tool.
func BenchmarkGet_Miss(b *testing.B) {
	r := NewToolRegistry()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get("nonexistent")
	}
}

// BenchmarkList benchmarks listing all tools.
func BenchmarkList(b *testing.B) {
	r := NewToolRegistry()
	for i := 0; i < 100; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.List()
	}
}

// BenchmarkListDefs benchmarks listing tool definitions.
func BenchmarkListDefs(b *testing.B) {
	r := NewToolRegistry()
	for i := 0; i < 100; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ListDefs()
	}
}

// BenchmarkConcurrentRegisterGet benchmarks concurrent Register+Get operations.
func BenchmarkConcurrentRegisterGet(b *testing.B) {
	r := NewToolRegistry()
	// Pre-register some tools
	for i := 0; i < 10; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("pre_%d", i)})
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := fmt.Sprintf("tool_%d", i%100)
			if i%3 == 0 {
				r.Register(&mockTool{name: name})
			} else {
				r.Get(name)
			}
			i++
		}
	})
}

// BenchmarkConcurrentReads benchmarks concurrent read-only operations.
func BenchmarkConcurrentReads(b *testing.B) {
	r := NewToolRegistry()
	for i := 0; i < 100; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			r.Get(fmt.Sprintf("tool_%d", i%100))
			i++
		}
	})
}

// BenchmarkConcurrentWrites benchmarks concurrent write-only operations.
func BenchmarkConcurrentWrites(b *testing.B) {
	r := NewToolRegistry()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
			i++
		}
	})
}

// BenchmarkMixedWorkload benchmarks a realistic mixed read/write workload.
func BenchmarkMixedWorkload(b *testing.B) {
	r := NewToolRegistry()
	// Pre-populate with tools
	for i := 0; i < 50; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := fmt.Sprintf("tool_%d", i%100)
			switch i % 10 {
			case 0, 1: // 20% writes
				r.Register(&mockTool{name: name})
			case 2, 3, 4: // 30% gets
				r.Get(name)
			case 5, 6, 7: // 30% list
				r.List()
			case 8, 9: // 20% listdefs
				r.ListDefs()
			}
			i++
		}
	})
}

// BenchmarkRegister_1000Tools benchmarks registering 1000 tools sequentially.
func BenchmarkRegister_1000Tools(b *testing.B) {
	for n := 0; n < b.N; n++ {
		r := NewToolRegistry()
		for i := 0; i < 1000; i++ {
			r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
		}
	}
}

// BenchmarkGet_Sequential benchmarks sequential Get operations on a populated registry.
func BenchmarkGet_Sequential(b *testing.B) {
	r := NewToolRegistry()
	for i := 0; i < 1000; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get(fmt.Sprintf("tool_%d", i%1000))
	}
}

// BenchmarkExecute benchmarks tool execution.
func BenchmarkExecute(b *testing.B) {
	tool := &mockTool{name: "test_tool"}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Execute(ctx, nil)
	}
}

// BenchmarkConcurrentMixedOperations benchmarks concurrent mixed operations
// with multiple goroutines doing different operations.
func BenchmarkConcurrentMixedOperations(b *testing.B) {
	r := NewToolRegistry()
	for i := 0; i < 50; i++ {
		r.Register(&mockTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := fmt.Sprintf("tool_%d", i%100)
			switch i % 5 {
			case 0:
				r.Register(&mockTool{name: name})
			case 1, 2:
				r.Get(name)
			case 3:
				r.List()
			case 4:
				r.ListDefs()
			}
			i++
		}
	})
}
