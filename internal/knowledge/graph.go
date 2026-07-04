// Package knowledge implements a lightweight in-memory knowledge graph that
// stores entity relationships (service → database → PII, payment → fraud → encryption)
// for the deterministic engine. This enables relationship-aware validation:
// "payment service uses database that stores PII" → mandatory controls cascade.
package knowledge

import (
	"sort"
	"sync"
)

// EntityType classifies a node in the knowledge graph.
type EntityType string

const (
	EntityService    EntityType = "service"
	EntityDatabase   EntityType = "database"
	EntityData       EntityType = "data"
	EntityThreat     EntityType = "threat"
	EntityControl    EntityType = "control"
	EntityPolicy     EntityType = "policy"
	EntityCompliance EntityType = "compliance"
)

// Node is an entity in the knowledge graph.
type Node struct {
	ID         string            `json:"id"`
	Type       EntityType        `json:"type"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Relation   string `json:"relation"`
	Confidence float64 `json:"confidence,omitempty"`
}

// Graph is an in-memory knowledge graph with thread-safe access.
type Graph struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	edges []Edge
	index map[string]map[string]bool // nodeID → set of connected nodeIDs
}

// NewGraph creates an empty knowledge graph.
func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[string]*Node),
		index: make(map[string]map[string]bool),
	}
}

// AddNode adds a node to the graph. If the node already exists, it is updated.
func (g *Graph) AddNode(n *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[n.ID] = n
}

// AddEdge adds a directed edge between two existing nodes.
func (g *Graph) AddEdge(e Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.edges = append(g.edges, e)
	if g.index[e.From] == nil {
		g.index[e.From] = make(map[string]bool)
	}
	g.index[e.From][e.To] = true
}

// GetNode returns a node by ID.
func (g *Graph) GetNode(id string) (*Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

// GetEdgesFrom returns all edges originating from a node.
func (g *Graph) GetEdgesFrom(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []Edge
	for _, e := range g.edges {
		if e.From == id {
			out = append(out, e)
		}
	}
	return out
}

// GetEdgesTo returns all edges pointing to a node.
func (g *Graph) GetEdgesTo(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []Edge
	for _, e := range g.edges {
		if e.To == id {
			out = append(out, e)
		}
	}
	return out
}

// Reachable returns all node IDs reachable from a starting node via BFS.
func (g *Graph) Reachable(startID string, maxDepth int) []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	visited := map[string]bool{startID: true}
	queue := []string{startID}
	depth := 0

	for len(queue) > 0 && depth < maxDepth {
		next := []string{}
		for _, id := range queue {
			for to := range g.index[id] {
				if !visited[to] {
					visited[to] = true
					next = append(next, to)
				}
			}
		}
		queue = next
		depth++
	}

	out := make([]string, 0, len(visited)-1)
	for id := range visited {
		if id != startID {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// NodesByType returns all nodes of a given type.
func (g *Graph) NodesByType(t EntityType) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*Node
	for _, n := range g.nodes {
		if n.Type == t {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Count returns the number of nodes and edges.
func (g *Graph) Count() (nodes, edges int) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes), len(g.edges)
}
