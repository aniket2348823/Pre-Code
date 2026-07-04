package knowledge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_AddNode(t *testing.T) {
	handler := NewHTTPHandler(NewGraph())

	body, _ := json.Marshal(QueryRequest{
		Operation: "add_node",
		Node:      &Node{ID: "svc-1", Type: EntityService, Name: "Payment API"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "added" {
		t.Errorf("expected status=added, got %q", resp["status"])
	}
}

func TestHandler_AddNode_Missing(t *testing.T) {
	handler := NewHTTPHandler(NewGraph())

	body, _ := json.Marshal(QueryRequest{Operation: "add_node"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_AddEdge(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "a", Type: EntityService, Name: "A"})
	g.AddNode(&Node{ID: "b", Type: EntityDatabase, Name: "B"})
	handler := NewHTTPHandler(g)

	body, _ := json.Marshal(QueryRequest{
		Operation: "add_edge",
		Edge:      &Edge{From: "a", To: "b", Relation: "uses"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandler_GetNode(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "n1", Type: EntityData, Name: "PII DB"})
	handler := NewHTTPHandler(g)

	body, _ := json.Marshal(QueryRequest{Operation: "get_node", NodeID: "n1"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var node Node
	json.Unmarshal(w.Body.Bytes(), &node)
	if node.Name != "PII DB" {
		t.Errorf("expected name 'PII DB', got %q", node.Name)
	}
}

func TestHandler_GetNode_NotFound(t *testing.T) {
	handler := NewHTTPHandler(NewGraph())

	body, _ := json.Marshal(QueryRequest{Operation: "get_node", NodeID: "nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_Reachable(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "a", Type: EntityService, Name: "A"})
	g.AddNode(&Node{ID: "b", Type: EntityDatabase, Name: "B"})
	g.AddEdge(Edge{From: "a", To: "b", Relation: "uses"})
	handler := NewHTTPHandler(g)

	body, _ := json.Marshal(QueryRequest{Operation: "reachable", StartID: "a", MaxDepth: 3})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["count"].(float64) != 1 {
		t.Errorf("expected 1 reachable node, got %v", resp["count"])
	}
}

func TestHandler_NodesByType(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "s1", Type: EntityService, Name: "S1"})
	g.AddNode(&Node{ID: "d1", Type: EntityDatabase, Name: "D1"})
	handler := NewHTTPHandler(g)

	body, _ := json.Marshal(QueryRequest{Operation: "nodes_by_type", NodeType: "service"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["count"].(float64) != 1 {
		t.Errorf("expected 1 service node, got %v", resp["count"])
	}
}

func TestHandler_Count(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "a", Type: EntityService, Name: "A"})
	g.AddNode(&Node{ID: "b", Type: EntityDatabase, Name: "B"})
	g.AddEdge(Edge{From: "a", To: "b", Relation: "uses"})
	handler := NewHTTPHandler(g)

	body, _ := json.Marshal(QueryRequest{Operation: "count"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]int
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["nodes"] != 2 {
		t.Errorf("expected 2 nodes, got %d", resp["nodes"])
	}
	if resp["edges"] != 1 {
		t.Errorf("expected 1 edge, got %d", resp["edges"])
	}
}

func TestHandler_UnknownOperation(t *testing.T) {
	handler := NewHTTPHandler(NewGraph())

	body, _ := json.Marshal(QueryRequest{Operation: "bogus"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler := NewHTTPHandler(NewGraph())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
