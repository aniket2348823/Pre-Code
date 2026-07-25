package graceful

import (
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

func TestNewDefaultTimeout(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 0)
	if s.timeout != 15*time.Second {
		t.Fatalf("expected 15s default timeout, got %v", s.timeout)
	}
}

func TestNewCustomTimeout(t *testing.T) {
	s := New(http.DefaultServeMux, ":8080", 30*time.Second)
	if s.timeout != 30*time.Second {
		t.Fatalf("expected 30s timeout, got %v", s.timeout)
	}
	if s.Addr() != ":8080" {
		t.Fatalf("expected :8080, got %q", s.Addr())
	}
}

func TestShutdownWithoutServe(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 1*time.Second)
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

func TestAddr(t *testing.T) {
	s := New(http.DefaultServeMux, ":9090", 10*time.Second)
	if s.Addr() != ":9090" {
		t.Errorf("expected :9090, got %q", s.Addr())
	}
}

func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func TestListenAndServe_BlocksAndShutdown(t *testing.T) {
	port := getFreePort(t)
	addr := "127.0.0.1:" + itoa(port)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	s := New(handler, addr, 5*time.Second)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get("http://" + addr)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return in time")
	}
}

func TestListenAndServe_ServesRequests(t *testing.T) {
	port := getFreePort(t)
	addr := "127.0.0.1:" + itoa(port)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	s := New(handler, addr, 2*time.Second)

	go func() {
		s.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get("http://" + addr)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("expected 'ok', got %q", string(body))
	}
	s.Shutdown()
}

func TestListenAndServe_MultipleRequests(t *testing.T) {
	port := getFreePort(t)
	addr := "127.0.0.1:" + itoa(port)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})
	s := New(handler, addr, 2*time.Second)

	go func() {
		s.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 5; i++ {
		resp, err := http.Get("http://" + addr)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	s.Shutdown()
}

func TestListenAndServe_ShutdownDuringServe(t *testing.T) {
	port := getFreePort(t)
	addr := "127.0.0.1:" + itoa(port)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	s := New(handler, addr, 2*time.Second)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ListenAndServe did not return after Shutdown")
	}
}

func TestListenAndServe_AlreadyBoundPort(t *testing.T) {
	port := getFreePort(t)
	addr := "127.0.0.1:" + itoa(port)

	// Bind the port first
	blocker, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to bind port: %v", err)
	}
	defer blocker.Close()

	// ListenAndServe should fail because port is taken
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	s := New(handler, addr, 1*time.Second)

	err = s.ListenAndServe()
	if err == nil {
		t.Fatal("expected error when port is already bound")
	}
}

func TestListenAndServe_SignalShutdown(t *testing.T) {
	port := getFreePort(t)
	addr := "127.0.0.1:" + itoa(port)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	s := New(handler, addr, 5*time.Second)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.ListenAndServe()
	}()

	time.Sleep(200 * time.Millisecond)

	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("find process: %v", err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		t.Skipf("signal not supported: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for shutdown")
	}
}

func TestShutdown_ErrorPath(t *testing.T) {
	handlerStarted := make(chan struct{})
	block := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-block
	})

	s := New(handler, "127.0.0.1:0", 1*time.Millisecond)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	go s.httpServer.Serve(l)
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		http.Get("http://" + l.Addr().String())
	}()

	<-handlerStarted

	err = s.Shutdown()
	if err == nil {
		t.Fatal("expected error from Shutdown")
	}

	close(block)
	<-done
}
