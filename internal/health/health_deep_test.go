package health

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOverall_NoComponents(t *testing.T) {
	h := New(time.Second)
	if h.Overall() != StatusUnknown {
		t.Errorf("expected unknown for empty, got %s", h.Overall())
	}
}

func TestOverall_AllHealthy(t *testing.T) {
	h := New(time.Second)
	for i := 0; i < 10; i++ {
		h.Register("c"+string(rune('0'+i)), func() Component {
			return Component{Status: StatusHealthy}
		})
	}
	h.RunChecks()
	if h.Overall() != StatusHealthy {
		t.Errorf("expected healthy, got %s", h.Overall())
	}
}

func TestOverall_OneDegraded(t *testing.T) {
	h := New(time.Second)
	h.Register("healthy", func() Component { return Component{Status: StatusHealthy} })
	h.Register("degraded", func() Component { return Component{Status: StatusDegraded} })
	h.RunChecks()
	if h.Overall() != StatusDegraded {
		t.Errorf("expected degraded, got %s", h.Overall())
	}
}

func TestOverall_OneUnhealthy(t *testing.T) {
	h := New(time.Second)
	h.Register("healthy", func() Component { return Component{Status: StatusHealthy} })
	h.Register("degraded", func() Component { return Component{Status: StatusDegraded} })
	h.Register("unhealthy", func() Component { return Component{Status: StatusUnhealthy} })
	h.RunChecks()
	if h.Overall() != StatusUnhealthy {
		t.Errorf("expected unhealthy, got %s", h.Overall())
	}
}

func TestRegister_Overwrite(t *testing.T) {
	h := New(time.Second)
	h.Register("comp", func() Component { return Component{Status: StatusHealthy} })
	h.Register("comp", func() Component { return Component{Status: StatusUnhealthy} })
	h.RunChecks()
	c := h.GetComponent("comp")
	if c.Status != StatusUnhealthy {
		t.Errorf("expected overwritten status unhealthy, got %s", c.Status)
	}
}

func TestRunChecks_Concurrent(t *testing.T) {
	h := New(time.Second)
	var count int64
	numChecks := 10
	numGoroutines := 5
	for i := 0; i < numChecks; i++ {
		h.Register("c"+string(rune('0'+i)), func() Component {
			atomic.AddInt64(&count, 1)
			return Component{Status: StatusHealthy}
		})
	}
	var wg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.RunChecks()
		}()
	}
	wg.Wait()
	// Each goroutine runs all checks; total should be >= numGoroutines * numChecks
	minExpected := int64(numGoroutines * numChecks)
	got := atomic.LoadInt64(&count)
	if got < minExpected {
		t.Errorf("expected at least %d checks, got %d", minExpected, got)
	}
}

func TestGetComponent_ReturnsConsistentData(t *testing.T) {
	h := New(time.Second)
	h.Register("comp", func() Component {
		return Component{Status: StatusHealthy, Metadata: map[string]string{"key": "val"}}
	})
	h.RunChecks()
	// Fetch twice — both should return the same data
	c1 := h.GetComponent("comp")
	c2 := h.GetComponent("comp")
	if c1.Metadata["key"] != c2.Metadata["key"] {
		t.Error("GetComponent should return consistent data")
	}
	if c1.Status != c2.Status {
		t.Error("status should be consistent across calls")
	}
}

func TestAllComponents_Empty(t *testing.T) {
	h := New(time.Second)
	all := h.AllComponents()
	if len(all) != 0 {
		t.Errorf("expected 0 components, got %d", len(all))
	}
}

func TestSummary_Empty(t *testing.T) {
	h := New(time.Second)
	s := h.Summary()
	if s["overall"] != "unknown" {
		t.Errorf("expected unknown, got %v", s["overall"])
	}
}

func TestSummary_AllStatuses(t *testing.T) {
	h := New(time.Second)
	h.Register("healthy", func() Component { return Component{Status: StatusHealthy} })
	h.Register("degraded", func() Component { return Component{Status: StatusDegraded} })
	h.Register("unhealthy", func() Component { return Component{Status: StatusUnhealthy} })
	h.RunChecks()
	s := h.Summary()
	if s["overall"] != "unhealthy" {
		t.Errorf("expected unhealthy overall, got %v", s["overall"])
	}
	statuses, ok := s["statuses"].(map[string]int)
	if !ok {
		t.Fatal("expected statuses map")
	}
	if statuses["healthy"] != 1 || statuses["degraded"] != 1 || statuses["unhealthy"] != 1 {
		t.Errorf("expected 1 of each status, got %v", statuses)
	}
}

func TestStart_StopMultipleTimes(t *testing.T) {
	h := New(50 * time.Millisecond)
	h.Register("tick", func() Component { return Component{Status: StatusHealthy} })
	h.Start()
	time.Sleep(80 * time.Millisecond)
	h.Stop()
	h.Stop()
	h.Stop()
}

func TestRunChecks_LatencyTracked(t *testing.T) {
	h := New(time.Second)
	h.Register("slow", func() Component {
		time.Sleep(50 * time.Millisecond)
		return Component{Status: StatusHealthy}
	})
	h.RunChecks()
	c := h.GetComponent("slow")
	if c.LatencyMs < 40 {
		t.Errorf("expected latency >= 40ms, got %d", c.LatencyMs)
	}
}

func TestRunChecks_ComponentNameFallback(t *testing.T) {
	h := New(time.Second)
	h.Register("my-component", func() Component {
		return Component{Status: StatusHealthy} // Name is empty, should use registered name
	})
	h.RunChecks()
	c := h.GetComponent("my-component")
	if c.Name != "my-component" {
		t.Errorf("expected name 'my-component', got %q", c.Name)
	}
}
