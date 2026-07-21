package health

import (
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
