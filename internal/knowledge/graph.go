// Package knowledge implements a pgvector-backed knowledge graph for
// relationship-aware validation. It stores entity relationships in PostgreSQL
// with pgvector embeddings for semantic search, enabling the pipeline to
// validate: "payment service uses database that stores PII → mandatory controls cascade."
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// encoding/json is imported at the top of the file

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
	ProjectID  string            `json:"project_id,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Embedding  []float32         `json:"-"` // vector embedding for semantic search
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// Edge represents a directed relationship between two nodes.
type Edge struct {
	From       string    `json:"from"`
	To         string    `json:"to"`
	Relation   string    `json:"relation"`
	Confidence float64   `json:"confidence,omitempty"`
	ProjectID  string    `json:"project_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ControlRule defines mandatory controls for a given entity pattern.
type ControlRule struct {
	Entity   string   `json:"entity"`   // e.g., "payment", "auth", "admin"
	Controls []string `json:"controls"` // e.g., ["audit_log", "encryption", "rate_limiting"]
	Severity string   `json:"severity"` // "critical", "high", "medium"
}

// DefaultControlRules returns the built-in control rules for known entity patterns.
func DefaultControlRules() []ControlRule {
	return []ControlRule{
		{Entity: "payment", Controls: []string{"audit_log", "encryption", "rate_limiting", "fraud_detection", "pci_compliance"}, Severity: "critical"},
		{Entity: "auth", Controls: []string{"session_management", "mfa", "password_policy", "brute_force_protection"}, Severity: "critical"},
		{Entity: "admin", Controls: []string{"access_control", "audit_log", "mfa", "ip_allowlist"}, Severity: "critical"},
		{Entity: "database", Controls: []string{"encryption_at_rest", "backup", "access_control", "connection_encryption"}, Severity: "high"},
		{Entity: "api", Controls: []string{"rate_limiting", "input_validation", "authentication", "request_size_limits"}, Severity: "high"},
		{Entity: "file", Controls: []string{"path_traversal_protection", "content_validation", "size_limits"}, Severity: "medium"},
		{Entity: "email", Controls: []string{"header_injection_protection", "rate_limiting", "unsubscribe"}, Severity: "medium"},
		{Entity: "webhook", Controls: []string{"signature_validation", "ssrf_protection", "timeout", "retry_limits"}, Severity: "high"},
		{Entity: "secrets", Controls: []string{"encryption", "access_control", "rotation", "audit_logging"}, Severity: "critical"},
		{Entity: "logging", Controls: []string{"pii_redaction", "log_injection_protection", "retention_policy"}, Severity: "medium"},
	}
}

// Graph is a pgvector-backed knowledge graph with in-memory cache.
type Graph struct {
	mu           sync.RWMutex
	nodes        map[string]*Node
	edges        []Edge
	index        map[string]map[string]bool // nodeID → set of connected nodeIDs
	controlRules []ControlRule
	pool         *pgxpool.Pool // nil = in-memory only mode
}

// NewGraph creates an empty in-memory knowledge graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:        make(map[string]*Node),
		index:        make(map[string]map[string]bool),
		controlRules: DefaultControlRules(),
	}
}

// NewGraphWithDB creates a pgvector-backed knowledge graph.
func NewGraphWithDB(pool *pgxpool.Pool) *Graph {
	g := NewGraph()
	g.pool = pool
	if pool != nil {
		if err := g.ensureTables(context.Background()); err != nil {
			slog.Warn("knowledge graph: failed to create tables, using in-memory mode", "error", err)
			g.pool = nil
		} else {
			slog.Info("knowledge graph: pgvector-backed mode initialized")
		}
	}
	return g
}

