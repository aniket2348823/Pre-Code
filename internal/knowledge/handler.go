package knowledge

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// QueryRequest represents a knowledge graph operation.
type QueryRequest struct {
	Operation string `json:"operation"` // "add_node", "add_edge", "get_node", "reachable", "nodes_by_type", "count"
	NodeID    string `json:"node_id,omitempty"`
	Node      *Node  `json:"node,omitempty"`
	Edge      *Edge  `json:"edge,omitempty"`
	StartID   string `json:"start_id,omitempty"`
	MaxDepth  int    `json:"max_depth,omitempty"`
	NodeType  string `json:"node_type,omitempty"`
}

// NewHTTPHandler creates a handler for the knowledge graph API.
// The graph parameter must be non-nil.
func NewHTTPHandler(graph *Graph) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req QueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.Error(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		switch req.Operation {
		case "add_node":
			if req.Node == nil {
				response.Error(w, http.StatusBadRequest, "node is required")
				return
			}
			graph.AddNode(req.Node)
			response.JSON(w, http.StatusOK, map[string]string{"status": "added"})

		case "add_edge":
			if req.Edge == nil {
				response.Error(w, http.StatusBadRequest, "edge is required")
				return
			}
			graph.AddEdge(*req.Edge)
			response.JSON(w, http.StatusOK, map[string]string{"status": "added"})

		case "get_node":
			if req.NodeID == "" {
				response.Error(w, http.StatusBadRequest, "node_id is required")
				return
			}
			node, ok := graph.GetNode(req.NodeID)
			if !ok {
				response.NotFound(w, "node not found")
				return
			}
			response.JSON(w, http.StatusOK, node)

		case "reachable":
			if req.StartID == "" {
				response.Error(w, http.StatusBadRequest, "start_id is required")
				return
			}
			if req.MaxDepth <= 0 {
				req.MaxDepth = 5
			}
			ids := graph.Reachable(req.StartID, req.MaxDepth)
			response.JSON(w, http.StatusOK, map[string]any{
				"start_id":  req.StartID,
				"reachable": ids,
				"count":     len(ids),
			})

		case "nodes_by_type":
			if req.NodeType == "" {
				response.Error(w, http.StatusBadRequest, "node_type is required")
				return
			}
			nodes := graph.NodesByType(EntityType(req.NodeType))
			response.JSON(w, http.StatusOK, map[string]any{
				"type":  req.NodeType,
				"nodes": nodes,
				"count": len(nodes),
			})

		case "count":
			nodes, edges := graph.Count()
			response.JSON(w, http.StatusOK, map[string]int{
				"nodes": nodes,
				"edges": edges,
			})

		default:
			response.Error(w, http.StatusBadRequest, "unknown operation: "+req.Operation)
		}
	}
}
