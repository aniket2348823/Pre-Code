package knowledge

import (
	"sync"
	"testing"
)

func TestGraph_ConcurrentAccess(t *testing.T) {
	g := NewGraph()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			g.AddNode(&Node{
				ID:   "node-" + string(rune('a'+id)),
				Type: EntityService,
				Name: "Service",
			})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.Count()
			g.GetNode("node-a")
			g.Reachable("node-a", 5)
		}()
	}

	// Concurrent edge writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.AddEdge(Edge{From: "node-a", To: "node-b", Relation: "uses"})
		}()
	}

	wg.Wait()

	nodes, edges := g.Count()
	if nodes == 0 {
		t.Error("expected at least 1 node after concurrent writes")
	}
	// Edges may or may not have been added depending on timing, but no panic
	t.Logf("final state: %d nodes, %d edges (no data race)", nodes, edges)
}