// ensureTables creates the knowledge graph tables if they don't exist.
func (g *Graph) ensureTables(ctx context.Context) error {
	if g.pool == nil {
		return nil
	}

	// Ensure pg_trgm extension for trigram search
	if _, err := g.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_trgm"); err != nil {
		slog.Warn("failed to ensure pg_trgm extension", "error", err)
	}

	_, err := g.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS kg_nodes (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			name TEXT NOT NULL,
			project_id TEXT DEFAULT '',
			attributes JSONB DEFAULT '{}',
			embedding vector(1536),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_kg_nodes_project ON kg_nodes(project_id);
		CREATE INDEX IF NOT EXISTS idx_kg_nodes_type ON kg_nodes(type);
		CREATE INDEX IF NOT EXISTS idx_kg_nodes_name ON kg_nodes USING gin(name gin_trgm_ops);

		CREATE TABLE IF NOT EXISTS kg_edges (
			id SERIAL PRIMARY KEY,
			from_node TEXT NOT NULL,
			to_node TEXT NOT NULL,
			relation TEXT NOT NULL,
			confidence DOUBLE PRECISION DEFAULT 0.5,
			project_id TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		);
		CREATE INDEX IF NOT EXISTS idx_kg_edges_from ON kg_edges(from_node);
		CREATE INDEX IF NOT EXISTS idx_kg_edges_to ON kg_edges(to_node);

		CREATE TABLE IF NOT EXISTS kg_control_rules (
			id SERIAL PRIMARY KEY,
			entity TEXT NOT NULL,
			controls TEXT[] NOT NULL,
			severity TEXT DEFAULT 'medium',
			project_id TEXT DEFAULT ''
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create knowledge graph tables: %w", err)
	}

	// Load default control rules if table is empty
	var count int
	g.pool.QueryRow(ctx, "SELECT COUNT(*) FROM kg_control_rules").Scan(&count)
	if count == 0 {
		for _, rule := range DefaultControlRules() {
			_, err := g.pool.Exec(ctx,
				"INSERT INTO kg_control_rules (entity, controls, severity) VALUES ($1, $2, $3)",
				rule.Entity, rule.Controls, rule.Severity)
			if err != nil {
				slog.Warn("failed to insert control rule", "entity", rule.Entity, "error", err)
			}
		}
	}

	return nil
}

// AddNode adds a node to the graph and persists to DB if available.
func (g *Graph) AddNode(n *Node) {
	g.mu.Lock()
	now := time.Now()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	g.nodes[n.ID] = n
	if g.index[n.ID] == nil {
		g.index[n.ID] = make(map[string]bool)
	}

	// Copy data for DB persistence
	pool := g.pool
	var attrsJSON string
	if n.Attributes != nil {
		b, _ := json.Marshal(n.Attributes)
		attrsJSON = string(b)
	} else {
		attrsJSON = "{}"
	}
	var embedding interface{}
	if len(n.Embedding) > 0 {
		embedding = pgvector.NewVector(n.Embedding)
	}
	id := n.ID
	nodeType := string(n.Type)
	name := n.Name
	projectID := n.ProjectID
	createdAt := n.CreatedAt
	updatedAt := n.UpdatedAt
	g.mu.Unlock()

	// Persist to DB (outside lock)
	if pool != nil {
		ctx := context.Background()
		_, err := pool.Exec(ctx, `
			INSERT INTO kg_nodes (id, type, name, project_id, attributes, embedding, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET
				type = EXCLUDED.type, name = EXCLUDED.name, project_id = EXCLUDED.project_id,
				attributes = EXCLUDED.attributes, embedding = EXCLUDED.embedding, updated_at = EXCLUDED.updated_at`,
			id, nodeType, name, projectID, attrsJSON, embedding, createdAt, updatedAt)
		if err != nil {
			slog.Warn("knowledge graph: failed to persist node", "id", id, "error", err)
		}
	}
}

