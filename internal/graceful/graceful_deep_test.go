package graceful

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestShutdown_AlreadyCancelledContext(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 1*time.Second)
	// Server not started, shutdown should still work
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestShutdown_TwiceNoPanic(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 1*time.Second)
	if err := s.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatal("double shutdown should not error")
	}
}

func TestShutdown_ZeroTimeout(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 0)
	if s.timeout != 15*time.Second {
		t.Errorf("zero timeout should default to 15s, got %v", s.timeout)
	}
}

func TestShutdown_ConcurrentShutdown(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 1*time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Shutdown()
		}()
	}
	wg.Wait()
}

func TestShutdown_DuringActiveRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	s := New(handler, ":0", 5*time.Second)
	// Should not panic
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestAddr(t *testing.T) {
	s := New(http.DefaultServeMux, ":9090", 10*time.Second)
	if s.Addr() != ":9090" {
		t.Errorf("expected :9090, got %q", s.Addr())
	}
}

func TestNewDefaultTimeout_Deep(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 0)
	if s.timeout != 15*time.Second {
		t.Errorf("expected 15s default, got %v", s.timeout)
	}
}

func TestNewCustomTimeout_Deep(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 30*time.Second)
	if s.timeout != 30*time.Second {
		t.Errorf("expected 30s, got %v", s.timeout)
	}
}

func TestShutdownWithActiveGoroutines(t *testing.T) {
	done := make(chan bool)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-done
		w.WriteHeader(http.StatusOK)
	})
	s := New(handler, ":0", 5*time.Second)
	close(done) // unblock any waiting
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestShutdownWithNilHandler(t *testing.T) {
	s := New(nil, ":0", 1*time.Second)
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown with nil handler: %v", err)
	}
}
