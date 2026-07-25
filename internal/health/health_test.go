package health

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	h := New(time.Second)
	if h == nil {
		t.Fatal("expected non-nil health checker")
	}
}

func TestRegisterAndCheck(t *testing.T) {
	h := New(time.Second)
	h.Register("test-component", func() Component {
		return Component{Status: StatusHealthy, Message: "all good"}
	})
	h.RunChecks()
	c := h.GetComponent("test-component")
	if c == nil {
		t.Fatal("expected component")
	}
	if c.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", c.Status)
	}
}

func TestOverallHealthy(t *testing.T) {
	h := New(time.Second)
	h.Register("a", func() Component { return Component{Status: StatusHealthy} })
	h.Register("b", func() Component { return Component{Status: StatusHealthy} })
	h.RunChecks()
	if h.Overall() != StatusHealthy {
		t.Errorf("expected healthy overall, got %s", h.Overall())
	}
}

func TestOverallDegraded(t *testing.T) {
	h := New(time.Second)
	h.Register("a", func() Component { return Component{Status: StatusHealthy} })
	h.Register("b", func() Component { return Component{Status: StatusDegraded} })
	h.RunChecks()
	if h.Overall() != StatusDegraded {
		t.Errorf("expected degraded overall, got %s", h.Overall())
	}
}

func TestOverallUnhealthy(t *testing.T) {
	h := New(time.Second)
	h.Register("a", func() Component { return Component{Status: StatusHealthy} })
	h.Register("b", func() Component { return Component{Status: StatusUnhealthy} })
	h.RunChecks()
	if h.Overall() != StatusUnhealthy {
		t.Errorf("expected unhealthy overall, got %s", h.Overall())
	}
}

func TestStartStop(t *testing.T) {
	h := New(50 * time.Millisecond)
	h.Register("tick", func() Component { return Component{Status: StatusHealthy} })
	h.Start()
	time.Sleep(120 * time.Millisecond)
	h.Stop()
	c := h.GetComponent("tick")
	if c == nil || c.Status != StatusHealthy {
		t.Error("expected component to be checked")
	}
}

func TestAllComponents(t *testing.T) {
	h := New(time.Second)
	h.Register("x", func() Component { return Component{Status: StatusHealthy} })
	h.Register("y", func() Component { return Component{Status: StatusDegraded} })
	h.RunChecks()
	all := h.AllComponents()
	if len(all) != 2 {
		t.Errorf("expected 2 components, got %d", len(all))
	}
}

func TestSummary(t *testing.T) {
	h := New(time.Second)
	h.Register("a", func() Component { return Component{Status: StatusHealthy} })
	h.RunChecks()
	s := h.Summary()
	if s["total"] != 1 {
		t.Errorf("expected 1 component, got %v", s["total"])
	}
}

func TestGetComponentNotFound(t *testing.T) {
	h := New(time.Second)
	if h.GetComponent("nonexistent") != nil {
		t.Error("expected nil for nonexistent component")
	}
}

func TestOverallUnknown(t *testing.T) {
	h := New(time.Second)
	if h.Overall() != StatusUnknown {
		t.Errorf("expected unknown for empty, got %s", h.Overall())
	}
}

func TestDoubleStop(t *testing.T) {
	h := New(50 * time.Millisecond)
	h.Register("tick", func() Component { return Component{Status: StatusHealthy} })
	h.Start()
	time.Sleep(80 * time.Millisecond)
	h.Stop()
	h.Stop() // should not panic
}

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

func TestSummary_DegradedNoUnhealthy(t *testing.T) {
	h := New(time.Second)
	h.Register("healthy", func() Component { return Component{Status: StatusHealthy} })
	h.Register("degraded", func() Component { return Component{Status: StatusDegraded} })
	h.RunChecks()
	s := h.Summary()
	if s["overall"] != "degraded" {
		t.Errorf("expected degraded overall, got %v", s["overall"])
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
	if statuses["unhealthy"] != 1 {
		t.Errorf("expected 1 unhealthy, got %v", statuses["unhealthy"])
	}
	if statuses["degraded"] != 1 {
		t.Errorf("expected 1 degraded, got %v", statuses["degraded"])
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
