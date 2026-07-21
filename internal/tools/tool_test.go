package tools

import (
	"context"
	"testing"
)

type mockTool struct {
	name string
}

func (m *mockTool) Name() string { return m.name }
func (m *mockTool) Description() string { return "mock tool" }
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
