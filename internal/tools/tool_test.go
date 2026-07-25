package tools

import (
	"context"
	"sync"
	"testing"
)

type mockTool struct {
	name string
}

func (m *mockTool) Name() string                { return m.name }
func (m *mockTool) Description() string         { return "mock tool" }
func (m *mockTool) Parameters() map[string]interface{} { return nil }
func (m *mockTool) RequiresHITL(params map[string]interface{}) bool { return false }
func (m *mockTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	return &ToolResult{Output: "mock output"}, nil
}

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	registry := NewToolRegistry()

	tool := &mockTool{name: "test_tool"}
	registry.Register(tool)

	got, ok := registry.Get("test_tool")
	if !ok {
		t.Fatal("Get() returned false for registered tool")
	}
	if got.Name() != "test_tool" {
		t.Errorf("Name() = %q, want test_tool", got.Name())
	}
}

func TestToolRegistry_GetNonExistent(t *testing.T) {
	registry := NewToolRegistry()

	_, ok := registry.Get("nonexistent")
	if ok {
		t.Error("Get() returned true for non-existent tool")
	}
}

func TestToolRegistry_List(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register(&mockTool{name: "tool_a"})
	registry.Register(&mockTool{name: "tool_b"})
	registry.Register(&mockTool{name: "tool_c"})

	names := registry.List()
	if len(names) != 3 {
		t.Errorf("List() returned %d tools, want 3", len(names))
	}
}

func TestToolRegistry_Empty(t *testing.T) {
	registry := NewToolRegistry()

	_, ok := registry.Get("anything")
	if ok {
		t.Error("Get() should return false for empty registry")
	}

	names := registry.List()
	if len(names) != 0 {
		t.Errorf("List() on empty registry returned %d items", len(names))
	}
}

func TestRegister_DuplicateName(t *testing.T) {
	r := NewToolRegistry()
	t1 := &mockTool{name: "tool1"}
	t2 := &mockTool{name: "tool1"}
	r.Register(t1)
	r.Register(t2)
	tools := r.List()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

func TestGet_NonExistent(t *testing.T) {
	r := NewToolRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get should return false for nonexistent tool")
	}
}

func TestList_100Tools(t *testing.T) {
	r := NewToolRegistry()
	for i := 0; i < 100; i++ {
		r.Register(&mockTool{name: "tool" + string(rune('A'+i%26))})
	}
	tools := r.List()
	if len(tools) == 0 {
		t.Error("expected tools")
	}
}

func TestListDefs(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&mockTool{name: "test_tool"})
	defs := r.ListDefs()
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
	if defs[0].Name != "test_tool" {
		t.Errorf("expected test_tool, got %s", defs[0].Name)
	}
}

func TestConcurrentRegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.Register(&mockTool{name: "tool" + string(rune('A'+n%26))})
			r.Get("tool" + string(rune('A'+n%26)))
		}(i)
	}
	wg.Wait()
}

func TestToolResult(t *testing.T) {
	result := &ToolResult{
		Output:  "test output",
		Cost:    0.01,
		Success: true,
	}
	if result.Output != "test output" {
		t.Error("output mismatch")
	}
	if !result.Success {
		t.Error("expected success")
	}
}