// AddEdge adds a directed edge between two existing nodes and persists to DB.
func (g *Graph) AddEdge(e Edge) {
	g.mu.Lock()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	g.edges = append(g.edges, e)
	if g.index[e.From] == nil {
		g.index[e.From] = make(map[string]bool)
	}
	g.index[e.From][e.To] = true

	// Copy data for DB persistence
	pool := g.pool
	from := e.From
	to := e.To
	relation := e.Relation
	confidence := e.Confidence
	projectID := e.ProjectID
	createdAt := e.CreatedAt
	g.mu.Unlock()

	// Persist to DB (outside lock)
	if pool != nil {
		ctx := context.Background()
		_, err := pool.Exec(ctx,
			"INSERT INTO kg_edges (from_node, to_node, relation, confidence, project_id, created_at) VALUES ($1, $2, $3, $4, $5, $6)",
			from, to, relation, confidence, projectID, createdAt)
		if err != nil {
			slog.Warn("knowledge graph: failed to persist edge", "from", from, "to", to, "error", err)
		}
	}
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

// ValidatePrompt checks a prompt and code against the knowledge graph's control rules.
// Returns a list of warnings for missing mandatory controls.
func (g *Graph) ValidatePrompt(ctx context.Context, prompt string, code string, projectID string) []ValidationWarning {
	lower := strings.ToLower(prompt + " " + code)
	var warnings []ValidationWarning

	g.mu.RLock()
	rules := make([]ControlRule, len(g.controlRules))
	copy(rules, g.controlRules)
	g.mu.RUnlock()

	// Check each control rule
	for _, rule := range rules {
		if strings.Contains(lower, rule.Entity) {
			for _, control := range rule.Controls {
				// Check if the control is mentioned (with various separators)
				controlNormalized := strings.ReplaceAll(control, "_", " ")
				controlHyphen := strings.ReplaceAll(control, "_", "-")
				if !strings.Contains(lower, controlNormalized) &&
					!strings.Contains(lower, controlHyphen) &&
					!strings.Contains(lower, control) {
					warnings = append(warnings, ValidationWarning{
						Entity:   rule.Entity,
						Control:  control,
						Severity: rule.Severity,
						Message:  fmt.Sprintf("%s requires %s", rule.Entity, control),
					})
				}
			}
		}
	}

	// Also check project-specific rules from DB if available
	if g.pool != nil {
		dbWarnings := g.checkDBRules(ctx, lower, projectID)
		warnings = append(warnings, dbWarnings...)
	}

	return warnings
}

// checkDBRules checks against project-specific rules stored in the database.
func (g *Graph) checkDBRules(ctx context.Context, lower string, projectID string) []ValidationWarning {
	var warnings []ValidationWarning

	rows, err := g.pool.Query(ctx,
		"SELECT entity, controls, severity FROM kg_control_rules WHERE project_id = $1 OR project_id = ''",
		projectID)
	if err != nil {
		return warnings
	}
	defer rows.Close()

	for rows.Next() {
		var entity, severity string
		var controls []string
		if err := rows.Scan(&entity, &controls, &severity); err != nil {
			continue
		}
		if strings.Contains(lower, entity) {
			for _, control := range controls {
				controlNorm := strings.ReplaceAll(control, "_", " ")
				if !strings.Contains(lower, controlNorm) && !strings.Contains(lower, control) {
					warnings = append(warnings, ValidationWarning{
						Entity:   entity,
						Control:  control,
						Severity: severity,
						Message:  fmt.Sprintf("%s requires %s", entity, control),
					})
				}
			}
		}
	}

	return warnings
}

// ValidationWarning represents a missing mandatory control.
type ValidationWarning struct {
	Entity   string `json:"entity"`
	Control  string `json:"control"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// AddControlRule adds a custom control rule for a project.
func (g *Graph) AddControlRule(ctx context.Context, rule ControlRule, projectID string) error {
	if g.pool != nil {
		_, err := g.pool.Exec(ctx,
			"INSERT INTO kg_control_rules (entity, controls, severity, project_id) VALUES ($1, $2, $3, $4)",
			rule.Entity, rule.Controls, rule.Severity, projectID)
		return err
	}
	return nil
}

// GetControlRules returns all control rules (built-in + project-specific).
func (g *Graph) GetControlRules(ctx context.Context, projectID string) []ControlRule {
	g.mu.RLock()
	rules := make([]ControlRule, len(g.controlRules))
	copy(rules, g.controlRules)
	g.mu.RUnlock()

	if g.pool != nil {
		rows, err := g.pool.Query(ctx,
			"SELECT entity, controls, severity FROM kg_control_rules WHERE project_id = $1", projectID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rule ControlRule
				if err := rows.Scan(&rule.Entity, &rule.Controls, &rule.Severity); err == nil {
					rules = append(rules, rule)
				}
			}
		}
	}

	return rules
}

// LoadFromDB loads all nodes and edges from the database into memory.
func (g *Graph) LoadFromDB(ctx context.Context) error {
	if g.pool == nil {
		return nil
	}

	// Load nodes
	rows, err := g.pool.Query(ctx, "SELECT id, type, name, project_id, created_at, updated_at FROM kg_nodes")
	if err != nil {
		return fmt.Errorf("failed to query nodes: %w", err)
	}
	defer rows.Close()

	nodes := make(map[string]*Node)
	for rows.Next() {
		var n Node
		var typeName string
		if err := rows.Scan(&n.ID, &typeName, &n.Name, &n.ProjectID, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		n.Type = EntityType(typeName)
		nodes[n.ID] = &n
	}

	// Load edges
	edgeRows, err := g.pool.Query(ctx, "SELECT from_node, to_node, relation, confidence, project_id, created_at FROM kg_edges")
	if err != nil {
		return fmt.Errorf("failed to query edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []Edge
	index := make(map[string]map[string]bool)
	for edgeRows.Next() {
		var e Edge
		if err := edgeRows.Scan(&e.From, &e.To, &e.Relation, &e.Confidence, &e.ProjectID, &e.CreatedAt); err != nil {
			continue
		}
		edges = append(edges, e)
		if index[e.From] == nil {
			index[e.From] = make(map[string]bool)
		}
		index[e.From][e.To] = true
	}

	// Apply under a single lock
	g.mu.Lock()
	for id, n := range nodes {
		g.nodes[id] = n
		if g.index[id] == nil {
			g.index[id] = make(map[string]bool)
		}
	}
	g.edges = append(g.edges, edges...)
	for from, tos := range index {
		if g.index[from] == nil {
			g.index[from] = make(map[string]bool)
		}
		for to := range tos {
			g.index[from][to] = true
		}
	}
	g.mu.Unlock()

	slog.Info("knowledge graph loaded from database",
		"nodes", len(g.nodes),
		"edges", len(g.edges),
	)
	return nil
}
